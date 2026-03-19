package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/robfig/cron/v3"
	"golang.org/x/net/html"
	"golang.org/x/text/encoding"
)

var ErrNotSupportedCrawler = errors.New("지원하지 않는 Crawler입니다")

// NewCrawlerFunc 크롤러 생성 함수 타입
type NewCrawlerFunc func(string, *config.ProviderDetailConfig, feed.Repository, *notify.Client) cron.Job

// SupportedCrawlers 지원되는 Crawler 목록
var SupportedCrawlers = make(map[config.ProviderSite]*SupportedCrawlerConfig)

type SupportedCrawlerConfig struct {
	NewCrawlerFn NewCrawlerFunc
}

func FindConfigFromSupportedCrawler(site config.ProviderSite) (*SupportedCrawlerConfig, error) {
	crawlerConfig, exists := SupportedCrawlers[site]
	if exists == true {
		return crawlerConfig, nil
	}

	return nil, ErrNotSupportedCrawler
}

// crawler
const EmptyBoardIDKey = "#empty#"

type crawlingArticlesFunc func() ([]*feed.Article, map[string]string, string, error)

type crawler struct {
	config *config.ProviderDetailConfig

	rssFeedProviderID string
	feedRepo          feed.Repository
	notifyClient      *notify.Client

	site            string
	siteID          string
	siteName        string
	siteDescription string
	siteUrl         string

	// 크롤링 할 최대 페이지 수
	crawlingMaxPageCount int

	crawlingArticlesFn crawlingArticlesFunc
}

func (c *crawler) Run() {
	applog.Debugf("%s('%s')의 크롤링 작업을 시작합니다.", c.site, c.siteID)

	articles, latestCrawledArticleIDsByBoard, errOccurred, err := c.crawlingArticlesFn()
	if err != nil {
		applog.Errorf("%s (error:%s)", errOccurred, err)

		if c.notifyClient != nil {
			c.notifyClient.NotifyError(context.Background(), fmt.Sprintf("%s\r\n\r\n%s", errOccurred, err))
		}

		return
	}

	if articles != nil {
		if len(articles) > 0 {
			applog.Debugf("%s('%s')의 크롤링 작업 결과로 %d건의 신규 게시글이 추출되었습니다. 신규 게시글을 DB에 추가합니다.", c.site, c.siteID, len(articles))

			insertedCnt, err := c.feedRepo.InsertArticles(c.rssFeedProviderID, articles)
			if err != nil {
				m := fmt.Sprintf("%s('%s')의 신규 게시글을 DB에 추가하는 중에 오류가 발생하여 크롤링 작업이 실패하였습니다.", c.site, c.siteID)

				applog.Errorf("%s (error:%s)", m, err)

				if c.notifyClient != nil {
					c.notifyClient.NotifyError(context.Background(), fmt.Sprintf("%s\r\n\r\n%s", m, err))
				}

				return
			}

			for boardID, articleID := range latestCrawledArticleIDsByBoard {
				if boardID == EmptyBoardIDKey {
					boardID = ""
				}

				if err = c.feedRepo.UpdateLatestCrawledArticleID(c.rssFeedProviderID, boardID, articleID); err != nil {
					m := fmt.Sprintf("%s('%s')의 크롤링 된 최근 게시글 ID의 DB 갱신이 실패하였습니다.", c.site, c.siteID)

					applog.Errorf("%s (error:%s)", m, err)

					if c.notifyClient != nil {
						c.notifyClient.NotifyError(context.Background(), fmt.Sprintf("%s\r\n\r\n%s", m, err))
					}
				}
			}

			if len(articles) != insertedCnt {
				applog.Warnf("%s('%s')의 크롤링 작업을 종료합니다. 전체 %d건 중에서 %d건의 신규 게시글이 DB에 추가되었습니다.", c.site, c.siteID, len(articles), insertedCnt)
			} else {
				applog.Debugf("%s('%s')의 크롤링 작업을 종료합니다. %d건의 신규 게시글이 DB에 추가되었습니다.", c.site, c.siteID, len(articles))
			}
		} else {
			for boardID, articleID := range latestCrawledArticleIDsByBoard {
				if boardID == EmptyBoardIDKey {
					boardID = ""
				}

				if err = c.feedRepo.UpdateLatestCrawledArticleID(c.rssFeedProviderID, boardID, articleID); err != nil {
					m := fmt.Sprintf("%s('%s')의 크롤링 된 최근 게시글 ID의 DB 갱신이 실패하였습니다.", c.site, c.siteID)

					applog.Errorf("%s (error:%s)", m, err)

					if c.notifyClient != nil {
						c.notifyClient.NotifyError(context.Background(), fmt.Sprintf("%s\r\n\r\n%s", m, err))
					}
				}
			}

			applog.Debugf("%s('%s')의 크롤링 작업을 종료합니다. 신규 게시글이 존재하지 않습니다.", c.site, c.siteID)
		}
	} else {
		applog.Warnf("%s('%s')의 크롤링 작업을 종료합니다. 서버의 일시적인 오류로 인하여 신규 게시글 추출이 실패하였습니다.", c.site, c.siteID)
	}
}

// noinspection GoUnhandledErrorResult
func (c *crawler) getWebPageDocument(url, title string, decoder *encoding.Decoder) (*goquery.Document, string, error) {
	res, err := http.Get(url)
	if err != nil {
		// 2022년 10월 중순경부터 네이버카페의 글을 일정 시간이 지난후에 http.Get()을 호출하게 되면 'connection reset by peer' 에러가 발생함!
		// 그래서 http.Get()에서 에러가 발생하면 최대 2번 호출하도록 변경함!!
		for i := 1; i <= 2; i++ {
			time.Sleep(100 * time.Millisecond)

			res, err = http.Get(url)
			if err == nil {
				goto SUCCEED
			}
		}

		return nil, fmt.Sprintf("%s 접근이 실패하였습니다.", title), err
	}
SUCCEED:
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Sprintf("%s 접근이 실패하였습니다.", title), fmt.Errorf("HTTP Response StatusCode %d", res.StatusCode)
	}
	defer res.Body.Close()

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		if strings.Contains(err.Error(), "unexpected EOF") && len(bodyBytes) != 0 {
			goto pars
		}
		return nil, fmt.Sprintf("%s의 내용을 읽을 수 없습니다.", title), err
	}

pars:
	if decoder != nil {
		bodyString, err := decoder.String(string(bodyBytes))
		if err != nil {
			return nil, fmt.Sprintf("%s의 문자열 디코딩이 실패하였습니다.", title), err
		}

		root, err := html.Parse(strings.NewReader(bodyString))
		if err != nil {
			return nil, fmt.Sprintf("%s의 HTML 파싱이 실패하였습니다.", title), err
		}

		return goquery.NewDocumentFromNode(root), "", nil
	} else {
		root, err := html.Parse(strings.NewReader(string(bodyBytes)))
		if err != nil {
			return nil, fmt.Sprintf("%s의 HTML 파싱이 실패하였습니다.", title), err
		}

		return goquery.NewDocumentFromNode(root), "", nil
	}
}
