package navercafe

import (
	"context"
	"fmt"
	"html"
	"net/http"
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

// articleAPIResponse 네이버 카페 게시글 API의 JSON 응답을 역직렬화(Unmarshal)하기 위한 데이터 홀더(Data Holder) 구조체입니다.
//
// 이 구조체는 오직 crawlContentViaAPI 함수 내부에서만 사용되는 일회성(One-off) 타입입니다.
// 따라서 재사용성보다 "필요한 정보만 선언"을 우선하여, 외부에 별도의 구조체를 노출시키지 않고
// 익명 구조체(Anonymous Struct)를 중첩(Nested)하여 한 곳에 모두 선언하였습니다.
//
// 대응하는 API 엔드포인트:
//
//	GET https://apis.naver.com/cafe-web/cafe-articleapi/v2/cafes/{clubID}/articles/{articleID}
//
// 주요 응답 필드:
//   - result.article.writeDate       : 게시글 작성 시각(Unix 밀리초). 목록 파싱에서 시각 정보를 추출하지 못한 과거 날짜 게시글의 정확한 작성 시각 복원에 사용됩니다.
//   - result.article.contentHtml     : 게시글 본문 HTML. 이미지·링크 등의 미디어 요소는 "[[[CONTENT-ELEMENT-N]]]" 플레이스홀더로 표시됩니다.
//   - result.article.contentElements : 본문 내 미디어 요소(IMAGE, LINK, STICKER) 목록. 각 요소를 처리한 뒤 contentHtml의 플레이스홀더를 실제 HTML 태그로 교체합니다.
type articleAPIResponse struct {
	Result struct {
		Article struct {
			WriteDate       int64  `json:"writeDate"`
			ContentHtml     string `json:"contentHtml"`
			ContentElements []struct {
				Type string `json:"type"`
				JSON struct {
					Image struct {
						URL      string `json:"url"`
						Service  string `json:"service"`
						Type     string `json:"type"`
						Width    int    `json:"width"`
						Height   int    `json:"height"`
						FileName string `json:"fileName"`
						FileSize int    `json:"fileSize"`
					} `json:"image"`
					Layout         string `json:"layout"`
					ImageURL       string `json:"imageUrl"`
					VideoURL       string `json:"videoUrl"`
					AudioURL       string `json:"audioUrl"`
					Desc           string `json:"desc"`
					TruncatedTitle string `json:"truncatedTitle"`
					TruncatedDesc  string `json:"truncatedDesc"`
					Domain         string `json:"domain"`
					LinkURL        string `json:"linkUrl"`
					StickerID      string `json:"stickerId"`
					MarketURL      string `json:"marketUrl"`
					URL            string `json:"url"`
					Width          int    `json:"width"`
					Height         int    `json:"height"`
					From           string `json:"from"`
				} `json:"json"`
			} `json:"contentElements"`
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
	//   최종 URL 예: "https://cafe.naver.com/ArticleRead.nhn?articleid=12345&clubid=cafe_id"
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

	// articleid는 URL 조립 시 정수(int64)가 필요하므로 strconv.ParseInt로 파싱합니다.
	// 문자열을 그대로 사용하지 않는 이유: fmt.Sprintf의 "%d" 포맷으로 URL에 삽입하기 위함이며, 파싱 실패 시 비정상적인 값임을 조기에 감지할 수 있습니다.
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
	//           <td class="p-nick">닉네임</td>
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

// crawlArticleContent 주어진 게시글(article)의 본문(Content)을 세 가지 방법으로 순차 시도하여 채웁니다.
//
// [본문 수집 전략 — 3단계 Fallback]
// 네이버 카페는 로그인 여부나 작성자 설정에 따라 콘텐츠 접근 방식이 달라집니다.
// 따라서 아래 순서로 대안을 시도하며, 앞 단계가 성공하면 이후 단계는 건너뜁니다.
//
//  1. crawlContentViaAPI
//     네이버 카페 공식 API를 통해 게시글 JSON 응답을 파싱, 이미지·링크·스티커 등 미디어 요소까지 복원합니다.
//  2. crawlContentViaPage
//     게시글 상세 페이지 HTML에서 "#tbody" 영역을 파싱, API가 실패하거나 콘텐츠를 반환하지 않은 경우 사용합니다.
//  3. crawlContentViaSearch
//     네이버 검색 결과 페이지에서 게시글 요약을 추출, 앞의 두 방법으로도 본문을 얻지 못한 최후의 수단입니다.
//
// [오류 보존 정책]
// 각 단계가 실패하더라도 즉시 반환하지 않고 마지막 오류(lastErr)를 보존합니다.
// 이후 단계가 본문을 성공적으로 채운 경우에만 lastErr를 nil로 초기화하여, 일시적 네트워크 오류로 인해 재시도 기회가 영구 손실되는 것을 방지합니다.
//
// [컨텍스트 취소/타임아웃 즉시 전파]
// ctx가 취소된 경우에는 이후 단계를 실행하지 않고 즉시 ctx.Err()를 반환합니다.
// 그렇지 않으면 취소 상황에서도 다음 단계가 에러 없이 반환될 때 lastErr가 nil이 되어 ErrContentUnavailable이 반환되고 재시도 기회가 영구 손실될 수 있습니다.
//
// [최종 반환 규칙]
//   - 세 단계 모두 시도 후 Content가 채워져 있으면 nil 반환 (성공).
//   - Content가 비어있고 lastErr가 non-nil이면 lastErr 반환 (재시도 대상).
//   - Content가 비어있고 lastErr가 nil이면 ErrContentUnavailable 반환
//     (원래 비어있는 글이거나 비공개 등 영구적 파싱 불가 상태이므로 재시도 스킵).
//
// 매개변수:
//   - ctx: 요청 타임아웃이나 시스템 종료 시그널에 의해 작업을 취소할 수 있는 컨텍스트
//   - article: 본문을 채워 넣을 대상 게시글 포인터 (Content 필드가 직접 수정됩니다)
//
// 반환값:
//   - nil: 세 단계 중 하나가 성공적으로 본문을 채운 경우
//   - provider.ErrContentUnavailable: 어떠한 시스템 오류도 없었으나 본문이 없는 경우 (재시도 스킵)
//   - error: 일시적 네트워크 오류 등으로 인해 재시도가 필요한 경우
func (c *crawler) crawlArticleContent(ctx context.Context, article *feed.Article) error {
	var lastErr error

	// -------------------------------------------------------------------------
	// [1단계] crawlContentViaAPI — 공식 API를 통한 본문 수집
	//
	// 네이버 카페 공식 API를 호출하여 본문(Content)과 작성일(CreatedAt)을 채웁니다.
	// 이미지·링크·스티커 등 미디어 요소까지 복원되는 가장 완전한 방법입니다.
	//
	// 실패 시 즉시 반환하지 않고 lastErr에 보존한 뒤 다음 단계로 넘어갑니다.
	// 단, ctx가 취소된 경우에는 이후 단계를 실행해도 무의미하므로 즉시 전파합니다.
	// ctx를 즉시 전파하지 않으면 이후 단계가 에러 없이 반환될 때 lastErr가 nil이 되어
	// ErrContentUnavailable이 반환되고 재시도 기회가 영구 손실될 수 있습니다.
	// -------------------------------------------------------------------------
	if err := c.crawlContentViaAPI(ctx, article); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		lastErr = err
	}

	if article.Content == "" {
		// -------------------------------------------------------------------------
		// [2단계] crawlContentViaPage — 상세 페이지 HTML 직접 파싱
		//
		// 1단계 API 호출이 실패했거나 본문을 반환하지 않은 경우의 첫 번째 Fallback입니다.
		// 게시글 상세 페이지 HTML에서 "#tbody" 영역을 직접 파싱하여 본문을 추출합니다.
		//
		// 성공(nil 반환) 후 실제로 본문이 채워진 경우에만 lastErr를 nil로 초기화합니다.
		// 본문이 빈 채로 nil을 반환하면(로그인 필요 등으로 내용이 없는 경우)
		// 1단계의 일시적 네트워크 에러를 보존하여 상위 루프의 재시도 기회가 유지되도록 합니다.
		// -------------------------------------------------------------------------
		if err := c.crawlContentViaPage(ctx, article); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			lastErr = err
		} else if article.Content != "" {
			lastErr = nil
		}

		if article.Content == "" {
			// -------------------------------------------------------------------------
			// [3단계] crawlContentViaSearch — 네이버 검색 결과에서 요약 추출
			//
			// 1·2단계 모두 실패한 경우의 마지막 Fallback입니다.
			// 네이버 검색 결과 페이지에서 해당 게시글의 요약(Summary)을 추출합니다.
			//
			// 2단계와 동일하게, 성공(nil 반환) 후 실제로 본문이 채워진 경우에만 lastErr를 nil로 초기화합니다.
			// -------------------------------------------------------------------------
			if err := c.crawlContentViaSearch(ctx, article); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}

				lastErr = err
			} else if article.Content != "" {
				lastErr = nil
			}
		}
	}

	// 세 단계 중 하나라도 본문을 채우는 데 성공하면 nil을 반환합니다.
	if article.Content != "" {
		return nil
	}

	// 마지막으로 발생한 에러가 non-nil이면 일시적인 네트워크 장애 등의 가능성이 있으므로
	// 상위 루프가 해당 게시글을 재시도할 수 있도록 에러를 그대로 전파합니다.
	if lastErr != nil {
		return lastErr
	}

	// 세 단계 모두 시도했으나 어떠한 시스템 에러도 없었는데 본문이 비어있는 상태라면,
	// 원래 내용이 없는 글이거나 비공개·접근 불가 등 영구적으로 수집이 불가한 상태입니다.
	// ErrContentUnavailable을 반환하여 상위 루프가 재시도 없이 조용히 건너뛰도록 합니다.
	return provider.ErrContentUnavailable
}

// crawlContentViaAPI 네이버 카페 공식 API를 호출하여 게시글 본문(Content)과 작성일(CreatedAt)을 article에 직접 채웁니다.
//
// [2단계 HTTP 요청 구조]
//  1. 상세 페이지 HTML 로드 : 게시글 상세 페이지에서 API 인증에 필요한 "art" 쿼리 파라미터를 추출합니다.
//  2. API JSON 요청        : 추출한 art 값을 포함하여 네이버 카페 게시글 API를 호출하고 JSON 응답을 파싱합니다.
//
// [API 엔드포인트]
//
//	GET https://apis.naver.com/cafe-web/cafe-articleapi/v2/cafes/{clubID}/articles/{articleID}?art={artValue}&...
//
// [콘텐츠 요소(ContentElements) 처리]
// API 응답의 contentHtml에는 이미지·링크·스티커 등 미디어 요소가 "[[[CONTENT-ELEMENT-N]]]" 플레이스홀더로 표시됩니다.
// contentElements 배열을 순회하며 각 타입에 맞는 HTML 태그로 플레이스홀더를 교체합니다.
//
//   - IMAGE   : <img src="..." alt="..."> 태그로 교체합니다.
//   - LINK    : <a href="..." target="_blank">제목</a> 태그로 교체합니다.
//     Layout이 SIMPLE_IMAGE 또는 WIDE_IMAGE인 경우에만 처리하며, 알 수 없는 Layout은 오류 알림을 발송합니다.
//   - STICKER : <img src="..." width="..." height="..."> 태그로 교체합니다.
//   - 그 외    : 알 수 없는 타입으로 간주하고 오류 알림을 발송합니다.
//
// [작성일 보정]
// 오늘 이전 게시글은 목록 파싱 시 시각 정보가 없어 CreatedAt이 "00:00:00"으로 고정됩니다.
// API 응답의 writeDate(Unix 밀리초)를 이용하여 정확한 작성 시각으로 보정합니다.
// writeDate가 0 이하인 경우(Unix Epoch 오류 방지)에는 보정을 생략합니다.
//
// [오류 처리 정책]
//   - apperrors.Forbidden 또는 apperrors.Unauthorized: 로그인 없이 접근 불가한 게시글입니다.
//     재시도해도 결과가 동일하므로 provider.ErrContentUnavailable을 반환합니다.
//   - 그 외 오류(네트워크 에러, 타임아웃 등): 경고 로그(Warn)를 남긴 뒤 오류를 전파합니다.
//
// 매개변수:
//   - ctx: 요청 타임아웃이나 시스템 종료 시그널에 의해 작업을 취소할 수 있는 컨텍스트
//   - article: 본문(Content)과 작성일(CreatedAt)을 채워 넣을 대상 게시글 포인터
//
// 반환값:
//   - nil: 성공적으로 본문을 채운 경우
//   - provider.ErrContentUnavailable: 접근이 거부된 경우 (상위 루프가 조용히 건너뜁니다)
//   - error: 일시적 장애가 발생한 경우 (경고 로그 후 오류 전파)
func (c *crawler) crawlContentViaAPI(ctx context.Context, article *feed.Article) error {
	// -------------------------------------------------------------------------
	// [Step 1] 상세 페이지 HTML 로드 및 API 인증 토큰(art) 추출
	//
	// 네이버 카페 게시글 API를 직접 호출하려면 API 요청 URL에 포함할 「art」 쿼리 파라미터가 필요합니다.
	// 이 값은 게시글 상세 페이지 HTML 소스 안에 포함되어 있으므로, 우선 상세 페이지를 로드한 뒤
	// rawHTML에서 「&art=」 문자열을 찾아 값을 추출합니다.
	//
	// [Referer 헤더 설정]
	// 네이버 카페 상세 페이지에 직접 진입 시 403(Forbidden) 응답을 받는 경우가 있습니다.
	// 네이버 검색 결과에서 유입되는 것처럼 위장하기 위해 Referer를 「https://search.naver.com/」으로 설정합니다.
	//
	// [오류 처리 정책]
	//   - apperrors.Forbidden 또는 apperrors.Unauthorized:
	//     서버가 정상 응답했으나 접근 자체가 거부된 경우(예: 비공개 게시글, 로그인 필요)로 재시도해도 결과가
	//     동일하므로 ErrContentUnavailable을 반환하여 상위 루프가 조용히 건너뛰도록 합니다.
	//   - 그 외 오류(네트워크 에러, 타임아웃 등):
	//     경고 로그(Warn)를 남긴 뒤 오류를 전파합니다.
	// -------------------------------------------------------------------------
	pageDesc := c.Messagef("대상 게시판('%s') 소속 게시글(고유 식별자: %s)의 상세 페이지", article.BoardName, article.ArticleID)

	// 외부 유입 위장을 위한 Referer 헤더 구성 (403 Forbidden 차단 우회)
	header := make(http.Header)
	header.Set("Referer", "https://search.naver.com/")

	// API 인증 토큰(art) 추출을 위한 상세 페이지 HTML 로드
	doc, err := c.Scraper().FetchHTMLDocument(ctx, fmt.Sprintf("%s/%s", c.Config().URL, article.ArticleID), header)
	if err != nil {
		if apperrors.Is(err, apperrors.Forbidden) || apperrors.Is(err, apperrors.Unauthorized) {
			return provider.ErrContentUnavailable
		}

		c.Logger().WithFields(applog.Fields{
			"component":  component,
			"club_id":    c.clubID,
			"board_id":   article.BoardID,
			"article_id": article.ArticleID,
			"target_url": fmt.Sprintf("%s/%s", c.Config().URL, article.ArticleID),
			"error":      err.Error(),
		}).Warn(c.Messagef("문서 요청 실패: %s 접근 및 데이터 수신 오류", pageDesc))

		return err
	}

	// [art 토큰 탐색 및 추출]
	// 상세 페이지 전체 HTML에서 API 호출에 필수적인 '&art=' 파라미터를 탐색합니다.
	// 토큰이 존재하지 않는 경우(예: 접근 권한 없음, 데이터 구조 변경 등), 재시도해도 실패가 확정적이므로
	// ErrContentUnavailable을 반환하여 상위 루프가 즉시 다음 작업으로 넘어가도록 유도합니다.
	rawHTML, _ := doc.Html()
	_, after, ok := strings.Cut(rawHTML, "&art=")
	if !ok {
		c.Logger().WithFields(applog.Fields{
			"component":  component,
			"club_id":    c.clubID,
			"board_id":   article.BoardID,
			"article_id": article.ArticleID,
			"target_url": fmt.Sprintf("%s/%s", c.Config().URL, article.ArticleID),
		}).Warn(c.Messagef("API 인증 토큰 추출 실패: %s 내부에서 'art' 쿼리 파라미터 식별 불가", pageDesc))

		return provider.ErrContentUnavailable
	}

	// 시작 위치부터 URL 파라미터 구분자('&')나 HTML 속성 종료 문자('"', '\'')가
	// 나타나기 전까지의 유효한 문자열 구간만 잘라내어 최종 'art' 토큰을 확정합니다.
	artValue := after
	endIdx := strings.IndexAny(artValue, "&\"'")
	if endIdx != -1 {
		artValue = artValue[:endIdx]
	}

	// @@@@@
	// -------------------------------------------------------------------------
	// [Step 2] 네이버 카페 게시글 API 호출 및 JSON 응답 파싱
	//
	// Step 1에서 추출한 art 토큰을 포함하여 네이버 카페 공식 API를 호출합니다.
	//
	// [API 엔드포인트]
	//   GET https://apis.naver.com/cafe-web/cafe-articleapi/v2/cafes/{clubID}/articles/{articleID}
	//       ?art={artValue}&useCafeId=true&requestFrom=A
	//
	// [오류 처리 정책]
	//   - apperrors.Forbidden 또는 apperrors.Unauthorized:
	//     접근이 거부된 게시글로 재시도해도 결과가 동일하므로 ErrContentUnavailable을 반환합니다.
	//     (특정 게시글의 경우 작성자가 로그인 없이 외부에서 접근할 수 없도록 설정한 경우 401이 반환됩니다.)
	//   - 그 외 오류(네트워크 에러, 타임아웃 등):
	//     경고 로그(Warn)를 남긴 뒤 오류를 전파합니다.
	// -------------------------------------------------------------------------
	apiDesc := c.Messagef("대상 게시판('%s') 소속 게시글(고유 식별자: %s)의 API 데이터", article.BoardName, article.ArticleID)

	apiURL := fmt.Sprintf("https://apis.naver.com/cafe-web/cafe-articleapi/v2/cafes/%s/articles/%s?art=%s&useCafeId=true&requestFrom=A", c.clubID, article.ArticleID, artValue)

	var apiResp articleAPIResponse
	if err := c.Scraper().FetchJSON(ctx, "GET", apiURL, nil, nil, &apiResp); err != nil {
		if apperrors.Is(err, apperrors.Forbidden) || apperrors.Is(err, apperrors.Unauthorized) {
			return provider.ErrContentUnavailable
		}

		c.Logger().WithFields(applog.Fields{
			"component":  component,
			"club_id":    c.clubID,
			"board_id":   article.BoardID,
			"article_id": article.ArticleID,
			"target_url": apiURL,
			"error":      err.Error(),
		}).Warn(c.Messagef("API 요청 실패: %s 접근 및 데이터 수신 오류 (비공개/권한 없음 추정)", apiDesc))

		return err
	}

	// -------------------------------------------------------------------------
	// [Step 3] 콘텐츠 요소(ContentElements) 플레이스홀더 교체
	//
	// API 응답의 contentHtml에는 이미지·링크·스티커 등 미디어 요소가
	// 「[[[CONTENT-ELEMENT-N]]]」 형식의 플레이스홀더로 표시되어 있습니다.
	// contentElements 배열을 인덱스(i)와 함께 순회하며, 각 타입에 맞는 실제 HTML 태그로 교체합니다.
	//
	//   - IMAGE  : <img src="..." alt="..."> 태그로 교체합니다.
	//              이미지 URL과 파일명(alt)은 element.JSON.Image 필드에서 추출합니다.
	//   - LINK   : <a href="..." target="_blank">제목</a> 태그로 교체합니다.
	//              Layout이 SIMPLE_IMAGE 또는 WIDE_IMAGE인 경우에만 처리합니다.
	//              그 외 알 수 없는 Layout은 새로운 타입이 추가된 것으로 간주하고 오류 알림을 발송합니다.
	//   - STICKER: <img src="..." width="..." height="..." ...> 태그로 교체합니다.
	//              스티커 이미지의 URL·너비·높이는 element.JSON에서 직접 읽습니다.
	//   - 그 외  : 알 수 없는 타입으로 간주하고 오류 알림을 발송합니다.
	// -------------------------------------------------------------------------
	article.Content = apiResp.Result.Article.ContentHtml
	for i, element := range apiResp.Result.Article.ContentElements {
		switch element.Type {
		case "IMAGE":
			imgTag := fmt.Sprintf("<img src=\"%s\" alt=\"%s\">", html.EscapeString(element.JSON.Image.URL), html.EscapeString(element.JSON.Image.FileName))
			article.Content = strings.ReplaceAll(article.Content, fmt.Sprintf("[[[CONTENT-ELEMENT-%d]]]", i), imgTag)

		case "LINK":
			if element.JSON.Layout == "SIMPLE_IMAGE" || element.JSON.Layout == "WIDE_IMAGE" {
				linkTag := fmt.Sprintf("<a href=\"%s\" target=\"_blank\">%s</a>", html.EscapeString(element.JSON.LinkURL), html.EscapeString(html.UnescapeString(element.JSON.TruncatedTitle)))
				article.Content = strings.ReplaceAll(article.Content, fmt.Sprintf("[[[CONTENT-ELEMENT-%d]]]", i), linkTag)
			} else {
				message := c.Messagef("미디어 요소 파싱 누락: %s 응답 데이터에서 식별되지 않은 LINK ContentElement Layout('%s') 감지", apiDesc, element.JSON.Layout)
				c.SendErrorNotification(message, nil)
			}

		case "STICKER":
			imgTag := fmt.Sprintf("<img src=\"%s\" width=\"%d\" height=\"%d\" nhn_extra_image=\"true\" style=\"cursor:pointer\">", html.EscapeString(element.JSON.URL), element.JSON.Width, element.JSON.Height)
			article.Content = strings.ReplaceAll(article.Content, fmt.Sprintf("[[[CONTENT-ELEMENT-%d]]]", i), imgTag)

		default:
			message := c.Messagef("미디어 요소 파싱 누락: %s 응답 데이터에서 식별되지 않은 ContentElement Type('%s') 감지", apiDesc, element.Type)
			c.SendErrorNotification(message, nil)
		}
	}

	// -------------------------------------------------------------------------
	// [Step 4] 작성일(CreatedAt) 보정
	//
	// 목록 파싱 단계(extractArticle)에서 과거 날짜 게시글은 시각 정보를 추출할 수 없어
	// CreatedAt의 시각 부분이 「00:00:00」으로 고정됩니다.
	// 이 경우 API 응답의 writeDate(Unix 밀리초)를 이용하여 정확한 작성 시각으로 보정합니다.
	//
	// [WriteDate == 0 방어]
	// WriteDate가 0이면 time.Unix(0, 0) = 1970-01-01 00:00:00 (Unix Epoch)이 반환됩니다.
	// Go의 time.IsZero()는 time.Time{} (0001-01-01)에 대해서만 true를 반환하므로,
	// WriteDate == 0인 경우 IsZero() 체크를 우회하여 Epoch 시각이 CreatedAt에 잘못 설정됩니다.
	// 이를 방지하기 위해 WriteDate가 명시적으로 양수(> 0)인 경우에만 보정을 수행합니다.
	// -------------------------------------------------------------------------
	if article.CreatedAt.Format("15:04:05") == "00:00:00" {
		if apiResp.Result.Article.WriteDate > 0 {
			article.CreatedAt = time.Unix(apiResp.Result.Article.WriteDate/1000, 0)
		}
	}

	return nil
}

// @@@@@
// crawlContentViaPage 게시글 상세 페이지 HTML을 직접 파싱하여 본문(Content)을 article에 직접 채웁니다.
//
// crawlContentViaAPI가 실패하거나 콘텐츠를 반환하지 않은 경우의 두 번째 Fallback입니다.
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
	doc, err := c.Scraper().FetchHTMLDocument(ctx, article.Link, nil)
	if err != nil {
		if apperrors.Is(err, apperrors.Forbidden) || apperrors.Is(err, apperrors.Unauthorized) {
			return provider.ErrContentUnavailable
		}
		c.Logger().WithFields(applog.Fields{
			"component":  component,
			"club_id":    c.clubID,
			"board_id":   article.BoardID,
			"article_id": article.ArticleID,
			"error":      err.Error(),
		}).Warn(c.Messagef("상세 페이지 요청 실패: 대상 게시판('%s')의 지정된 게시글(ID: %s) HTML 문서 데이터 수신 오류", article.BoardName, article.ArticleID))
		return err
	}

	ncSelection := doc.Find("#tbody")
	if ncSelection.Length() == 0 {
		// 로그인을 하지 않아 접근 권한이 없는 페이지인 경우 오류가 발생하므로 로그 처리를 하지 않는다.
		return provider.ErrContentUnavailable
	}

	article.Content = strutil.NormalizeMultiline(ncSelection.Text())

	// 내용에 이미지 태그가 포함되어 있다면 모두 추출한다.
	doc.Find("#tbody img").Each(func(i int, s *goquery.Selection) {
		var src, _ = s.Attr("src")
		if src != "" {
			var alt, _ = s.Attr("alt")
			var style, _ = s.Attr("style")
			article.Content += fmt.Sprintf(`%s<img src="%s" alt="%s" style="%s">`, "\r\n", html.EscapeString(src), html.EscapeString(alt), html.EscapeString(style))
		}
	})

	return nil
}

// @@@@@
// crawlContentViaSearch 네이버 검색 결과 페이지에서 게시글 요약을 추출하여 본문(Content)을 article에 직접 채웁니다.
//
// crawlContentViaAPI와 crawlContentViaPage 모두 실패한 경우의
// 세 번째이자 마지막 Fallback입니다.
//
// [검색 URL 구성]
// 게시글 제목(article.Title)을 키워드로, 카페 ID(c.Config().ID)를 필터로 사용하여
// 네이버 카페 게시글 검색 URL을 구성합니다.
//
//	예: https://search.naver.com/search.naver?where=article&query=제목&cafe_url=카페ID&...
//
// [본문 추출]
// 검색 결과 페이지에서 해당 게시글 링크와 일치하는 "a.total_dsc" 앵커를 찾아
// 텍스트를 NormalizeMultiline으로 정규화하여 article.Content에 저장합니다.
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
	searchUrl := fmt.Sprintf("https://search.naver.com/search.naver?where=article&query=%s&ie=utf8&st=date&date_option=0&date_from=&date_to=&board=&srchby=title&dup_remove=0&cafe_url=%s&without_cafe_url=&sm=tab_opt&nso=so:dd,p:all,a:t&t=0&mson=0&prdtype=0", url.QueryEscape(article.Title), c.Config().ID)

	doc, err := c.Scraper().FetchHTMLDocument(ctx, searchUrl, nil)
	if err != nil {
		if apperrors.Is(err, apperrors.Forbidden) || apperrors.Is(err, apperrors.Unauthorized) {
			return provider.ErrContentUnavailable
		}

		c.Logger().WithFields(applog.Fields{
			"component":  component,
			"club_id":    c.clubID,
			"board_id":   article.BoardID,
			"article_id": article.ArticleID,
			"error":      err.Error(),
		}).Warn(c.Messagef("검색 결과 요청 실패: 대상 게시판('%s')의 지정된 게시글(ID: %s) HTML 문서 데이터 수신 오류", article.BoardName, article.ArticleID))

		return err
	}

	ncSelection := doc.Find(fmt.Sprintf("a.total_dsc[href='%s/%s']", c.Config().URL, article.ArticleID))
	if ncSelection.Length() == 1 {
		article.Content = strutil.NormalizeMultiline(ncSelection.Text())
	}

	// 내용에 이미지 태그가 포함되어 있다면 모두 추출한다.
	doc.Find(fmt.Sprintf("a.thumb_single[href='%s/%s'] img", c.Config().URL, article.ArticleID)).Each(func(i int, s *goquery.Selection) {
		var src, _ = s.Attr("src")
		if src != "" {
			var alt, _ = s.Attr("alt")
			var style, _ = s.Attr("style")
			article.Content += fmt.Sprintf(`%s<img src="%s" alt="%s" style="%s">`, "\r\n", html.EscapeString(src), html.EscapeString(alt), html.EscapeString(style))
		}
	})

	return nil
}
