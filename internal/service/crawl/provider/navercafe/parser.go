package navercafe

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/strutil"
	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"
)

// articleAPIResponse 네이버 카페 게시글 API의 JSON 응답을 Go 구조체로 변환하기 위한 타입입니다.
//
// 이 구조체는 crawlContentViaAPI 함수 내부에서만 사용되는 전용 타입입니다.
// 재사용성보다 "필요한 필드만 선언"을 우선하여, 실제 API 응답 전체가 아닌 크롤링에 필요한
// 두 가지 필드(writeDate, contentHtml)만 선택적으로 매핑합니다.
// Go의 encoding/json은 선언되지 않은 필드를 자동으로 무시하므로, 이 방식이 안전합니다.
//
// 대응하는 API 엔드포인트:
//
//	GET https://article.cafe.naver.com/gw/v4/cafes/{clubID}/articles/{articleID}
//
// API가 반환하는 JSON 응답의 실제 구조 (크롤링에 사용되는 필드만 발췌):
//
//	{
//	  "result": {
//	    "article": {
//	      "writeDate": 1712345678901,     // 작성 시각 (Unix 밀리초)
//	      "contentHtml": "<p>본문 내용</p>" // 완성된 본문 HTML
//	    }
//	  }
//	}
type articleAPIResponse struct {
	Result struct {
		Article struct {
			// WriteDate 게시글 작성 시각(Unix 밀리초).
			// 목록 파싱 단계에서 과거 날짜 게시글의 시각 정보(HH:MM)를 추출할 수 없는 경우,
			// 이 값을 이용하여 CreatedAt의 시각 부분을 정확하게 보정합니다.
			WriteDate int64 `json:"writeDate"`

			// ContentHtml 게시글 본문 HTML(SmartEditor One 기반).
			// 이미지·링크·스티커 등 모든 미디어 요소가 포함된 완성된 형태로 반환됩니다.
			// goquery로 파싱하여 텍스트와 이미지를 추출하는 데 사용됩니다.
			ContentHtml string `json:"contentHtml"`
		} `json:"article"`
	} `json:"result"`
}

// extractArticle 게시글 목록의 단일 TR 행(<tr>)을 파싱하여 feed.Article 구조체로 변환합니다.
//
// 네이버 카페 목록 페이지 HTML의 <tr> 행 하나에서 작성일·게시판 ID·게시판 이름·
// 게시글 ID·제목·상세페이지 링크·작성자 닉네임을 순서대로 추출합니다.
//
// [답글(Reply) 행 처리]
// 게시글에 답글이 달리면 원본 <tr> 바로 아래에 답글 토글용 숨겨진 <tr>이 삽입됩니다.
// 이 행은 실제 게시글이 아니므로 감지 즉시 (nil, nil)을 반환하여 호출자가 건너뛰도록 합니다.
//
// 매개변수:
//   - s: <tr> 행에 해당하는 goquery 선택 객체
//
// 반환값:
//   - *feed.Article: 성공적으로 파싱된 게시글 정보
//   - error: 필수 HTML 요소가 없거나 속성 추출에 실패한 경우 apperrors.ParsingFailed 타입의 non-nil 오류
func (c *crawler) extractArticle(s *goquery.Selection) (*feed.Article, error) {
	// -------------------------------------------------------------------------
	// [Step 0] 답글(Reply) 행 감지 및 조기 반환
	//
	// 네이버 카페 게시판에서 게시글에 답글이 달리면, 원본 게시글 행(<tr>) 바로 아래에
	// 답글 토글(접기/펼치기)을 위한 숨겨진 <tr> 행이 하나 자동으로 삽입됩니다.
	// 이 행은 <td> 셀이 정확히 1개뿐이며, 그 id 속성이 "reply_"로 시작하는 특징이 있습니다.
	//
	//   <tr>
	//     <td id="reply_12345" style="display:none">...</td>  ← 감지 대상
	//   </tr>
	//
	// goquery의 .Attr() 대신 Nodes[0].Attr 슬라이스를 직접 순회하는 이유:
	// goquery의 .Attr("id")는 id 속성이 아예 없을 때 ("", false)를 반환하지만,
	// 여기서는 id 값의 접두어 패턴("reply_")까지 검사해야 하므로 저수준 접근이 더 명확합니다.
	//
	// 이 행은 실제 게시글 데이터가 아니므로 (nil, nil)을 반환하여 호출자가 건너뛰도록 합니다.
	// -------------------------------------------------------------------------
	replyNode := s.Find("td")
	if replyNode.Length() == 1 {
		// <td>가 딱 1개인 행만 추가로 id 패턴을 검사합니다.
		// 일반 게시글 행은 복수의 <td>를 가지므로 이 분기에 진입하지 않습니다.
		for _, attr := range replyNode.Nodes[0].Attr {
			if attr.Key == "id" && strings.HasPrefix(attr.Val, "reply_") {
				return nil, nil // 답글 토글 행 — 실제 게시글이 아니므로 스킵
			}
		}
	}

	article := &feed.Article{}

	// -------------------------------------------------------------------------
	// [Step 1] 작성일 추출
	//
	// "td.td_date" 셀의 텍스트를 읽어 작성일(CreatedAt)을 파싱합니다.
	// 네이버 카페 목록 페이지는 게시글 등록 시점에 따라 날짜 표시 형식이 달라집니다.
	//
	//   <td class="td_date">14:30</td>        ← 오늘 등록된 글: 시각(HH:MM)만 표시
	//   <td class="td_date">2024.03.15.</td>  ← 과거 날짜 글: 연월일만 표시 (끝에 점 포함)
	//
	// provider.ParseCreatedAt은 두 형식을 모두 인식합니다.
	// 단, 과거 날짜 글은 시각 정보가 없으므로 CreatedAt의 시각 부분이 00:00:00으로 고정됩니다.
	// 이 경우 이후의 crawlContentViaAPI 단계에서 API 응답의 writeDate로 시각을 보정합니다.
	//
	// <td>가 정확히 1개여야 합니다. 0개이면 셀 자체가 없는 것이고, 2개 이상이면 레이아웃이 변경된 것이므로
	// 두 경우 모두 파싱 실패로 처리합니다.
	// -------------------------------------------------------------------------
	dateNode := s.Find("td.td_date")
	if dateNode.Length() != 1 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글 HTML 요소에서 작성일 마크업을 식별할 수 없어 데이터 파싱에 실패했습니다")
	}

	createdAt, err := provider.ParseCreatedAt(strings.TrimSpace(dateNode.Text()))
	if err != nil {
		return nil, err
	}
	article.CreatedAt = createdAt

	// -------------------------------------------------------------------------
	// [Step 2] 게시판 ID & 이름 추출
	//
	// "td.td_article > div.board-name a.link_name" 앵커의 href 속성에서 게시판 ID를, 텍스트 컨텐츠에서 게시판 이름을 추출합니다.
	//
	//   <td class="td_article">
	//     <div class="board-name">
	//       <a class="link_name" href="https://cafe.naver.com/ArticleList.nhn?search.clubid=123&search.menuid=456&...">
	//         자유게시판
	//       </a>
	//     </div>
	//   </td>
	//
	//   - 게시판 ID  : href의 쿼리 파라미터 "search.menuid" 값  (예: "456")
	//   - 게시판 이름 : 앵커의 텍스트 컨텐츠                     (예: "자유게시판")
	//
	// 앵커는 정확히 1개여야 합니다. 0개이면 마크업이 없는 것이고, 2개 이상이면
	// 레이아웃이 변경된 것이므로 두 경우 모두 파싱 실패로 처리합니다.
	// -------------------------------------------------------------------------
	boardNode := s.Find("td.td_article > div.board-name a.link_name")
	if boardNode.Length() != 1 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글 HTML 요소에서 게시판 정보 마크업을 식별할 수 없어 데이터 파싱에 실패했습니다")
	}

	// .Attr("href")는 속성 자체가 없으면 exists=false, 있어도 값이 비어있으면 boardURL=""가 됩니다.
	// 두 경우 모두 게시판 ID를 추출할 수 없으므로 파싱 실패로 처리합니다.
	boardURL, exists := boardNode.Attr("href")
	if !exists || boardURL == "" {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시판 정보 추출을 위한 링크(href) 속성이 누락되었거나 유효하지 않아 데이터 파싱에 실패했습니다")
	}

	// [게시판 URL 2단계 파싱]
	// Step 1: href 문자열을 URL 구조체로 분해합니다. 파싱 실패 시 이후 쿼리 파라미터 접근이 불가하므로 즉시 오류를 반환합니다.
	u, err := url.Parse(boardURL)
	if err != nil {
		return nil, apperrors.Wrap(err, apperrors.ParsingFailed, "게시판 URL 문자열의 형식이 유효하지 않아 데이터 파싱에 실패했습니다")
	}

	// Step 2: 쿼리 문자열을 키-값 맵으로 변환합니다.
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, apperrors.Wrap(err, apperrors.ParsingFailed, "게시판 URL의 쿼리 문자열 구문 해석에 실패했습니다")
	}

	// query.Get은 키가 없거나 값이 비어있으면 ""를 반환합니다.
	// 게시판 ID가 비어있으면 이후 중복 체크나 필터링을 수행할 수 없으므로 파싱 실패로 처리합니다.
	article.BoardID = strings.TrimSpace(query.Get("search.menuid"))
	if article.BoardID == "" {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시판 고유 식별자(ID) 파라미터가 누락되었거나 유효하지 않아 데이터 파싱에 실패했습니다")
	}

	article.BoardName = strings.TrimSpace(boardNode.Text())

	// -------------------------------------------------------------------------
	// [Step 3] 제목 & 게시글 ID & 상세페이지 링크 추출
	//
	// "td.td_article > div.board-list a.article" 앵커 하나에서 제목과 게시글 ID를 함께 추출하고,
	// 이를 조합하여 상세페이지 URL을 조립합니다.
	//
	//   <td class="td_article">
	//     <div class="board-list">
	//       <a class="article" href=".../ArticleRead.nhn?articleid=12345&clubid=...&...">
	//         게시글 제목
	//       </a>
	//     </div>
	//   </td>
	//
	//   - 제목      : 앵커의 텍스트 컨텐츠 (.Text())
	//   - 게시글 ID : 앵커의 href 쿼리 파라미터 "articleid" 값 (예: "12345")
	//
	// 앵커는 정확히 1개여야 합니다. 0개이면 마크업이 없는 것이고, 2개 이상이면
	// 레이아웃이 변경된 것이므로 두 경우 모두 파싱 실패로 처리합니다.
	//
	// [상세페이지 URL 조립]
	// href를 그대로 사용하지 않고 articleid와 clubID만으로 URL을 직접 조립합니다.
	// 이유: href는 상대 경로이거나, 세션 정보 등 불필요한 파라미터가 포함될 수 있기 때문입니다.
	//   최종 URL 예: "{c.Config().URL}/ArticleRead.nhn?articleid=12345&clubid={c.clubID}"
	//            → "https://cafe.naver.com/ludypang/ArticleRead.nhn?articleid=12345&clubid=12303558"
	// -------------------------------------------------------------------------
	titleNode := s.Find("td.td_article > div.board-list a.article")
	if titleNode.Length() != 1 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글 HTML 요소에서 제목 마크업을 식별할 수 없어 데이터 파싱에 실패했습니다")
	}

	article.Title = strings.TrimSpace(titleNode.Text())

	// .Attr("href")는 속성 자체가 없으면 exists=false, 있어도 값이 비어있으면 articleURL=""가 됩니다.
	// 두 경우 모두 게시글 ID를 추출할 수 없으므로 파싱 실패로 처리합니다.
	articleURL, exists := titleNode.Attr("href")
	if !exists || articleURL == "" {
		return nil, apperrors.New(apperrors.ParsingFailed, "상세 페이지 정보 추출을 위한 링크(href) 속성이 누락되었거나 유효하지 않아 데이터 파싱에 실패했습니다")
	}

	// [상세페이지 URL 2단계 파싱]
	// Step 1: href 문자열을 URL 구조체로 분해합니다.
	u, err = url.Parse(articleURL)
	if err != nil {
		return nil, apperrors.Wrap(err, apperrors.ParsingFailed, "상세 페이지 URL 문자열의 형식이 유효하지 않아 데이터 파싱에 실패했습니다")
	}

	// Step 2: 쿼리 문자열을 키-값 맵으로 변환합니다.
	query, err = url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, apperrors.Wrap(err, apperrors.ParsingFailed, "상세 페이지 URL의 쿼리 문자열 구문 해석에 실패했습니다")
	}

	// articleid를 strconv.ParseInt로 정수 변환하는 주된 이유는 유효성 검사입니다.
	// 숫자가 아닌 값(빈 문자열, 알파벳 등)이 들어온 경우 파싱 실패를 조기에 감지할 수 있습니다.
	// 부가적으로 fmt.Sprintf의 "%d" 포맷으로 URL 조립 시에도 그대로 사용합니다.
	articleID, err := strconv.ParseInt(query.Get("articleid"), 10, 64)
	if err != nil {
		return nil, apperrors.Wrap(err, apperrors.ParsingFailed, "게시글의 고유 식별자(articleid) 파라미터가 누락되었거나 유효하지 않아 데이터 파싱에 실패했습니다")
	}

	// ArticleID는 문자열 필드이므로 int64에서 다시 문자열로 변환하여 저장합니다.
	article.ArticleID = strconv.FormatInt(articleID, 10)

	// articleid(int64)와 clubID(string)를 조합하여 정규화된 상세페이지 URL을 조립합니다.
	article.Link = fmt.Sprintf("%s/ArticleRead.nhn?articleid=%d&clubid=%s", c.Config().URL, articleID, c.clubID)

	// -------------------------------------------------------------------------
	// [Step 4] 작성자 추출
	//
	// "td.td_name > div.pers_nick_area td.p-nick" 셀의 텍스트에서 작성자 닉네임을 추출합니다.
	//
	//   <td class="td_name">
	//     <div class="pers_nick_area">
	//       <table>
	//         <tr>
	//           <td class="p-nick"><a>닉네임</a></td>
	//         </tr>
	//       </table>
	//     </div>
	//   </td>
	//
	// <td.p-nick>은 정확히 1개여야 합니다. 0개이면 작성자 마크업이 없는 것이고,
	// 2개 이상이면 레이아웃이 변경된 것이므로 두 경우 모두 파싱 실패로 처리합니다.
	// -------------------------------------------------------------------------
	authorNode := s.Find("td.td_name > div.pers_nick_area td.p-nick")
	if authorNode.Length() != 1 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글 HTML 요소에서 작성자 마크업을 식별할 수 없어 데이터 파싱에 실패했습니다")
	}

	article.Author = strings.TrimSpace(authorNode.Text())

	return article, nil
}

// crawlArticleContent 주어진 게시글(article)의 본문(Content)을 parsers 목록에 정의된 파서를 순차 시도하여 채웁니다.
//
// [본문 수집 전략 — 단계별 Fallback]
// 네이버 카페는 로그인 여부·카페 공개 설정에 따라 콘텐츠 접근 방식이 달라집니다.
// 따라서 parsers 목록의 순서대로 파서를 시도하며, 앞 단계가 본문을 채우는 데 성공하면 이후 단계는 건너뜁니다.
// 현재 등록된 파서 및 순서는 함수 본문의 parsers 슬라이스 선언부를 참고하세요.
//
// [오류 보존 정책]
// 각 단계가 실패하더라도 즉시 반환하지 않고 마지막 오류(lastErr)를 보존합니다.
// 이후 단계가 본문을 성공적으로 채운 경우에만 lastErr를 nil로 초기화하여,
// 일시적 네트워크 오류로 인한 재시도 기회가 영구 손실되는 것을 방지합니다.
//
// [컨텍스트 취소 즉시 전파]
// ctx가 취소된 경우에는 이후 단계를 실행하지 않고 즉시 ctx.Err()를 반환합니다.
// 이를 전파하지 않으면 이후 단계가 에러 없이 반환될 때 lastErr가 nil이 되어
// ErrContentUnavailable이 반환되고 재시도 기회가 영구 손실될 수 있습니다.
//
// [최종 반환 규칙]
//   - Content가 채워져 있으면 nil 반환 (성공)
//   - Content가 비어있고 lastErr가 non-nil이면 lastErr 반환 (재시도 대상)
//   - Content가 비어있고 lastErr가 nil이면 ErrContentUnavailable 반환
//     (원래 비어있는 글이거나 비공개 등 영구적 파싱 불가 상태이므로 재시도 스킵)
//
// 매개변수:
//   - ctx: 요청 타임아웃이나 시스템 종료 시그널에 의해 작업을 취소할 수 있는 컨텍스트
//   - article: 본문을 채워 넣을 대상 게시글 포인터 (Content 필드가 직접 수정됩니다)
//
// 반환값:
//   - nil: 파서 중 하나가 성공적으로 본문을 채운 경우
//   - provider.ErrContentUnavailable: 어떠한 시스템 오류도 없었으나 본문이 없는 경우 (재시도 스킵)
//   - error: 일시적 네트워크 오류 등으로 인해 재시도가 필요한 경우
func (c *crawler) crawlArticleContent(ctx context.Context, article *feed.Article) error {
	var lastErr error

	// 파서(본문 수집 전략) 목록을 우선순위 순서대로 정의합니다.
	parsers := []func(context.Context, *feed.Article) error{
		c.crawlContentViaAPI,
		c.crawlContentViaPage,
		c.crawlContentViaSearch,
	}

	for _, parser := range parsers {
		// 이전 단계에서 이미 본문이 채워졌다면 이후 파서를 실행할 필요가 없으므로 루프를 종료합니다.
		if article.Content != "" {
			break
		}

		if err := parser(ctx, article); err != nil {
			// ctx가 취소된 경우에는 이후 파서를 실행해도 무의미하므로 즉시 전파합니다.
			// 이를 전파하지 않으면 이후 파서가 에러 없이 반환될 때 lastErr가 nil이 되어
			// ErrContentUnavailable이 반환되고 재시도 기회가 영구 손실될 수 있습니다.
			if ctx.Err() != nil {
				return ctx.Err()
			}

			lastErr = err
		} else if article.Content != "" {
			// 에러 없이 본문이 성공적으로 채워진 경우, 이전 단계에서 저장한 에러를 초기화하고 탈출합니다.
			// 이를 초기화하지 않으면 이전 단계의 일시적 에러가 최종 반환값에 영향을 줄 수 있습니다.
			lastErr = nil

			break
		}

		// 에러는 없었으나 본문이 빈 상태로 반환된 경우(비공개·접근 불가 등)에는 다음 파서로 넘어갑니다.
	}

	// 파서 중 하나라도 본문을 채우는 데 성공했다면 nil을 반환합니다.
	if article.Content != "" {
		return nil
	}

	// 마지막으로 발생한 에러가 non-nil이면 일시적인 네트워크 장애 등의 가능성이 있으므로
	// 상위 루프(CrawlArticleContentsConcurrently)가 해당 게시글을 재시도할 수 있도록 에러를 그대로 전파합니다.
	if lastErr != nil {
		return lastErr
	}

	// 모든 파서를 시도했으나 어떠한 시스템 에러도 없이 본문이 비어있다면,
	// 원래 내용이 없는 글이거나 비공개·접근 불가 등 영구적으로 수집이 불가한 상태입니다.
	// ErrContentUnavailable을 반환하여 상위 루프가 재시도 없이 조용히 건너뛰도록 합니다.
	return provider.ErrContentUnavailable
}

// crawlContentViaAPI 네이버 카페 공식 gw/v4 API를 직접 호출하여 게시글 본문(Content)을 article에 채웁니다.
//
// 인증 토큰 없이 API를 직접 호출합니다. 비공개·회원 전용 게시글에는 401이 반환됩니다.
//
// [오류 처리 정책 — API 요청 실패]
//   - apperrors.Forbidden 또는 apperrors.Unauthorized:
//     로그인이 없어 접근 자체가 거부된 게시글입니다. 재시도해도 결과는 동일하므로
//     ErrContentUnavailable을 반환하여 상위 루프가 조용히 건너뛰도록 합니다.
//   - 그 외 오류(네트워크 에러, 타임아웃 등):
//     일시적 장애일 수 있으므로 경고 로그(Warn)를 남긴 뒤 오류를 전파합니다.
//
// [본문 추출 — contentHtml 파싱]
// API 응답의 contentHtml에는 이미지·링크·스티커 등 모든 미디어 요소가 포함된 완성된 HTML이
// 직접 반환됩니다. 이를 goquery로 파싱하여 순수 텍스트는 .Text()로, 이미지는 <img> 태그를
// 순회하여 각각 추출합니다. 텍스트와 이미지를 분리하는 방식은 crawlContentViaPage 및 crawlContentViaSearch와
// 동일한 구조를 따릅니다.
//
// [작성일 보정 — writeDate]
// 목록 파싱 단계(extractArticle)에서 게시된 지 오래된 게시글은 날짜(YYYY.MM.DD)만 추출할 수 있고
// 시각(HH:MM)을 알 수 없어 CreatedAt의 시각 부분이 00:00:00으로 고정됩니다.
// API 응답의 writeDate(Unix 밀리초)를 이용하여 이 경우에만 정확한 작성 시각으로 보정합니다.
//
// 매개변수:
//   - ctx: 요청 타임아웃이나 시스템 종료 시그널에 의해 작업을 취소할 수 있는 컨텍스트
//   - article: 본문과 작성일을 채워 넣을 대상 게시글 포인터 (Content, CreatedAt 필드가 직접 수정됩니다)
//
// 반환값:
//   - nil: 성공적으로 본문을 채운 경우
//   - provider.ErrContentUnavailable: 로그인 없이 접근할 수 없는 게시글인 경우 (재시도 스킵)
//   - error: 네트워크 오류 등 일시적 장애가 발생한 경우 (경고 로그 후 오류 전파)
func (c *crawler) crawlContentViaAPI(ctx context.Context, article *feed.Article) error {
	// -------------------------------------------------------------------------
	// [Step 1] 네이버 카페 게시글 API 호출 및 JSON 응답 파싱
	//
	// gw/v4 API를 GET 방식으로 호출하여 게시글 본문(contentHtml)과 작성 시각(writeDate)을 수신합니다.
	// 비공개·회원 전용 게시글은 HTTP 401이 반환되며, 이 경우 재시도해도 결과가 동일하므로
	// ErrContentUnavailable을 즉시 반환합니다.
	//
	// 예시 엔드포인트:
	//   GET https://article.cafe.naver.com/gw/v4/cafes/{clubID}/articles/{articleID}
	// -------------------------------------------------------------------------
	apiURL := fmt.Sprintf("https://article.cafe.naver.com/gw/v4/cafes/%s/articles/%s", c.clubID, article.ArticleID)

	var apiResp articleAPIResponse
	if err := c.Scraper().FetchJSON(ctx, "GET", apiURL, nil, nil, &apiResp); err != nil {
		if apperrors.Is(err, apperrors.Forbidden) || apperrors.Is(err, apperrors.Unauthorized) {
			return provider.ErrContentUnavailable
		}

		c.Logger().WithFields(applog.Fields{
			"component":  component,
			"club_id":    c.clubID,
			"board_id":   article.BoardID,
			"board_name": article.BoardName,
			"article_id": article.ArticleID,
			"link":       article.Link,
			"target_url": apiURL,
			"error":      err.Error(),
		}).Warn(c.Messagef("API 요청 실패: 대상 게시판('%s')의 게시글(ID: %s) 데이터 수신 오류", article.BoardName, article.ArticleID))

		return err
	}

	// -------------------------------------------------------------------------
	// [Step 2] 본문(Content) 추출 — contentHtml 파싱
	//
	// API가 반환하는 contentHtml은 SmartEditor One으로 작성된 완성된 HTML 조각입니다.
	// goquery로 파싱하여 순수 텍스트와 이미지를 각각 분리 추출합니다.
	//
	// [텍스트 추출]
	// NormalizeMultiline(contentDoc.Text())로 전체 텍스트를 수집합니다.
	// HTML 태그를 제거한 뒤, 연속된 빈 줄·불필요한 공백 등을 정규화합니다.
	//
	// [이미지 추출]
	// contentHtml 내 <img> 태그를 순회하여 src가 있는 이미지만 수집합니다.
	// 각 이미지는 CRLF("\r\n")로 구분하여 본문 텍스트 하단에 순서대로 추가합니다.
	// src·alt·style 속성값은 XSS 방지를 위해 html.EscapeString으로 이스케이프합니다.
	// -------------------------------------------------------------------------
	contentDoc, err := goquery.NewDocumentFromReader(strings.NewReader(apiResp.Result.Article.ContentHtml))
	if err != nil {
		c.Logger().WithFields(applog.Fields{
			"component":  component,
			"club_id":    c.clubID,
			"board_id":   article.BoardID,
			"board_name": article.BoardName,
			"article_id": article.ArticleID,
			"link":       article.Link,
			"error":      err.Error(),
		}).Warn(c.Messagef("API 응답 파싱 실패: 대상 게시판('%s')의 게시글(ID: %s) HTML 본문 파싱 오류", article.BoardName, article.ArticleID))

		return err
	}

	article.Content = strings.TrimSpace(strutil.NormalizeMultiline(contentDoc.Text()))

	// contentHtml 내 모든 <img> 태그를 순회하여 이미지를 본문 하단에 순서대로 추가합니다.
	// src 속성이 없는 <img>(예: 장식용 빈 태그)는 건너뜁니다.
	contentDoc.Find("img").Each(func(i int, s *goquery.Selection) {
		src, _ := s.Attr("src")
		if src != "" {
			if article.Content != "" {
				article.Content += "\r\n"
			}

			alt, _ := s.Attr("alt")
			style, _ := s.Attr("style")
			article.Content += fmt.Sprintf(`<img src="%s" alt="%s" style="%s">`, html.EscapeString(src), html.EscapeString(alt), html.EscapeString(style))
		}
	})

	// -------------------------------------------------------------------------
	// [Step 3] 작성일(CreatedAt) 보정
	//
	// 목록 파싱 단계(extractArticle)에서 게시된 지 오래된 게시글은 날짜(YYYY.MM.DD)만 표시되고
	// 시각(HH:MM)이 없어 CreatedAt의 시각 부분이 「00:00:00」으로 고정됩니다.
	// 이 경우에만 API 응답의 writeDate(Unix 밀리초)를 이용하여 정확한 작성 시각으로 보정합니다.
	//
	// [WriteDate == 0 방어]
	// WriteDate가 0이면 time.Unix(0, 0) → 1970-01-01 00:00:00 (Unix Epoch)이 반환됩니다.
	// Go의 time.IsZero()는 time.Time{}(0001-01-01)에 대해서만 true를 반환하므로,
	// WriteDate == 0을 별도로 체크하지 않으면 Epoch 시각이 CreatedAt에 잘못 설정됩니다.
	// 이를 방지하기 위해 WriteDate가 명시적으로 양수(> 0)인 경우에만 보정을 수행합니다.
	// -------------------------------------------------------------------------
	if article.CreatedAt.Format("15:04:05") == "00:00:00" {
		if apiResp.Result.Article.WriteDate > 0 {
			article.CreatedAt = time.Unix(apiResp.Result.Article.WriteDate/1000, 0)
		}
	}

	return nil
}

// crawlContentViaPage 게시글 상세 페이지 HTML을 직접 파싱하여 본문(Content)을 article에 직접 채웁니다.
//
// [본문 추출]
// 상세 페이지의 "#tbody" 요소에서 전체 텍스트를 NormalizeMultiline으로 정규화하여 수집합니다.
// 이후 동일 영역 내 <img> 태그를 순회하여 이미지를 본문 하단에 추가합니다.
//
// [로그인 필요 페이지 감지]
// "#tbody" 요소가 존재하지 않으면 로그인 없이는 접근할 수 없는 페이지로 간주합니다.
// 이 경우 로그를 남기지 않고 provider.ErrContentUnavailable을 조용히 반환합니다.
//
// [오류 처리 정책]
//   - apperrors.Forbidden 또는 apperrors.Unauthorized: 접근이 거부된 경우입니다.
//     재시도해도 결과가 동일하므로 provider.ErrContentUnavailable을 반환합니다.
//   - 그 외 오류(네트워크 에러, 타임아웃 등): 경고 로그(Warn)를 남긴 뒤 오류를 전파합니다.
//
// 매개변수:
//   - ctx: 요청 타임아웃이나 시스템 종료 시그널에 의해 작업을 취소할 수 있는 컨텍스트
//   - article: 본문(Content)을 채워 넣을 대상 게시글 포인터
//
// 반환값:
//   - nil: 성공적으로 본문을 채운 경우 (본문이 비어있어도 nil을 반환할 수 있습니다)
//   - provider.ErrContentUnavailable: 접근이 거부되거나 로그인이 필요한 경우 (상위 루프가 조용히 건너뜁니다)
//   - error: 일시적 장애가 발생한 경우 (경고 로그 후 오류 전파)
func (c *crawler) crawlContentViaPage(ctx context.Context, article *feed.Article) error {
	// -------------------------------------------------------------------------
	// [Step 1] 상세 페이지 HTML 조회 및 예외 처리
	//
	// 게시글 상세 페이지 URL(article.Link)에 접속하여 전체 HTML 문서를 파싱합니다.
	// 비공개·회원 전용 게시글 접근 시에는 HTTP 401/403 에러가 반환되며, 이 경우 더 이상의 재시도 요청이 무의미하므로
	// 즉시 ErrContentUnavailable을 반환하여 수집을 스킵합니다.
	// 그 외 타임아웃, 문서 파싱 예외 등 일시적인 네트워크 장애는 Warn 로그를 기록하고 에러를 전파합니다.
	// -------------------------------------------------------------------------
	doc, err := c.Scraper().FetchHTMLDocument(ctx, article.Link, nil)
	if err != nil {
		if apperrors.Is(err, apperrors.Forbidden) || apperrors.Is(err, apperrors.Unauthorized) {
			return provider.ErrContentUnavailable
		}

		c.Logger().WithFields(applog.Fields{
			"component":  component,
			"club_id":    c.clubID,
			"board_id":   article.BoardID,
			"board_name": article.BoardName,
			"article_id": article.ArticleID,
			"link":       article.Link,
			"error":      err.Error(),
		}).Warn(c.Messagef("상세 페이지 요청 실패: 대상 게시판('%s')의 게시글(ID: %s) 데이터 수신 오류", article.BoardName, article.ArticleID))

		return err
	}

	// -------------------------------------------------------------------------
	// [Step 2] 본문 컨테이너(#tbody) 검증 및 순수 텍스트 추출
	//
	// 네이버 카페 게시글의 실제 본문만 포함되어 있는 최상위 노드는 "#tbody"입니다.
	// 만약 해당 마크업 구조를 찾을 수 없다면 로그인 세션이나 권한 부족으로 인해 우회된 다른 형태의
	// 차단 안내 페이지인 것으로 판단하고, 조용히 시스템적인 오류 처리 없이(Unavailable) 탈출합니다.
	// 정상적인 경우 #tbody 내부의 텍스트 노드만을 축출해 다중 개행(\n\n\n 등)을 한 줄로 압축 정규화시킵니다.
	// -------------------------------------------------------------------------
	contentNode := doc.Find("#tbody")
	if contentNode.Length() == 0 {
		return provider.ErrContentUnavailable
	}

	article.Content = strings.TrimSpace(strutil.NormalizeMultiline(contentNode.Text()))

	// -------------------------------------------------------------------------
	// [Step 3] 본문 내 멀티미디어(이미지) 요소 추가
	//
	// 문서 내의 모든 <img> 태그를 순회하며 src(이미지 경로), alt, style 속성을 추출합니다.
	// 순수 텍스트 바로 아랫단에 공백 개행(CRLF)을 기준으로 이미지를 하나씩 순차적으로 누적하여 조립합니다.
	// XSS 혹은 스크립트 인젝션 등의 보안 위협을 원천 차단하기 위해 모든 속성 값을 안전하게(EscapeString) 이스케이프 합니다.
	// -------------------------------------------------------------------------
	doc.Find("#tbody img").Each(func(i int, s *goquery.Selection) {
		var src, _ = s.Attr("src")
		if src != "" {
			if article.Content != "" {
				article.Content += "\r\n"
			}

			var alt, _ = s.Attr("alt")
			var style, _ = s.Attr("style")
			article.Content += fmt.Sprintf(`<img src="%s" alt="%s" style="%s">`, html.EscapeString(src), html.EscapeString(alt), html.EscapeString(style))
		}
	})

	return nil
}

// crawlContentViaSearch 네이버 검색 결과 페이지에서 게시글 요약을 추출하여 본문(Content)을 article에 직접 채웁니다.
//
// [검색 URL 구성]
// 게시글 제목(article.Title)을 키워드로, 카페 ID(c.Config().ID)를 필터로 사용하여 네이버 카페 게시글 검색 URL을 구성합니다.
//
//	예: https://search.naver.com/search.naver?where=article&query=제목&cafe_url=카페ID&...
//
// [본문 추출]
// 검색 결과 페이지에서 해당 게시글 링크와 일치하는 "a.total_dsc" 앵커를 찾아 텍스트를 NormalizeMultiline으로 정규화하여 article.Content에 저장합니다.
// 검색 결과에서 게시글을 찾지 못하면 본문을 채우지 않고 nil을 반환합니다.
//
// [썸네일 이미지 추출]
// "a.thumb_single" 앵커 내부의 <img> 태그를 순회하며 이미지를 본문 하단에 추가합니다.
//
// [오류 처리 정책]
//   - apperrors.Forbidden 또는 apperrors.Unauthorized: 검색 페이지 접근이 거부된 경우입니다.
//     재시도해도 결과가 동일하므로 provider.ErrContentUnavailable을 반환합니다.
//   - 그 외 오류(네트워크 에러, 타임아웃 등): 경고 로그(Warn)를 남긴 뒤 오류를 전파합니다.
//
// 매개변수:
//   - ctx: 요청 타임아웃이나 시스템 종료 시그널에 의해 작업을 취소할 수 있는 컨텍스트
//   - article: 본문(Content)을 채워 넣을 대상 게시글 포인터
//
// 반환값:
//   - nil: 성공적으로 본문을 채웠거나, 검색 결과에서 게시글을 찾지 못한 경우
//   - provider.ErrContentUnavailable: 접근이 거부된 경우 (상위 루프가 조용히 건너뜁니다)
//   - error: 일시적 장애가 발생한 경우 (경고 로그 후 오류 전파)
func (c *crawler) crawlContentViaSearch(ctx context.Context, article *feed.Article) error {
	// -------------------------------------------------------------------------
	// [Step 1] 검색 결과 페이지 HTML 조회 및 예외 처리
	//
	// 게시글 제목(article.Title)과 카페 식별자(c.Config().ID)를 활용하여 네이버 통합 검색(카페글) URL을 구성합니다.
	// API 조회나 상세 웹페이지를 직접 열 수 없을 때 우회적으로 요약본이라도 수집하기 위한 최후의 수단(Fallback)입니다.
	// 비공개 설정 등으로 검색 불가 및 401/403 권한 에러가 발생할 경우, ErrContentUnavailable을 반환하여 수집을 즉시 스킵합니다.
	// 일시적인 통신 오류일 경우 실패 사유(target_url 포함)를 Warn 로그로 꼼꼼히 기록한 뒤 에러를 반환합니다.
	// -------------------------------------------------------------------------
	searchURL := fmt.Sprintf("https://search.naver.com/search.naver?where=article&query=%s&ie=utf8&st=date&date_option=0&date_from=&date_to=&board=&srchby=title&dup_remove=0&cafe_url=%s&without_cafe_url=&sm=tab_opt&nso=so:dd,p:all,a:t&t=0&mson=0&prdtype=0", url.QueryEscape(article.Title), c.Config().ID)

	doc, err := c.Scraper().FetchHTMLDocument(ctx, searchURL, nil)
	if err != nil {
		if apperrors.Is(err, apperrors.Forbidden) || apperrors.Is(err, apperrors.Unauthorized) {
			return provider.ErrContentUnavailable
		}

		c.Logger().WithFields(applog.Fields{
			"component":  component,
			"club_id":    c.clubID,
			"board_id":   article.BoardID,
			"board_name": article.BoardName,
			"article_id": article.ArticleID,
			"link":       article.Link,
			"target_url": searchURL,
			"error":      err.Error(),
		}).Warn(c.Messagef("검색 결과 요청 실패: 대상 게시판('%s')의 게시글(ID: %s) 데이터 수신 오류", article.BoardName, article.ArticleID))

		return err
	}

	// -------------------------------------------------------------------------
	// [Step 2] 검색 결과 내 요약문(Description) 텍스트 추출
	//
	// 검색 결과 목록에서 목표하는 게시글의 정확한 원본 URL과 일치하는 '본문 요약 영역 (a.total_dsc)'을 탐색합니다.
	// 검색 결과 특성 상 본문 전체가 아닌 일부 내용과 줄임표(...)가 포함되지만, 누락 방지를 위해 이를 추출합니다.
	// 추출된 텍스트는 좌우 공백 및 특수 공백이 제거되며, 다중 개행 정규화를 거쳐 article.Content에 담깁니다.
	// 만약 해당 요약문을 전혀 찾을 수 없다면 본문을 빈 값으로 둔 채 다음 단계로 넘어갑니다.
	// -------------------------------------------------------------------------
	descNode := doc.Find(fmt.Sprintf("a.total_dsc[href='%s/%s']", c.Config().URL, article.ArticleID))
	if descNode.Length() == 1 {
		text := descNode.Text()
		text = strings.ReplaceAll(text, "\u200b", "")
		article.Content = strings.TrimSpace(strutil.NormalizeMultiline(text))
	}

	// -------------------------------------------------------------------------
	// [Step 3] 검색 결과 내 썸네일(Thumbnail) 이미지 추출 및 병합
	//
	// 검색 결과 화면에서 우측 등에 표시되는 대표 썸네일 이미지 영역(a.thumb_single)을 찾아냅니다.
	// 유효한 이미지 소스(src)가 발견되면, 앞서 추출한 요약문 텍스트 바로 밑에 개행(\r\n)을 주고 합칩니다.
	// HTML 렌더링 시 XSS 공격 등 각종 보안 취약점을 차단하기 위해 속성 값을 모두 안전하게 이스케이프(EscapeString) 처리합니다.
	// -------------------------------------------------------------------------
	doc.Find(fmt.Sprintf("a.thumb_single[href='%s/%s'] img", c.Config().URL, article.ArticleID)).Each(func(i int, s *goquery.Selection) {
		var src, _ = s.Attr("src")
		if src != "" {
			if article.Content != "" {
				article.Content += "\r\n"
			}

			var alt, _ = s.Attr("alt")
			var style, _ = s.Attr("style")
			article.Content += fmt.Sprintf(`<img src="%s" alt="%s" style="%s">`, html.EscapeString(src), html.EscapeString(alt), html.EscapeString(style))
		}
	})

	return nil
}
