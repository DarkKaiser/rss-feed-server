package crawling

import (
	"errors"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/services/ws/model"
	"github.com/robfig/cron"
	"golang.org/x/net/html"
	"golang.org/x/text/encoding"
	"io/ioutil"
	"net/http"
	"strings"
)

var (
	errNotSupportedCrawler = errors.New("지원하지 않는 Crawler입니다")
)

//
// supportedCrawlers
//
type newCrawlerFunc func(string, *g.ProviderConfig, model.ModelGetter) cron.Job

// 구현된 Crawler 목록
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
}

//noinspection GoUnhandledErrorResult
func httpWebPageDocument(url, title string, decoder *encoding.Decoder) (*goquery.Document, string, error) {
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
