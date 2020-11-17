package crawling

import (
	"errors"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
	"github.com/darkkaiser/rss-feed-server/services/ws/model"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/html"
	"golang.org/x/text/encoding"
	"io/ioutil"
	"net/http"
	"strings"
)

var errNotSupportedCrawler = errors.New("지원하지 않는 Crawler입니다")

//
// supportedCrawlers
//
type newCrawlerFunc func(string, *g.ProviderConfig, model.ModelGetter) cron.Job

// 지원되는 Crawler 목록
var supportedCrawlers = make(map[g.RssFeedSupportedSite]*supportedCrawlerConfig)

type supportedCrawlerConfig struct {
	newCrawlerFn newCrawlerFunc
}

func findConfigFromSupportedCrawler(site g.RssFeedSupportedSite) (*supportedCrawlerConfig, error) {
	crawlerConfig, exists := supportedCrawlers[site]
	if exists == true {
		return crawlerConfig, nil
	}

	return nil, errNotSupportedCrawler
}

//
// crawler
//
const emptyBoardIDKey = "#empty#"

type crawlingArticlesFunc func() ([]*model.RssFeedProviderArticle, map[string]string, string, error)

type crawler struct {
	config *g.ProviderConfig

	rssFeedProviderID        string
	rssFeedProvidersAccessor model.RssFeedProvidersAccessor

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
	log.Debugf("%s('%s')의 크롤링 작업을 시작합니다.", c.site, c.siteID)

	articles, latestCrawledArticleIDsByBoard, errOccurred, err := c.crawlingArticlesFn()
	if err != nil {
		log.Errorf("%s (error:%s)", errOccurred, err)

		notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", errOccurred, err), true)

		return
	}

	if len(articles) > 0 {
		log.Debugf("%s('%s')의 크롤링 작업 결과로 %d건의 신규 게시글이 추출되었습니다. 신규 게시글을 DB에 추가합니다.", c.site, c.siteID, len(articles))

		insertedCnt, err := c.rssFeedProvidersAccessor.InsertArticles(c.rssFeedProviderID, articles)
		if err != nil {
			m := fmt.Sprintf("%s('%s')의 신규 게시글을 DB에 추가하는 중에 오류가 발생하여 크롤링 작업이 실패하였습니다.", c.site, c.siteID)

			log.Errorf("%s (error:%s)", m, err)

			notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)

			return
		}

		for boardID, articleID := range latestCrawledArticleIDsByBoard {
			if boardID == emptyBoardIDKey {
				boardID = ""
			}

			if err = c.rssFeedProvidersAccessor.UpdateLatestCrawledArticleID(c.rssFeedProviderID, boardID, articleID); err != nil {
				m := fmt.Sprintf("%s('%s')의 크롤링 된 최근 게시글 ID의 DB 갱신이 실패하였습니다.", c.site, c.siteID)

				log.Errorf("%s (error:%s)", m, err)

				notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)
			}
		}

		if len(articles) != insertedCnt {
			log.Warnf("%s('%s')의 크롤링 작업을 종료합니다. 전체 %d건 중에서 %d건의 신규 게시글이 DB에 추가되었습니다.", c.site, c.siteID, len(articles), insertedCnt)
		} else {
			log.Debugf("%s('%s')의 크롤링 작업을 종료합니다. %d건의 신규 게시글이 DB에 추가되었습니다.", c.site, c.siteID, len(articles))
		}
	} else {
		for boardID, articleID := range latestCrawledArticleIDsByBoard {
			if boardID == emptyBoardIDKey {
				boardID = ""
			}

			if err = c.rssFeedProvidersAccessor.UpdateLatestCrawledArticleID(c.rssFeedProviderID, boardID, articleID); err != nil {
				m := fmt.Sprintf("%s('%s')의 크롤링 된 최근 게시글 ID의 DB 갱신이 실패하였습니다.", c.site, c.siteID)

				log.Errorf("%s (error:%s)", m, err)

				notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)
			}
		}

		log.Debugf("%s('%s')의 크롤링 작업을 종료합니다. 신규 게시글이 존재하지 않습니다.", c.site, c.siteID)
	}
}

//noinspection GoUnhandledErrorResult
func (c *crawler) getWebPageDocument(url, title string, decoder *encoding.Decoder) (*goquery.Document, string, error) {
	res, err := http.Get(url)
	if err != nil {
		return nil, fmt.Sprintf("%s 접근이 실패하였습니다.", title), err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Sprintf("%s 접근이 실패하였습니다.", title), fmt.Errorf("HTTP Response StatusCode %d", res.StatusCode)
	}

	bodyBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Sprintf("%s의 내용을 읽을 수 없습니다.", title), err
	}
	defer res.Body.Close()

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
