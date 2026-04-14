package ssangbonges

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
// 게시판 유형별로 HTML 구조(CSS 셀렉터, 데이터 위치)가 다르기 때문에 타입에 따라 별도의 파서 함수로 위임합니다.
//
// 지원하는 게시판 유형:
//   - boardTypeList1("L_1"): 번호·제목·작성자·등록일이 표 형식으로 나오는 일반 목록형 게시판
//   - boardTypePhoto1("P_1"): 썸네일 사진이 그리드로 나열되는 포토 갤러리형 게시판
//
// 매개변수:
//   - boardID: 게시판의 고유 ID
//   - boardType: 게시판 유형 식별자
//   - detailURLTemplate: 상세페이지 URL의 경로 템플릿 (boardIDPlaceholder 포함)
//   - s: goquery로 읽어들인 게시글 한 줄(row 또는 li)에 해당하는 HTML 선택 객체
//
// 반환값:
//   - *feed.Article: 파싱된 게시글 정보 (제목, 링크, 작성자, 등록일 등)
//   - error: 지원하지 않는 boardType이거나 개별 필드 파싱에 실패한 경우 non-nil
func (c *crawler) extractArticle(boardID, boardType, detailURLTemplate string, s *goquery.Selection) (*feed.Article, error) {
	switch boardType {
	case boardTypeList1:
		return c.extractList1Article(boardID, detailURLTemplate, s)

	case boardTypePhoto1:
		return c.extractPhoto1Article(boardID, detailURLTemplate, s)

	default:
		return nil, apperrors.Newf(apperrors.System, "지원하지 않는 게시판 유형('%s')이 감지되어 파싱 작업을 수행할 수 없습니다", boardType)
	}
}

// extractList1Article 일반 목록형(boardTypeList1) 게시판의 HTML 행(<tr>) 하나를 파싱하여 feed.Article 구조체로 변환합니다.
//
// 매개변수:
//   - boardID: 게시판 고유 ID (상세페이지 URL 조립에 사용)
//   - detailURLTemplate: boardIDPlaceholder를 포함하는 상세페이지 경로 템플릿
//   - s: <tr> 행에 해당하는 goquery 선택 객체
//
// 반환값:
//   - *feed.Article: 성공적으로 파싱된 게시글 정보
//   - error: HTML 구조 불일치나 필수 속성 누락 시 apperrors.ParsingFailed 타입의 오류
func (c *crawler) extractList1Article(boardID, detailURLTemplate string, s *goquery.Selection) (*feed.Article, error) {
	var article = &feed.Article{}

	// -------------------------------------------------------------------------
	// [Step 1] 제목 & 게시글 ID 추출
	//
	// 이 게시판의 <tr> 행에서 제목과 게시글 ID는 "td.bbs_tit > a" 앵커 하나에 함께 담겨 있습니다.
	//
	//   <td class="bbs_tit">
	//     <a data-id="12345">공지사항 제목</a>
	//   </td>
	//
	//   - 제목     : 앵커의 텍스트 컨텐츠 (.Text())
	//   - 게시글 ID: 앵커의 "data-id" 속성값 (.Attr("data-id"))
	//
	// 앵커는 행당 정확히 1개여야 합니다.
	// 0개이면 셀 자체가 없는 것이고, 2개 이상이면 레이아웃이 변경된 것이므로
	// 두 경우 모두 HTML 구조 이상으로 판단하고 파싱을 중단합니다.
	// -------------------------------------------------------------------------
	articleAnchor := s.Find("td.bbs_tit > a")
	if articleAnchor.Length() != 1 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글 HTML 요소에서 제목 마크업을 식별할 수 없어 데이터 파싱에 실패했습니다")
	}
	article.Title = strings.TrimSpace(articleAnchor.Text())

	// .Attr("data-id")는 속성 자체가 없으면 exists=false, 있어도 값이 비어있으면 ArticleID=""가 됩니다.
	// 두 경우 모두 게시글 ID를 특정할 수 없으므로 파싱 실패로 처리합니다.
	var exists bool
	article.ArticleID, exists = articleAnchor.Attr("data-id")
	if !exists || article.ArticleID == "" {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글의 고유 식별자(ID) 속성이 누락되었거나 유효하지 않아 데이터 파싱에 실패했습니다")
	}

	// -------------------------------------------------------------------------
	// [Step 2] 상세페이지 링크 조립
	//
	// detailURLTemplate 안의 boardIDPlaceholder("#{board_id}")를 실제 boardID로 치환한 뒤,
	// 쿼리 파라미터 "nttSn"에 게시글 ID를 이어 붙여 최종 URL을 완성합니다.
	//
	// 예: "https://example.com/board/#{board_id}/view?nttSn=12345"
	//      → "https://example.com/board/B001/view?nttSn=12345"
	// -------------------------------------------------------------------------
	article.Link = strings.ReplaceAll(fmt.Sprintf("%s%s&nttSn=%s", c.Config().URL, detailURLTemplate, article.ArticleID), boardIDPlaceholder, boardID)

	// -------------------------------------------------------------------------
	// [Step 3] 작성자 & 등록일 추출
	//
	// 행(<tr>) 안의 모든 <td> 셀을 수집한 뒤, 앞이 아닌 '뒤에서부터' 인덱스를 계산하여 대상 셀을 특정합니다.
	// 게시판 HTML은 앞쪽 셀 개수가 가변적(예: 공지 여부에 따라 번호 셀이 추가됨)이지만, 뒷쪽 순서(작성자·등록일)는
	// 항상 고정되어 있어 이 방식이 HTML 구조 변경에 더 강합니다.
	//
	// 예상 셀 배치 (뒤에서):
	//   - rowCells.Length() - 3 : 작성자 셀  → 텍스트 예시: "작성자 홍길동"
	//   - rowCells.Length() - 2 : 등록일 셀  → 텍스트 예시: "등록일 2024.03.15."
	//   - rowCells.Length() - 1 : 조회수 셀  → 파싱 불필요
	//
	// 최소 4개 미만이면 기대하는 셀 자체가 존재할 수 없으므로 즉시 에러를 반환합니다.
	// -------------------------------------------------------------------------
	rowCells := s.Find("td")
	if rowCells.Length() < 4 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글의 하위 노드 개수가 규격에 미달하여 작성자 및 등록일 정보를 검출할 수 없습니다")
	}

	// [작성자 파싱]
	// 셀 텍스트 형식: "작성자 홍길동"
	// "작성자" 접두어는 스크린 리더용 레이블이며, 실제 이름 앞에 항상 붙어 있습니다.
	// 접두어 검증 후 TrimPrefix로 제거하여 순수 이름만 저장합니다.
	author := strings.TrimSpace(rowCells.Eq(rowCells.Length() - 3).Text())
	if !strings.HasPrefix(author, "작성자") {
		return nil, apperrors.New(apperrors.ParsingFailed, "작성자 텍스트의 문자열 패턴이 예상 구조와 일치하지 않아 데이터 파싱에 실패했습니다")
	}
	article.Author = strings.TrimSpace(strings.TrimPrefix(author, "작성자"))

	// [등록일 파싱]
	// 셀 텍스트 형식: "등록일 2024.03.15."
	// Step 1: "등록일" 접두어 검증 → 예상 포맷이 맞는지 확인
	// Step 2: "등록일" 문자열 제거 → " 2024.03.15." 형태로 축소
	// Step 3: 날짜 문자열 정규화 (ParseCreatedAt이 요구하는 "YYYY-MM-DD" 형식으로 변환)
	//         ① 공백 제거(TrimSpace): "2024.03.15."
	//         ② 점 → 대시 치환(ReplaceAll): "2024-03-15-"
	//         ③ 후행 대시 제거(TrimRight): "2024-03-15"
	//         ※ TrimSpace만으로는 후행 '-'가 남아 ParseCreatedAt의 패턴 매칭에 실패합니다.
	var createdAtStr = strings.TrimSpace(rowCells.Eq(rowCells.Length() - 2).Text())
	if !strings.HasPrefix(createdAtStr, "등록일") {
		return nil, apperrors.New(apperrors.ParsingFailed, "등록일 텍스트의 문자열 패턴이 예상 구조와 일치하지 않아 데이터 파싱에 실패했습니다")
	}
	createdAtStr = strings.TrimSpace(strings.TrimPrefix(createdAtStr, "등록일"))
	createdAtStr = strings.TrimRight(strings.TrimSpace(strings.ReplaceAll(createdAtStr, ".", "-")), "-")

	var err error
	if article.CreatedAt, err = provider.ParseCreatedAt(createdAtStr); err != nil {
		return nil, err
	}

	return article, nil
}

// extractPhoto1Article 포토 갤러리형(boardTypePhoto1) 게시판의 HTML 항목(<li>) 하나를 파싱하여 feed.Article 구조체로 변환합니다.
//
// 포토 갤러리는 일반 목록형과 HTML 구조가 완전히 다릅니다.
// 제목은 <a class="selectNttInfo" title="...">, 게시글 ID는 동일 태그의 data-param 속성에서 추출합니다.
// 작성자는 목록 화면에 표시되지 않으며, crawlArticleContent에서 상세 페이지를 통해 보완합니다.
//
// [비공개 포토 게시판(학교앨범) 특수 처리]
// 학교앨범(boardIDSchoolAlbum) 게시판은 비공개로 상세 페이지 접근이 차단되어 있습니다. (단, 늘봄갤러리 등 타 포토 게시판은 접근이 가능하여 본 우회를 적용하지 않습니다.)
// 이 경우 목록 화면의 썸네일 이미지를 본문으로 대체하고, 작성자를 "쌍봉초등학교"로 고정하여 상세 페이지 조회를 우회(Bypass)합니다.
// Content가 미리 채워지면 crawlArticleContent가 content.Content != "" 를 감지하여 스킵합니다.
//
// 매개변수:
//   - boardID: 게시판 고유 ID (학교앨범 판별 및 상세페이지 URL 조립에 사용)
//   - detailURLTemplate: boardIDPlaceholder를 포함하는 상세페이지 경로 템플릿
//   - s: <li> 항목에 해당하는 goquery 선택 객체
//
// 반환값:
//   - *feed.Article: 성공적으로 파싱된 게시글 정보
//   - error: HTML 구조 불일치나 필수 속성 누락 시 apperrors.ParsingFailed 타입의 오류
func (c *crawler) extractPhoto1Article(boardID, detailURLTemplate string, s *goquery.Selection) (*feed.Article, error) {
	var article = &feed.Article{}

	// -------------------------------------------------------------------------
	// [Step 1] 제목 & 게시글 ID 추출
	//
	// 포토 게시판의 <li> 항목에서 제목과 게시글 ID는 "a.selectNttInfo" 앵커 하나에
	// 함께 담겨 있습니다. 일반 목록형과 달리 제목이 태그 텍스트가 아닌 속성값에 저장됩니다.
	//
	//   <a class="selectNttInfo" title="게시글 제목" data-param="12345">...</a>
	//
	//   - 제목     : 앵커의 「title」 속성값 (.Attr("title"))
	//   - 게시글 ID: 앵커의 「data-param」 속성값 (.Attr("data-param"))
	//
	// 앵커는 항목당 정확히 1개여야 합니다.
	// 0개이면 마크업 자체가 없는 것이고, 2개 이상이면 레이아웃이 변경된 것이므로
	// 두 경우 모두 HTML 구조 이상으로 판단하고 파싱을 중단합니다.
	//
	// title 속성이 아예 존재하지 않으면(exists=false) 제목을 특정할 수 없으므로
	// 속성이 없는 경우와 빈 값인 경우를 각각 구분하여 에러를 반환합니다.
	//
	// data-param은 속성 자체가 없거나(exists=false) 값이 비어있으면(ArticleID="")
	// 게시글 ID를 특정할 수 없으므로 두 경우 모두 파싱 실패로 처리합니다.
	// -------------------------------------------------------------------------
	articleAnchor := s.Find("a.selectNttInfo")
	if articleAnchor.Length() != 1 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글 HTML 요소에서 제목 마크업을 식별할 수 없어 데이터 파싱에 실패했습니다")
	}

	var exists bool

	// .Attr("title")는 속성 자체가 없으면 exists=false, 있어도 값이 비어있으면 Title="" 가 됩니다.
	// 두 경우 모두 제목을 특정할 수 없으므로 파싱 실패로 처리합니다.
	article.Title, exists = articleAnchor.Attr("title")
	article.Title = strings.TrimSpace(article.Title)
	if !exists || article.Title == "" {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글 제목 속성이 누락되었거나 유효하지 않아 데이터 파싱에 실패했습니다")
	}

	// .Attr("data-param")는 속성 자체가 없으면 exists=false, 있어도 값이 비어있으면 ArticleID="" 가 됩니다.
	// 두 경우 모두 게시글 ID를 특정할 수 없으므로 파싱 실패로 처리합니다.
	article.ArticleID, exists = articleAnchor.Attr("data-param")
	if !exists || article.ArticleID == "" {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글의 고유 식별자(ID) 속성이 누락되었거나 유효하지 않아 데이터 파싱에 실패했습니다")
	}

	// -------------------------------------------------------------------------
	// [Step 2] 상세페이지 링크 조립
	//
	// detailURLTemplate 안의 boardIDPlaceholder("#{board_id}")를 실제 boardID로 치환한 뒤,
	// 쿼리 파라미터 "nttSn"에 게시글 ID를 이어 붙여 최종 URL을 완성합니다.
	//
	// 예: "https://example.com/board/#{board_id}/view?nttSn=12345"
	//      → "https://example.com/board/B001/view?nttSn=12345"
	// -------------------------------------------------------------------------
	article.Link = strings.ReplaceAll(fmt.Sprintf("%s%s&nttSn=%s", c.Config().URL, detailURLTemplate, article.ArticleID), boardIDPlaceholder, boardID)

	// -------------------------------------------------------------------------
	// [Step 3] 비공개 포토 게시판 특수 처리 — 상세페이지 조회 우회(Bypass)
	//
	// 학교앨범(boardIDSchoolAlbum)은 비공개 게시판으로 상세페이지에 접근할 수 없습니다.
	// (늘봄갤러리 등 타 포토 게시판은 정상 접근이 가능하므로 우회하지 않고 상세 파싱을 수행합니다.)
	// 이 경우 상세페이지 HTTP 요청을 보내는 대신, 목록 화면의 썸네일 이미지를 본문(Content)으로 대체합니다.
	//
	// [썸네일 이미지 파싱]
	// 쌍봉초등학교 포토 게시판의 썸네일은 <img> 태그가 아닌 <span style="background-image:url(...)">
	// 형태로 구현되어 있으므로, style 속성을 파싱하여 이미지 경로를 추출해야 합니다.
	//
	// [이미지 URL 정규화]
	// src 속성이 상대 경로인 경우 url.ResolveReference로 절대 URL로 변환합니다.
	//   정상: baseURL.ResolveReference(relURL) 로 절대 URL 생성
	//   실패: URL 파싱 자체가 실패하면 베이스 URL에 단순 문자열 접합(fallback)으로 처리합니다.
	//
	// [작성자 고정]
	// 목록에 작성자가 표시되지 않고 상세페이지도 접근 불가하므로 "쌍봉초등학교"로 고정합니다.
	// Content가 미리 채워지면 crawlArticleContent가 content.Content != "" 조건을 감지하여
	// 이 게시글의 상세페이지 조회를 자동으로 건너뜁니다.
	// -------------------------------------------------------------------------
	if boardID == boardIDSchoolAlbum {
		var src string

		// [썸네일 이미지 경로 추출]
		// 쌍봉초등학교 포토 게시판의 썸네일은 <img src="..."> 태그가 아닌
		// <div class="img"><span style="background-image:url(/path/to/img.jpg);"></span></div>
		// 형태의 CSS 인라인 스타일로 구현되어 있습니다.
		// 따라서 <span>의 style 속성을 직접 파싱하여 괄호 안의 이미지 경로를 추출합니다.
		imgSpan := s.Find("div.img > span").First()
		if imgSpan.Length() > 0 {
			style, _ := imgSpan.Attr("style")

			// strings.Cut으로 "url(" 문자열을 기준으로 앞/뒤를 분리합니다.
			// ok가 false이면 style 속성에 background-image 값이 없는 것이므로 src는 빈 문자열로 유지됩니다.
			// 예: "background-image:url(/data/.../img.jpg);" → after = "/data/.../img.jpg);"
			if _, after, ok := strings.Cut(style, "url("); ok {
				src = after

				// 닫는 괄호 ")"의 위치를 찾아 이미지 경로의 끝을 확정합니다.
				// 예: "/data/.../img.jpg);" → src = "/data/.../img.jpg"
				if endIdx := strings.Index(src, ")"); endIdx >= 0 {
					src = src[:endIdx]

					// CSS에 따라 경로가 url('/path') 또는 url("/path") 형태로 따옴표로 감싸여 있을 수 있습니다.
					// 양끝의 따옴표를 제거하여 순수한 경로 문자열만 남깁니다.
					src = strings.Trim(src, `"'`)
				} else {
					// 닫는 괄호가 없는 비정상적인 형식이므로 파싱 결과를 폐기합니다.
					src = ""
				}
			}
		}

		if src != "" {
			// 포토 게시판 목록의 썸네일 <span>에는 별도의 alt 텍스트가 없습니다.
			// 스크린 리더 접근성 및 이미지 로드 실패 시 대체 텍스트를 위해 게시글 제목을 alt로 사용합니다.
			alt := article.Title

			// 추출한 이미지 경로(src)가 상대 경로("/path/to/img.jpg")인 경우, 절대 URL로 변환해야 합니다.
			// article.Link(게시글 상세 URL)를 기준으로 삼아 src(상대 경로)를 합산합니다.
			// 두 값 모두 파싱에 성공한 경우에만 url.ResolveReference를 사용하여 안전하게 절대 URL로 변환합니다.
			parsedSrc, errParseSrc := url.Parse(src)
			parsedLink, errParseLink := url.Parse(article.Link)

			if errParseSrc == nil && errParseLink == nil {
				resolvedURL := parsedLink.ResolveReference(parsedSrc).String()
				article.Content = fmt.Sprintf(`<img src="%s" alt="%s">`, html.EscapeString(resolvedURL), html.EscapeString(alt))
			} else {
				// URL 객체 파싱 자체가 실패한 경우(비정상적인 URL 형식 등),
				// 안전한 Fallback으로 사이트 기본 URL에 추출한 경로를 단순 문자열 접합하여 Content를 구성합니다.
				article.Content = fmt.Sprintf(`<img src="%s" alt="%s">`, html.EscapeString(c.Config().URL+src), html.EscapeString(alt))
			}
		}

		article.Author = "쌍봉초등학교"
	}

	// -------------------------------------------------------------------------
	// [Step 4] 등록일 추출
	//
	// 포토 게시판은 아래 HTML 구조로 메타 정보를 제공합니다.
	//
	//   <a class="selectNttInfo">
	//     <p class="txt">
	//       <span class="date">2024.03.15.</span>   ← Eq(0): 등록일
	//       <span class="date">조회 128</span>      ← Eq(1): 조회수 (사용 안 함)
	//     </p>
	//   </a>
	//
	// span.date는 반드시 2개여야 합니다.
	// 개수가 다르면 레이아웃 변경으로 간주하고 즉시 에러를 반환합니다.
	//
	// 날짜 문자열 정규화 (ParseCreatedAt이 요구하는 "YYYY-MM-DD" 형식으로 변환):
	//   ① 접두어 없이 텍스트 그대로 사용: "2024.03.15."
	//   ② 점 → 대시 치환(ReplaceAll): "2024-03-15-"
	//   ③ 후행 대시 제거(TrimRight)  : "2024-03-15"
	//   ※ TrimSpace만으로는 후행 '-'가 남아 ParseCreatedAt의 패턴 매칭에 실패합니다.
	// -------------------------------------------------------------------------
	dateSpans := s.Find("a.selectNttInfo > p.txt > span.date")
	if dateSpans.Length() != 2 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글 하위 노드의 구조가 규격에 맞지 않아 등록일 정보 식별에 실패했습니다")
	}

	// dateSpans.Eq(0)은 첫 번째 span.date(등록일)를 선택합니다.
	// 텍스트가 비어있는 경우 span 태그는 존재하지만 내용이 없는 것이므로 파싱 실패로 처리합니다.
	var createdAtStr = strings.TrimSpace(dateSpans.Eq(0).Text())
	if createdAtStr == "" {
		return nil, apperrors.New(apperrors.ParsingFailed, "등록일 텍스트 데이터가 누락되었거나 비어있어 속성 추출에 실패했습니다")
	}
	createdAtStr = strings.TrimRight(strings.TrimSpace(strings.ReplaceAll(createdAtStr, ".", "-")), "-")

	var err error
	if article.CreatedAt, err = provider.ParseCreatedAt(createdAtStr); err != nil {
		return nil, err
	}

	return article, nil
}

// crawlArticleContent 주어진 게시글(article)의 상세 페이지를 방문하여 본문(Content)과 작성자(Author) 정보를 article에 직접 채웁니다.
//
// [조기 반환 조건 — 이미 본문이 채워진 경우]
// 함수 진입 시점에 article.Content가 이미 비어있지 않으면 상세 페이지 조회 없이 즉시 nil을 반환합니다.
// 이 상황은 비공개 게시판인 학교앨범(boardIDSchoolAlbum)에서 주로 발생합니다.
// 해당 게시판은 권한 문제로 상세 접근이 불가하므로, 상위의 extractPhoto1Article에서
// 목록 화면의 썸네일 이미지를 본문으로 미리 채워두어 이 경로를 통해 스킵됩니다.
//
// [오류 처리 정책 — 상세 페이지 접근 실패]
// 상세 페이지 HTTP 요청이 실패한 경우 오류 유형에 따라 다르게 처리합니다.
//
//   - apperrors.Forbidden 또는 apperrors.Unauthorized: 서버가 정상 응답했으나 접근 자체가 거부된 경우(예: 비공개 또는 권한 없음)입니다.
//     이 경우 재시도해도 결과는 동일하므로, provider.ErrContentUnavailable을 반환하여
//     상위 루프(CrawlArticleContentsConcurrently)가 해당 게시글을 조용히 건너뛰도록 합니다.
//   - 그 외 오류(네트워크 에러, 타임아웃 등): 일시적인 장애일 수 있으므로 경고 로그(Warn)를 남긴 뒤 오류를 그대로 전파합니다.
//
// [작성자 처리 — 포토 게시판]
// 포토 게시판은 목록 화면에 작성자가 노출되지 않아 article.Author가 빈 문자열로 들어옵니다.
// 이 경우 상세 페이지의 메타 영역("div.bbs_ViewA > ul.bbsV_data > li")에서 작성자를 추출합니다.
// 상세 페이지 내 해당 요소가 없거나 "작성자" 접두어 패턴이 맞지 않으면 "쌍봉초등학교"로 대체합니다.
//
// [본문 추출]
// "div.bbs_ViewA > div.bbsV_cont"의 직계 자식 요소를 순서대로 순회하며 텍스트를 수집합니다.
// 이후 동일 영역 내 <img> 태그를 순회하여 이미지를 본문 하단에 추가합니다.
// 단, data:image/ 형식의 Base64 인코딩된 인라인 이미지는 데이터 크기가 과도하게 커서
// 스마트폰에서 앱 크래시를 유발할 수 있으므로 수집 대상에서 명시적으로 제외합니다.
//
// 매개변수:
//   - ctx: 요청 타임아웃이나 시스템 종료 시그널에 의해 작업을 취소할 수 있는 컨텍스트
//   - article: 본문과 작성자를 채워 넣을 대상 게시글 포인터 (Content, Author 필드가 직접 수정됩니다)
//
// 반환값:
//   - nil: 성공적으로 본문을 채웠거나, 이미 본문이 설정되어 있어 조기 반환한 경우
//   - provider.ErrContentUnavailable: 상세 페이지 접근이 거부된 경우 (상위 루프가 조용히 건너뜁니다)
//   - error: 네트워크 오류 등 일시적 장애가 발생한 경우 (경고 로그 후 오류 전파)
func (c *crawler) crawlArticleContent(ctx context.Context, article *feed.Article) error {
	// [조기 반환 — 본문이 이미 채워진 경우]
	// article.Content가 비어있지 않으면 상세 페이지 요청을 생략하고 즉시 반환합니다.
	// CrawlArticleContentsConcurrently의 재시도 루프는 2회차부터 Content를 체크하지만,
	// 1회차는 항상 진입하므로 불필요한 네트워크 요청을 방어하기 위해 여기서도 명시적으로 확인합니다.
	if article.Content != "" {
		return nil
	}

	// -------------------------------------------------------------------------
	// [Step 1] 상세 페이지 HTML 로드
	//
	// fetchHTMLViaPostForm을 통해 게시글 상세 페이지를 POST 방식으로 요청합니다.
	// 오류 발생 시 유형에 따라 처리 방식을 아래와 같이 분기합니다.
	//
	//   - apperrors.Forbidden 또는 apperrors.Unauthorized:
	//     서버가 정상 응답했으나 접근 자체가 거부된 경우(예: 비공개 게시글, 로그인 필요)로 재시도해도 결과가
	//     동일하므로 ErrContentUnavailable을 반환하여 상위 루프가 조용히 건너뛰도록 합니다.
	//   - 그 외 오류(네트워크 에러, 타임아웃 등):
	//     경고 로그(Warn)를 남긴 뒤 오류를 전파합니다.
	// -------------------------------------------------------------------------
	doc, err := c.fetchHTMLViaPostForm(ctx, article.Link, c.Messagef("대상 게시판('%s')의 지정된 게시글(ID: %s) 상세 페이지 접근 및 데이터 수신에 실패했습니다", article.BoardName, article.ArticleID))
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
	// [Step 2] 작성자 추출 — 포토 게시판 전용
	//
	// 포토 게시판의 목록 화면에는 작성자가 표시되지 않으므로,
	// extractPhoto1Article 단계에서 article.Author는 빈 문자열 상태로 남습니다.
	// 이 경우에만 상세 페이지의 메타 영역에서 작성자를 추출합니다.
	//
	// 상세 페이지의 메타 정보는 아래와 같은 HTML 구조로 제공됩니다.
	//
	//   <div class="bbs_ViewA">
	//     <ul class="bbsV_data">
	//       <li>작성자 홍길동</li>         ← Eq(0): 작성자 (파싱 대상)
	//       <li>등록일 2024.03.15.</li>   ← Eq(1): 등록일 (사용 안 함)
	//       <li>조회수 128</li>           ← Eq(2): 조회수 (사용 안 함)
	//     </ul>
	//   </div>
	//
	// <li> 요소가 정확히 3개가 아니거나, 첫 번째 <li>의 텍스트가 "작성자" 접두어로 시작하지 않으면
	// HTML 레이아웃 변경 또는 접근 제한으로 간주하고, 디버그 로그를 남긴 뒤 "쌍봉초등학교"로 대체합니다.
	// -------------------------------------------------------------------------
	if article.Author == "" {
		metaItems := doc.Find("div.bbs_ViewA > ul.bbsV_data > li")
		if metaItems.Length() != 3 {
			c.Logger().WithFields(applog.Fields{
				"component":  component,
				"board_id":   article.BoardID,
				"board_name": article.BoardName,
				"article_id": article.ArticleID,
				"link":       article.Link,
			}).Warn("작성자 수집 실패: HTML 메타 구조 식별 불가 (게시글 비공개/권한 없음 추정)")

			article.Author = "쌍봉초등학교"
		} else {
			author := strings.TrimSpace(metaItems.Eq(0).Text())
			if !strings.HasPrefix(author, "작성자") {
				c.Logger().WithFields(applog.Fields{
					"component":   component,
					"board_id":    article.BoardID,
					"board_name":  article.BoardName,
					"article_id":  article.ArticleID,
					"link":        article.Link,
					"author_text": author,
				}).Warn("작성자 수집 실패: 지정된 텍스트 패턴 불일치 (게시글 비공개/권한 없음 추정)")

				article.Author = "쌍봉초등학교"
			} else {
				article.Author = strings.TrimSpace(strings.TrimPrefix(author, "작성자"))
			}
		}
	}

	// -------------------------------------------------------------------------
	// [Step 3] 본문(텍스트) 추출
	//
	// 상세 페이지의 본문 컨테이너("div.bbs_ViewA > div.bbsV_cont")를 선택합니다.
	// 해당 요소가 존재하지 않으면 비공개 또는 권한 없는 게시글로 간주하고 ErrContentUnavailable을 반환하여
	// 상위 루프가 조용히 건너뛰도록 합니다.
	//
	// 컨테이너의 직계 자식 요소를 하나씩 순회하며 각 블록의 텍스트를 수집합니다.
	// 각 텍스트는 NormalizeMultiline으로 정규화하고, 비어있지 않은 블록만 CRLF("\r\n")로 구분하여
	// article.Content에 순서대로 누적합니다.
	// -------------------------------------------------------------------------
	contentNode := doc.Find("div.bbs_ViewA > div.bbsV_cont")
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

	contentNode.Contents().Each(func(i int, s *goquery.Selection) {
		textChunk := strings.TrimSpace(strutil.NormalizeMultiline(s.Text()))
		if textChunk != "" {
			if article.Content != "" {
				article.Content += "\r\n"
			}

			article.Content += textChunk
		}
	})

	// -------------------------------------------------------------------------
	// [Step 4] 본문 이미지 추출
	//
	// 본문 컨테이너 내의 모든 <img> 태그를 순회하며 src 속성을 수집합니다.
	// 수집된 이미지는 본문 텍스트 하단에 <img> 태그 형태로 CRLF와 함께 추가합니다.
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

				resolvedURL := parsedLink.ResolveReference(parsedSrc).String()
				article.Content += fmt.Sprintf(`<img src="%s" alt="%s">`, html.EscapeString(resolvedURL), html.EscapeString(alt))
			}
		}
	})

	return nil
}
