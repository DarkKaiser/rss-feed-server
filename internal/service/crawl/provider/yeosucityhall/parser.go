package yeosucityhall

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/strutil"
	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"
)

// noinspection GoErrorStringFormat
func (c *crawler) extractArticle(boardType string, s *goquery.Selection) (*feed.Article, error) {
	switch boardType {
	case yeosuCityHallCrawlerBoardTypePhotoNews:
		return c.extractPhotoNewsArticle(s)
	case yeosuCityHallCrawlerBoardTypeList1, yeosuCityHallCrawlerBoardTypeList2:
		return c.extractListArticle(boardType, s)
	case yeosuCityHallCrawlerBoardTypeCardNews:
		return c.extractCardNewsArticle(s)
	default:
		return nil, fmt.Errorf("구현되지 않은 게시판 Type('%s') 입니다.", boardType)
	}
}

func (c *crawler) extractPhotoNewsArticle(s *goquery.Selection) (*feed.Article, error) {
	var exists bool
	var article = &feed.Article{}

	// 링크
	as := s.Find("a.item_cont")
	if as.Length() != 1 {
		return nil, errors.New("게시글에서 링크 정보를 찾을 수 없습니다.")
	}
	article.Link, exists = as.Attr("href")
	if exists == false {
		return nil, errors.New("게시글에서 상세페이지 URL 추출이 실패하였습니다.")
	}
	if !strings.HasPrefix(article.Link, "http://") && !strings.HasPrefix(article.Link, "https://") {
		article.Link = fmt.Sprintf("%s%s", c.Config().URL, article.Link)
	}

	// 제목
	as = s.Find("a.item_cont > div.cont_box > div.title_box")
	if as.Length() != 1 {
		return nil, errors.New("게시글에서 제목 정보를 찾을 수 없습니다.")
	}
	as.Find("img[alt*='새로운글'], img[alt*='새글'], span[class*='new']").Remove()
	article.Title = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(as.Text()), "새로운글"))

	// 게시글 ID
	var err error
	if article.ArticleID, err = c.extractArticleIDFromURL(article.Link); err != nil {
		return nil, err
	}

	// 등록자
	as = s.Find("a.item_cont > div.cont_box > dl > dd")
	if as.Length() != 3 {
		return nil, errors.New("게시글에서 등록자 및 등록일 정보를 찾을 수 없습니다.")
	}
	authorTokens := strings.Split(strings.TrimSpace(as.Eq(0).Text()), " ")
	article.Author = strings.TrimSpace(authorTokens[len(authorTokens)-1])

	// 등록일
	var createdDateString = strings.TrimSpace(as.Eq(1).Text())
	if article.CreatedAt, err = provider.ParseCreatedAt(createdDateString); err != nil {
		return nil, err
	}

	return article, nil
}

func (c *crawler) extractListArticle(boardType string, s *goquery.Selection) (*feed.Article, error) {
	var exists bool
	var article = &feed.Article{}

	// 제목, 링크
	as := s.Find("a.basic_cont")
	if as.Length() != 1 {
		return nil, errors.New("게시글에서 제목 정보를 찾을 수 없습니다.")
	}
	as.Find("img[alt*='새로운글'], img[alt*='새글'], span[class*='new']").Remove()
	article.Title = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(as.Text()), "새로운글"))
	article.Link, exists = as.Attr("href")
	if exists == false {
		return nil, errors.New("게시글에서 상세페이지 URL 추출이 실패하였습니다.")
	}
	if !strings.HasPrefix(article.Link, "http://") && !strings.HasPrefix(article.Link, "https://") {
		article.Link = fmt.Sprintf("%s%s", c.Config().URL, article.Link)
	}

	if boardType == yeosuCityHallCrawlerBoardTypeList2 {
		// 분류
		as = s.Find("td.list_cate")
		if as.Length() != 1 {
			return nil, errors.New("게시글에서 분류 정보를 찾을 수 없습니다.")
		}
		classification := strings.TrimSpace(as.Text())
		if classification != "" {
			article.Title = fmt.Sprintf("[ %s ] %s", classification, article.Title)
		}
	}

	// 게시글 ID
	var err error
	if article.ArticleID, err = c.extractArticleIDFromURL(article.Link); err != nil {
		return nil, err
	}

	// 등록자
	as = s.Find("td")
	if as.Length() < 5 {
		return nil, errors.New("게시글에서 등록자/담당부서 및 등록일 정보를 찾을 수 없습니다(항목 부족).")
	}
	article.Author = strings.TrimSpace(as.Eq(as.Length() - 3).Text())

	// 등록일
	var createdDateString = strings.TrimSpace(as.Eq(as.Length() - 2).Text())
	if article.CreatedAt, err = provider.ParseCreatedAt(createdDateString); err != nil {
		return nil, err
	}

	return article, nil
}

func (c *crawler) extractCardNewsArticle(s *goquery.Selection) (*feed.Article, error) {
	var exists bool
	var article = &feed.Article{}

	// 링크
	as := s.Find("div.cont_box ul > li > div.board_share_box > ul > li.share_btn > a")
	if as.Length() == 0 {
		return nil, errors.New("게시글에서 링크 정보를 찾을 수 없습니다.")
	}
	article.Link, exists = as.Eq(0).Attr("data-url")
	if exists == false {
		return nil, errors.New("게시글에서 상세페이지 URL 추출이 실패하였습니다.")
	}
	if !strings.HasPrefix(article.Link, "http://") && !strings.HasPrefix(article.Link, "https://") {
		article.Link = fmt.Sprintf("%s%s", c.Config().URL, article.Link)
	}

	// 제목
	as = s.Find("div.cont_box > h3")
	if as.Length() != 1 {
		return nil, errors.New("게시글에서 제목 정보를 찾을 수 없습니다.")
	}
	article.Title = strings.TrimSpace(as.Text())

	// 게시글 ID
	var err error
	if article.ArticleID, err = c.extractArticleIDFromURL(article.Link); err != nil {
		return nil, err
	}

	// 등록자
	as = s.Find("div.cont_box > dl > dd")
	if as.Length() != 2 {
		return nil, errors.New("게시글에서 등록자 및 등록일 정보를 찾을 수 없습니다.")
	}
	article.Author = strings.TrimSpace(as.Eq(1).Text())

	// 등록일
	var createdDateString = strings.TrimSpace(as.Eq(0).Text())
	if article.CreatedAt, err = provider.ParseCreatedAt(createdDateString); err != nil {
		return nil, err
	}

	return article, nil
}

func (c *crawler) crawlingArticleContent(ctx context.Context, article *feed.Article) error {
	doc, err := c.Scraper().FetchHTMLDocument(ctx, article.Link, nil)
	if err != nil {
		if apperrors.Is(err, apperrors.ExecutionFailed) {
			return provider.ErrContentUnavailable
		}
		errOccurred := c.Messagef("%s 게시판의 게시글('%s') 상세페이지 접근이 실패하였습니다.", article.BoardName, article.ArticleID)
		applog.Warnf("%s (error:%v)", errOccurred, err)
		return err
	}

	ysSelection := doc.Find("div.contbox > div.viewbox")
	if ysSelection.Length() == 0 {
		applog.Warnf("게시글('%s')에서 내용 정보를 찾을 수 없습니다.", article.ArticleID)
		return provider.ErrContentUnavailable
	}

	article.Content = strutil.NormalizeMultiline(ysSelection.Text())

	// 내용에 이미지 태그가 포함되어 있다면 모두 추출한다.
	ysSelection.Find("img").Each(func(i int, s *goquery.Selection) {
		var src, _ = s.Attr("src")
		if src != "" {
			var alt, _ = s.Attr("alt")
			var style, _ = s.Attr("style")

			if strings.HasPrefix(src, "data:image/") == true {
				// ※ data:image의 데이터 크기가 너무 큰 항목인 경우 스마트폰 앱이 죽는 현상이 생기므로 기능 비활성화함!!!
				// article.Content += fmt.Sprintf(`%s<img src="%s" alt="%s" style="%s">`, "\r\n", src, alt, style)
			} else {
				baseURL, errBase := url.Parse(article.Link)
				relURL, errRel := url.Parse(src)

				if errBase == nil && errRel == nil {
					resolvedURL := baseURL.ResolveReference(relURL).String()
					article.Content += fmt.Sprintf(`%s<img src="%s" alt="%s" style="%s">`, "\r\n", html.EscapeString(resolvedURL), html.EscapeString(alt), html.EscapeString(style))
				}
			}
		}
	})

	return nil
}

func (c *crawler) extractArticleIDFromURL(link string) (string, error) {
	u, err := url.Parse(link)
	if err != nil {
		return "", fmt.Errorf("게시글에서 상세페이지 URL 파싱이 실패하였습니다. (error:%s)", err)
	}
	m, _ := url.ParseQuery(u.RawQuery)
	if m["idx"] != nil && m["idx"][0] != "" {
		return m["idx"][0], nil
	}
	return "", errors.New("게시글에서 게시글 ID 추출이 실패하였습니다.")
}
