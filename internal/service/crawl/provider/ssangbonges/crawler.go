package ssangbonges

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"
)

// component 크롤링 서비스의 쌍봉초등학교 Provider 로깅용 컴포넌트 이름
const component = "crawl.provider.ssangbonges"

const (
	// 리스트 1(번호, 제목, 작성자, 등록일, 조회)
	ssangbongSchoolCrawlerBoardTypeList1 string = "L_1"

	// 포토 1
	ssangbongSchoolCrawlerBoardTypePhoto1 string = "P_1"

	// 회원제(비공개) 처리된 학교앨범 게시판 고유 ID
	ssangbongSchoolCrawlerBoardIDSchoolAlbum string = "156453"
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
		NewCrawler: func(params provider.NewCrawlerParams) (provider.Crawler, error) {
			crawlerInstance := &crawler{
				Base: provider.NewBase(
					params,
					3,
				),
			}

			crawlerInstance.SetCrawlArticles(crawlerInstance.crawlArticles)

			applog.Debug(crawlerInstance.FormatMessage("Crawler가 생성되었습니다."))

			return crawlerInstance, nil
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
	*provider.Base
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ provider.Crawler = (*crawler)(nil)

// noinspection GoErrorStringFormat,GoUnhandledErrorResult
func (c *crawler) crawlArticles(ctx context.Context) ([]*feed.Article, map[string]string, string, error) {
	var articles = make([]*feed.Article, 0)
	var newLatestCrawledArticleIDsByBoard = make(map[string]string)

	for _, b := range c.Config().Boards {
		boardArticles, cursor, msg, err := c.crawlSingleBoard(ctx, b)
		if err != nil {
			return nil, nil, msg, err
		}

		articles = append(articles, boardArticles...)
		if cursor != "" {
			newLatestCrawledArticleIDsByBoard[b.ID] = cursor
		}
	}

	//
	// 게시글 내용 크롤링 (Worker Pool 방식을 통한 제한적 동시 수집 및 개별 재시도)
	// 내용은 크롤링이 실패해도 에러를 발생하지 않고 무시한다.
	// (기존의 불안정한 전체 순차 이중 루프 방식 대신 대상 서버 부하를 고려해 최대 동시 작업 수를 제한하고, 실패 건별로 독립적인 재시도를 수행합니다)
	//
	if err := c.CrawlArticleContentsConcurrently(ctx, articles, 2, c.crawlingArticleContent); err != nil {
		return nil, nil, c.FormatMessage("시스템 종료 시그널 또는 타임아웃이 발생하여 크롤링 작업이 중단되었습니다."), err
	}

	// DB에 오래된 게시글부터 추가되도록 하기 위해 역순으로 재배열한다.
	for i, j := 0, len(articles)-1; i < j; i, j = i+1, j-1 {
		articles[i], articles[j] = articles[j], articles[i]
	}

	return articles, newLatestCrawledArticleIDsByBoard, "", nil
}

func (c *crawler) crawlSingleBoard(ctx context.Context, b *config.BoardConfig) ([]*feed.Article, string, string, error) {
	var articles = make([]*feed.Article, 0)

	boardTypeConfig, exists := ssangbongSchoolCrawlerBoardTypes[b.Type]
	if exists == false {
		return nil, "", c.FormatMessage("게시판 Type별 정보를 구하는 중에 오류가 발생하였습니다."), fmt.Errorf("구현되지 않은 게시판 Type('%s') 입니다.", b.Type)
	}

	latestCrawledArticleID, latestCrawledCreatedDate, err := c.FeedRepo().GetCrawlingCursor(ctx, c.ProviderID(), b.ID)
	if err != nil {
		return nil, "", c.FormatMessage("%s 게시판에 마지막으로 추가된 게시글 정보를 찾는 중에 오류가 발생하였습니다.", b.Name), err
	}

	var newLatestCrawledArticleID = ""

	//
	// 게시글 크롤링
	//
	for pageNo := 1; pageNo <= c.MaxPageCount(); pageNo++ {
		ssangbongSchoolPageUrl := strings.ReplaceAll(fmt.Sprintf("%s%s&currPage=%d", c.Config().URL, boardTypeConfig.urlPath1, pageNo), ssangbongSchoolUrlPathReplaceStringWithBoardID, b.ID)

		doc, err := c.fetchDocumentWithPOST(ctx, ssangbongSchoolPageUrl, c.FormatMessage("%s 게시판 접근이 실패하였습니다.", b.Name))
		if err != nil {
			return nil, "", err.Error(), err
		}

		ssangbongSchoolSelection := doc.Find(boardTypeConfig.articleSelector)
		if len(ssangbongSchoolSelection.Nodes) == 0 { // 읽어들인 게시글이 0건인지 확인
			if pageNo > 1 {
				// 2024년 03월 08일 기준으로 체험/행사활동안내, 방과후학교 > 방과후갤러리 게시판의 경우 입력된 데이터가 몇 건 없어서
				// 페이지가 1페이지 ~ 2페이지만 존재하므로 그 이상을 읽게 되면 무조건 빈 값이 반환되므로
				// 특별히 예외처리를 한다. 추후에 데이터가 충분히 추가되면 아래 IF 문은 삭제해도 된다.
				if b.ID == "156457" || b.ID == "156475" {
					return articles, newLatestCrawledArticleID, "", nil
				}
			}

			// 커서 미갱신 상태로 조기 종료
			return articles, "", "", nil
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
			return nil, "", c.FormatMessage("%s 게시판의 게시글 추출이 실패하였습니다. CSS셀렉터를 확인하세요.", b.Name), err
		}

		if foundAlreadyCrawledArticle == true {
			break
		}
	}

	return articles, newLatestCrawledArticleID, "", nil
}

func (c *crawler) fetchDocumentWithPOST(ctx context.Context, url, title string) (*goquery.Document, error) {
	querySplitIndex := strings.Index(url, "?")
	if querySplitIndex == -1 {
		return nil, fmt.Errorf("%s URL에서 쿼리스트링을 찾을 수 없습니다.", title)
	}

	reqBody := bytes.NewBufferString(url[querySplitIndex+1:])

	head := make(http.Header)
	head.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	head.Set("Content-Type", "application/x-www-form-urlencoded")
	// Host와 Origin 헤더 등은 fetcher가 자동으로 설정하거나 기본 클라이언트 정책을 따릅니다.

	doc, err := c.Scraper().FetchHTML(ctx, "POST", url[:querySplitIndex], reqBody, head)
	if err != nil {
		return nil, fmt.Errorf("%s (error:%v)", title, err)
	}

	return doc, nil
}
