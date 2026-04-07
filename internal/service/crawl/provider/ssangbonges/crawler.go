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
// 게시판마다 목록용 URL 패턴이나 화면 구조(HTML 셀렉터)가 다르기 때문에 이러한 설정값들이 꼭 필요합니다.
type boardTypeConfig struct {
	// listURLTemplate 게시판 목록 페이지를 가져올 때 사용하는 URL 경로의 기본 형태입니다.
	// #{board_id} 플레이스홀더를 포함하며, 실제 크롤링 시점에 게시판 ID와 페이지 번호로 치환되어 완성된 URL이 만들어집니다.
	listURLTemplate string

	// detailURLTemplate 게시글 하나를 클릭해서 들어갔을 때, 그 상세 내용을 부르기 위한 URL 주소의 기본 형태입니다.
	detailURLTemplate string

	// articleSelector 게시판 목록 화면에서 각각의 '게시글 한 줄(또는 사진 한 장)'을 특정해 내기 위해 사용하는 CSS 셀렉터입니다.
	articleSelector string

	// articleGroupSelector 게시글 목록 전체를 크게 감싸고 있는 부모 컨테이너를 집어내기 위한 CSS 셀렉터입니다.
	// articleSelector로 글을 하나도 찾지 못했을 때, 이 컨테이너가 화면에 정상적으로 존재하는지 먼저 확인합니다.
	// 컨테이너조차 찾지 못하면 "웹사이트 구조가 바뀐 에러"로, 컨테이너는 있는데 글만 없으면 "빈 게시판"으로 구분합니다.
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
	// ========================================
	// 1단계: 게시판 유형 설정 조회
	// ========================================
	// boardTypes 맵에서 b.Type(예: "L_1", "P_1")에 해당하는 CSS 셀렉터 및 URL 템플릿 설정을 조회합니다.
	// 설정이 존재하지 않는다면 init()에 등록되지 않은 게시판 유형이 설정 파일에 잘못 입력된 것이므로,
	// 즉시 크롤링을 중단하고 관리자에게 알림을 전송합니다.
	boardTypeCfg, exists := boardTypes[b.Type]
	if exists == false {
		return nil, "", c.Messagef("게시판 유형별 파싱 구성 정보를 매핑할 수 없어 크롤링 프로세스 진입이 거부되었습니다."), apperrors.Newf(apperrors.System, "시스템에 지원되지 않는 게시판 유형('%s')이 감지되었습니다", b.Type)
	}

	// ========================================
	// 2단계: 최근 수집 이력 조회
	// ========================================
	// DB에서 이 게시판의 마지막 수집 기준점(lastCursor: 게시글 ID, lastCreatedDate: 등록일)을 불러옵니다.
	// 이 값들은 이미 수집한 게시글을 건너뛰는 중복 판별 로직의 핵심 기준으로 사용됩니다.
	// DB 조회 자체에 실패하면 중복 판별이 불가능해 데이터 무결성을 보장할 수 없으므로, 전체를 롤백(error 반환)합니다.
	lastCursor, lastCreatedDate, err := c.FeedRepo().GetCrawlingCursor(ctx, c.ProviderID(), b.ID)
	if err != nil {
		return nil, "", c.Messagef("%s 대상 게시판의 최근 수집 이력(Cursor)을 데이터베이스에서 조회하는 과정에서 예외가 발생하였습니다.", b.Name), err
	}

	// ========================================
	// 3단계: 변수 초기화
	// ========================================

	// 이번 순환에서 신규로 확인된 게시글들을 담을 저장소입니다.
	// nil 대신 빈 슬라이스로 시작하여, 신규 게시글이 없어도 항상 non-nil 슬라이스를 반환하도록 보장합니다.
	var articles = make([]*feed.Article, 0)

	// newCursor는 빈 문자열("")로 초기화하여, 이번 수집에서 실제로 신규 게시글을 발견한 경우에만 값이 채워지도록 합니다.
	// 만약 이전 커서값으로 초기화하면 신규 게시글이 단 한 건도 없어도 매번 불필요한 DB Upsert가 발생하며,
	// 엣지 케이스에서 커서가 과거 값으로 역행하는 버그를 유발할 수 있습니다.
	var newCursor = ""

	// ========================================
	// 4단계: 페이지 순회
	// ========================================
	// 1페이지부터 MaxPageCount까지 순서대로 탐색하며 신규 게시글을 수집합니다.
	// 이미 수집한 게시글을 만나거나(reachedLastCursor), 마지막 페이지에 도달하면(게시글 0건) 탐색을 중단합니다.
	for page := 1; page <= c.MaxPageCount(); page++ {
		// ----------------------------------------
		// 4-1단계: URL 조립 & HTML 요청
		// ----------------------------------------
		// 미리 틀을 잡아둔 주소 템플릿(listURLTemplate)에서 "#{board_id}" 부분을 찾아,
		// 실제 대상 게시판의 ID 값(b.ID)으로 교체하여 접속할 최종 웹사이트 주소를 만듭니다.
		pageURL := strings.ReplaceAll(fmt.Sprintf("%s%s&currPage=%d", c.Config().URL, boardTypeCfg.listURLTemplate, page), boardIDPlaceholder, b.ID)

		doc, err := c.fetchHTMLViaPostForm(ctx, pageURL, c.Messagef("'%s' 게시판의 %d번 페이지 목록을 불러오지 못했습니다.", b.Name, page))
		if err != nil {
			// [전체 롤백 정책] 에러 발생 시, 이전 페이지들에서 성공적으로 모아둔 데이터도 미련 없이 버리고 즉시 중단합니다.
			// 1. 에러를 무시하고 커서를 전진시키면: 해당 페이지의 게시물들이 영구적으로 수집 누락됩니다.
			// 2. 데이터 누락을 막기 위해 예전 커서는 유지하되 "앞서 성공한 결과만이라도 DB에 저장"하는 타협안을 택하면:
			//    다음 스케줄에 또다시 1페이지부터 긁어오면서, 이미 저장된 글인데도 신규 글인 줄 알고
			//    불필요한 파싱과 DB 중복 검사를 시도하게 됩니다. 이를 매 스케줄마다 무한 반복하면 엄청난 부하를 줍니다.
			// 따라서 데이터 꼬임 방지와 서버 보호를 위해 결과물 전체를 깨끗이 엎어버리는 설계를 택한 것입니다.
			return nil, "", c.Messagef("'%s' 게시판의 %d번 페이지 목록을 불러오지 못했습니다.", b.Name, page), err
		}

		// ----------------------------------------
		// 4-2단계: 게시글 행 추출 및 유효성 검증
		// ----------------------------------------
		articleRows := doc.Find(boardTypeCfg.articleSelector)

		// 게시글이 하나도 없을 때: 마지막 페이지 도달 / 빈 게시판 / CSS 셀렉터 오류 중 하나를 판별합니다.
		if len(articleRows.Nodes) == 0 {
			// [케이스 A] 2페이지부터: 글이 없다면 게시판의 마지막 페이지를 넘어선 것이므로 수집을 마칩니다.
			// (1페이지부터 N페이지까지 탐색 중 처음으로 빈 페이지를 만난 것 = 모든 글을 다 읽은 것)
			if page > 1 {
				break
			}

			// [케이스 B] 1페이지부터: 처음부터 글이 없다면 두 가지 상황 중 하나입니다.
			// articleSelector로 개별 게시글 행은 못 찾았지만, 게시글 목록 전체를 감싸는
			// 부모 컨테이너(articleGroupSelector)가 존재하는지 확인하여 원인을 구분합니다.
			if doc.Find(boardTypeCfg.articleGroupSelector).Length() > 0 {
				// [정상] 부모 컨테이너는 렌더링되어 있지만 내부가 비어 있음
				// → 아직 아무도 글을 올리지 않은, 실제로 빈 게시판입니다. 정상 종료합니다.
				return articles, "", "", nil
			}

			// [오류] 부모 컨테이너 자체가 화면에 없음
			// → 웹사이트의 HTML 구조가 변경되어 기존 CSS 셀렉터가 더 이상 유효하지 않은 상태입니다.
			// 관리자가 해당 게시판에 직접 접속해 HTML을 확인하고, articleSelector / articleGroupSelector를 최신 구조로 수정해야 합니다.
			msg := c.Messagef("'%s' 게시판의 DOM 구조가 변경되어 게시글 그룹 컨테이너 요소 식별에 실패하였습니다. 데이터 추출 규칙(CSS Selector)의 무결성 점검 및 업데이트가 요구됩니다.", b.Name)
			errExtract := apperrors.New(apperrors.System, "원격 웹사이트 레이아웃 변경으로 인하여 파싱 컨테이너 노드 추출에 실패하였습니다")

			return nil, "", msg, errExtract
		}

		// ----------------------------------------
		// 4-3단계: 게시글 행 순회 (중복 판별 & 커서 갱신)
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
			// [상단 고정 공지글 예외 처리]
			// `<td class="mPre">` 클래스를 포함하는 행은 모든 페이지의 최상단에 고정 노출되는 '공지사항'입니다.
			// 고정 공지글은 일반 게시글과 달리 순차적인 ID 흐름을 따르지 않으며, 페이지가 넘어가도 계속 반복 등장합니다.
			//
			// 만약 이 고정 공지글을 일반 신규 게시글과 동일하게 파싱하여 처리하려 하면, 다음과 같은 치명적인 문제가 발생합니다.
			// - 고정 공지글의 고유 번호(ID)가 이전에 수집했던 최신 커서(`lastCursor`)보다 작을 경우:
			//   크롤러가 "이미 수집 완료된 예전 글 구간에 도달했다"고 오판하여 조기 종료(Early Exit) 조건이 발동됩니다.
			//   결과적으로 그 아래에 있는 진짜 '신규 일반 게시글'들의 수집이 통째로 누락되는 장애로 이어집니다.
			//
			// 따라서 데이터 무결성과 커서 비교 로직의 안정성을 보장하기 위해, 이러한 고정 공지 행은 파싱하지 않고 즉시 스킵(Skip)합니다.
			if s.Find("td.mPre").Length() > 0 {
				return true
			}

			// [개별 게시글 추출 오류에 대한 부분 실패 처리]
			// 단일 게시글에서 HTML 돔 구조 이탈 등으로 파싱 에러가 발생하여도 전체 크롤링 작업을 즉시 중단(Abort)하지 않습니다.
			//
			// 만약 단일 오류 시 작업을 강제 중단하게 되면, 등록 형식을 지키지 않은 불량 게시물 단 1개 때문에
			// 연달아 등록된 멀쩡한 다른 신규 게시글들까지 모두 수집되지 못하고 파이프라인이 멈추는
			// 심각한 '포이즌 필(Poison Pill)' 병목 현상 및 알림 누락 장애가 발생할 수 있습니다.
			//
			// 따라서 전체 시스템의 견고함(Robustness)을 유지하기 위해, 정보를 제대로 추출하지 못한 예외적인 게시물은
			// 경고(Warning) 로그만 남긴 뒤 부드럽게 무시(Skip)하고, 다음 게시글에 대한 순회가 멈춤 없이 계속 진행되도록 설계되었습니다.
			article, err := c.extractArticle(b.ID, b.Type, boardTypeCfg.detailURLTemplate, s)
			if err != nil {
				c.Logger().WithFields(applog.Fields{
					"board_id":   b.ID,
					"board_name": b.Name,
					"board_type": b.Type,
					"error":      err.Error(),
				}).Warn(c.Messagef("개별 게시글 처리 스킵: 데이터 추출 실패"))

				return true
			}

			article.BoardID = b.ID
			article.BoardName = b.Name
			article.BoardType = b.Type

			// [중복 판별: 이미 수집한 게시글인지 확인합니다]
			//
			// ★ 이 로직은 반드시 아래의 'articles 추가 및 newCursor 갱신' 코드보다 먼저 실행되어야 합니다. ★
			// 만약 순서가 바뀌어 커서를 먼저 갱신한 뒤 중복 여부를 판별하면,
			// "이미 수집 완료된 게시글"의 ID가 신규 최고 커서(newCursor)로 잘못 등록될 수 있습니다.
			//
			// [비교 전략: 왜 2단계 비교를 사용하는가?]
			// 단순히 문자열로만 비교(article.ArticleID <= lastCursor)할 경우, 자릿수(자릿수)가 서로 다른 ID에서
			// 오판이 발생합니다. 예를 들어, 사전순으로는 "9" > "10"이지만 숫자로는 9 < 10입니다.
			// 이를 방지하기 위해 아래의 2단계 비교 전략을 사용합니다.
			//
			// [1단계 — 정수 변환 후 대소 비교 (우선 적용)]
			//   두 ID 모두 정수(int64)로 변환에 성공하면, 정확한 숫자 대소 비교를 수행합니다.
			//   게시글이 삭제되어 ID가 연속되지 않더라도 정수 비교는 항상 정확하므로 무한 루프를 방지합니다.
			//
			// [2단계 — 문자열 폴백 비교 (정수 변환 실패 시)]
			//   ID를 정수로 변환할 수 없는 경우(예: 비정형 문자열 ID), '길이 우선, 같은 길이면 사전순'으로 비교합니다.
			//   자릿수(길이)가 같은 숫자 문자열은 사전순 정렬 == 숫자 정렬이므로 이 방식이 올바르게 동작합니다.
			parsedArticleID, errParseArticleID := strconv.ParseInt(article.ArticleID, 10, 64)
			parsedLastCursor, errParseLastCursor := strconv.ParseInt(lastCursor, 10, 64)

			if errParseArticleID == nil && errParseLastCursor == nil && lastCursor != "" {
				// [1단계: 정수 비교]
				// 게시글 ID와 lastCursor 모두 정수로 변환하는 데 성공했습니다. (가장 신뢰할 수 있는 비교 방법)
				// 현재 게시글 ID가 lastCursor 이하라면, 이미 이전 크롤링 사이클에서 수집이 완료된 게시글입니다.
				// 즉시 순회를 멈추고 다음 페이지 탐색도 중단합니다.
				if parsedArticleID <= parsedLastCursor {
					reachedLastCursor = true
					return false
				}
			} else {
				// [2단계: 문자열 폴백 비교]
				// 게시글 ID 또는 lastCursor 중 하나 이상을 정수로 변환하지 못했습니다. (비정형 문자열 ID 등)
				// 순수 사전순(lexicographic) 비교는 자릿수가 다를 때 오판("9" > "10")이 발생하므로,
				// 먼저 문자열 길이(자릿수)를 비교하고, 길이가 같은 경우에만 사전순으로 비교합니다.
				// 자릿수가 같은 숫자 문자열은 사전순 정렬 == 숫자 정렬이 성립하므로 정확하게 동작합니다.
				if lastCursor != "" {
					if len(article.ArticleID) < len(lastCursor) || (len(article.ArticleID) == len(lastCursor) && article.ArticleID <= lastCursor) {
						reachedLastCursor = true
						return false
					}
				}
			}

			// [날짜 기반 조기 탈출: ID 비교를 보완하는 2차 안전망]
			// 위의 ID 비교만으로 "이미 수집한 구간"을 판별하기 어려운 상황(예: ID가 역행하거나 비정형일 때)을 대비하여,
			// 게시글의 등록일(날짜)을 추가 기준으로 사용합니다.
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
			if !lastCreatedDate.IsZero() && article.CreatedAt.Format("2006-01-02") < lastCreatedDate.Format("2006-01-02") {
				reachedLastCursor = true
				return false
			}

			// [articles-first 정책: 게시글 저장이 항상 커서 갱신보다 먼저 이루어져야 합니다]
			// 만약 newCursor를 먼저 갱신한 뒤 articles에 append하는 도중 런타임 패닉이 발생하면,
			// 커서는 이미 앞으로 전진했지만 게시글은 슬라이스에 추가되지 않아 해당 게시글이 영구적으로 유실됩니다.
			// 이를 방지하기 위해 반드시 articles에 먼저 추가한 뒤 newCursor를 갱신합니다.
			articles = append(articles, article)

			// [newCursor 갱신: 이번 수집의 최신 기준점을 추적합니다]
			// 수집된 신규 게시글들 중 ID가 가장 큰(가장 최신인) 게시글의 ID를 newCursor에 기록합니다.
			// 크롤링이 완료된 후 이 값은 DB에 저장되어, 다음 사이클에서 "여기까지는 이미 읽었다"는 기준점으로 사용됩니다.
			//
			// 중복 판별 로직과 동일하게 2단계 비교 전략을 사용합니다.
			if newCursor == "" {
				// [초기값 설정] 이번 순회에서 처음으로 신규 게시글을 발견했으므로 해당 ID를 그대로 커서로 설정합니다.
				newCursor = article.ArticleID
			} else {
				// [최대값 갱신] 이미 커서가 설정되어 있는 경우, 현재 게시글 ID가 더 크면 커서를 갱신합니다.
				parsedArticleID, errParseArticleID := strconv.ParseInt(article.ArticleID, 10, 64)
				parsedNewCursor, errParseNewCursor := strconv.ParseInt(newCursor, 10, 64)

				if errParseArticleID == nil && errParseNewCursor == nil {
					// [1단계: 정수 비교] 두 값 모두 정수 변환 성공 → 현재 게시글 ID가 더 크면 newCursor를 갱신합니다.
					if parsedArticleID > parsedNewCursor {
						newCursor = article.ArticleID
					}
				} else {
					// [2단계: 문자열 폴백] 정수 변환 실패 시 → '길이 우선, 같은 길이면 사전순'으로 더 큰 ID를 newCursor로 갱신합니다.
					if len(article.ArticleID) > len(newCursor) || (len(article.ArticleID) == len(newCursor) && article.ArticleID > newCursor) {
						newCursor = article.ArticleID
					}
				}
			}

			return true
		})

		// ----------------------------------------
		// 4-4단계: 중단 조건 (루프 탈출) 검증
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
	// 5단계: 역순 정렬
	// ========================================
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
		errType := apperrors.UnderlyingType(err)
		if errType == apperrors.Unknown {
			errType = apperrors.ExecutionFailed
		}

		return nil, apperrors.Wrapf(err, errType, "%s (오류 원인: 대상 웹서버 데이터 수집 및 HTML 파싱 과정에서 예외가 발생하였습니다)", errMsgPrefix)
	}

	return doc, nil
}
