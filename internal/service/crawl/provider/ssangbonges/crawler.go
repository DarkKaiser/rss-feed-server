package ssangbonges

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"
)

// component 크롤링 서비스의 쌍봉초등학교 Provider 로깅용 컴포넌트 이름
const component = "crawl.provider.ssangbonges"

const (
	// boardTypeList1 글과 텍스트 위주로 구성된 일반적인 목록형 게시판을 의미합니다.
	// (예: 번호, 제목, 작성자, 등록일 등이 표 형식으로 나타나는 가장 기본적인 형태입니다)
	boardTypeList1 = "L_1"

	// boardTypePhoto1 썸네일 사진들이 바둑판(그리드) 형식으로 나열되어 있는 포토 갤러리 형태의 게시판을 의미합니다.
	boardTypePhoto1 = "P_1"

	// boardIDSchoolAlbum 웹사이트 내에서 비공개 처리된 '학교앨범' 게시판의 실제 ID 번호입니다.
	// 이 게시판은 권한 문제로 게시글 내용을 상세 조회할 수 없습니다. 따라서 코드 내부에서 이 ID를 발견하면,
	// 상세 조회 대신 목록 화면에 보이는 썸네일 이미지를 본문으로 대신 가져오도록 특별한 예외 처리를 수행합니다.
	boardIDSchoolAlbum = "156453"

	// boardIDPlaceholder 게시판의 다양한 URL 주소 패턴을 만들 때, 실제 게시판 ID 값이 들어갈 자리를 비워두기 위해 사용하는 문자열(플레이스홀더)입니다.
	// 수집기가 동작할 때 이 빈 자리에 실제 대상 게시판 ID를 채워 넣어 완성된 주소를 만듭니다.
	boardIDPlaceholder = "#{board_id}"
)

// boardTypeConfig 형태가 서로 다른 게시판들을 어떻게 읽어올지 설정 정보를 담아두는 구조체입니다.
// 게시판마다 목록용 주소나 화면 디자인(HTML 요소)이 다르기 때문에 이러한 설정값들이 꼭 필요합니다.
type boardTypeConfig struct {
	// listURLTemplate 게시판 목록 첫 페이지나 다음 페이지들을 부를 때 사용하는 URL 주소의 기본 형태입니다.
	listURLTemplate string

	// detailURLTemplate 게시글 하나를 클릭해서 들어갔을 때, 그 상세 내용을 부르기 위한 URL 주소의 기본 형태입니다.
	detailURLTemplate string

	// articleSelector 게시판 목록 화면에서 각각의 '게시글 한 줄(또는 사진 한 장)'을 특정해내기 위해 사용하는 CSS 셀렉터입니다.
	articleSelector string

	// articleGroupSelector 게시글 목록 전체를 크게 감싸고 있는 부모 상자(컨테이너)를 집어내기 위한 CSS 셀렉터입니다.
	// 만약 articleSelector로 글을 하나도 찾지 못했다면, 이 부모 상자가 화면에 정상적으로 존재하는지 먼저 확인합니다.
	// 상자조차 찾지 못했다면 "웹사이트 구조가 바뀌어서 에러가 발생했다"라고 판단하고, 상자는 있는데 글만 없다면 "등록된 글이 하나도 없는 빈 게시판"으로 쉽게 구분할 수 있도록 돕습니다.
	articleGroupSelector string
}

// boardTypes 게시판의 유형(예: 일반 목록형, 포토 갤러리형)을 키(Key) 값으로 하여, 해당 게시판에 필요한 파싱 설정 정보(boardTypeConfig)를 매핑해 둔 맵(Map)입니다.
// 프로그램이 구동되는 시점(init 함수)에 최초 한 번만 데이터가 초기화되며, 이후에는 내용이 변경되지 않습니다.
// 이러한 읽기 전용 속성 덕분에, 데이터 경합(Data Race)이나 충돌을 걱정할 필요 없이 여러 크롤링 작업에서 동시에 안전하게 참조할 수 있습니다.
var boardTypes map[string]*boardTypeConfig

func init() {
	provider.MustRegister(config.ProviderSiteSsangbongElementarySchool, &provider.CrawlerConfig{
		NewCrawler: newCrawler,
	})

	// 게시판 유형별 설정정보를 초기화한다.
	boardTypes = map[string]*boardTypeConfig{
		boardTypeList1: {
			listURLTemplate:      fmt.Sprintf("/ys-ssangbong_es/na/ntt/selectNttList.do?mi=%s&bbsId=%s", boardIDPlaceholder, boardIDPlaceholder),
			detailURLTemplate:    fmt.Sprintf("/ys-ssangbong_es/na/ntt/selectNttInfo.do?mi=%s&bbsId=%s", boardIDPlaceholder, boardIDPlaceholder),
			articleSelector:      "div.subContent > div.bbs_ListA > table > tbody > tr",
			articleGroupSelector: "div.subContent > div.bbs_ListA",
		},
		boardTypePhoto1: {
			listURLTemplate:      fmt.Sprintf("/ys-ssangbong_es/na/ntt/selectNttList.do?mi=%s&bbsId=%s", boardIDPlaceholder, boardIDPlaceholder),
			detailURLTemplate:    fmt.Sprintf("/ys-ssangbong_es/na/ntt/selectNttInfo.do?mi=%s&bbsId=%s", boardIDPlaceholder, boardIDPlaceholder),
			articleSelector:      "div.subContent > div.photo_list > ul > li",
			articleGroupSelector: "div.subContent > div.photo_list",
		},
	}
}

func newCrawler(params provider.NewCrawlerParams) (provider.Crawler, error) {
	c := &crawler{
		Base: provider.NewBase(params, 3),
	}

	c.SetCrawlArticles(c.crawlArticles)

	c.Logger().WithFields(applog.Fields{
		"component":   component,
		"board_count": len(c.Config().Boards),
	}).Debug(c.Messagef("크롤러 생성 완료: Provider 초기화 수행"))

	return c, nil
}

type crawler struct {
	*provider.Base
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ provider.Crawler = (*crawler)(nil)

// crawlArticles 설정에 등록된 쌍봉초등학교의 모든 게시판을 순회하여 신규 게시글의 목록과 본문을 수집합니다.
//
// 실행 흐름 (2단계):
//  1. 목록 수집: 설정에 등록된 각 게시판을 순서대로 순회하며 신규 게시글 목록을 수집합니다.
//     - 개별 게시판에서 오류가 발생해도 전체를 멈추지 않고 다음 게시판으로 계속 진행합니다.
//     - 이렇게 하면 문제가 있는 게시판 하나 때문에 정상 동작하는 나머지 게시판의 데이터까지 잃는 것을 방지합니다.
//  2. 본문 수집: 1단계에서 수집한 게시글들의 상세 본문을 최대 2개씩 병렬로 가져옵니다.
//     - 동시성을 2로 제한하여 대상 웹서버에 과도한 부하가 가해지는 것을 방어합니다.
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
//   - map[string]string: 게시판별 최신 커서 맵 (key: boardID, value: 최신 articleID). 신규 게시글이 없는 게시판은 포함되지 않습니다.
//   - string: 항상 빈 문자열("") 반환. 개별 게시판 오류는 내부에서 직접 알림 처리됩니다.
//   - error: 항상 nil 반환. 게시판 단위 오류는 내부에서 격리 처리하므로 이 함수 자체는 실패하지 않습니다.
func (c *crawler) crawlArticles(ctx context.Context) ([]*feed.Article, map[string]string, string, error) {
	// 수집기가 모든 게시판을 돌아다니며 새로 긁어온 게시글 전체를 한곳에 담아둘 슬라이스입니다.
	var articles = make([]*feed.Article, 0)

	// 각 게시판마다 "어디까지 읽었는지" 가장 나중에(가장 최신 게시물) 수집한 게시글 ID(커서)를 기억해 두는 맵입니다.
	var newCursors = make(map[string]string)

	for _, b := range c.Config().Boards {
		boardArticles, cursor, message, err := c.crawlSingleBoard(ctx, b)
		if err != nil {
			c.SendErrorNotification(message, err)

			// 특정 게시판에서 오류가 발생하더라도 전체 크롤링 로직이 멈추지 않도록 무시하고 다음 게시판으로 넘어갑니다.
			// 이렇게 하면 에러가 없는 다른 정상적인 게시판의 소중한 데이터들을 안전하게 보존할 수 있습니다.
			continue
		}

		articles = append(articles, boardArticles...)
		if cursor != "" {
			newCursors[b.ID] = cursor
		}
	}

	// 수집된 게시글들의 상세 본문 내용을 읽어오는 작업입니다.
	// 대상 웹사이트의 서버 부하를 막기 위해, 한 번에 최대 2개씩만 동시에 병렬로 작업(Worker Pool)하도록 제한했습니다.
	// 목록을 가져오는 것과 달리, 본문 수집은 실패하더라도 전체 작업에 영향을 주지 않고 부드럽게 무시합니다.
	if err := c.CrawlArticleContentsConcurrently(ctx, articles, 2, c.crawlArticleContent); err != nil {
		// 만약 타임아웃이나 시스템 종료 신호 때문에 본문을 단 1개도 채우지 못하고 강제로 작업이 중단되더라도,
		// 다음과 같은 이유로 이미 성공적으로 읽어 둔 '목록 데이터(제목, 링크)'와 '최신 커서 위치'는 롤백하지 않고 그대로 보존합니다.
		// 1. 방어적 설계: 에러 발생 시 정보를 버려버리면, 다음 수집 시 똑같은 게시물에서 또 타임아웃이 발생하여 크롤러가 영원히 정지하는 무한 루프(Poison Pill) 장애가 발생할 수 있습니다.
		// 2. 서비스 지속성: 다행히 RSS 서비스의 핵심은 '새 글 알림'입니다. 비록 본문은 누락되더라도 새 글의 제목과 원본 링크를 성공적으로 전달했다면 최소한의 목적은 달성된 것입니다.

		c.SendErrorNotification(c.Messagef("게시글 본문 파싱 프로세스 중 응답 타임아웃 또는 시스템 종료 시그널(Interrupt)이 감지되어 해당 크롤링 세션이 중단되었습니다."), err)
	}

	return articles, newCursors, "", nil
}

// crawlSingleBoard 설정에 등록된 게시판 하나를 크롤링하여 신규 게시글 목록과 최신 커서를 수집합니다.
// crawlArticles가 게시판별로 반복 호출하는 단위 작업 함수입니다.
//
// 동작 흐름:
//  1. boardTypes 맵에서 게시판 유형(b.Type)에 맞는 CSS 셀렉터 및 URL 설정을 조회합니다.
//  2. DB에서 마지막으로 수집했던 게시글 ID(lastCursor)와 등록일(lastCreatedDate)을 읽어옵니다.
//  3. 1페이지부터 MaxPageCount까지 순서대로 페이지를 탐색하며 신규 게시글을 수집합니다.
//     - 각 페이지는 fetchHTMLViaPostForm을 통해 POST 요청으로 가져옵니다.
//     - 고정 공지사항(td.mPre가 있는 행)은 중복 판별 로직을 오염시키므로 무시(스킵)합니다.
//     - 이미 수집한 게시글(lastCursor 이하)을 만나면 탐색을 즉시 중단합니다.
//     - 2페이지 이상에서 게시글이 0건이면 마지막 페이지로 간주하고 탐색을 종료합니다.
//  4. 수집된 게시글들을 시간 순서(오래된 글 → 최신 글)로 뒤집어 반환합니다.
//     이는 상위 레이어의 DB 삽입이 오래된 글부터 순서대로 처리되도록 보장합니다.
//
// 중요한 설계 결정:
//   - 페이지 접근 실패 시 전체 롤백(error 반환):
//     부분 수집 상태에서 커서를 갱신하면, 수집하지 못한 게시글이 영구 누락됩니다.
//     따라서 페이지 접근 에러 시에는 부분 결과 없이 전체를 롤백(error 반환)합니다.
//   - 중복 판별: 정수 대소 비교를 우선하고, 변환 실패 시 문자열 사전순 비교로 폴백합니다.
//     이는 게시글 삭제로 ID가 연속되지 않는 상황에서도 무한 루프가 발생하지 않도록 방어합니다.
//   - articles-first 정책: articles에 먼저 append한 뒤 newCursor를 갱신합니다.
//     순서가 반전되면, 커서만 전진한 상태에서 런타임 패닉이 발생할 경우 게시글이 영구 누락됩니다.
//   - newCursor는 "" 로 초기화하여 신규 게시글이 실제로 수집될 때만 갱신합니다.
//     이전 커서값으로 초기화하면 신규 게시글이 없어도 불필요한 DB Upsert가 발생합니다.
//
// 반환값:
//   - []*feed.Article: 수집된 신규 게시글 목록 (오래된 글 → 최신 글 순서)
//   - string: 이번 수집에서 확인된 게시글 중 가장 큰 ID (newCursor). 신규 게시글이 없으면 빈 문자열("").
//   - string: 오류 메시지 접두사. 오류 발생 시 알림에 사용될 문맥 정보, 정상 시 빈 문자열("").
//   - error: 게시판 설정 오류, DB 조회 실패, 페이지 접근 실패, CSS 파싱 오류 시 non-nil. 정상 시 nil.
func (c *crawler) crawlSingleBoard(ctx context.Context, b *config.BoardConfig) ([]*feed.Article, string, string, error) {
	// [게시판 유형 설정 조회]
	// boardTypes 맵에서 b.Type(예: "L_1", "P_1")에 해당하는 CSS 셀렉터 및 URL 템플릿 설정을 조회합니다.
	// 설정이 존재하지 않는다면 init()에 등록되지 않은 게시판 유형이 설정 파일에 잘못 입력된 것이므로,
	// 즉시 크롤링을 중단하고 관리자에게 알림을 전송합니다.
	boardTypeCfg, exists := boardTypes[b.Type]
	if exists == false {
		return nil, "", c.Messagef("게시판 유형별 파싱 구성 정보를 매핑할 수 없어 크롤링 프로세스 진입이 거부되었습니다."), apperrors.Newf(apperrors.System, "시스템에 지원되지 않는 게시판 유형('%s')이 감지되었습니다", b.Type)
	}

	// [최근 수집 이력 조회]
	// DB에서 이 게시판의 마지막 수집 기준점(lastCursor: 게시글 ID, lastCreatedDate: 등록일)을 불러옵니다.
	// 이 값들은 이미 수집한 게시글을 건너뛰는 중복 판별 로직의 핵심 기준으로 사용됩니다.
	// DB 조회 자체에 실패하면 중복 판별이 불가능해 데이터 무결성을 보장할 수 없으므로, 전체를 롤백(error 반환)합니다.
	lastCursor, lastCreatedDate, err := c.FeedRepo().GetCrawlingCursor(ctx, c.ProviderID(), b.ID)
	if err != nil {
		return nil, "", c.Messagef("%s 대상 게시판의 최근 수집 이력(Cursor)을 데이터베이스에서 조회하는 과정에서 예외가 발생하였습니다.", b.Name), err
	}

	// [신규 커서 초기화]
	// newCursor는 빈 문자열("")로 초기화하여, 이번 수집에서 실제로 신규 게시글을 발견한 경우에만 값이 채워지도록 합니다.
	// 만약 이전 커서값으로 초기화하면 신규 게시글이 단 한 건도 없어도 매번 불필요한 DB Upsert가 발생하며,
	// 엣지 케이스에서 커서가 과거 값으로 역행하는 버그를 유발할 수 있습니다.
	var newCursor = ""

	// [수집 결과 슬라이스 초기화]
	// 이번 순환에서 신규로 확인된 게시글들을 담을 저장소입니다.
	// nil 대신 빈 슬라이스로 시작하여, 신규 게시글이 없어도 항상 non-nil 슬라이스를 반환하도록 보장합니다.
	var articles = make([]*feed.Article, 0)

	// ─────────────────────────────────────────────────────────
	// 페이지 순회: 1페이지부터 MaxPageCount까지 순서대로 탐색하며 신규 게시글을 수집합니다.
	// 이미 수집한 게시글을 만나거나(isAlreadyCrawled), 마지막 페이지에 도달하면(게시글 0건) 탐색을 중단합니다.
	// ─────────────────────────────────────────────────────────
	for page := 1; page <= c.MaxPageCount(); page++ {
		// @@@@@
		// [URL 조립] boardTypeCfg.listURLTemplate에 포함된 boardIDPlaceholder("#{board_id}")를
		// 실제 게시판 ID(b.ID)로 치환하여 해당 페이지의 POST 요청 대상 URL을 완성합니다.
		pageURL := strings.ReplaceAll(fmt.Sprintf("%s%s&currPage=%d", c.Config().URL, boardTypeCfg.listURLTemplate, page), boardIDPlaceholder, b.ID)

		doc, err := c.fetchHTMLViaPostForm(ctx, pageURL, c.Messagef("%s 게시판 접근이 실패하였습니다.", b.Name))
		if err != nil {
			// [전체 롤백 정책] 페이지 접근에 실패하면 지금까지 수집한 부분 결과를 버리고 error를 반환합니다.
			// 부분 수집 상태에서 커서를 전진시키면, 수집하지 못한 페이지의 게시글들이 영구 누락됩니다.
			// 또한 커서를 그대로 두면 실패한 페이지를 다음 스케줄에서 무한 반복 재시도하여
			// 대상 서버에 의도치 않은 DDoS 수준의 부하를 유발할 수 있습니다.
			return nil, "", c.Messagef("%s 게시판 접근이 실패하였습니다. (page: %d)", b.Name, page), err
		}

		articleRows := doc.Find(boardTypeCfg.articleSelector)
		if len(articleRows.Nodes) == 0 {
			// 현재 페이지에서 게시글 행이 하나도 감지되지 않은 경우입니다.
			if page > 1 {
				// 2페이지 이상에서 게시글이 없다면 마지막 페이지를 넘어선 것이므로 탐색을 정상 종료합니다.
				break
			}

			// 1페이지에서 게시글이 없는 경우, 두 가지 원인 중 하나입니다.
			// 부모 컨테이너(articleGroupSelector)의 존재 여부로 원인을 구분합니다.
			if doc.Find(boardTypeCfg.articleGroupSelector).Length() > 0 {
				// [정상] 부모 컨테이너는 있지만 자식 행(게시글)이 없다 → 실제로 아무 글도 없는 빈 게시판입니다.
				return articles, "", "", nil
			}

			// [오류] 부모 컨테이너조차 없다 → 웹사이트 레이아웃 변경으로 CSS 셀렉터가 깨진 상태입니다.
			// 이 경우 관리자가 articleSelector / articleGroupSelector 를 업데이트해야 합니다.
			return nil, "", c.Messagef("%s 게시판의 게시글 추출이 실패하였습니다. CSS셀렉터를 확인하세요.", b.Name), apperrors.New(apperrors.System, "게시글 추출이 실패하였습니다.")
		}

		// isAlreadyCrawled 현재 페이지에서 이미 수집한 게시글을 만났는지 여부를 표시하는 플래그입니다.
		// EachWithBreak 내부에서 true로 설정되면, 순환 종료 후 아래의 break로 페이지 탐색 전체를 중단합니다.
		var isAlreadyCrawled = false

		// 수집된 게시글 행(Row)을 위에서 아래로 순서대로 순회합니다.
		// 중간에 중단 조건이 필요하므로 EachWithBreak 를 사용하여 false를 반환하면 순환을 즐시 중단할 수 있습니다.
		articleRows.EachWithBreak(func(i int, s *goquery.Selection) bool {
			// [고정 공지사항 스킵] <td class="mPre"> 가 있는 행은 상단 고정 공지글로 간주하고 건너뜁니다.
			//
			// 고정 공지는 행 번호 대신 '공' 텍스트가 표시되며, 게시판 페이지가 바뀌어도 항상 최상단에 고정됩니다.
			// 이 행을 일반 게시글처럼 처리하면 커서 비교 로직에 오염이 발생합니다.
			// 예를 들어, 고정 공지의 ID가 lastCursor보다 작으면 탐색이 조기 중단되어
			// 실제 신규 게시글이 있음에도 수집되지 않는 치명적인 버그가 발생합니다.
			if s.Find("td.mPre").Length() > 0 {
				return true
			}

			article, err := c.extractArticle(b.ID, b.Type, boardTypeCfg.detailURLTemplate, s)
			if err != nil {
				applog.Warn(c.Messagef("%s 게시판에서 개별 게시글 추출이 실패하여 스킵합니다. (error:%s)", b.Name, err))
				return true
			}
			article.BoardID = b.ID
			article.BoardName = b.Name
			article.BoardType = b.Type

			// [중복 판별 — 반드시 커서 갱신보다 먼저 수행해야 합니다]
			// 이미 수집한 게시글(lastCursor 이하)을 만나면 탐색을 즉시 중단합니다.
			// 중복 판별을 커서 갱신 이후에 하면, 이미 수집된 게시글 ID가 newCursor에 잘못 반영될 수 있습니다.
			//
			// [비교 전략] 정수 대소 비교를 우선하고, 변환 실패 시 문자열 길이 기반 사전순 비교로 폴백합니다.
			// 단순 문자열 비교(article.ArticleID <= lastCursor)는 자릿수가 다른 ID에서 오판을 일으킵니다.
			// (예: "9" > "10" — 사전순으로는 더 크지만 숫자로는 더 작음)
			// 게시글이 삭제되어 ID가 연속되지 않아도 정수 비교는 정확하게 동작하므로 무한 루프를 방지합니다.
			articleIDInt, errArticle := strconv.ParseInt(article.ArticleID, 10, 64)
			lastCursorInt, errCursor := strconv.ParseInt(lastCursor, 10, 64)

			if errArticle == nil && errCursor == nil && lastCursor != "" {
				// [정수 비교] 두 값 모두 숫자로 변환 성공 → 정확한 대소 비교
				if articleIDInt <= lastCursorInt {
					isAlreadyCrawled = true
					return false
				}
			} else {
				// [문자열 폴백] 숫자 변환 실패 시 → '길이 우선, 같은 길이면 사전순' 방식으로 비교합니다.
				// 자릿수가 같은 숫자 문자열은 사전순 정렬이 곧 숫자 정렬과 동일하므로 올바르게 동작합니다.
				if lastCursor != "" {
					if len(article.ArticleID) < len(lastCursor) || (len(article.ArticleID) == len(lastCursor) && article.ArticleID <= lastCursor) {
						isAlreadyCrawled = true
						return false
					}
				}
			}
			// [날짜 기반 조기 탈출] ID 비교만으로는 탐색 범위를 줄이기 어려운 상황을 보완합니다.
			// 게시글의 등록일이 lastCreatedDate보다 명백히 과거(날짜 단위)라면 이미 수집한 게시글임이 확실하므로 탐색을 중단합니다.
			//
			// 주의: ParseCreatedAt는 당일 게시글은 정확한 시각을 파싱하지만, 과거 날짜는 시각을 00:00:00으로 고정합니다.
			// 시각 정보 오차로 인한 경계값 오판을 방지하기 위해 "yyyy-MM-dd" 날짜 문자열로 변환 후 순수 날짜 단위로만 비교합니다.
			// 같은 날짜인 경우는 이 조건이 적용되지 않으며, 위의 ID 비교에서만 처리됩니다.
			if !lastCreatedDate.IsZero() && article.CreatedAt.Format("2006-01-02") < lastCreatedDate.Format("2006-01-02") {
				isAlreadyCrawled = true
				return false
			}

			// [articles-first 정책] 반드시 articles에 먼저 추가한 뒤 newCursor를 갱신해야 합니다.
			// 만약 커서를 먼저 갱신하고 append하는 도중 런타임 패닉이 발생하면,
			// 커서는 전진했지만 게시글은 저장되지 않아 해당 게시글이 영구적으로 유실됩니다.
			articles = append(articles, article)

			// 수집된 신규 게시글 중 가장 큰(최신) ID를 newCursor에 기록합니다.
			// 이 값은 다음 크롤링 사이클에서 "여기까지는 이미 읽었다"는 기준점으로 DB에 저장됩니다.
			// 중복 판별 로직과 동일하게, 정수 비교를 우선하고 변환 실패 시 문자열 비교로 폴백합니다.
			if newCursor == "" {
				// 첫 번째 신규 게시글이므로 무조건 커서로 설정합니다.
				newCursor = article.ArticleID
			} else {
				articleIDInt, errArticle := strconv.ParseInt(article.ArticleID, 10, 64)
				newCursorInt, errCursor := strconv.ParseInt(newCursor, 10, 64)
				if errArticle == nil && errCursor == nil {
					// [정수 비교] 현재 게시글 ID가 기존 newCursor보다 크면 갱신합니다.
					if articleIDInt > newCursorInt {
						newCursor = article.ArticleID
					}
				} else {
					// [문자열 폴백] 길이 우선, 같은 길이면 사전순으로 더 큰 ID를 newCursor로 갱신합니다.
					if len(article.ArticleID) > len(newCursor) || (len(article.ArticleID) == len(newCursor) && article.ArticleID > newCursor) {
						newCursor = article.ArticleID
					}
				}
			}

			return true
		})
		// 개별 게시글 파싱 오류는 경고 로그만 남기고 해당 행을 스킵합니다.
		// 과거에는 파싱 오류 발생 시 즉시 크롤링을 중단하는 방식을 사용했으나,
		// 일부 게시글의 오류 때문에 이상 없는 다른 게시글까지 수집이 안 되는 문제가 있어 현재 방식으로 변경되었습니다.

		// 현재 페이지에서 이미 수집한 게시글을 만난 경우, 나머지 페이지도 수집된 것에 해당하므로 탐색을 즉시 중단합니다.
		if isAlreadyCrawled == true {
			break
		}
	}

	// [역순 정렬]
	// 웹사이트는 최신 글이 맨 위에 오는 구조이므로, 현재 articles는 최신 → 오래된 순으로 담겨 있습니다.
	// DB 삽입 시 오래된 글부터 순서대로 처리되도록 뒤집어서 반환합니다.
	for i, j := 0, len(articles)-1; i < j; i, j = i+1, j-1 {
		articles[i], articles[j] = articles[j], articles[i]
	}

	return articles, newCursor, "", nil
}

// fetchHTMLViaPostForm 쌍봉초등학교 웹사이트의 특수한 요청 방식에 맞춰 HTML 문서를 가져오는 헬퍼 함수입니다.
//
// 배경:
// 일반적인 웹사이트는 게시판 목록을 "GET /list?page=1&boardId=123" 형태로 요청합니다.
// 그러나 쌍봉초등학교 사이트는 보안 정책상 GET 요청을 허용하지 않으며,
// 동일한 파라미터를 HTTP POST 요청의 'Body'에 담아 전송하는 방식을 사용합니다.
//
// 동작 원리:
//  1. '?' 기준으로 URL 분리:
//     - 앞부분 → POST 요청이 전송될 실제 목적지 주소 (endpointURL)
//     - 뒷부분 → 요청 Body에 실어 보낼 폼 데이터 (queryString)
//  2. Content-Type을 "application/x-www-form-urlencoded"로 설정하여 서버가 데이터를 올바른 포맷으로 인식하게 합니다.
//  3. 분리된 queryString을 요청 Body에 담아 endpointURL로 POST 요청을 전송합니다.
//
// 매개변수:
//   - ctx: 요청 타임아웃이나 시스템 종료 시그널에 의해 작업을 취소할 수 있는 컨텍스트
//   - reqURL: 분리 및 변환할 원본 URL (예: "https://.../selectNttList.do?mi=123&bbsId=456")
//   - errMsgPrefix: 에러 발생 시 반환할 에러 메시지에 앞에 붙일 문맥 정보
//
// 반환값:
//   - *goquery.Document: 응답받은 HTML 을 파싱한 문서 객체
//   - error: URL 형식이 잘못되었거나 HTTP 요청이 실패한 경우 에러를 반환합니다.
func (c *crawler) fetchHTMLViaPostForm(ctx context.Context, reqURL, errMsgPrefix string) (*goquery.Document, error) {
	// [1단계: URL 분리] '?' 기준으로 엔드포인트와 쿼리스트링을 분리합니다.
	endpointURL, queryString, ok := strings.Cut(reqURL, "?")
	if !ok {
		return nil, apperrors.Newf(apperrors.System, "%s (오류 원인: 대상 URL에서 필수 쿼리스트링을 추출할 수 없어 POST 요청 구성에 실패하였습니다)", errMsgPrefix)
	}

	// [2단계: 요청 헤더 구성]
	header := make(http.Header)
	header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	header.Set("Content-Type", "application/x-www-form-urlencoded")

	// [3단계: 요청 바디 구성] 분리한 queryString을 그대로 POST Body로 변환합니다.
	// 일반적으로 GET 요청의 쿼리 파라미터로 쓰이는 문자열(예: "mi=123&bbsId=456")을 그대로 Body에 담는 것이 이 사이트의 요청 방식입니다.
	reqBody := bytes.NewBufferString(queryString)

	// [4단계: POST 요청 전송 및 HTML 파싱]
	// 구성한 endpointURL, header, reqBody로 HTTP POST 요청을 전송하고, 응답 HTML을 파싱합니다.
	doc, err := c.Scraper().FetchHTML(ctx, "POST", endpointURL, reqBody, header)
	if err != nil {
		return nil, apperrors.Wrapf(err, apperrors.ExecutionFailed, "%s (오류 원인: 대상 웹서버 데이터 수집 및 HTML 파싱 과정에서 예외가 발생하였습니다)", errMsgPrefix)
	}

	return doc, nil
}
