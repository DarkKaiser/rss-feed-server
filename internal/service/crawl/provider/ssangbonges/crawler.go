package ssangbonges

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"

	"github.com/PuerkitoBio/goquery"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/robfig/cron/v3"
)

const (
	// 리스트 1(번호, 제목, 작성자, 등록일, 조회)
	ssangbongSchoolCrawlerBoardTypeList1 string = "L_1"

	// 포토 1
	ssangbongSchoolCrawlerBoardTypePhoto1 string = "P_1"
)

var ssangbongSchoolCrawlerBoardTypes map[string]*ssangbongSchoolCrawlerBoardTypeConfig

type ssangbongSchoolCrawlerBoardTypeConfig struct {
	urlPath1        string
	urlPath2        string
	articleSelector string
}

const ssangbongSchoolUrlPathReplaceStringWithBoardID = "#{board_id}"

func init() {
	provider.MustRegister(config.ProviderSiteSsangbongElementarySchool, &provider.CrawlerConfig{
		NewCrawler: func(rssFeedProviderID string, providerConfig *config.ProviderDetailConfig, feedRepo feed.Repository, notifyClient *notify.Client) cron.Job {
			site := "쌍봉초등학교 홈페이지"

			crawlerInstance := &crawler{
				Base: provider.Base{
					Config: providerConfig,

					RssFeedProviderID: rssFeedProviderID,
					FeedRepo:          feedRepo,
					NotifyClient:      notifyClient,

					Site:            site,
					SiteID:          providerConfig.ID,
					SiteName:        providerConfig.Name,
					SiteDescription: providerConfig.Description,
					SiteUrl:         providerConfig.URL,

					CrawlingMaxPageCount: 3,
				},
			}

			crawlerInstance.Base.CrawlArticles = crawlerInstance.crawlArticles

			applog.Debug(fmt.Sprintf("%s('%s') Crawler가 생성되었습니다.", crawlerInstance.Site, crawlerInstance.SiteID))

			return crawlerInstance
		},
	})

	// 게시판 유형별 설정정보를 초기화한다.
	ssangbongSchoolCrawlerBoardTypes = map[string]*ssangbongSchoolCrawlerBoardTypeConfig{
		ssangbongSchoolCrawlerBoardTypePhoto1: {
			urlPath1:        fmt.Sprintf("/ys-ssangbong_es/na/ntt/selectNttList.do?mi=%s&bbsId=%s", ssangbongSchoolUrlPathReplaceStringWithBoardID, ssangbongSchoolUrlPathReplaceStringWithBoardID),
			urlPath2:        fmt.Sprintf("/ys-ssangbong_es/na/ntt/selectNttInfo.do?mi=%s&bbsId=%s", ssangbongSchoolUrlPathReplaceStringWithBoardID, ssangbongSchoolUrlPathReplaceStringWithBoardID),
			articleSelector: "div.subContent > div.photo_list > ul > li",
		},
		ssangbongSchoolCrawlerBoardTypeList1: {
			urlPath1:        fmt.Sprintf("/ys-ssangbong_es/na/ntt/selectNttList.do?mi=%s&bbsId=%s", ssangbongSchoolUrlPathReplaceStringWithBoardID, ssangbongSchoolUrlPathReplaceStringWithBoardID),
			urlPath2:        fmt.Sprintf("/ys-ssangbong_es/na/ntt/selectNttInfo.do?mi=%s&bbsId=%s", ssangbongSchoolUrlPathReplaceStringWithBoardID, ssangbongSchoolUrlPathReplaceStringWithBoardID),
			articleSelector: "div.subContent > div.bbs_ListA > table > tbody > tr",
		},
	}
}

type crawler struct {
	provider.Base
}

// noinspection GoErrorStringFormat,GoUnhandledErrorResult
func (c *crawler) crawlArticles(ctx context.Context) ([]*feed.Article, map[string]string, string, error) {
	var articles = make([]*feed.Article, 0)
	var newLatestCrawledArticleIDsByBoard = make(map[string]string)

	for _, b := range c.Config.Boards {
		boardTypeConfig, exists := ssangbongSchoolCrawlerBoardTypes[b.Type]
		if exists == false {
			return nil, nil, fmt.Sprintf("%s('%s')의 게시판 Type별 정보를 구하는 중에 오류가 발생하였습니다.", c.Site, c.SiteID), fmt.Errorf("구현되지 않은 게시판 Type('%s') 입니다.", b.Type)
		}

		latestCrawledArticleID, latestCrawledCreatedDate, err := c.FeedRepo.GetCrawlingCursor(ctx, c.RssFeedProviderID, b.ID)
		if err != nil {
			return nil, nil, fmt.Sprintf("%s('%s') %s 게시판에 마지막으로 추가된 게시글 정보를 찾는 중에 오류가 발생하였습니다.", c.Site, c.SiteID, b.Name), err
		}

		var newLatestCrawledArticleID = ""

		//
		// 게시글 크롤링
		//
		for pageNo := 1; pageNo <= c.CrawlingMaxPageCount; pageNo++ {
			ssangbongSchoolPageUrl := strings.ReplaceAll(fmt.Sprintf("%s%s&currPage=%d", c.SiteUrl, boardTypeConfig.urlPath1, pageNo), ssangbongSchoolUrlPathReplaceStringWithBoardID, b.ID)

			doc, errOccurred, err := c.GetWebPageDocumentWithPOST(ssangbongSchoolPageUrl, fmt.Sprintf("%s('%s') %s 게시판", c.Site, c.SiteID, b.Name))
			if err != nil {
				return nil, nil, errOccurred, err
			}

			ssangbongSchoolSelection := doc.Find(boardTypeConfig.articleSelector)
			if len(ssangbongSchoolSelection.Nodes) == 0 { // 읽어들인 게시글이 0건인지 확인
				if pageNo > 1 {
					// 2024년 03월 08일 기준으로 체험/행사활동안내, 방과후학교 > 방과후갤러리 게시판의 경우 입력된 데이터가 몇 건 없어서
					// 페이지가 1페이지 ~ 2페이지만 존재하므로 그 이상을 읽게 되면 무조건 빈 값이 반환되므로
					// 특별히 예외처리를 한다. 추후에 데이터가 충분히 추가되면 아래 IF 문은 삭제해도 된다.
					if b.ID == "156457" || b.ID == "156475" {
						goto SPECIALEXIT
					}
				}

				// 다음 게시판을 크롤링한다.
				goto NEXTBOARD
			}

			var foundAlreadyCrawledArticle = false
			ssangbongSchoolSelection.EachWithBreak(func(i int, s *goquery.Selection) bool {
				var article *feed.Article
				if article, err = c.extractArticle(b.ID, b.Type, boardTypeConfig.urlPath2, s); err != nil {
					return false
				}
				article.BoardID = b.ID
				article.BoardName = b.Name
				article.BoardType = b.Type

				// 크롤링 된 게시글 목록 중에서 가장 최근의 게시글 ID를 구한다.
				if newLatestCrawledArticleID == "" {
					newLatestCrawledArticleID = article.ArticleID
				}

				// 이미 크롤링 작업을 했었던 게시글인지 확인한다. 이후의 게시글 추출 작업은 취소된다.
				if article.ArticleID == latestCrawledArticleID {
					foundAlreadyCrawledArticle = true
					return false
				}
				if latestCrawledCreatedDate.IsZero() == false && article.CreatedAt.Before(latestCrawledCreatedDate) == true {
					foundAlreadyCrawledArticle = true
					return false
				}

				articles = append(articles, article)

				return true
			})
			if err != nil {
				return nil, nil, fmt.Sprintf("%s('%s') %s 게시판의 게시글 추출이 실패하였습니다. CSS셀렉터를 확인하세요.", c.Site, c.SiteID, b.Name), err
			}

			if foundAlreadyCrawledArticle == true {
				break
			}
		}

	SPECIALEXIT:
		if newLatestCrawledArticleID != "" {
			newLatestCrawledArticleIDsByBoard[b.ID] = newLatestCrawledArticleID
		}

	NEXTBOARD:
	}

	//
	// 게시글 내용 크롤링 : 내용은 크롤링이 실패해도 에러를 발생하지 않고 무시한다.
	// 동시에 여러개의 게시글을 읽는 경우 에러가 발생하는 경우가 생기므로 최대 1개씩 순차적으로 읽는다.
	// 만약 에러가 발생하여 게시글 내용을 크롤링 하지 못한 경우가 생길 수 있으므로 2번 크롤링한다.
	//
	for i := 0; i < 2; i++ {
		for _, article := range articles {
			if article.Content == "" {
				c.crawlingArticleContent(article)
			}
		}
	}

	// DB에 오래된 게시글부터 추가되도록 하기 위해 역순으로 재배열한다.
	for i, j := 0, len(articles)-1; i < j; i, j = i+1, j-1 {
		articles[i], articles[j] = articles[j], articles[i]
	}

	return articles, newLatestCrawledArticleIDsByBoard, "", nil
}

func (c *crawler) GetWebPageDocumentWithPOST(url, title string) (*goquery.Document, string, error) {
	querySplitIndex := strings.Index(url, "?")
	req, err := http.NewRequest("POST", url[:querySplitIndex], bytes.NewBufferString(url[querySplitIndex+1:]))
	if err != nil {
		return nil, fmt.Sprintf("%s 접근이 실패하였습니다.", title), err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("Accept-Language", "ko-KR,ko;q=0.9,en-US;q=0.8,en;q=0.7")
	req.Header.Set("Cache-Control", "max-age=0")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Host", strings.ReplaceAll(strings.ReplaceAll(c.SiteUrl, "http://", ""), "https://", ""))
	req.Header.Set("Origin", c.SiteUrl)
	req.Header.Set("Referer", url)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 11.0; Surface Duo) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Mobile Safari/537.36")

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Sprintf("%s 접근이 실패하였습니다.", title), err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Sprintf("%s 접근이 실패하였습니다.", title), fmt.Errorf("HTTP Response StatusCode %d", res.StatusCode)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(res.Body)

	resBodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Sprintf("%s의 내용을 읽을 수 없습니다.", title), err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(resBodyBytes)))
	if err != nil {
		return nil, fmt.Sprintf("%s의 HTML 파싱이 실패하였습니다.", title), err
	}

	return doc, "", nil
}
