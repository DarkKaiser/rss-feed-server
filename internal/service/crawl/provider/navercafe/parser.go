package navercafe

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/strutil"
	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"
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

// extractArticle 게시글 목록의 단일 TR 행을 파싱하여 게시글 정보를 반환합니다.
//
// 반환값:
//   - (nil, nil)  : 답글 표시 행(reply row)으로, 호출자는 이 행을 건너뜁니다.
//   - (nil, error): DOM 파싱 오류 발생
//   - (article, nil): 파싱 성공
func (c *crawler) extractArticle(s *goquery.Selection) (*feed.Article, error) {
	// 게시글의 답글을 표시하는 행인지 확인한다.
	// 게시글 제목 오른쪽에 답글이라는 링크가 있으며 이 링크를 클릭하면 아래쪽에 등록된 답글이 나타난다.
	// 이 때 사용할 목적으로 답글이 있는 게시물 아래에 보이지 않는 <TR> 태그가 하나 있다.
	as := s.Find("td")
	if as.Length() == 1 {
		for _, attr := range as.Nodes[0].Attr {
			if attr.Key == "id" && strings.HasPrefix(attr.Val, "reply_") {
				return nil, nil // 답글 행 - 스킵
			}
		}
	}

	article := &feed.Article{}

	// 작성일
	as = s.Find("td.td_date")
	if as.Length() != 1 {
		return nil, errors.New("게시글에서 작성일 정보를 찾을 수 없습니다.")
	}
	createdDate, err := provider.ParseCreatedAt(strings.TrimSpace(as.Text()))
	if err != nil {
		return nil, err
	}
	article.CreatedAt = createdDate

	// 게시판 ID, 이름
	as = s.Find("td.td_article > div.board-name a.link_name")
	if as.Length() != 1 {
		return nil, errors.New("게시글에서 게시판 정보를 찾을 수 없습니다.")
	}
	boardUrl, exists := as.Attr("href")
	if !exists {
		return nil, errors.New("게시글에서 게시판 URL 추출이 실패하였습니다.")
	}
	u, err := url.Parse(boardUrl)
	if err != nil {
		return nil, fmt.Errorf("게시글에서 게시판 URL 파싱이 실패하였습니다. (error:%s)", err)
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("게시글에서 게시판 URL 파싱이 실패하였습니다. (error:%s)", err)
	}
	article.BoardID = strings.TrimSpace(q.Get("search.menuid"))
	if article.BoardID == "" {
		return nil, errors.New("게시글에서 게시판 ID 추출이 실패하였습니다.")
	}
	article.BoardName = strings.TrimSpace(as.Text())

	// 제목
	as = s.Find("td.td_article > div.board-list a.article")
	if as.Length() != 1 {
		return nil, errors.New("게시글에서 제목 정보를 찾을 수 없습니다.")
	}
	article.Title = strings.TrimSpace(as.Text())
	link, exists := as.Attr("href")
	if !exists {
		return nil, errors.New("게시글에서 상세페이지 URL 추출이 실패하였습니다.")
	}

	// 게시글 ID
	u, err = url.Parse(link)
	if err != nil {
		return nil, fmt.Errorf("게시글에서 상세페이지 URL 파싱이 실패하였습니다. (error:%s)", err)
	}
	q, err = url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("게시글에서 상세페이지 URL 파싱이 실패하였습니다. (error:%s)", err)
	}
	articleID, err := strconv.ParseInt(q.Get("articleid"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("게시글에서 게시글 ID 추출이 실패하였습니다. (error:%s)", err)
	}
	article.ArticleID = strconv.FormatInt(articleID, 10)
	article.Link = fmt.Sprintf("%s/ArticleRead.nhn?articleid=%d&clubid=%s", c.Config().URL, articleID, c.siteClubID)

	// 작성자
	as = s.Find("td.td_name > div.pers_nick_area td.p-nick")
	if as.Length() != 1 {
		return nil, errors.New("게시글에서 작성자 정보를 찾을 수 없습니다.")
	}
	article.Author = strings.TrimSpace(as.Text())

	return article, nil
}

func (c *crawler) crawlingArticleContent(ctx context.Context, article *feed.Article) error {
	var lastErr error

	if err := c.crawlingArticleContentUsingAPI(ctx, article); err != nil {
		// 컨텍스트 취소/타임아웃은 즉시 전파합니다.
		// 전파하지 않으면, 이후 단계가 에러 없이 반환될 때 lastErr가 nil이 되어
		// ErrContentUnavailable가 반환되고 재시도 기회가 영구 손실됩니다.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lastErr = err
	}

	if article.Content == "" {
		if err := c.crawlingArticleContentUsingLink(ctx, article); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = err
		} else if article.Content != "" {
			// Link가 성공(nil)했고 실제로 본문을 채운 경우에만 이전 에러를 초기화합니다.
			// 본문이 비어 있는 채로 nil을 반환한 경우(검색 결과 없음 등)에는
			// 이전 단계의 API 일시 에러를 보존하여 재시도 기회를 유지합니다.
			lastErr = nil
		}

		if article.Content == "" {
			if err := c.crawlingArticleContentUsingNaverSearch(ctx, article); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				lastErr = err
			} else if article.Content != "" {
				// Naver 검색이 성공(nil)했고 실제로 본문을 채운 경우에만 이전 에러를 초기화합니다.
				lastErr = nil
			}
		}
	}

	if article.Content != "" {
		return nil
	}

	if lastErr != nil {
		// 마지막으로 발생한 에러가 타임아웃 등 네트워크 에러일 수 있으므로 재시도 대상(error 반환)
		return lastErr
	}

	// 어떠한 시스템 에러도 없었는데도 내용이 없다면
	// 원래 비어있는 글이거나 스크래퍼가 인식하지 못한 영구적 파싱 실패 상태(비공개 등)이므로 재시도 스킵
	return provider.ErrContentUnavailable
}

func (c *crawler) crawlingArticleContentUsingAPI(ctx context.Context, article *feed.Article) error {
	//
	// 네이버 카페 상세페이지를 로드하여 art 쿼리 문자열을 구한다.
	//
	title := c.Messagef("%s 게시글('%s')의 상세페이지", article.BoardName, article.ArticleID)

	head := make(http.Header)
	head.Set("Referer", "https://search.naver.com/")
	doc, err := c.Scraper().FetchHTMLDocument(ctx, fmt.Sprintf("%s/%s", c.Config().URL, article.ArticleID), head)
	if err != nil {
		if apperrors.Is(err, apperrors.ExecutionFailed) {
			return provider.ErrContentUnavailable
		}
		applog.Warnf("%s 접근이 실패하였습니다. (error:%v)", title, err)
		return err
	}

	bodyString, _ := doc.Html()

	pos := strings.Index(bodyString, "&art=")
	if pos == -1 {
		applog.Warnf("%s의 art 쿼리 문자열을 찾을 수 없습니다.", title)
		return provider.ErrContentUnavailable
	}
	artValue := bodyString[pos+5:]
	endIdx := strings.IndexAny(artValue, "&\"'")
	if endIdx != -1 {
		artValue = artValue[:endIdx]
	}

	//
	// 구한 art 쿼리 문자열을 이용하여 네이버 카페 게시글 API를 호출한다.
	//
	title = c.Messagef("%s 게시글('%s')의 API 페이지", article.BoardName, article.ArticleID)

	apiURL := fmt.Sprintf("https://apis.naver.com/cafe-web/cafe-articleapi/v2/cafes/%s/articles/%s?art=%s&useCafeId=true&requestFrom=A", c.siteClubID, article.ArticleID, artValue)

	var apiResult naverCafeArticleAPIResult
	if err := c.Scraper().FetchJSON(ctx, "GET", apiURL, nil, nil, &apiResult); err != nil {
		if apperrors.Is(err, apperrors.ExecutionFailed) {
			return provider.ErrContentUnavailable
		}
		// 특정 게시글은 401(Unauthorized)이 반환되는 경우가 있음!!!
		// 작성자가 네이버 로그인 없이는 외부에서 접근할 수 없도록 설정한 경우입니다.
		applog.Warnf("%s 접근이 실패하였습니다. (error:%v)", title, err)
		return err
	}

	article.Content = apiResult.Result.Article.ContentHtml
	for i, element := range apiResult.Result.Article.ContentElements {
		switch element.Type {
		case "IMAGE":
			imgString := fmt.Sprintf("<img src=\"%s\" alt=\"%s\">", html.EscapeString(element.JSON.Image.URL), html.EscapeString(element.JSON.Image.FileName))
			article.Content = strings.ReplaceAll(article.Content, fmt.Sprintf("[[[CONTENT-ELEMENT-%d]]]", i), imgString)

		case "LINK":
			if element.JSON.Layout == "SIMPLE_IMAGE" || element.JSON.Layout == "WIDE_IMAGE" {
				linkString := fmt.Sprintf("<a href=\"%s\" target=\"_blank\">%s</a>", html.EscapeString(element.JSON.LinkURL), html.EscapeString(html.UnescapeString(element.JSON.TruncatedTitle)))
				article.Content = strings.ReplaceAll(article.Content, fmt.Sprintf("[[[CONTENT-ELEMENT-%d]]]", i), linkString)
			} else {
				m := fmt.Sprintf("%s 응답 데이터에서 알 수 없는 LINK ContentElement Layout('%s')이 입력되었습니다.", title, element.JSON.Layout)
				c.SendErrorNotification(m, nil)
			}

		case "STICKER":
			imgString := fmt.Sprintf("<img src=\"%s\" width=\"%d\" height=\"%d\" nhn_extra_image=\"true\" style=\"cursor:pointer\">", html.EscapeString(element.JSON.URL), element.JSON.Width, element.JSON.Height)
			article.Content = strings.ReplaceAll(article.Content, fmt.Sprintf("[[[CONTENT-ELEMENT-%d]]]", i), imgString)

		default:
			m := fmt.Sprintf("%s 응답 데이터에서 알 수 없는 ContentElement Type('%s')이 입력되었습니다.", title, element.Type)
			c.SendErrorNotification(m, nil)
		}
	}

	// 오늘 이전의 게시글이라서 작성일(시간) 추출을 못한 경우에 한해서 작성일(시간)을 다시 추출한다.
	if article.CreatedAt.Format("15:04:05") == "00:00:00" {
		// WriteDate가 0이면 time.Unix(0, 0) = 1970-01-01 00:00:00 (Unix Epoch)이 반환됩니다.
		// Go의 time.IsZero()는 time.Time{} (0001-01-01)에 대해서만 true를 반환하므로,
		// WriteDate == 0인 경우 IsZero() 체크를 우회하여 Epoch 시각이 CreatedAt에 잘못 설정됩니다.
		// 명시적으로 양수(> 0)인 경우에만 작성일을 재설정합니다.
		if apiResult.Result.Article.WriteDate > 0 {
			article.CreatedAt = time.Unix(apiResult.Result.Article.WriteDate/1000, 0)
		}
	}

	return nil
}

func (c *crawler) crawlingArticleContentUsingLink(ctx context.Context, article *feed.Article) error {
	doc, err := c.Scraper().FetchHTMLDocument(ctx, article.Link, nil)
	if err != nil {
		if apperrors.Is(err, apperrors.ExecutionFailed) {
			return provider.ErrContentUnavailable
		}
		applog.Warnf(c.Messagef("%s 게시글('%s')의 상세페이지 (error:%v)", article.BoardName, article.ArticleID, err))
		return err
	}

	ncSelection := doc.Find("#tbody")
	if ncSelection.Length() == 0 {
		// 로그인을 하지 않아 접근 권한이 없는 페이지인 경우 오류가 발생하므로 로그 처리를 하지 않는다.
		return provider.ErrContentUnavailable
	}

	article.Content = strutil.NormalizeMultiline(ncSelection.Text())

	// 내용에 이미지 태그가 포함되어 있다면 모두 추출한다.
	doc.Find("#tbody img").Each(func(i int, s *goquery.Selection) {
		var src, _ = s.Attr("src")
		if src != "" {
			var alt, _ = s.Attr("alt")
			var style, _ = s.Attr("style")
			article.Content += fmt.Sprintf(`%s<img src="%s" alt="%s" style="%s">`, "\r\n", html.EscapeString(src), html.EscapeString(alt), html.EscapeString(style))
		}
	})

	return nil
}

func (c *crawler) crawlingArticleContentUsingNaverSearch(ctx context.Context, article *feed.Article) error {
	searchUrl := fmt.Sprintf("https://search.naver.com/search.naver?where=article&query=%s&ie=utf8&st=date&date_option=0&date_from=&date_to=&board=&srchby=title&dup_remove=0&cafe_url=%s&without_cafe_url=&sm=tab_opt&nso=so:dd,p:all,a:t&t=0&mson=0&prdtype=0", url.QueryEscape(article.Title), c.Config().ID)

	doc, err := c.Scraper().FetchHTMLDocument(ctx, searchUrl, nil)
	if err != nil {
		if apperrors.Is(err, apperrors.ExecutionFailed) {
			return provider.ErrContentUnavailable
		}
		applog.Warnf(c.Messagef("%s 게시글('%s')의 네이버 검색페이지 (error:%v)", article.BoardName, article.ArticleID, err))
		return err
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
			article.Content += fmt.Sprintf(`%s<img src="%s" alt="%s" style="%s">`, "\r\n", html.EscapeString(src), html.EscapeString(alt), html.EscapeString(style))
		}
	})

	return nil
}
