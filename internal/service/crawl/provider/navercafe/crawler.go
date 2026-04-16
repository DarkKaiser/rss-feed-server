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

// crawlArticles 네이버 카페의 게시글 목록과 본문을 수집합니다.
//
// 실행 흐름 (2단계):
//  1. 목록 수집: 1페이지부터 MaxPageCount까지 순서대로 탐색하며 신규 게시글 목록을 수집합니다.
//     - 이 크롤러는 카페 게시글을 직접 방문하는 대신, 전체글보기 목록 페이지를 통해 글을 수집합니다.
//     - 이미 수집한 게시글(parsedLastCursor 이하)을 만나거나, 마지막 페이지(게시글 0건)에 도달하면 탐색을 중단합니다.
//     - 네이버 검색 색인 반영 지연으로 인해 delayCutoffTime 이후에 등록된 최신 글은 수집을 보류합니다.
//  2. 본문 수집: 1단계에서 수집한 게시글들의 상세 본문을 최대 3개씩 병렬로 가져옵니다.
//     - 동시성을 3으로 제한하여 대상 웹서버에 과도한 부하가 가해지는 것을 방어합니다.
//     - 타임아웃이나 시스템 종료 신호로 인해 본문 수집이 중단되더라도, 1단계에서 이미 확보한
//     목록 데이터(제목, 링크)와 커서(어디까지 읽었는지)는 롤백하지 않고 그대로 반환합니다.
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
		return nil, nil, c.Messagef("전체 게시글의 최근 수집 이력(Cursor)을 데이터베이스에서 조회하는 과정에서 예외가 발생하였습니다."), err
	}

	// ========================================
	// 2단계: 변수 초기화
	// ========================================

	// lastCursor(문자열)를 정수형으로 변환합니다. 커서가 없으면(첫 수집) 0으로 초기화합니다.
	var parsedLastCursor int64 = 0
	if lastCursor != "" {
		parsedLastCursor, err = strconv.ParseInt(lastCursor, 10, 64)
		if err != nil {
			return nil, nil, c.Messagef("최근 수집 이력(Cursor)에 기록된 게시글 식별자(ID)를 정수형 데이터로 변환하는 과정에서 예외가 발생하였습니다."), err
		}
	}

	// 이번 순환에서 신규로 확인된 게시글들을 담을 저장소입니다.
	// nil 대신 빈 슬라이스로 시작하여, 신규 게시글이 없어도 항상 non-nil 슬라이스를 반환하도록 보장합니다.
	var articles = make([]*feed.Article, 0)

	// 이번 수집에서 확인된 게시글들 중 가장 큰 ID를 추적합니다.
	// parsedLastCursor로 초기화하여, 신규 게시글이 하나라도 수집될 때만 실제로 전진하도록 합니다.
	var newCursor = parsedLastCursor

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
		// 대상 네이버 카페의 기본 주소(URL)에 고유 식별자(clubID)와 탐색 중인 현재 페이지 번호를 조합하여,
		// 카페 전체의 최신 게시글을 50개 단위로 반환하는 '전체글보기' 접속용 최종 웹사이트 주소를 만듭니다.
		pageURL := fmt.Sprintf("%s/ArticleList.nhn?search.clubid=%s&userDisplay=50&search.boardtype=L&search.totalCount=501&search.page=%d", c.Config().URL, c.clubID, page)

		doc, err := c.Scraper().FetchHTMLDocument(ctx, pageURL, nil)
		if err != nil {
			// [전체 롤백 정책] 에러 발생 시, 이전 페이지들에서 성공적으로 모아둔 데이터도 미련 없이 버리고 즉시 중단합니다.
			// 1. 에러를 무시하고 커서를 전진시키면: 해당 페이지의 게시물들이 영구적으로 수집 누락됩니다.
			// 2. 데이터 누락을 막기 위해 예전 커서는 유지하되 "앞서 성공한 결과만이라도 DB에 저장"하는 타협안을 택하면:
			//    다음 스케줄에 또다시 1페이지부터 긁어오면서, 이미 저장된 글인데도 신규 글인 줄 알고
			//    불필요한 파싱과 DB 중복 검사를 시도하게 됩니다. 이를 매 스케줄마다 무한 반복하면 엄청난 부하를 줍니다.
			// 따라서 데이터 꼬임 방지와 서버 보호를 위해 결과물 전체를 깨끗이 엎어버리는 설계를 택한 것입니다.
			return nil, nil, c.Messagef("전체 게시글 목록의 %d번 페이지 목록을 불러오지 못했습니다.", page), err
		}

		// ----------------------------------------
		// 3-3단계: 게시글 행 추출 및 유효성 검증
		// ----------------------------------------

		// 공지사항(board-notice)을 제외한 일반 게시글 행(tr)만 선택합니다.
		articleRows := doc.Find("div.article-board > table > tbody > tr:not(.board-notice)")

		// 게시글이 하나도 없을 때: 마지막 페이지 도달 / 빈 게시판 / CSS 셀렉터 오류 중 하나를 판별합니다.
		if len(articleRows.Nodes) == 0 {
			// [케이스 A] 2페이지부터: 글이 없다면 게시판의 마지막 페이지를 넘어선 것이므로 수집을 마칩니다.
			// (1페이지부터 N페이지까지 탐색 중 처음으로 빈 페이지를 만난 것 = 모든 글을 다 읽은 것)
			if page > 1 {
				break
			}

			// [케이스 B] 1페이지에서 일반 게시글(tr)은 없으나 공지글(tr.board-notice) 속성이 존재하는 엣지 케이스를 확인합니다.
			if doc.Find("div.article-board > table > tbody > tr.board-notice").Length() > 0 {
				// 공지글이 성공적으로 추출되었다는 것은 상위 컨테이너를 포함한 HTML 구조와 파싱 규칙이 정상임을 증명합니다.
				// 따라서 시스템 에러가 아닌 정상적인 '빈 게시판(새 글 없음)' 상태로 판단하여 탐색 루프를 조기 종료합니다.
				break
			}

			// [오류] 공지글조차 없는 경우, 웹사이트의 HTML 구조가 변경되어 기존 CSS 셀렉터가 더 이상 유효하지 않은 상태입니다.
			// 관리자가 해당 카페에 직접 접속해 HTML을 확인하고, articleRows 셀렉터를 최신 구조로 수정해야 합니다.
			msg := c.Messagef("전체 게시글 목록의 DOM 구조가 변경되었거나 파싱 규칙이 일치하지 않아 게시글 데이터 추출에 실패하였습니다. 데이터 추출 규칙(CSS Selector)의 무결성 점검 및 업데이트가 요구됩니다.")
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
		// goquery의 EachWithBreak() 함수 내부에서 'return false'를 호출하더라도,
		// 이는 단지 현재 페이지 내부의 '게시글 행(Row)' 순회만 조기 종료시킬 뿐,
		// 가장 바깥쪽에 있는 '전체 페이지(Page) 탐색 루프'까지 중단시키지는 못합니다.
		// 따라서 예전 글을 만나는 즉시 이 플래그를 true로 활성화하여 내부 순회를 끊고 빠져나온 뒤,
		// 외부 루프 하단에서 이 상태값을 확인하여 불필요한 다음 페이지 호출을 완전히 종료(break)하기 위함입니다.
		var reachedLastCursor = false

		// 수집된 웹페이지의 게시글 행(Row)을 위에서 아래로 순서대로 순회합니다.
		// 중간에 중단 조건(예: 예전 글 발견)이 발생하면 false를 반환하여 행 순회를 즉시 중단할 수 있습니다.
		articleRows.EachWithBreak(func(i int, s *goquery.Selection) bool {
			// [개별 게시글 추출 오류에 대한 부분 실패 처리]
			// 단일 게시글에서 HTML 돔 구조 이탈 등으로 파싱 에러가 발생하여도 전체 크롤링 작업을 즉시 중단(Abort)하지 않습니다.
			//
			// 만약 단일 오류 시 작업을 강제 중단하게 되면, 등록 형식을 지키지 않은 불량 게시물 단 1개 때문에
			// 연달아 등록된 멀쩡한 다른 신규 게시글들까지 모두 수집되지 못하고 파이프라인이 멈추는
			// 심각한 '포이즌 필(Poison Pill)' 병목 현상 및 알림 누락 장애가 발생할 수 있습니다.
			//
			// 따라서 전체 시스템의 견고함(Robustness)을 유지하기 위해, 정보를 제대로 추출하지 못한 예외적인 게시물은
			// 경고(Warning) 로그만 남긴 뒤 부드럽게 무시(Skip)하고, 다음 게시글에 대한 순회가 멈춤 없이 계속 진행되도록 설계되었습니다.
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
			// [답글(Reply) 제외 처리]
			// 목록에서 원본 게시글 아래에 들여쓰기로 달리는 '답글' 행은 파서가 의도적으로 nil을 반환합니다.
			// RSS 알림의 주 목적은 '새로운 본 게시글'을 전달하는 것이므로, 이러한 답글 데이터는 수집 대상에서 제외하고 가볍게 다음 행으로 넘어갑니다.
			if article == nil {
				return true
			}

			// [중복 판별: 이미 수집한 게시글인지 확인합니다]
			//
			// ★ 이 로직은 반드시 아래의 딜레이 체크(delayCutoffTime)보다 먼저 실행되어야 합니다. ★
			// 만약 딜레이 체크를 먼저 수행하면, 아직 색인에 반영되지 않아 딜레이에 걸린 게시글(예: 방금 등록된 최신 글)은
			// 'return true'로 곧장 넘어가 버립니다. 이 경우 이미 수집이 완료된 오래된 게시글조차 딜레이만 통과하면
			// 탐색 종료 조건(reachedLastCursor)이 영영 발동하지 못해, 크롤러가 불필요하게 무한히 다음 페이지를 탐색하는 무한 루프에 빠집니다.
			//
			// 네이버 카페의 게시글 ID는 항상 양의 정수이므로 정수 대소 비교 한 가지만으로 충분합니다.
			// 현재 게시글 ID가 마지막으로 수집한 게시글 ID(parsedLastCursor) 이하라면,
			// 이전 크롤링 사이클에서 이미 수집이 완료된 게시글입니다. 순회를 즉시 멈추고 다음 페이지 탐색도 중단합니다.
			parsedArticleID, _ := strconv.ParseInt(article.ArticleID, 10, 64)
			if parsedArticleID <= parsedLastCursor {
				reachedLastCursor = true
				return false
			}

			// [딜레이 필터: 네이버 검색 색인 반영 지연을 보완합니다]
			// 이 크롤러는 네이버 카페 게시글을 직접 방문하는 대신, 네이버 검색 색인을 통해 게시글 목록을 수집합니다.
			// 그런데 새 글이 등록되면 카페에는 즉시 나타나지만, 검색 색인에 반영되기까지 보통 수십 분이 소요됩니다.
			// 만약 색인 미반영 상태에서 수집을 시도하면, 게시글 본문 파싱이 불완전하게 이루어져 누락이 발생할 수 있습니다.
			//
			// 따라서 아래 두 조건을 모두 만족하는 게시글은 "아직 색인 반영 대기 중"으로 판단하여 이번 사이클의 수집 대상에서 제외하고,
			// 다음 수집 사이클로 미룹니다.
			//   조건 1) article.CreatedAt.Format("15:04:05") != "00:00:00"
			//          : 시각 정보(HH:MM:SS)가 00:00:00이 아닌 경우에만 딜레이를 적용합니다.
			//            목록 파싱에서 '오늘 등록된 글'은 정확한 시각이 파싱되지만,
			//            '과거 날짜 글(예: 2024-03-15)'은 시각 정보가 없어 항상 00:00:00으로 초기화됩니다.
			//            과거 날짜 글에 딜레이를 적용하면 정상적인 신규 게시글을 영원히 수집하지 못하는 버그가 생기므로,
			//            시각이 00:00:00으로 고정된 게시글은 딜레이를 적용하지 않고 그냥 통과시킵니다.
			//   조건 2) article.CreatedAt.After(delayCutoffTime)
			//          : 게시글 등록 시각이 딜레이 기준 시각(현재 시각 - crawlingDelayMinutes)보다 최신인 경우입니다.
			//            즉, 등록된 지 얼마 되지 않아 아직 색인에 반영되지 않았을 가능성이 있는 게시글을 걸러냅니다.
			//
			// ※ 딜레이에 걸려 수집을 보류한 게시글은 newCursor에도 반영하지 않습니다.
			//   만약 반영하면, 다음 사이클에서 해당 게시글의 ID가 parsedLastCursor 이하로 판정되어
			//   정작 수집해야 할 시점에 '이미 수집된 게시글'로 오인되어 영구 누락됩니다.
			if article.CreatedAt.Format("15:04:05") != "00:00:00" && article.CreatedAt.After(delayCutoffTime) {
				return true
			}

			// [날짜 기반 조기 탈출: ID 비교를 보완하는 2차 안전망]
			// 위의 정수 ID 비교는 네이버 카페의 순차적 ID 구조에서 신뢰도가 높지만, 만에 하나 ID 파싱 실패나 예상치 못한
			// 데이터 이상이 발생하는 경우를 대비하여 게시글의 등록일(날짜)을 추가 기준으로 사용하는 2차 안전망입니다.
			// 현재 게시글의 등록일이 마지막 수집 기준일(lastCreatedDate)보다 명확히 과거(날짜 단위)라면,
			// 이미 수집이 완료된 날짜 구간이므로 탐색을 즉시 중단합니다.
			//
			// [왜 시각(Time)이 아닌 날짜(Date) 단위로만 비교하는가?]
			// provider.ParseCreatedAt은 파싱 포맷에 따라 CreatedAt이 다르게 생성됩니다.
			//   - 당일 게시글 (예: "14:30"): 정확한 시각(HH:MM:SS)으로 파싱됩니다.
			//   - 과거 날짜 게시글 (예: "2024-03-15"): 시각 정보가 없어 항상 00:00:00으로 고정됩니다.
			// 따라서 두 값을 time.Time으로 직접 비교하면, 같은 날에 등록된 글이라도 시각 차이로 인해
			// "과거 글"로 오판되어 유효한 신규 게시글이 가려지는 경계값 오류가 발생할 수 있습니다.
			// 이를 방지하기 위해 두 값을 모두 "yyyy-MM-dd" 날짜 문자열로 변환 후 순수 날짜 단위로만 비교합니다.
			// 같은 날짜인 경우는 이 조건이 적용되지 않으며, 위의 ID 비교 결과로만 판단합니다.
			//
			// ★ 이 로직은 반드시 newCursor 갱신보다 먼저 수행해야 합니다. ★
			//   커서 갱신 이후에 수행하면, 수집하지 않을 게시글의 ID가 커서에 반영되어
			//   다음 사이클에서 실제 신규 게시글이 이미 수집된 것으로 잘못 판정되어 영구 누락됩니다.
			if !lastCreatedDate.IsZero() && article.CreatedAt.Format("2006-01-02") < lastCreatedDate.Format("2006-01-02") {
				reachedLastCursor = true
				return false
			}

			// [newCursor 갱신: 이번 수집의 최신 기준점을 추적합니다]
			// 수집된 신규 게시글들 중 ID가 가장 큰(가장 최신인) 게시글의 ID를 newCursor에 기록합니다.
			// 크롤링이 완료된 후 이 값은 DB에 저장되어, 다음 사이클에서 "여기까지는 이미 읽었다"는 기준점으로 사용됩니다.
			if newCursor < parsedArticleID {
				newCursor = parsedArticleID
			}

			// [게시판 필터: 수집 대상 게시판 소속 게시글만 최종 결과에 포함합니다]
			// 이 크롤러는 카페 전체의 게시글을 한꺼번에 읽어 오므로, 설정에 명시되지 않은 다른 게시판의 글도 함께 탐색됩니다.
			// 이 시점에는 해당 게시글의 커서(newCursor)를 이미 갱신한 상태이므로, 설정 밖 게시판의 글이라도
			// "여기까지 이미 읽었다"는 위치는 정상적으로 기록됩니다.
			// 따라서 대상 게시판에 소속되지 않은 게시글은 articles에만 추가하지 않고 가볍게 건너뛰면 됩니다.
			if !c.Config().HasBoard(article.BoardID) {
				return true
			}

			articles = append(articles, article)

			return true
		})

		// ----------------------------------------
		// 3-5단계: 중단 조건 (루프 탈출) 검증
		// ----------------------------------------
		// 현재 페이지의 '게시글 행(Row)' 순회를 마친 후, 다음 페이지를 요청할지 여부를 최종 판단합니다.
		// 만약 행을 순회하는 동안 이미 수집 파악이 끝난 예전 게시글을 만나 reachedLastCursor가 활성화되었다면,
		// 더 과거 페이지를 뒤져보더라도 모두 '수집된 예전 글'일 것이 자명합니다.
		// 따라서 대상 웹사이트의 서버 통신 부하를 줄이고 성능을 확보하기 위해, 전체 페이지 탐색 루프를 즉시 종료시킵니다.
		if reachedLastCursor {
			break
		}
	}

	// ========================================
	// 4단계: 본문 수집 (Worker Pool 방식)
	// ========================================
	// 수집된 게시글들의 상세 본문 내용을 읽어오는 작업입니다.
	// 대상 웹사이트의 서버 부하를 막기 위해, 한 번에 최대 3개씩만 동시에 병렬로 작업(Worker Pool)하도록 제한합니다.
	// 목록을 가져오는 것과 달리, 본문 수집은 실패하더라도 전체 작업에 영향을 주지 않고 부드럽게 무시합니다.
	if err := c.CrawlArticleContentsConcurrently(ctx, articles, 3, c.crawlArticleContent); err != nil {
		// 만약 타임아웃이나 시스템 종료 신호 때문에 본문을 단 1개도 채우지 못하고 강제로 작업이 중단되더라도,
		// 다음과 같은 이유로 이미 성공적으로 읽어 둔 '목록 데이터(제목, 링크)'와 '최신 커서 위치'는 롤백하지 않고 그대로 보존합니다.
		// 1. 방어적 설계: 에러 발생 시 정보를 버려버리면, 다음 수집 시 똑같은 게시물에서 또 타임아웃이 발생하여 크롤러가 영원히 정지하는 무한 루프(Poison Pill) 장애가 발생할 수 있습니다.
		// 2. 서비스 지속성: 다행히 RSS 서비스의 핵심은 '새 글 알림'입니다. 비록 본문은 누락되더라도 새 글의 제목과 원본 링크를 성공적으로 전달했다면 최소한의 목적은 달성된 것입니다.

		c.ReportError(c.Messagef("게시글 본문 파싱 프로세스 중 응답 타임아웃 또는 시스템 종료 시그널(Interrupt)이 감지되어 해당 크롤링 세션이 중단되었습니다."), err)
	}

	// ========================================
	// 5단계: 커서 갱신 및 역순 정렬
	// ========================================

	// [커서 갱신: 다음 수집 사이클의 시작점을 DB에 기록합니다]
	// newCursor는 parsedLastCursor(이전 커서 초기값)로 초기화되었습니다.
	// 따라서 이번 사이클에서 신규 게시글을 하나라도 수집했다면, newCursor는 parsedLastCursor보다 커집니다.
	// 이 조건(newCursor > parsedLastCursor)이 충족될 때만 DB를 갱신합니다.
	//
	// 조건을 만족하지 않는데도 갱신하면:
	//   - newCursor == parsedLastCursor인 경우: 기존에 저장된 유효한 커서가 같은 값으로 불필요하게 덮어씌워집니다.
	//   - 예외적으로 0을 Upsert하는 경우: 다음 사이클에서 모든 게시글을 신규로 오판하여 대량 중복 수집이 발생합니다.
	//
	// 네이버 카페는 게시판별 커서가 없습니다. 단일 카페 전체를 하나의 커서(EmptyBoardID)로 관리합니다.
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
