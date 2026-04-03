package yeosucityhall

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"
)

// component 크롤링 서비스의 여수시청 Provider 로깅용 컴포넌트 이름
const component = "crawl.provider.yeosucityhall"

const (
	// 포토뉴스
	yeosuCityHallCrawlerBoardTypePhotoNews string = "P"

	// 카드뉴스
	yeosuCityHallCrawlerBoardTypeCardNews string = "C"

	// 리스트 1(번호, 제목, 등록자, 등록일, 조회)
	yeosuCityHallCrawlerBoardTypeList1 string = "L_1"

	// 리스트 2(번호, 분류, 제목, 담당부서, 등록일, 조회)
	yeosuCityHallCrawlerBoardTypeList2 string = "L_2"
)

// serverAnomalyResult detectServerAnomaly 함수의 반환 타입으로,
// 서버 이상 감지 결과를 나타내는 열거형입니다.
type serverAnomalyResult int

const (
	// serverAnomalyNone 이상 없음. 정상 처리를 계속합니다.
	serverAnomalyNone serverAnomalyResult = iota
	// serverAnomalyNextBoard 이 게시판의 데이터가 없으므로 다음 게시판으로 건너뜁니다. (커서 미갱신)
	serverAnomalyNextBoard
	// serverAnomalyEndOfData 정상적으로 게시판의 모든 데이터를 소진하여 탐색을 조기 종료합니다. (커서 갱신)
	serverAnomalyEndOfData
	// serverAnomalyCSSError CSS 셀렉터로 게시글을 파싱할 수 없습니다.
	serverAnomalyCSSError
	// serverAnomalyTypeError 구현되지 않은 게시판 타입입니다.
	serverAnomalyTypeError
)

// detectServerAnomaly 여수시청 서버의 이상 여부를 게시판 타입별 기준에 따라 판별합니다.
//
// 배경:
//   - 포토뉴스/카드뉴스 타입: 서버 이상 시 articleSelector 노드 수 = 0
//     (articleGroupSelector가 1개이면 서버 이상, 0개이면 CSS 파싱 실패로 구분)
//   - 리스트 타입: 서버 이상 시 articleSelector 노드 수 = 1 이며 td.data_none 존재
//     (노드 수 = 0이면 CSS 파싱 실패)
//
// 호출자는 반환된 serverAnomalyResult에 따라 다음 동작을 결정합니다.
func detectServerAnomaly(doc *goquery.Document, sel *goquery.Selection, cfg *yeosuCityHallCrawlerBoardTypeConfig, b *config.BoardConfig, pageNo int) serverAnomalyResult {
	count := sel.Length()

	switch b.Type {
	case yeosuCityHallCrawlerBoardTypePhotoNews, yeosuCityHallCrawlerBoardTypeCardNews:
		// 포토뉴스/카드뉴스: 데이터가 없으면 노드 수 = 0 (articleGroupSelector로 정상 페이지 여부 재확인)
		if count == 0 {
			if doc.Find(cfg.articleGroupSelector).Length() == 1 {
				if pageNo > 1 {
					return serverAnomalyEndOfData
				}
				return serverAnomalyNextBoard
			}
			return serverAnomalyCSSError
		}

	case yeosuCityHallCrawlerBoardTypeList1, yeosuCityHallCrawlerBoardTypeList2:
		// 리스트: 데이터가 없으면 노드 수 = 1 이며 td에 data_none 클래스 존재
		// count == 0이면 tr 요소가 하나도 파싱되지 않은 것이므로 명백한 CSS/구조 변경 에러입니다.
		if count == 0 {
			// 단, 일반 글은 없고 공지만 존재하는 경우 CSS 오류가 아닌 정상적인 형태의 빈 게시판으로 간주합니다.
			if doc.Find("#content table.board_basic > tbody > tr.notice").Length() > 0 {
				if pageNo > 1 {
					return serverAnomalyEndOfData
				}
				return serverAnomalyNextBoard
			}
			return serverAnomalyCSSError
		}
		if count == 1 {
			td := sel.First().Find("td")
			if td.Length() == 1 {
				if td.HasClass("data_none") {
					if pageNo > 1 {
						return serverAnomalyEndOfData
					}
					return serverAnomalyNextBoard
				}
			}
		}

	default:
		return serverAnomalyTypeError
	}

	return serverAnomalyNone
}

var yeosuCityHallCrawlerBoardTypes map[string]*yeosuCityHallCrawlerBoardTypeConfig

type yeosuCityHallCrawlerBoardTypeConfig struct {
	urlPath              string
	articleSelector      string
	articleGroupSelector string
}

const yeosuCityHallUrlPathReplaceStringWithBoardID = "#{board_id}"

func init() {
	provider.MustRegister(config.ProviderSiteYeosuCityHall, &provider.CrawlerConfig{
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
	yeosuCityHallCrawlerBoardTypes = map[string]*yeosuCityHallCrawlerBoardTypeConfig{
		yeosuCityHallCrawlerBoardTypePhotoNews: {
			urlPath:              fmt.Sprintf("/www/govt/news/%s", yeosuCityHallUrlPathReplaceStringWithBoardID),
			articleSelector:      "#content div.board_list_box div.board_list div.item",
			articleGroupSelector: "#content div.board_list_box",
		},
		yeosuCityHallCrawlerBoardTypeList1: {
			urlPath:              fmt.Sprintf("/www/govt/news/%s", yeosuCityHallUrlPathReplaceStringWithBoardID),
			articleSelector:      "#content table.board_basic > tbody > tr:not(.notice)",
			articleGroupSelector: "#content",
		},
		yeosuCityHallCrawlerBoardTypeList2: {
			urlPath:              fmt.Sprintf("/www/govt/news/%s", yeosuCityHallUrlPathReplaceStringWithBoardID),
			articleSelector:      "#content table.board_basic > tbody > tr:not(.notice)",
			articleGroupSelector: "#content",
		},
		yeosuCityHallCrawlerBoardTypeCardNews: {
			urlPath:              fmt.Sprintf("/www/govt/news/%s", yeosuCityHallUrlPathReplaceStringWithBoardID),
			articleSelector:      "#content div.board_list_box div.board_list > div.board_list > div.board_photo > div.item_wrap > div.item",
			articleGroupSelector: "#content div.board_list_box",
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
			c.SendErrorNotification(msg, err)
			continue // 개별 게시판 오류 발생 시 전체 로직을 멈추지 않고 시스템 전파(누수)를 차단하여 성공한 다른 게시판 데이터 보존
		}

		articles = append(articles, boardArticles...)
		if cursor != "" {
			newLatestCrawledArticleIDsByBoard[b.ID] = cursor
		}
	}

	//
	// 게시글 내용 크롤링 (Worker Pool 방식을 통한 제한적 동시 수집 및 개별 재시도)
	// 내용은 크롤링이 실패해도 에러를 발생하지 않고 무시한다.
	// (기존의 불안정한 단일/이중 루프 순차 처리 방식 대신 여수시청 홈페이지 성능을 고려해 동시 작업 수를 2개로 통제하고, 실패 건별로 독립 재시도를 지원합니다)
	//
	if err := c.CrawlArticleContentsConcurrently(ctx, articles, 2, c.crawlingArticleContent); err != nil {
		// 본문 수집 중 시스템 에러(context 취소 또는 타임아웃)가 발생했더라도,
		// 이미 목록 크롤링으로 확보된 게시글과 커서 정보는 보존합니다.
		// nil을 반환하면 다음 사이클에서 동일 게시글을 처음부터 다시 수집하는 중복 재처리 루프가 발생합니다.
		// 본문이 없는 상태로 저장하고 커서를 갱신하여 중복 수집을 방지합니다.
		errOccurred := c.FormatMessage("본문 수집 중 시스템 종료 시그널 또는 타임아웃이 발생하여 크롤링 작업이 중단되었습니다.")
		c.SendErrorNotification(errOccurred, err)
	}

	return articles, newLatestCrawledArticleIDsByBoard, "", nil
}

func (c *crawler) crawlSingleBoard(ctx context.Context, b *config.BoardConfig) ([]*feed.Article, string, string, error) {
	var articles = make([]*feed.Article, 0)

	boardTypeConfig, exists := yeosuCityHallCrawlerBoardTypes[b.Type]
	if exists == false {
		return nil, "", c.FormatMessage("게시판 Type별 정보를 구하는 중에 오류가 발생하였습니다."), fmt.Errorf("구현되지 않은 게시판 Type('%s') 입니다.", b.Type)
	}

	latestCrawledArticleID, latestCrawledCreatedDate, err := c.FeedRepo().GetCrawlingCursor(ctx, c.ProviderID(), b.ID)
	if err != nil {
		return nil, "", c.FormatMessage("%s 게시판에 마지막으로 추가된 게시글 정보를 찾는 중에 오류가 발생하였습니다.", b.Name), err
	}

	// 이전 커서값이 아닌 빈 문자열로 초기화하여, 신규 게시글이 실제로 수집된 경우에만
	// 커서를 갱신합니다. latestCrawledArticleID 로 초기화하면 신규 게시글이 없어도
	// 불필요한 DB Upsert가 발생하고, 특수 상황에서 커서 역전이 일어날 수 있습니다.
	var newLatestCrawledArticleID = ""

	//
	// 게시글 크롤링
	//
CrawlLoop:
	for pageNo := 1; pageNo <= c.MaxPageCount(); pageNo++ {
		ysPageUrl := strings.ReplaceAll(fmt.Sprintf("%s%s?page=%d", c.Config().URL, boardTypeConfig.urlPath, pageNo), yeosuCityHallUrlPathReplaceStringWithBoardID, b.ID)

		doc, err := c.Scraper().FetchHTMLDocument(ctx, ysPageUrl, nil)
		if err != nil {
			// 부분 수집 시 커서를 갱신하지 않으면 스케줄링 주기에 따라 무한 재처리 및 
			// 타겟 서버 DDoS 부하를 유발하는 치명적 버그가 발생할 수 있으므로, 
			// 페이지 접근 에러 시 부분 반환 대신 전체 롤백(error 반환) 처리합니다.
			return nil, "", c.FormatMessage("%s 게시판 접근이 실패하였습니다. (page: %d)", b.Name, pageNo), err
		}

		ysSelection := doc.Find(boardTypeConfig.articleSelector)
		switch detectServerAnomaly(doc, ysSelection, boardTypeConfig, b, pageNo) {
		case serverAnomalyNextBoard:
			return articles, "", "", nil
		case serverAnomalyEndOfData:
			break CrawlLoop
		case serverAnomalyCSSError:
			return nil, "", c.FormatMessage("%s 게시판의 게시글 추출이 실패하였습니다. CSS셀렉터를 확인하세요.", b.Name), errors.New("게시글 추출이 실패하였습니다.")
		case serverAnomalyTypeError:
			return nil, "", c.FormatMessage("%s 게시판의 게시글 추출이 실패하였습니다.", b.Name), fmt.Errorf("구현되지 않은 게시판 Type('%s') 입니다.", b.Type)
		}

		var foundAlreadyCrawledArticle = false
		ysSelection.EachWithBreak(func(i int, s *goquery.Selection) bool {
			article, err := c.extractArticle(b.Type, s)
			if err != nil {
				applog.Warn(c.FormatMessage("%s 게시판에서 개별 게시글 추출이 실패하여 스킵합니다. (error:%s)", b.Name, err))
				return true
			}
			article.BoardID = b.ID
			article.BoardName = b.Name
			article.BoardType = b.Type

			// 이미 크롤링 작업을 했었던 게시글인지 먼저 확인한다. 이후의 게시글 추출 작업은 취소된다.
			// 중복 판별을 커서 갱신보다 먼저 수행하여, 이미 수집된 게시글 ID가 최신 커서에
			// 잘못 반영되는 것을 방지합니다.
			articleIDInt, errArt := strconv.ParseInt(article.ArticleID, 10, 64)
			latestIDInt, errLatest := strconv.ParseInt(latestCrawledArticleID, 10, 64)

			if errArt == nil && errLatest == nil && latestCrawledArticleID != "" {
				if articleIDInt <= latestIDInt {
					foundAlreadyCrawledArticle = true
					return false
				}
			} else {
				// 숫자로 변환이 불가능한 ID일 경우를 대비한 가드 로직 (문자열 길이가 짧거나, 같을 때 사전식 작거나 같은 경우 처리)
				if latestCrawledArticleID != "" {
					id1, id2 := article.ArticleID, latestCrawledArticleID
					if len(id1) < len(id2) || (len(id1) == len(id2) && id1 <= id2) {
						foundAlreadyCrawledArticle = true
						return false
					}
				}
			}
			// ParseCreatedDate는 당일이 아닌 과거 날짜의 시각을 00:00:00 으로 고정합니다.
			// 시각 정보 불일치에 따른 오판을 방지하기 위해 날짜 문자열(yyyy-MM-dd) 포맷으로 변환하여
			// 순수 연월일 단위로만 일자 경과 여부를 비교합니다. (동일 날짜는 ID 비교로만 처리됨)
			if !latestCrawledCreatedDate.IsZero() && article.CreatedAt.Format("2006-01-02") < latestCrawledCreatedDate.Format("2006-01-02") {
				foundAlreadyCrawledArticle = true
				return false
			}

			// 게시글을 articles에 먼저 추가한 후 커서를 갱신합니다.
			// 순서 역전(커서 갱신 → append) 상태에서 패닉 등 런타임 오류 발생 시
			// 커서만 전진하고 게시글이 영구 누락되는 데이터 무결성 오류를 방지합니다.
			articles = append(articles, article)

			// 신규 게시글로 확인된 경우에만 최신 커서를 갱신합니다.
			if newLatestCrawledArticleID == "" {
				newLatestCrawledArticleID = article.ArticleID
			} else {
				artIDInt, err1 := strconv.ParseInt(article.ArticleID, 10, 64)
				newLatestIDInt, err2 := strconv.ParseInt(newLatestCrawledArticleID, 10, 64)
				if err1 == nil && err2 == nil {
					if artIDInt > newLatestIDInt {
						newLatestCrawledArticleID = article.ArticleID
					}
				} else {
					id1, id2 := article.ArticleID, newLatestCrawledArticleID
					if len(id1) > len(id2) || (len(id1) == len(id2) && id1 > id2) {
						newLatestCrawledArticleID = article.ArticleID
					}
				}
			}

			return true
		})
		// 개별 파싱 에러(parseErr)에 의해 즉시 크롤링을 포기하는 방식을 폐기하고, 경고 로그만 남기며 계속 진행됨

		if foundAlreadyCrawledArticle == true {
			break
		}
	}

	for i, j := 0, len(articles)-1; i < j; i, j = i+1, j-1 {
		articles[i], articles[j] = articles[j], articles[i]
	}

	return articles, newLatestCrawledArticleID, "", nil
}
