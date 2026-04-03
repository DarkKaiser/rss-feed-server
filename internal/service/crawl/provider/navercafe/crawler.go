package navercafe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/PuerkitoBio/goquery"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"
)

// component 크롤링 서비스의 네이버 카페 Provider 로깅용 컴포넌트 이름
const component = "crawl.provider.navercafe"

func init() {
	provider.MustRegister(config.ProviderSiteNaverCafe, &provider.CrawlerConfig{
		NewCrawler: func(params provider.NewCrawlerParams) (provider.Crawler, error) {
			data := naverCafeCrawlerConfigData{}
			if err := data.fillFromMap(params.Config.Data); err != nil {
				return nil, fmt.Errorf("작업 데이터가 유효하지 않아 %s('%s') Crawler 생성이 실패하였습니다. (error:%s)", params.Config.Name, params.Config.ID, err)
			}

			const defaultCrawlingDelayMinutes = 40
			if data.CrawlingDelayMinutes <= 0 {
				data.CrawlingDelayMinutes = defaultCrawlingDelayMinutes
			}

			crawlerInstance := &crawler{
				Base: provider.NewBase(
					params,
					10,
				),

				siteClubID: data.ClubID,

				crawlingDelayTimeMinutes: data.CrawlingDelayMinutes,
			}

			crawlerInstance.SetCrawlArticles(crawlerInstance.crawlArticles)

			applog.Debug(crawlerInstance.FormatMessage("Crawler가 생성되었습니다."))

			return crawlerInstance, nil
		},
	})
}

type naverCafeCrawlerConfigData struct {
	ClubID string `json:"club_id"`

	// CrawlingDelayMinutes 게시글이 등록된 후 네이버 검색 색인에 반영되기까지
	// 걸리는 예상 지연 시간(분)입니다. 기본값은 40분입니다.
	// 네이버 검색 색인 속도에 따라 설정 파일(data.crawling_delay_minutes)에서 조정하세요.
	CrawlingDelayMinutes int `json:"crawling_delay_minutes"`
}

func (d *naverCafeCrawlerConfigData) fillFromMap(m map[string]interface{}) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, d); err != nil {
		return err
	}
	return nil
}

type crawler struct {
	*provider.Base

	siteClubID string

	// 크롤링 지연 시간(분)
	// 네이버 검색을 이용하여 카페 게시글을 검색한 후 게시글 내용을 크롤링하는 방법을 이용하는 경우
	// 게시글이 등록되고 나서 일정 시간(그때그때 검색 시스템의 상황에 따라 차이가 존재함)이 경과한 후에
	// 검색이 가능하므로 크롤링 지연 시간을 둔다.
	crawlingDelayTimeMinutes int
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ provider.Crawler = (*crawler)(nil)

func (c *crawler) crawlArticles(ctx context.Context) ([]*feed.Article, map[string]string, string, error) {
	idString, latestCrawledCreatedDate, err := c.FeedRepo().GetCrawlingCursor(ctx, c.ProviderID(), "")
	if err != nil {
		return nil, nil, c.FormatMessage("마지막으로 추가된 게시글 정보를 찾는 중에 오류가 발생하였습니다."), err
	}
	var latestCrawledArticleID int64 = 0
	if idString != "" {
		latestCrawledArticleID, err = strconv.ParseInt(idString, 10, 64)
		if err != nil {
			return nil, nil, c.FormatMessage("마지막으로 추가된 게시글 ID를 숫자로 변환하는 중에 오류가 발생하였습니다."), err
		}
	}

	articles := make([]*feed.Article, 0)
	newLatestCrawledArticleID := latestCrawledArticleID

	//
	// 게시글 크롤링
	//
	for pageNo := 1; pageNo <= c.MaxPageCount(); pageNo++ {
		// 각 페이지 처리 시작 시점의 현재 시각으로 딜레이 기준값을 새로 계산합니다.
		// 여러 페이지를 순회하는 동안 실제 시간이 경과하므로, 루프 외부에서 고정된 기준값을 사용하면
		// 딜레이를 이미 통과한 게시글이 "아직 이른" 것으로 잘못 판정되어 과소 수집될 수 있습니다.
		crawlingDelayStartTime := time.Now().Add(time.Duration(-1*c.crawlingDelayTimeMinutes) * time.Minute)

		ncPageUrl := fmt.Sprintf("%s/ArticleList.nhn?search.clubid=%s&userDisplay=50&search.boardtype=L&search.totalCount=501&search.page=%d", c.Config().URL, c.siteClubID, pageNo)

		doc, err := c.Scraper().FetchHTMLDocument(ctx, ncPageUrl, nil)
		if err != nil {
			return nil, nil, c.FormatMessage("페이지 접근이 실패하였습니다."), err
		}

		ncSelection := doc.Find("div.article-board > table > tbody > tr:not(.board-notice)")
		if len(ncSelection.Nodes) == 0 { // 전체글보기의 게시글이 0건일 경우
			if pageNo > 1 {
				// 2페이지 이상에서 게시글이 0건이라면 등록된 게시글을 모두 읽음(EndOfData) 처리
				break
			}
			
			// 1페이지에서 일반 게시글은 0건이지만 공지글이 존재하는 경우, 정상적인 형태의 빈 게시판으로 처리합니다.
			if doc.Find("div.article-board > table > tbody > tr.board-notice").Length() > 0 {
				break
			}

			// 1페이지에서 게시글이 없는 경우 CSS 파싱이 실패한 것으로 본다.
			return nil, nil, c.FormatMessage("게시글 추출이 실패하였습니다. CSS셀렉터를 확인하세요."), errors.New("게시글 추출이 실패하였습니다.")
		}

		var foundAlreadyCrawledArticle = false
		ncSelection.EachWithBreak(func(i int, s *goquery.Selection) bool {
			article, err := c.extractArticle(s)
			if err != nil {
				applog.Warn(c.FormatMessage("개별 게시글 추출이 실패하여 스킵합니다. (error:%s)", err))
				return true
			}
			if article == nil {
				return true // 답글 행(reply row) - 스킵
			}

			articleID, _ := strconv.ParseInt(article.ArticleID, 10, 64)

			// 이미 크롤링 작업을 했었던 게시글인지 먼저 확인한다. 이후의 게시글 추출 작업은 취소된다.
			// ※ 딜레이 체크보다 먼저 수행해야 합니다.
			//   딜레이 체크를 먼저 하면, 이미 수집된 게시글(ID ≤ latestCrawledArticleID)이 딜레이 미통과로
			//   return true 처리되어 탐색 종료 조건이 발동하지 않고 무한히 다음 페이지를 탐색하게 됩니다.
			if articleID <= latestCrawledArticleID {
				foundAlreadyCrawledArticle = true
				return false
			}

			// 크롤링 대기 시간을 경과한 게시글인지 확인한다.
			// 아직 경과하지 않은 게시글이라면 크롤링 하지 않는다.
			// 단, 시간 정보가 파싱되지 않아 기준이 00:00:00으로 고정된 과거 날짜 데이터는 무조건 딜레이를 통과하도록 예외를 둔다.
			// ※ 딜레이 미통과 게시글은 커서(newLatestCrawledArticleID)에도 반영하지 않습니다.
			//   반영할 경우, 다음 사이클에서 해당 게시글이 latestCrawledArticleID 이하로 판정되어 영구 누락됩니다.
			if article.CreatedAt.Format("15:04:05") != "00:00:00" && article.CreatedAt.After(crawlingDelayStartTime) {
				return true
			}

			// ParseCreatedDate는 당일이 아닌 과거 날짜의 시각을 00:00:00 으로 고정할 수 있습니다.
			// 따라서 본문 파싱 단계에서 API를 통해 실제 작성 시간으로 덮어씌워지는 경우(예: 14:30:20),
			// 단순 Before 비교 시 목록에서 파싱된 00:00:00과의 충돌로 인해 신규 게시글이 영구 누락될 수 있습니다.
			// 이를 방지하기 위해 날짜 문자열(yyyy-MM-dd) 포맷으로 변환하여 순수 연월일 단위로만 일자 경과 여부를 비교합니다.
			// ※ 커서(newLatestCrawledArticleID) 갱신보다 반드시 먼저 수행해야 합니다.
			//   커서 갱신 이후에 수행하면, 수집하지 않을 게시글의 ID가 커서에 반영되어 다음 사이클에서
			//   실제 신규 게시글이 이미 수집된 것으로 잘못 판정되어 영구 누락될 수 있습니다.
			if !latestCrawledCreatedDate.IsZero() && article.CreatedAt.Format("2006-01-02") < latestCrawledCreatedDate.Format("2006-01-02") {
				foundAlreadyCrawledArticle = true
				return false
			}

			// 크롤링 된 게시글 목록 중에서 가장 최근의 게시글 ID를 구한다.
			// 딜레이를 통과하고 CreatedAt 조건도 만족한 게시글만 커서 갱신 대상으로 처리합니다.
			if newLatestCrawledArticleID < articleID {
				newLatestCrawledArticleID = articleID
			}

			// 추출해야 할 게시판인지 확인한다.
			if !c.Config().HasBoard(article.BoardID) {
				return true
			}

			articles = append(articles, article)

			return true
		})
		// 개별 파싱 에러(parseErr)에 의해 즉시 크롤링을 포기하는 방식을 폐기하고, 경고 로그만 남기며 계속 진행됨

		if foundAlreadyCrawledArticle == true {
			break
		}
	}

	//
	// 게시글 내용 크롤링 (Worker Pool 방식을 통한 제한적 동시 수집 및 개별 재시도)
	// 내용은 크롤링이 실패해도 에러를 발생하지 않고 무시한다.
	//
	if err := c.CrawlArticleContentsConcurrently(ctx, articles, 3, c.crawlingArticleContent); err != nil {
		// 본문 수집 중 시스템 에러(context 취소 또는 타임아웃)가 발생했더라도,
		// 이미 목록 크롤링으로 확보된 게시글과 커서 정보는 보존합니다.
		// nil을 반환하면 다음 사이클에서 동일 게시글을 처음부터 다시 수집하는 중복 재처리 루프가 발생합니다.
		// 본문이 없는 상태로 저장하고 커서를 갱신하여 중복 수집을 방지합니다.
		errOccurred := c.FormatMessage("본문 수집 중 시스템 종료 시그널 또는 타임아웃이 발생하여 크롤링 작업이 중단되었습니다.")
		c.SendErrorNotification(errOccurred, err)
	}

	var newLatestCrawledArticleIDsByBoard = map[string]string{}
	// 딜레이 통과 게시글이 하나라도 있어서 커서가 실제로 전진한 경우에만 갱신합니다.
	// newLatestCrawledArticleID == latestCrawledArticleID 인 경우(= 커서 미갱신)에 빈 문자열이나 "0"을
	// DB에 Upsert하면 기존에 저장된 유효한 커서를 덮어쓰게 됩니다.
	if newLatestCrawledArticleID > latestCrawledArticleID {
		newLatestCrawledArticleIDsByBoard[provider.EmptyBoardID] = strconv.FormatInt(newLatestCrawledArticleID, 10)
	}

	// DB에 오래된 게시글부터 추가되도록 하기 위해 배열을 역순으로 재배열합니다.
	for i, j := 0, len(articles)-1; i < j; i, j = i+1, j-1 {
		articles[i], articles[j] = articles[j], articles[i]
	}

	return articles, newLatestCrawledArticleIDsByBoard, "", nil
}
