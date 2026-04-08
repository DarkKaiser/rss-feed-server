package navercafe

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/PuerkitoBio/goquery"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"
)

// component 크롤링 서비스의 네이버 카페 Provider 로깅용 컴포넌트 이름
const component = "crawl.provider.navercafe"

func init() {
	provider.MustRegister(config.ProviderSiteNaverCafe, &provider.CrawlerConfig{
		NewCrawler: newCrawler,
	})
}

func newCrawler(params provider.NewCrawlerParams) (provider.Crawler, error) {
	settings, err := provider.ParseSettings[crawlerSettings](params.Config.Data)
	if err != nil {
		return nil, err
	}

	c := &crawler{
		Base: provider.NewBase(params, 10),

		clubID: settings.ClubID,

		crawlingDelayMinutes: settings.CrawlingDelayMinutes,
	}

	c.SetCrawlArticles(c.crawlArticles)

	c.Logger().WithFields(applog.Fields{
		"component":     component,
		"board_count":   len(c.Config().Boards),
		"club_id":       c.clubID,
		"delay_minutes": c.crawlingDelayMinutes,
	}).Debug(c.Messagef("크롤러 생성 완료: Provider 초기화 수행"))

	return c, nil
}

type crawler struct {
	*provider.Base

	// clubID 크롤링 대상 네이버 카페의 고유 식별자(Club ID)입니다.
	// 게시글 목록 조회, 게시글 링크 생성, 내부 API 호출 등 네이버 카페에 관련된
	// 모든 URL 및 API 요청에서 카페를 특정하기 위해 사용됩니다.
	// 설정 파일(club_id)에서 주입되며, JSON 직렬화 키는 "club_id"입니다.
	clubID string

	// crawlingDelayMinutes 게시글이 등록된 후 네이버 검색 색인에 반영되기까지 기다려야 하는 예상 지연 시간(분)입니다.
	//
	// 이 크롤러는 네이버 카페 게시글을 직접 방문하는 대신, 네이버 검색 색인을 통해 글 목록을 수집합니다.
	// 그런데 새 글이 카페에 등록되더라도 네이버 검색 색인에는 즉시 반영되지 않고 일정 시간(보통 수십 분)이 경과한 뒤에야
	// 검색 결과에 나타납니다. 이로 인해 방금 등록된 신규 게시글은 크롤링 대상에서 일시적으로 누락될 수 있습니다.
	//
	// 이 값은 해당 지연 시간을 보완하는 일종의 "수집 보류 대기 시간"으로, 크롤링 시점 기준으로 이 분(分)만큼 이내에 등록된
	// 게시글은 색인 미반영 상태로 간주하여 해당 수집 사이클에서 수집을 보류하고 다음 사이클로 미룹니다.
	// 기본값은 40분이며, 설정 파일(crawling_delay_minutes)에서 조정할 수 있습니다.
	crawlingDelayMinutes int
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ provider.Crawler = (*crawler)(nil)

// @@@@@
// crawlArticles 네이버 카페의 게시글 목록과 본문을 수집합니다.
//
// 실행 흐름 (2단계):
//  1. 목록 수집: 1페이지부터 MaxPageCount까지 순서대로 탐색하며 신규 게시글 목록을 수집합니다.
//     - 이 크롤러는 카페 게시글을 직접 방문하는 대신, 전체글보기 목록 페이지를 통해 글을 수집합니다.
//     - 이미 수집한 게시글(parsedLastCursor 이하)을 만나거나, 마지막 페이지(게시글 0건)에 도달하면 탐색을 중단합니다.
//     - 네이버 검색 색인 반영 지연으로 인해 delayCutoffTime 이후에 등록된 최신 글은 수집을 보류합니다.
//  2. 본문 수집: 1단계에서 수집한 게시글들의 상세 본문을 최대 3개씩 병렬로 가져옵니다.
//     - 동시성을 3으로 제한하여 대상 웹서버에 과도한 부하가 가해지는 것을 방어합니다.
//     - 타임아웃이나 시스템 종료 신호로 인해 본문 수집이 중단되더라도,
//     1단계에서 이미 확보한 목록 데이터(제목, 링크)와 커서(어디까지 읽었는지)는 롤백하지 않고 그대로 반환합니다.
//
// 본문 중단 시 롤백하지 않는 이유:
//   - 방어적 설계: 롤백 시 다음 스케줄에서 같은 게시글을 재처리하다 또 타임아웃이 발생하여
//     크롤러가 영구적으로 정지하는 무한 루프(Poison Pill) 장애를 유발할 수 있습니다.
//   - 서비스 지속성: RSS의 핵심 가치는 '새 글 알림'입니다. 본문이 누락되더라도
//     제목과 원본 링크를 보존했다면 최소한의 서비스 목적은 달성된 것입니다.
//
// 반환값:
//   - []*feed.Article: 수집된 신규 게시글 목록 (본문이 누락된 항목이 포함될 수 있습니다)
//   - map[string]string: 최신 커서 맵 (key: EmptyBoardID, value: 최신 articleID). 신규 게시글이 없으면 빈 맵.
//   - string: 항상 빈 문자열("") 반환.
//   - error: 항상 nil 반환. 게시판 단위 오류는 내부에서 격리 처리하므로 이 함수 자체는 실패하지 않습니다.
func (c *crawler) crawlArticles(ctx context.Context) ([]*feed.Article, map[string]string, string, error) {
	// ========================================
	// 1단계: 최근 수집 이력 조회
	// ========================================
	// DB에서 이 카페의 마지막 수집 기준점(lastCursor: 게시글 ID 문자열, lastCreatedDate: 등록일)을 불러옵니다.
	// 이 값들은 이미 수집한 게시글을 건너뛰는 중복 판별 로직의 핵심 기준으로 사용됩니다.
	// DB 조회 자체에 실패하면 중복 판별이 불가능해 데이터 무결성을 보장할 수 없으므로, 전체를 롤백(error 반환)합니다.
	lastCursor, lastCreatedDate, err := c.FeedRepo().GetCrawlingCursor(ctx, c.ProviderID(), "")
	if err != nil {
		return nil, nil, c.Messagef("마지막으로 추가된 게시글 정보를 찾는 중에 오류가 발생하였습니다."), err
	}

	// ========================================
	// 2단계: 변수 초기화
	// ========================================

	// lastCursor(문자열)를 정수형으로 변환합니다. 커서가 없으면(첫 수집) 0으로 초기화합니다.
	var parsedLastCursor int64 = 0
	if lastCursor != "" {
		parsedLastCursor, err = strconv.ParseInt(lastCursor, 10, 64)
		if err != nil {
			return nil, nil, c.Messagef("마지막으로 추가된 게시글 ID를 숫자로 변환하는 중에 오류가 발생하였습니다."), err
		}
	}

	// 이번 순환에서 신규로 확인된 게시글들을 담을 저장소입니다.
	// nil 대신 빈 슬라이스로 시작하여, 신규 게시글이 없어도 항상 non-nil 슬라이스를 반환하도록 보장합니다.
	articles := make([]*feed.Article, 0)

	// newCursor는 이번 수집에서 확인된 게시글들 중 가장 큰 ID를 추적합니다.
	// parsedLastCursor로 초기화하여, 신규 게시글이 하나라도 수집될 때만 실제로 전진하도록 합니다.
	newCursor := parsedLastCursor

	// ========================================
	// 3단계: 페이지 순회 (목록 수집)
	// ========================================
	// 1페이지부터 MaxPageCount까지 순서대로 탐색하며 신규 게시글을 수집합니다.
	// 이미 수집한 게시글을 만나거나(reachedLastCursor), 마지막 페이지에 도달하면(게시글 0건) 탐색을 중단합니다.
	for page := 1; page <= c.MaxPageCount(); page++ {
		// ----------------------------------------
		// 3-1단계: 딜레이 기준 시각 계산
		// ----------------------------------------
		// 각 페이지 처리 시작 시점의 현재 시각으로 딜레이 기준값을 새로 계산합니다.
		// 여러 페이지를 순회하는 동안 실제 시간이 경과하므로, 루프 외부에서 고정된 기준값을 사용하면
		// 딜레이를 이미 통과한 게시글이 "아직 이른" 것으로 잘못 판정되어 과소 수집될 수 있습니다.
		delayCutoffTime := time.Now().Add(time.Duration(-1*c.crawlingDelayMinutes) * time.Minute)

		// ----------------------------------------
		// 3-2단계: URL 조립 & HTML 요청
		// ----------------------------------------
		pageURL := fmt.Sprintf("%s/ArticleList.nhn?search.clubid=%s&userDisplay=50&search.boardtype=L&search.totalCount=501&search.page=%d", c.Config().URL, c.clubID, page)

		doc, err := c.Scraper().FetchHTMLDocument(ctx, pageURL, nil)
		if err != nil {
			return nil, nil, c.Messagef("페이지 접근이 실패하였습니다."), err
		}

		// ----------------------------------------
		// 3-3단계: 게시글 행 추출 및 유효성 검증
		// ----------------------------------------
		// 공지사항(board-notice)을 제외한 일반 게시글 행(tr)만 선택합니다.
		articleRows := doc.Find("div.article-board > table > tbody > tr:not(.board-notice)")

		// 게시글이 하나도 없을 때: 마지막 페이지 도달 / 빈 게시판 / CSS 셀렉터 오류 중 하나를 판별합니다.
		if len(articleRows.Nodes) == 0 {
			// [케이스 A] 2페이지부터: 글이 없다면 게시판의 마지막 페이지를 넘어선 것이므로 수집을 마칩니다.
			if page > 1 {
				break
			}

			// [케이스 B] 1페이지에서 일반 게시글은 0건이지만 공지글이 존재하는 경우,
			// 정상적인 형태의 빈 게시판으로 처리합니다.
			if doc.Find("div.article-board > table > tbody > tr.board-notice").Length() > 0 {
				break
			}

			// [오류] 공지글조차 없는 경우, 웹사이트의 HTML 구조가 변경되어
			// 기존 CSS 셀렉터가 더 이상 유효하지 않은 상태입니다.
			// 관리자가 해당 카페에 직접 접속해 HTML을 확인하고,
			// articleRows 셀렉터를 최신 구조로 수정해야 합니다.
			msg := c.Messagef("도메인 구조가 변경되어 게시글 노드 추출에 실패하였습니다. CSS 셀렉터(article-board)의 업데이트가 요구됩니다.")
			errExtract := apperrors.New(apperrors.System, "원격 웹사이트 레이아웃 변경으로 인하여 파싱 컨테이너 노드 추출에 실패하였습니다")

			return nil, nil, msg, errExtract
		}

		// ----------------------------------------
		// 3-4단계: 게시글 행 순회 (중복 판별 & 커서 갱신)
		// ----------------------------------------

		// [크롤링 조기 종료(Early Exit) 상태 플래그]
		// 현재 페이지 탐색 중 "이미 수집 완료된 예전 게시글"을 마주쳤는지 기억하는 상태 변수입니다.
		//
		// [왜 이 변수가 별도로 필요한가요?]
		// EachWithBreak() 내부에서 'return false'를 호출하더라도,
		// 이는 단지 현재 페이지 내부의 '게시글 행(Row)' 순회만 조기 종료시킬 뿐,
		// 가장 바깥쪽에 있는 '전체 페이지(Page) 탐색 루프'까지 중단시키지는 못합니다.
		// 따라서 예전 글을 만나는 즉시 이 플래그를 true로 활성화하여 내부 순회를 끊고 빠져나온 뒤,
		// 외부 루프 하단에서 이 상태값을 확인하여 불필요한 다음 페이지 호출을 완전히 종료(break)합니다.
		var reachedLastCursor = false

		// 수집된 웹페이지의 게시글 행(Row)을 위에서 아래로 순서대로 순회합니다.
		// 중간에 중단 조건(예: 예전 글 발견)이 발생하면 false를 반환하여 행 순회를 즉시 중단할 수 있습니다.
		articleRows.EachWithBreak(func(i int, s *goquery.Selection) bool {
			// [개별 게시글 추출 오류에 대한 부분 실패 처리]
			// 단일 게시글에서 파싱 에러가 발생해도 전체 크롤링 작업을 즉시 중단(Abort)하지 않습니다.
			// 불량 게시물 1개 때문에 연달아 등록된 정상적인 신규 게시글들까지 수집되지 못하는
			// '포이즌 필(Poison Pill)' 장애를 방지하기 위해, 경고 로그만 남기고 다음 게시글로 넘어갑니다.
			article, err := c.extractArticle(s)
			if err != nil {
				c.Logger().WithFields(applog.Fields{
					"component": component,
					"club_id":   c.clubID,
					"page":      page,
					"row_index": i,
					"error":     err.Error(),
				}).Warn(c.Messagef("개별 게시글 처리 스킵: 데이터 추출 실패"))

				return true
			}
			if article == nil {
				return true // 답글 행(reply row) - 스킵
			}

			parsedArticleID, _ := strconv.ParseInt(article.ArticleID, 10, 64)

			// [중복 판별: 이미 수집한 게시글인지 확인합니다]
			//
			// ★ 이 로직은 반드시 딜레이 체크(delayCutoffTime)보다 먼저 실행되어야 합니다. ★
			// 딜레이 체크를 먼저 하면, 이미 수집된 게시글(ID ≤ parsedLastCursor)이 딜레이 미통과로
			// return true 처리되어 탐색 종료 조건이 발동하지 않고 무한히 다음 페이지를 탐색하게 됩니다.
			if parsedArticleID <= parsedLastCursor {
				reachedLastCursor = true
				return false
			}

			// [딜레이 필터: 네이버 검색 색인 반영 지연을 보완합니다]
			// 이 크롤러는 네이버 검색 색인을 통해 게시글을 수집하므로,
			// 새 글이 등록되어도 색인에 즉시 반영되지 않아 수집 누락이 발생할 수 있습니다.
			// delayCutoffTime 이후에 등록된 글은 색인 미반영 상태로 간주하여 다음 사이클로 수집을 미룹니다.
			//
			// 단, 시간 정보가 파싱되지 않아 00:00:00으로 고정된 과거 날짜 데이터는 딜레이를 통과시킵니다.
			// ※ 딜레이 미통과 게시글은 newCursor에도 반영하지 않습니다.
			//   반영할 경우, 다음 사이클에서 해당 게시글이 parsedLastCursor 이하로 판정되어 영구 누락됩니다.
			if article.CreatedAt.Format("15:04:05") != "00:00:00" && article.CreatedAt.After(delayCutoffTime) {
				return true
			}

			// [날짜 기반 조기 탈출: ID 비교를 보완하는 2차 안전망]
			// ParseCreatedAt은 과거 날짜 게시글의 시각을 00:00:00으로 고정할 수 있습니다.
			// 따라서 본문 파싱 단계에서 API를 통해 실제 작성 시간(예: 14:30:20)으로 덮어씌워지는 경우,
			// 단순 Before 비교 시 목록에서 파싱된 00:00:00과의 충돌로 신규 게시글이 영구 누락될 수 있습니다.
			// 이를 방지하기 위해 "yyyy-MM-dd" 날짜 문자열로 변환하여 순수 연월일 단위로만 비교합니다.
			//
			// ★ 이 로직은 반드시 newCursor 갱신보다 먼저 수행해야 합니다. ★
			//   커서 갱신 이후에 수행하면, 수집하지 않을 게시글의 ID가 커서에 반영되어
			//   다음 사이클에서 실제 신규 게시글이 이미 수집된 것으로 잘못 판정되어 영구 누락됩니다.
			if !lastCreatedDate.IsZero() && article.CreatedAt.Format("2006-01-02") < lastCreatedDate.Format("2006-01-02") {
				reachedLastCursor = true
				return false
			}

			// [newCursor 갱신: 이번 수집의 최신 기준점을 추적합니다]
			// 딜레이를 통과하고 CreatedAt 조건도 만족한 게시글만 커서 갱신 대상으로 처리합니다.
			// 수집된 게시글들 중 ID가 가장 큰(가장 최신인) 게시글의 ID를 newCursor에 기록합니다.
			if newCursor < parsedArticleID {
				newCursor = parsedArticleID
			}

			// 설정에 등록된 수집 대상 게시판의 게시글만 articles에 추가합니다.
			if !c.Config().HasBoard(article.BoardID) {
				return true
			}

			articles = append(articles, article)

			return true
		})

		// 현재 페이지의 행 순회 중 이미 수집 완료된 예전 게시글을 발견했다면,
		// 더 과거 페이지는 탐색할 필요가 없으므로 전체 페이지 탐색 루프를 즉시 종료합니다.
		if reachedLastCursor {
			break
		}
	}

	// ========================================
	// 4단계: 본문 수집 (Worker Pool 방식)
	// ========================================
	// 목록 수집에서 확보한 게시글들의 상세 본문을 최대 3개씩 병렬로 가져옵니다.
	// 본문 수집이 실패해도 에러를 전파하지 않고, 이미 확보된 목록 데이터와 커서를 보존합니다.
	if err := c.CrawlArticleContentsConcurrently(ctx, articles, 3, c.crawlingArticleContent); err != nil {
		// 본문 수집 중 시스템 에러(context 취소 또는 타임아웃)가 발생했더라도,
		// 이미 목록 크롤링으로 확보된 게시글과 커서 정보는 보존합니다.
		// nil을 반환하면 다음 사이클에서 동일 게시글을 처음부터 다시 수집하는 중복 재처리 루프가 발생합니다.
		// 본문이 없는 상태로 저장하고 커서를 갱신하여 중복 수집을 방지합니다.
		c.SendErrorNotification(c.Messagef("본문 수집 중 시스템 종료 시그널 또는 타임아웃이 발생하여 크롤링 작업이 중단되었습니다."), err)
	}

	// ========================================
	// 5단계: 커서 갱신 및 역순 정렬
	// ========================================

	// 딜레이를 통과한 신규 게시글이 하나라도 있어서 newCursor가 실제로 전진한 경우에만 갱신합니다.
	// newCursor == parsedLastCursor인 경우(= 커서 미갱신)에 빈 문자열이나 "0"을
	// DB에 Upsert하면 기존에 저장된 유효한 커서를 덮어쓰게 됩니다.
	var newCursors = map[string]string{}
	if newCursor > parsedLastCursor {
		newCursors[provider.EmptyBoardID] = strconv.FormatInt(newCursor, 10)
	}

	// 웹사이트는 최신 글이 맨 위에 오는 구조이므로, 현재 articles는 최신 → 오래된 순으로 담겨 있습니다.
	// DB 삽입 시 오래된 글부터 순서대로 처리되도록 뒤집어서 반환합니다.
	for i, j := 0, len(articles)-1; i < j; i, j = i+1, j-1 {
		articles[i], articles[j] = articles[j], articles[i]
	}

	return articles, newCursors, "", nil
}
