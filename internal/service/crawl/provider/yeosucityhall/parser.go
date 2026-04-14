package yeosucityhall

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/strutil"
	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"
)

// extractArticle 게시판 유형(boardType)에 맞는 파서 함수를 선택하여 HTML 행(row) 하나를 feed.Article로 변환합니다.
// 여수시청 웹사이트는 게시판 유형마다 HTML 구조(CSS 셀렉터, 데이터 위치)가 서로 달라,
// 각 유형에 특화된 개별 파서 함수로 처리를 위임하는 디스패처(Dispatcher) 역할을 합니다.
//
// 지원하는 게시판 유형:
//   - boardTypeList1("L_1"), boardTypeList2("L_2"): 번호·제목·작성자·등록일이 표 형식으로 나오는 일반 목록형 게시판
//   - boardTypePhotoNews("P"):                      썸네일 사진이 그리드로 나열되는 포토뉴스형 게시판
//   - boardTypeCardNews("C"):                       카드 형태로 콘텐츠가 배치되는 카드뉴스형 게시판
//
// 매개변수:
//   - boardType: 게시판 유형 식별자 (지원하지 않는 값이 전달되면 apperrors.System 오류를 반환합니다)
//   - s: goquery로 읽어들인 게시글 한 줄(row 또는 item)에 해당하는 HTML 선택 객체
//
// 반환값:
//   - *feed.Article: 파싱된 게시글 정보 (제목, 링크, 작성자, 등록일 등)
//   - error: 지원하지 않는 boardType이거나 개별 필드 파싱에 실패한 경우 non-nil
func (c *crawler) extractArticle(boardType string, s *goquery.Selection) (*feed.Article, error) {
	switch boardType {
	case boardTypeList1, boardTypeList2:
		return c.extractListArticle(boardType, s)

	case boardTypePhotoNews:
		return c.extractPhotoNewsArticle(s)

	case boardTypeCardNews:
		return c.extractCardNewsArticle(s)

	default:
		return nil, apperrors.Newf(apperrors.System, "지원하지 않는 게시판 유형('%s')이 감지되어 파싱 작업을 수행할 수 없습니다", boardType)
	}
}

// extractListArticle 일반 목록형(boardTypeList1, boardTypeList2) 게시판의 HTML 행(<tr>) 하나를 파싱하여 feed.Article 구조체로 변환합니다.
//
// 매개변수:
//   - boardType: 게시판 유형 식별자. boardTypeList2인 경우 추가로 분류(Category) 필드를 파싱합니다.
//   - s: <tr> 행에 해당하는 goquery 선택 객체
//
// 반환값:
//   - *feed.Article: 성공적으로 파싱된 게시글 정보
//   - error: HTML 구조 불일치나 필수 속성 누락 시 apperrors.ParsingFailed 타입의 오류
func (c *crawler) extractListArticle(boardType string, s *goquery.Selection) (*feed.Article, error) {
	var article = &feed.Article{}

	// -------------------------------------------------------------------------
	// [Step 1] 제목 & 상세페이지 링크 추출
	//
	// 이 게시판의 <tr> 행에서 제목과 링크는 "a.basic_cont" 앵커 하나에 함께 담겨 있습니다.
	//
	//   <td>
	//     <a class="basic_cont" href="/board/view?idx=12345">게시글 제목</a>
	//   </td>
	//
	//   - 제목 : 앵커의 텍스트 컨텐츠 (.Text())
	//   - 링크 : 앵커의 "href" 속성값 (.Attr("href"))
	//
	// 앵커는 행당 정확히 1개여야 합니다.
	// 0개이면 셀 자체가 없는 것이고, 2개 이상이면 레이아웃이 변경된 것이므로
	// 두 경우 모두 HTML 구조 이상으로 판단하고 파싱을 중단합니다.
	// -------------------------------------------------------------------------
	articleAnchor := s.Find("a.basic_cont")
	if articleAnchor.Length() != 1 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글 HTML 요소에서 제목 마크업을 식별할 수 없어 데이터 파싱에 실패했습니다")
	}

	// [제목 파싱]
	// 새로 등록된 게시글에는 제목 안에 "새로운글" 이미지(img)나 스팬(span) 태그가 삽입됩니다.
	// .Text() 호출 전에 DOM에서 미리 제거하지 않으면 제목 텍스트에 "새로운글" 문자열이 혼입됩니다.
	articleAnchor.Find("img[alt*='새로운글'], img[alt*='새글'], span[class*='new']").Remove()

	// .Text()로 앵커의 전체 텍스트를 추출한 뒤 양끝 공백을 제거합니다.
	// 간혹 DOM 제거가 누락된 "새로운글" 접두어가 남아있을 수 있으므로 TrimPrefix로 한 번 더 방어합니다.
	article.Title = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(articleAnchor.Text()), "새로운글"))

	// [상세페이지 링크 파싱]
	// .Attr("href")는 속성 자체가 없으면 exists=false, 있어도 값이 비어있으면 article.Link=""가 됩니다.
	// 두 경우 모두 상세페이지로 이동할 수 없으므로 파싱 실패로 처리합니다.
	var exists bool
	article.Link, exists = articleAnchor.Attr("href")
	if !exists || article.Link == "" {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글의 상세페이지 링크(href) 속성이 누락되었거나 유효하지 않아 데이터 파싱에 실패했습니다")
	}

	// href가 "http://" 또는 "https://"로 시작하지 않으면 상대 URL로 판단합니다.
	// 상대 URL인 경우 사이트 기본 URL(c.Config().URL)을 앞에 붙여 절대 URL로 변환합니다.
	//   예: "/board/view?idx=12345" → "https://www.yeosu.go.kr/board/view?idx=12345"
	if !strings.HasPrefix(article.Link, "http://") && !strings.HasPrefix(article.Link, "https://") {
		article.Link = fmt.Sprintf("%s%s", c.Config().URL, article.Link)
	}

	// -------------------------------------------------------------------------
	// [Step 2] 분류(Category) 추출 — boardTypeList2 전용
	//
	// boardTypeList2 게시판은 각 게시글에 분류 태그(예: [공지], [행사])가 추가로 붙습니다.
	// 분류 정보는 "td.list_cate" 셀 하나에 텍스트로 제공됩니다.
	//
	//   <td class="list_cate">공지</td>
	//
	// 분류가 비어있지 않으면 "[ 분류 ] 제목" 형태로 제목 앞에 접두어를 붙여 강조합니다.
	// -------------------------------------------------------------------------
	if boardType == boardTypeList2 {
		cateCell := s.Find("td.list_cate")
		if cateCell.Length() != 1 {
			return nil, apperrors.New(apperrors.ParsingFailed, "게시글 HTML 요소에서 분류 마크업을 식별할 수 없어 데이터 파싱에 실패했습니다")
		}

		category := strings.TrimSpace(cateCell.Text())
		if category != "" {
			article.Title = fmt.Sprintf("[ %s ] %s", category, article.Title)
		}
	}

	// -------------------------------------------------------------------------
	// [Step 3] 게시글 ID 추출
	//
	// 여수시청 게시판의 상세페이지 링크에는 쿼리 파라미터 "idx"에 게시글 고유 ID가 담겨 있습니다.
	//   예: "/board/view?idx=12345" → ArticleID = "12345"
	//
	// extractArticleIDFromURL이 URL을 파싱하여 "idx" 파라미터 값을 추출합니다.
	// -------------------------------------------------------------------------
	var err error
	if article.ArticleID, err = c.extractArticleIDFromURL(article.Link); err != nil {
		return nil, err
	}

	// -------------------------------------------------------------------------
	// [Step 4] 작성자 & 등록일 추출
	//
	// 행(<tr>) 안의 모든 <td> 셀을 수집한 뒤, 앞이 아닌 '뒤에서부터' 인덱스를 계산하여 대상 셀을 특정합니다.
	// 게시판 HTML은 앞쪽 셀 개수가 가변적(예: 분류 셀 유무에 따라 다름)이지만, 뒷쪽 순서(작성자·등록일)는
	// 항상 고정되어 있어 이 방식이 HTML 구조 변경에 더 강합니다.
	//
	// 예상 셀 배치 (뒤에서):
	//   - rowCells.Length() - 3 : 작성자(등록자/담당부서) 셀
	//   - rowCells.Length() - 2 : 등록일 셀  → 텍스트 예시: "2024.03.15" 또는 "14:30"
	//   - rowCells.Length() - 1 : 조회수 셀  → 파싱 불필요
	//
	// 최소 5개 미만이면 기대하는 셀 자체가 존재할 수 없으므로 즉시 에러를 반환합니다.
	// -------------------------------------------------------------------------
	rowCells := s.Find("td")
	if rowCells.Length() < 5 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글의 하위 노드 개수가 규격에 미달하여 작성자 및 등록일 정보를 검출할 수 없습니다")
	}

	// [작성자 파싱]
	// 셀 텍스트 형식: "홍길동" (담당부서명 또는 등록자명 그대로 노출)
	article.Author = strings.TrimSpace(rowCells.Eq(rowCells.Length() - 3).Text())

	// [등록일 파싱]
	// 셀 텍스트 형식: "2024.03.15" 또는 오늘 등록된 경우 "14:30" 형태로 제공됩니다.
	var createdAtStr = strings.TrimSpace(rowCells.Eq(rowCells.Length() - 2).Text())
	if article.CreatedAt, err = provider.ParseCreatedAt(createdAtStr); err != nil {
		return nil, err
	}

	return article, nil
}

// extractPhotoNewsArticle 포토뉴스형(boardTypePhotoNews) 게시판의 HTML 항목 하나를 파싱하여 feed.Article 구조체로 변환합니다.
//
// 포토뉴스 게시판은 일반 목록형과 HTML 구조가 완전히 다릅니다.
// 링크와 제목은 "a.item_cont" 앵커 하나에 함께 담겨 있으며, 작성자·등록일은 동일 앵커 내부의 <dl> > <dd> 요소들에서 추출합니다.
//
// 매개변수:
//   - s: 게시글 항목에 해당하는 goquery 선택 객체
//
// 반환값:
//   - *feed.Article: 성공적으로 파싱된 게시글 정보
//   - error: HTML 구조 불일치나 필수 속성 누락 시 apperrors.ParsingFailed 타입의 오류
func (c *crawler) extractPhotoNewsArticle(s *goquery.Selection) (*feed.Article, error) {
	var article = &feed.Article{}

	// -------------------------------------------------------------------------
	// [Step 1] 상세페이지 링크 추출
	//
	// 이 게시판의 항목에서 링크는 "a.item_cont" 앵커 하나에 담겨 있습니다.
	//
	//   <a class="item_cont" href="/board/photo/view?idx=12345">
	//     ...
	//   </a>
	//
	//   - 링크 : 앵커의 "href" 속성값 (.Attr("href"))
	//
	// 앵커는 항목당 정확히 1개여야 합니다.
	// 0개이면 마크업 자체가 없는 것이고, 2개 이상이면 레이아웃이 변경된 것이므로
	// 두 경우 모두 HTML 구조 이상으로 판단하고 파싱을 중단합니다.
	// -------------------------------------------------------------------------
	articleAnchor := s.Find("a.item_cont")
	if articleAnchor.Length() != 1 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글 HTML 요소에서 링크 마크업을 식별할 수 없어 데이터 파싱에 실패했습니다")
	}

	// .Attr("href")는 속성 자체가 없으면 exists=false, 있어도 값이 비어있으면 article.Link=""가 됩니다.
	// 두 경우 모두 상세페이지로 이동할 수 없으므로 파싱 실패로 처리합니다.
	var exists bool
	article.Link, exists = articleAnchor.Attr("href")
	if !exists || article.Link == "" {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글의 상세페이지 링크(href) 속성이 누락되었거나 유효하지 않아 데이터 파싱에 실패했습니다")
	}

	// href가 "http://" 또는 "https://"로 시작하지 않으면 상대 URL로 판단합니다.
	// 상대 URL인 경우 사이트 기본 URL(c.Config().URL)을 앞에 붙여 절대 URL로 변환합니다.
	//   예: "/board/view?idx=12345" → "https://www.yeosu.go.kr/board/view?idx=12345"
	if !strings.HasPrefix(article.Link, "http://") && !strings.HasPrefix(article.Link, "https://") {
		article.Link = fmt.Sprintf("%s%s", c.Config().URL, article.Link)
	}

	// -------------------------------------------------------------------------
	// [Step 2] 제목 추출
	//
	// 제목은 앵커("a.item_cont") 내부의 "div.title_box" 요소에 텍스트로 담겨 있습니다.
	//
	//   <a class="item_cont">
	//     <div class="cont_box">
	//       <div class="title_box">게시글 제목</div>
	//     </div>
	//   </a>
	// -------------------------------------------------------------------------
	titleNode := s.Find("a.item_cont > div.cont_box > div.title_box")
	if titleNode.Length() != 1 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글 HTML 요소에서 제목 마크업을 식별할 수 없어 데이터 파싱에 실패했습니다")
	}

	// 새로 등록된 게시글에는 제목 안에 "새로운글" 이미지(img)나 스팬(span) 태그가 삽입됩니다.
	// .Text() 호출 전에 DOM에서 미리 제거하지 않으면 제목 텍스트에 "새로운글" 문자열이 혼입됩니다.
	titleNode.Find("img[alt*='새로운글'], img[alt*='새글'], span[class*='new']").Remove()

	// .Text()로 앵커의 전체 텍스트를 추출한 뒤 양끝 공백을 제거합니다.
	// 간혹 DOM 제거가 누락된 "새로운글" 접두어가 남아있을 수 있으므로 TrimPrefix로 한 번 더 방어합니다.
	article.Title = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(titleNode.Text()), "새로운글"))

	// -------------------------------------------------------------------------
	// [Step 3] 게시글 ID 추출
	//
	// 여수시청 게시판의 상세페이지 링크에는 쿼리 파라미터 "idx"에 게시글 고유 ID가 담겨 있습니다.
	//   예: "/board/photo/view?idx=12345" → ArticleID = "12345"
	//
	// extractArticleIDFromURL이 URL을 파싱하여 "idx" 파라미터 값을 추출합니다.
	// -------------------------------------------------------------------------
	var err error
	if article.ArticleID, err = c.extractArticleIDFromURL(article.Link); err != nil {
		return nil, err
	}

	// -------------------------------------------------------------------------
	// [Step 4] 작성자 & 등록일 추출
	//
	// 앵커("a.item_cont") 내부의 <dl> > <dd> 요소들에서 메타 정보를 추출합니다.
	//
	//   <a class="item_cont">
	//     <div class="cont_box">
	//       <dl>
	//         <dd>담당부서 홍길동</dd>   ← Eq(0): 작성자
	//         <dd>2024.03.15</dd>      ← Eq(1): 등록일
	//         <dd>...</dd>             ← Eq(2): 기타 (사용 안 함)
	//       </dl>
	//     </div>
	//   </a>
	//
	// <dd>는 반드시 3개여야 합니다.
	// 개수가 다르면 레이아웃 변경으로 간주하고 즉시 에러를 반환합니다.
	// -------------------------------------------------------------------------
	metaItems := s.Find("a.item_cont > div.cont_box > dl > dd")
	if metaItems.Length() != 3 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글의 하위 노드 개수가 규격에 미달하여 작성자 및 등록일 정보를 검출할 수 없습니다")
	}

	// [작성자 파싱]
	// 셀 텍스트 형식: "담당부서명 홍길동" — 공백으로 구분된 마지막 토큰이 실제 이름입니다.
	authorTokens := strings.Split(strings.TrimSpace(metaItems.Eq(0).Text()), " ")
	article.Author = strings.TrimSpace(authorTokens[len(authorTokens)-1])

	// [등록일 파싱]
	// 셀 텍스트 형식: "2024.03.15" 또는 오늘 등록된 경우 "14:30" 형태로 제공됩니다.
	var createdAtStr = strings.TrimSpace(metaItems.Eq(1).Text())
	if article.CreatedAt, err = provider.ParseCreatedAt(createdAtStr); err != nil {
		return nil, err
	}

	return article, nil
}

// extractCardNewsArticle 카드뉴스형(boardTypeCardNews) 게시판의 HTML 항목 하나를 파싱하여 feed.Article 구조체로 변환합니다.
//
// 카드뉴스 게시판은 일반 목록형·포토뉴스형과 HTML 구조가 완전히 다릅니다.
// 링크는 공유 버튼 앵커의 data-url 속성에서, 제목은 h3 요소에서, 작성자·등록일은 dl > dd 메타 요소들에서 추출합니다.
//
// 매개변수:
//   - s: 게시글 항목에 해당하는 goquery 선택 객체
//
// 반환값:
//   - *feed.Article: 성공적으로 파싱된 게시글 정보
//   - error: HTML 구조 불일치나 필수 속성 누락 시 apperrors.ParsingFailed 타입의 오류
func (c *crawler) extractCardNewsArticle(s *goquery.Selection) (*feed.Article, error) {
	var article = &feed.Article{}

	// -------------------------------------------------------------------------
	// [Step 1] 상세페이지 링크 추출
	//
	// 카드뉴스 게시판은 별도의 앵커 대신 공유(share) 버튼의 data-url 속성에 링크가 담겨 있습니다.
	//
	//   <div class="cont_box">
	//     <ul>
	//       <li>
	//         <div class="board_share_box">
	//           <ul>
	//             <li class="share_btn">
	//               <a data-url="/board/cardnews/view?idx=12345">...</a>   ← 파싱 대상
	//             </li>
	//           </ul>
	//         </div>
	//       </li>
	//     </ul>
	//   </div>
	//
	//   - 링크 : 앵커의 「data-url」 속성값 (.Attr("data-url"))
	//
	// 앵커가 1개 이상 존재해야 합니다.
	// 0개이면 공유 버튼 마크업 자체가 없는 것이므로 HTML 구조 이상으로 판단하고 파싱을 중단합니다.
	// -------------------------------------------------------------------------
	articleAnchor := s.Find("div.cont_box ul > li > div.board_share_box > ul > li.share_btn > a")
	if articleAnchor.Length() == 0 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글 HTML 요소에서 링크 마크업을 식별할 수 없어 데이터 파싱에 실패했습니다")
	}

	// .Attr("data-url")는 속성 자체가 없으면 exists=false, 있어도 값이 비어있으면 article.Link=""가 됩니다.
	// 두 경우 모두 상세페이지로 이동할 수 없으므로 파싱 실패로 처리합니다.
	var exists bool
	article.Link, exists = articleAnchor.Attr("data-url")
	if !exists || article.Link == "" {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글의 상세페이지 링크(data-url) 속성이 누락되었거나 유효하지 않아 데이터 파싱에 실패했습니다")
	}

	// data-url이 "http://" 또는 "https://"로 시작하지 않으면 상대 URL로 판단합니다.
	// 상대 URL인 경우 사이트 기본 URL(c.Config().URL)을 앞에 붙여 절대 URL로 변환합니다.
	//   예: "/board/cardnews/view?idx=12345" → "https://www.yeosu.go.kr/board/cardnews/view?idx=12345"
	if !strings.HasPrefix(article.Link, "http://") && !strings.HasPrefix(article.Link, "https://") {
		article.Link = fmt.Sprintf("%s%s", c.Config().URL, article.Link)
	}

	// -------------------------------------------------------------------------
	// [Step 2] 제목 추출
	//
	// 제목은 항목 내부의 "div.cont_box > h3" 요소에 텍스트로 담겨 있습니다.
	//
	//   <div class="cont_box">
	//     <h3>카드뉴스 제목</h3>
	//   </div>
	//
	// h3 요소는 항목당 정확히 1개여야 합니다.
	// 0개이면 마크업 자체가 없는 것이고, 2개 이상이면 레이아웃이 변경된 것이므로
	// 두 경우 모두 HTML 구조 이상으로 판단하고 파싱을 중단합니다.
	// -------------------------------------------------------------------------
	titleNode := s.Find("div.cont_box > h3")
	if titleNode.Length() != 1 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글 HTML 요소에서 제목 마크업을 식별할 수 없어 데이터 파싱에 실패했습니다")
	}

	// .Text()로 앵커의 전체 텍스트를 추출한 뒤 양끝 공백을 제거합니다.
	article.Title = strings.TrimSpace(titleNode.Text())

	// -------------------------------------------------------------------------
	// [Step 3] 게시글 ID 추출
	//
	// 여수시청 게시판의 상세페이지 링크에는 쿼리 파라미터 "idx"에 게시글 고유 ID가 담겨 있습니다.
	//   예: "/board/cardnews/view?idx=12345" → ArticleID = "12345"
	//
	// extractArticleIDFromURL이 URL을 파싱하여 "idx" 파라미터 값을 추출합니다.
	// -------------------------------------------------------------------------
	var err error
	if article.ArticleID, err = c.extractArticleIDFromURL(article.Link); err != nil {
		return nil, err
	}

	// -------------------------------------------------------------------------
	// [Step 4] 작성자 & 등록일 추출
	//
	// 항목 내부의 "div.cont_box > dl > dd" 요소들에서 메타 정보를 추출합니다.
	//
	//   <div class="cont_box">
	//     <dl>
	//       <dd>2024.03.15</dd>  ← Eq(0): 등록일
	//       <dd>홍길동</dd>       ← Eq(1): 작성자(등록자)
	//     </dl>
	//   </div>
	//
	// <dd>는 반드시 2개여야 합니다.
	// 개수가 다르면 레이아웃 변경으로 간주하고 즉시 에러를 반환합니다.
	// -------------------------------------------------------------------------
	metaItems := s.Find("div.cont_box > dl > dd")
	if metaItems.Length() != 2 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글의 하위 노드 개수가 규격에 미달하여 작성자 및 등록일 정보를 검출할 수 없습니다")
	}

	// [작성자 파싱]
	// 셀 텍스트 형식: "홍길동" (등록자명 그대로 노출)
	article.Author = strings.TrimSpace(metaItems.Eq(1).Text())

	// [등록일 파싱]
	// 셀 텍스트 형식: "2024.03.15" 또는 오늘 등록된 경우 "14:30" 형태로 제공됩니다.
	var createdAtStr = strings.TrimSpace(metaItems.Eq(0).Text())
	if article.CreatedAt, err = provider.ParseCreatedAt(createdAtStr); err != nil {
		return nil, err
	}

	return article, nil
}

// crawlArticleContent 주어진 게시글(article)의 상세 페이지를 방문하여 본문(Content)을 article에 직접 채웁니다.
//
// [오류 처리 정책 — 상세 페이지 접근 실패]
// 상세 페이지 HTTP 요청이 실패한 경우 오류 유형에 따라 다르게 처리합니다.
//
//   - apperrors.Forbidden 또는 apperrors.Unauthorized: 서버가 정상 응답했으나 접근 자체가 거부된 경우(예: 비공개 또는 권한 없음)입니다.
//     이 경우 재시도해도 결과는 동일하므로, provider.ErrContentUnavailable을 반환하여
//     상위 루프(CrawlArticleContentsConcurrently)가 해당 게시글을 조용히 건너뛰도록 합니다.
//   - 그 외 오류(네트워크 에러, 타임아웃 등): 일시적인 장애일 수 있으므로 경고 로그(Warn)를 남긴 뒤 오류를 그대로 전파합니다.
//
// [본문 추출]
// 상세 페이지의 본문 컨테이너("div.contbox > div.viewbox") 전체 텍스트를 NormalizeMultiline으로 정규화하여 수집합니다.
// 이후 동일 영역 내 <img> 태그를 순회하여 이미지를 본문 하단에 추가합니다.
// 단, data:image/ 형식의 Base64 인코딩된 인라인 이미지는 데이터 크기가 과도하게 커서
// 스마트폰에서 앱 크래시를 유발할 수 있으므로 수집 대상에서 명시적으로 제외합니다.
//
// 매개변수:
//   - ctx: 요청 타임아웃이나 시스템 종료 시그널에 의해 작업을 취소할 수 있는 컨텍스트
//   - article: 본문을 채워 넣을 대상 게시글 포인터 (Content 필드가 직접 수정됩니다)
//
// 반환값:
//   - nil: 성공적으로 본문을 채운 경우
//   - provider.ErrContentUnavailable: 상세 페이지 접근이 거부된 경우 (상위 루프가 조용히 건너뜁니다)
//   - error: 네트워크 오류 등 일시적 장애가 발생한 경우 (경고 로그 후 오류 전파)
func (c *crawler) crawlArticleContent(ctx context.Context, article *feed.Article) error {
	// -------------------------------------------------------------------------
	// [Step 1] 상세 페이지 HTML 로드
	//
	// FetchHTMLDocument를 통해 게시글 상세 페이지를 GET 방식으로 요청합니다.
	// 오류 발생 시 유형에 따라 처리 방식을 아래와 같이 분기합니다.
	//
	//   - apperrors.Forbidden 또는 apperrors.Unauthorized:
	//     서버가 정상 응답했으나 접근 자체가 거부된 경우(예: 비공개 게시글, 로그인 필요)로 재시도해도 결과가
	//     동일하므로 ErrContentUnavailable을 반환하여 상위 루프가 조용히 건너뛰도록 합니다.
	//   - 그 외 오류(네트워크 에러, 타임아웃 등):
	//     경고 로그(Warn)를 남긴 뒤 오류를 전파합니다.
	// -------------------------------------------------------------------------
	doc, err := c.Scraper().FetchHTMLDocument(ctx, article.Link, nil)
	if err != nil {
		if apperrors.Is(err, apperrors.Forbidden) || apperrors.Is(err, apperrors.Unauthorized) {
			return provider.ErrContentUnavailable
		}

		c.Logger().WithFields(applog.Fields{
			"component":  component,
			"board_id":   article.BoardID,
			"board_name": article.BoardName,
			"article_id": article.ArticleID,
			"link":       article.Link,
			"error":      err.Error(),
		}).Warn(c.Messagef("상세 페이지 수집 실패: 데이터 추출 중 예외 발생"))

		return err
	}

	// -------------------------------------------------------------------------
	// [Step 2] 본문(텍스트) 추출
	//
	// 상세 페이지의 본문 컨테이너("div.contbox > div.viewbox")를 선택합니다.
	// 해당 요소가 존재하지 않으면 비공개 또는 권한 없는 게시글로 간주하고 ErrContentUnavailable을 반환하여
	// 상위 루프가 조용히 건너뛰도록 합니다.
	//
	// 컨테이너 전체 텍스트를 NormalizeMultiline으로 정규화하여 article.Content에 저장합니다.
	// -------------------------------------------------------------------------
	contentNode := doc.Find("div.contbox > div.viewbox")
	if contentNode.Length() == 0 {
		c.Logger().WithFields(applog.Fields{
			"component":  component,
			"board_id":   article.BoardID,
			"board_name": article.BoardName,
			"article_id": article.ArticleID,
			"link":       article.Link,
		}).Warn("본문 수집 실패: 콘텐츠 HTML 컨테이너 식별 불가 (게시글 비공개/권한 없음 추정)")

		return provider.ErrContentUnavailable
	}

	article.Content = strings.TrimSpace(strutil.NormalizeMultiline(contentNode.Text()))

	// -------------------------------------------------------------------------
	// [Step 3] 본문 이미지 추출
	//
	// 본문 컨테이너 내의 모든 <img> 태그를 순회하며 src 속성을 수집합니다.
	// 수집된 이미지는 본문 텍스트 하단에 <img> 태그 형태로 CRLF와 함께 추가합니다.
	// 여수시청 게시글은 이미지에 인라인 스타일(style 속성)이 포함된 경우가 있어 함께 수집합니다.
	//
	// [Base64 인라인 이미지 제외]
	// "data:image/" 접두어로 시작하는 Base64 인코딩 이미지는 데이터 크기가 과도하여
	// 스마트폰 앱 크래시를 유발할 수 있으므로 명시적으로 건너뜁니다.
	//
	// [이미지 URL 정규화]
	// src가 상대 경로("/path/to/img.jpg")인 경우, article.Link(상세 페이지 URL)를 기준으로
	// url.ResolveReference를 통해 절대 URL로 변환합니다.
	// parsedSrc 또는 parsedLink의 URL 파싱이 실패한 경우 해당 이미지는 건너뜁니다.
	// -------------------------------------------------------------------------
	contentNode.Find("img").Each(func(i int, s *goquery.Selection) {
		src, _ := s.Attr("src")
		if src != "" {
			// "data:image/" 형식의 Base64 인코딩 인라인 이미지는 데이터 크기가 과도하게 커서 스마트폰 앱 크래시를 유발하므로 수집 대상에서 제외합니다.
			if strings.HasPrefix(src, "data:image/") {
				return
			}

			parsedSrc, errParseSrc := url.Parse(src)
			parsedLink, errParseLink := url.Parse(article.Link)

			if errParseSrc == nil && errParseLink == nil {
				if article.Content != "" {
					article.Content += "\r\n"
				}

				alt, _ := s.Attr("alt")
				style, _ := s.Attr("style")

				resolvedURL := parsedLink.ResolveReference(parsedSrc).String()
				article.Content += fmt.Sprintf(`<img src="%s" alt="%s" style="%s">`, html.EscapeString(resolvedURL), html.EscapeString(alt), html.EscapeString(style))
			}
		}
	})

	return nil
}

// extractArticleIDFromURL 상세페이지 URL에서 여수시청 게시글의 고유 ID를 추출합니다.
//
// 여수시청 게시판의 상세페이지 URL은 쿼리 파라미터 "idx"에 게시글 고유 ID를 담습니다.
//
//	예: "https://www.yeosu.go.kr/board/view?idx=12345" → "12345"
//
// 매개변수:
//   - link: 상세페이지 절대 URL 문자열
//
// 반환값:
//   - string: 추출된 게시글 고유 ID ("idx" 파라미터 값)
//   - error: URL 파싱 실패 또는 "idx" 파라미터가 없거나 비어있는 경우 apperrors.ParsingFailed 타입의 non-nil 오류
func (c *crawler) extractArticleIDFromURL(link string) (string, error) {
	// url.Parse로 링크 문자열을 구조체로 분해합니다.
	// 파싱 자체가 실패하면 이후 쿼리 파라미터 접근이 불가하므로 즉시 에러를 반환합니다.
	u, err := url.Parse(link)
	if err != nil {
		return "", apperrors.Newf(apperrors.ParsingFailed, "상세페이지 URL 문자열의 형식이 유효하지 않아 데이터 파싱에 실패했습니다 (error:%s)", err)
	}

	// url.ParseQuery로 쿼리 문자열을 키-값 맵으로 변환합니다.
	// "idx" 키가 맵에 존재하고(nil 아님) 첫 번째 값이 비어있지 않아야 유효한 ID입니다.
	// 두 조건 중 하나라도 충족하지 않으면 게시글 ID를 특정할 수 없으므로 아래에서 에러를 반환합니다.
	query, _ := url.ParseQuery(u.RawQuery)
	if query["idx"] != nil && query["idx"][0] != "" {
		return query["idx"][0], nil
	}

	return "", apperrors.New(apperrors.ParsingFailed, "게시글의 고유 식별자(idx) 파라미터가 누락되었거나 유효하지 않아 데이터 파싱에 실패했습니다")
}
