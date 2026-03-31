package navercafe

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/strutil"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
)

type naverCafeArticleAPIResult struct {
	Result struct {
		Article struct {
			WriteDate       int64  `json:"writeDate"`
			ContentHtml     string `json:"contentHtml"`
			ContentElements []struct {
				Type string `json:"type"`
				JSON struct {
					Image struct {
						URL      string `json:"url"`
						Service  string `json:"service"`
						Type     string `json:"type"`
						Width    int    `json:"width"`
						Height   int    `json:"height"`
						FileName string `json:"fileName"`
						FileSize int    `json:"fileSize"`
					} `json:"image"`
					Layout         string `json:"layout"`
					ImageURL       string `json:"imageUrl"`
					VideoURL       string `json:"videoUrl"`
					AudioURL       string `json:"audioUrl"`
					Desc           string `json:"desc"`
					TruncatedTitle string `json:"truncatedTitle"`
					TruncatedDesc  string `json:"truncatedDesc"`
					Domain         string `json:"domain"`
					LinkURL        string `json:"linkUrl"`
					StickerID      string `json:"stickerId"`
					MarketURL      string `json:"marketUrl"`
					URL            string `json:"url"`
					Width          int    `json:"width"`
					Height         int    `json:"height"`
					From           string `json:"from"`
				} `json:"json"`
			} `json:"contentElements"`
		} `json:"article"`
	} `json:"result"`
}

func (c *crawler) crawlingArticleContent(ctx context.Context, article *feed.Article) {
	c.crawlingArticleContentUsingAPI(ctx, article)
	if article.Content == "" {
		c.crawlingArticleContentUsingLink(ctx, article)
		if article.Content == "" {
			c.crawlingArticleContentUsingNaverSearch(ctx, article)
		}
	}
}

func (c *crawler) crawlingArticleContentUsingAPI(ctx context.Context, article *feed.Article) {
	//
	// 네이버 카페 상세페이지를 로드하여 art 쿼리 문자열을 구한다.
	//
	title := c.FormatMessage("%s 게시글('%s')의 상세페이지", article.BoardName, article.ArticleID)

	head := make(http.Header)
	head.Set("Referer", "https://search.naver.com/")
	doc, err := c.Scraper().FetchHTMLDocument(ctx, fmt.Sprintf("%s/%s", c.Config().URL, article.ArticleID), head)
	if err != nil {
		applog.Warnf("%s 접근이 실패하였습니다. (error:%v)", title, err)
		return
	}

	bodyString, _ := doc.Html()

	pos := strings.Index(bodyString, "&art=")
	if pos == -1 {
		applog.Warnf("%s의 art 쿼리 문자열을 찾을 수 없습니다.", title)
		return
	}
	artValue := bodyString[pos+5:]
	artValue = artValue[:strings.Index(artValue, "&")]

	//
	// 구한 art 쿼리 문자열을 이용하여 네이버 카페 게시글 API를 호출한다.
	//
	title = c.FormatMessage("%s 게시글('%s')의 API 페이지", article.BoardName, article.ArticleID)

	res2, err := http.Get(fmt.Sprintf("https://apis.naver.com/cafe-web/cafe-articleapi/v2/cafes/%s/articles/%s?art=%s&useCafeId=true&requestFrom=A", c.siteClubID, article.ArticleID, artValue))
	if err != nil {
		applog.Warnf("%s 접근이 실패하였습니다. (error:%s)", title, err)
		return
	}
	if res2.StatusCode != http.StatusOK {
		// 특정 게시글은 StatusBadRequest(401)가 반환되는 경우가 있음!!!
		// 이 경우는 해당 게시글이 네이버 로그인을 하지 않으면 외부에서(네이버 검색 서비스) 접근이 되지 않도록
		// 작성자가 설정하였기 때문에 그런 것 같음!!!
		applog.Warnf("%s 접근이 실패하였습니다. (HTTP 상태코드:%d)", title, res2.StatusCode)
		return
	}
	defer res2.Body.Close()

	bodyBytes, err := io.ReadAll(res2.Body)
	if err != nil {
		applog.Warnf("%s의 내용을 읽을 수 없습니다. (error:%s)", title, err)
		return
	}

	var apiResult naverCafeArticleAPIResult
	err = json.Unmarshal(bodyBytes, &apiResult)
	if err != nil {
		m := fmt.Sprintf("%s 응답 데이터의 JSON 변환이 실패하였습니다.", title)
		c.SendErrorNotification(m, err)
		return
	}

	article.Content = apiResult.Result.Article.ContentHtml
	for i, element := range apiResult.Result.Article.ContentElements {
		switch element.Type {
		case "IMAGE":
			article.Content = strings.ReplaceAll(article.Content, fmt.Sprintf("[[[CONTENT-ELEMENT-%d]]]", i), element.JSON.Image.URL)

		case "LINK":
			if element.JSON.Layout == "SIMPLE_IMAGE" || element.JSON.Layout == "WIDE_IMAGE" {
				linkString := fmt.Sprintf("<a href=\"%s\" target=\"_blank\">%s</a>", element.JSON.LinkURL, html.UnescapeString(element.JSON.TruncatedTitle))
				article.Content = strings.ReplaceAll(article.Content, fmt.Sprintf("[[[CONTENT-ELEMENT-%d]]]", i), linkString)
			} else {
				m := fmt.Sprintf("%s 응답 데이터에서 알 수 없는 LINK ContentElement Layout('%s')이 입력되었습니다.", title, element.JSON.Layout)
				c.SendErrorNotification(m, nil)
			}

		case "STICKER":
			imgString := fmt.Sprintf("<img src=\"%s\" width=\"%d\" height=\"%d\" nhn_extra_image=\"true\" style=\"cursor:pointer\">", element.JSON.URL, element.JSON.Width, element.JSON.Height)
			article.Content = strings.ReplaceAll(article.Content, fmt.Sprintf("[[[CONTENT-ELEMENT-%d]]]", i), imgString)

		default:
			m := fmt.Sprintf("%s 응답 데이터에서 알 수 없는 ContentElement Type('%s')이 입력되었습니다.", title, element.Type)
			c.SendErrorNotification(m, nil)
		}
	}

	// 오늘 이전의 게시글이라서 작성일(시간) 추출을 못한 경우에 한해서 작성일(시간)을 다시 추출한다.
	if article.CreatedAt.Format("15:04:05") == "23:59:59" {
		writeDate := time.Unix(apiResult.Result.Article.WriteDate/1000, 0)
		if writeDate.IsZero() == false {
			article.CreatedAt = writeDate
		}
	}
}

func (c *crawler) crawlingArticleContentUsingLink(ctx context.Context, article *feed.Article) {
	doc, err := c.Scraper().FetchHTMLDocument(ctx, article.Link, nil)
	if err != nil {
		applog.Warnf(c.FormatMessage("%s 게시글('%s')의 상세페이지 (error:%v)", article.BoardName, article.ArticleID, err))
		return
	}

	ncSelection := doc.Find("#tbody")
	if ncSelection.Length() == 0 {
		// 로그인을 하지 않아 접근 권한이 없는 페이지인 경우 오류가 발생하므로 로그 처리를 하지 않는다.
		return
	}

	article.Content = strutil.NormalizeMultiline(ncSelection.Text())

	// 내용에 이미지 태그가 포함되어 있다면 모두 추출한다.
	doc.Find("#tbody img").Each(func(i int, s *goquery.Selection) {
		var src, _ = s.Attr("src")
		if src != "" {
			var alt, _ = s.Attr("alt")
			var style, _ = s.Attr("style")
			article.Content += fmt.Sprintf(`%s<img src="%s" alt="%s" style="%s">`, "\r\n", src, alt, style)
		}
	})
}

func (c *crawler) crawlingArticleContentUsingNaverSearch(ctx context.Context, article *feed.Article) {
	searchUrl := fmt.Sprintf("https://search.naver.com/search.naver?where=article&query=%s&ie=utf8&st=date&date_option=0&date_from=&date_to=&board=&srchby=title&dup_remove=0&cafe_url=%s&without_cafe_url=&sm=tab_opt&nso=so:dd,p:all,a:t&t=0&mson=0&prdtype=0", url.QueryEscape(article.Title), c.Config().ID)

	doc, err := c.Scraper().FetchHTMLDocument(ctx, searchUrl, nil)
	if err != nil {
		applog.Warnf(c.FormatMessage("%s 게시글('%s')의 네이버 검색페이지 (error:%v)", article.BoardName, article.ArticleID, err))
		return
	}

	ncSelection := doc.Find(fmt.Sprintf("a.total_dsc[href='%s/%s']", c.Config().URL, article.ArticleID))
	if ncSelection.Length() == 1 {
		article.Content = strutil.NormalizeMultiline(ncSelection.Text())
	}

	// 내용에 이미지 태그가 포함되어 있다면 모두 추출한다.
	doc.Find(fmt.Sprintf("a.thumb_single[href='%s/%s'] img", c.Config().URL, article.ArticleID)).Each(func(i int, s *goquery.Selection) {
		var src, _ = s.Attr("src")
		if src != "" {
			var alt, _ = s.Attr("alt")
			var style, _ = s.Attr("style")
			article.Content += fmt.Sprintf(`%s<img src="%s" alt="%s" style="%s">`, "\r\n", src, alt, style)
		}
	})
}
