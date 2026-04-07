package yeosucityhall

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	fetchermocks "github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher/mocks"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// 테스트 헬퍼
// ─────────────────────────────────────────────────────────────────────────────

// newTestCrawler 파서 테스트용 crawler를 생성합니다.
func newTestCrawler(f ...*fetchermocks.MockFetcher) *crawler {
	var mf *fetchermocks.MockFetcher
	if len(f) > 0 {
		mf = f[0]
	} else {
		mf = fetchermocks.NewMockFetcher()
	}
	r := new(mockFeedRepo)
	return setupTestCrawler(nil, mf, r, nil)
}

// trSelectionFromHTML 테이블 HTML에서 첫 번째 <tr>의 goquery.Selection을 반환합니다.
func trSelectionFromHTML(t *testing.T, rawHTML string) *goquery.Selection {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	require.NoError(t, err)
	return doc.Find("tr").First()
}

// divSelectionFromHTML 주어진 HTML에서 selector에 해당하는 첫 번째 요소의 goquery.Selection을 반환합니다.
func divSelectionFromHTML(t *testing.T, rawHTML, selector string) *goquery.Selection {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	require.NoError(t, err)
	return doc.Find(selector).First()
}

// ─────────────────────────────────────────────────────────────────────────────
// extractArticle (분기 라우팅 테스트)
// ─────────────────────────────────────────────────────────────────────────────

func TestExtractArticle_UnsupportedBoardType(t *testing.T) {
	c := newTestCrawler()
	s := trSelectionFromHTML(t, `<table><tbody><tr><td>dummy</td></tr></tbody></table>`)

	_, err := c.extractArticle("UNKNOWN_TYPE", s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.System))
}

func TestExtractArticle_RoutesTo_ListArticle_L1(t *testing.T) {
	c := newTestCrawler()
	s := trSelectionFromHTML(t, `<table><tbody><tr>
		<td>1</td>
		<td class="align_left"><a href="/www/govt/news/notice?idx=100&mode=view" class="basic_cont">제목A</a></td>
		<td>청년정책관</td>
		<td>2024-03-15</td>
		<td>10</td>
	</tr></tbody></table>`)

	article, err := c.extractArticle(boardTypeList1, s)

	require.NoError(t, err)
	assert.Equal(t, "100", article.ArticleID)
	assert.Equal(t, "제목A", article.Title)
}

func TestExtractArticle_RoutesTo_PhotoNewsArticle(t *testing.T) {
	c := newTestCrawler()
	s := divSelectionFromHTML(t, `<div class="board_list">
		<div class="item">
			<a href="/www/govt/news/hotnews?idx=200&mode=view" class="item_cont">
				<div class="cont_box">
					<div class="title_box">포토제목</div>
					<dl>
						<dt class="text_hidden">작성자: </dt><dd>홍보팀 홍길동</dd>
						<dt class="text_hidden">날짜: </dt><dd>2024-03-15</dd>
						<dt>조회</dt><dd>5</dd>
					</dl>
				</div>
			</a>
		</div>
	</div>`, "div.item")

	article, err := c.extractArticle(boardTypePhotoNews, s)

	require.NoError(t, err)
	assert.Equal(t, "200", article.ArticleID)
}

func TestExtractArticle_RoutesTo_CardNewsArticle(t *testing.T) {
	c := newTestCrawler()
	s := divSelectionFromHTML(t, `<div class="board_list">
		<div class="item">
			<div class="cont_box">
				<div class="top_util"><ul><li class="share_wrap">
					<div class="board_share_box"><ul class="share_btn">
						<li class="facebook share_btn"><a href="#none" class="share_btn" data-url="/www/govt/news/card_news?idx=300&mode=view">페북</a></li>
					</ul></div>
				</li></ul></div>
				<h3>카드제목</h3>
				<dl>
					<dt class="text_hidden">날짜</dt><dd>2024-03-15</dd>
					<dt class="text_hidden">작성자</dt><dd>홍길동</dd>
				</dl>
			</div>
		</div>
	</div>`, "div.item")

	article, err := c.extractArticle(boardTypeCardNews, s)

	require.NoError(t, err)
	assert.Equal(t, "300", article.ArticleID)
}

// ─────────────────────────────────────────────────────────────────────────────
// extractListArticle (L_1 / L_2)
// ─────────────────────────────────────────────────────────────────────────────

// newListRow L_1 테스트용 <tr> HTML을 생성합니다.
func newListRow(idx, title, author, date string) string {
	return `<table><tbody><tr>
		<td>1</td>
		<td class="align_left"><a href="/www/govt/news/notice?idx=` + idx + `&mode=view" class="basic_cont">` + title + `</a></td>
		<td>` + author + `</td>
		<td>` + date + `</td>
		<td>10</td>
	</tr></tbody></table>`
}

func TestExtractListArticle_Success_L1(t *testing.T) {
	c := newTestCrawler()
	s := trSelectionFromHTML(t, newListRow("9999", "공지 테스트", "청년인구정책관", "2024-03-15"))

	article, err := c.extractListArticle(boardTypeList1, s)

	require.NoError(t, err)
	assert.Equal(t, "9999", article.ArticleID)
	assert.Equal(t, "공지 테스트", article.Title)
	assert.Equal(t, "청년인구정책관", article.Author)
	assert.Equal(t, "https://www.yeosu.go.kr/www/govt/news/notice?idx=9999&mode=view", article.Link)
	assert.Equal(t, 2024, article.CreatedAt.Year())
	assert.Equal(t, 3, int(article.CreatedAt.Month()))
	assert.Equal(t, 15, article.CreatedAt.Day())
}

func TestExtractListArticle_Success_L2_WithCategory(t *testing.T) {
	c := newTestCrawler()
	s := trSelectionFromHTML(t, `<table><tbody><tr>
		<td>1</td>
		<td class="list_cate">공지</td>
		<td class="align_left"><a href="/www/govt/news/release?idx=100&mode=view" class="basic_cont">제목</a></td>
		<td>홍보팀</td>
		<td>2024-03-15</td>
		<td>5</td>
	</tr></tbody></table>`)

	article, err := c.extractListArticle(boardTypeList2, s)

	require.NoError(t, err)
	assert.Equal(t, "[ 공지 ] 제목", article.Title)
	assert.Equal(t, "100", article.ArticleID)
}

func TestExtractListArticle_Success_L2_EmptyCategory(t *testing.T) {
	c := newTestCrawler()
	s := trSelectionFromHTML(t, `<table><tbody><tr>
		<td>1</td>
		<td class="list_cate"></td>
		<td class="align_left"><a href="/www/govt/news/release?idx=101&mode=view" class="basic_cont">빈분류제목</a></td>
		<td>홍보팀</td>
		<td>2024-03-15</td>
		<td>2</td>
	</tr></tbody></table>`)

	article, err := c.extractListArticle(boardTypeList2, s)

	require.NoError(t, err)
	// 분류가 비어있으면 접두어 없이 원래 제목 그대로
	assert.Equal(t, "빈분류제목", article.Title)
}

func TestExtractListArticle_TitleAnchorMissing(t *testing.T) {
	c := newTestCrawler()
	s := trSelectionFromHTML(t, `<table><tbody><tr>
		<td>1</td><td class="align_left"><span>앵커없음</span></td>
		<td>관리자</td><td>2024-01-01</td><td>0</td>
	</tr></tbody></table>`)

	_, err := c.extractListArticle(boardTypeList1, s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractListArticle_TitleAnchorMultiple(t *testing.T) {
	c := newTestCrawler()
	s := trSelectionFromHTML(t, `<table><tbody><tr>
		<td class="align_left">
			<a href="/w?idx=1" class="basic_cont">제목1</a>
			<a href="/w?idx=2" class="basic_cont">제목2</a>
		</td>
		<td>관리자</td><td>2024-01-01</td><td>0</td><td>0</td>
	</tr></tbody></table>`)

	_, err := c.extractListArticle(boardTypeList1, s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractListArticle_HrefMissing(t *testing.T) {
	c := newTestCrawler()
	s := trSelectionFromHTML(t, `<table><tbody><tr>
		<td class="align_left"><a class="basic_cont">href없음</a></td>
		<td>관리자</td><td>2024-01-01</td><td>0</td><td>0</td>
	</tr></tbody></table>`)

	_, err := c.extractListArticle(boardTypeList1, s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractListArticle_HrefEmpty(t *testing.T) {
	c := newTestCrawler()
	s := trSelectionFromHTML(t, `<table><tbody><tr>
		<td class="align_left"><a href="" class="basic_cont">빈href</a></td>
		<td>관리자</td><td>2024-01-01</td><td>0</td><td>0</td>
	</tr></tbody></table>`)

	_, err := c.extractListArticle(boardTypeList1, s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractListArticle_RelativeURL_Normalized(t *testing.T) {
	c := newTestCrawler()
	s := trSelectionFromHTML(t, newListRow("777", "링크확인", "관리자", "2024-01-01"))

	article, err := c.extractListArticle(boardTypeList1, s)

	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(article.Link, "https://www.yeosu.go.kr"))
	assert.Contains(t, article.Link, "idx=777")
}

func TestExtractListArticle_NewArticleIconRemoved(t *testing.T) {
	c := newTestCrawler()
	s := trSelectionFromHTML(t, `<table><tbody><tr>
		<td>1</td>
		<td class="align_left">
			<a href="/www/govt/news/notice?idx=500&mode=view" class="basic_cont">
				진짜제목<span class="icon_box"><span class="icon_new">새로운글</span></span>
			</a>
		</td>
		<td>관리자</td><td>2024-03-15</td><td>0</td>
	</tr></tbody></table>`)

	article, err := c.extractListArticle(boardTypeList1, s)

	require.NoError(t, err)
	assert.Equal(t, "진짜제목", article.Title)
	assert.NotContains(t, article.Title, "새로운글")
}

func TestExtractListArticle_L2_CategoryCellMissing(t *testing.T) {
	c := newTestCrawler()
	s := trSelectionFromHTML(t, newListRow("1", "제목", "관리자", "2024-01-01"))

	_, err := c.extractListArticle(boardTypeList2, s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractListArticle_TooFewCells(t *testing.T) {
	c := newTestCrawler()
	s := trSelectionFromHTML(t, `<table><tbody><tr>
		<td class="align_left"><a href="/w?idx=1" class="basic_cont">제목</a></td>
		<td>관리자</td><td>2024-01-01</td><td>0</td>
	</tr></tbody></table>`)

	_, err := c.extractListArticle(boardTypeList1, s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractListArticle_DateToday_HHMMSSFormat(t *testing.T) {
	c := newTestCrawler()
	s := trSelectionFromHTML(t, newListRow("42", "오늘글", "관리자", "10:02:47"))

	article, err := c.extractListArticle(boardTypeList1, s)

	require.NoError(t, err)
	assert.False(t, article.CreatedAt.IsZero())
}

func TestExtractListArticle_DatePast_DotFormat(t *testing.T) {
	c := newTestCrawler()
	// "2024.03.15." 형식 (후행 점 포함)
	s := trSelectionFromHTML(t, newListRow("43", "과거글", "관리자", "2024.03.15."))

	article, err := c.extractListArticle(boardTypeList1, s)

	require.NoError(t, err)
	assert.Equal(t, 2024, article.CreatedAt.Year())
	assert.Equal(t, 3, int(article.CreatedAt.Month()))
	assert.Equal(t, 15, article.CreatedAt.Day())
}

// ─────────────────────────────────────────────────────────────────────────────
// extractPhotoNewsArticle (P)
// ─────────────────────────────────────────────────────────────────────────────

// newPhotoItem 포토뉴스 <div class="item"> HTML을 생성합니다.
func newPhotoItem(idx, titleText, author, date string) string {
	return `<div class="board_list"><div class="item">
		<a href="/www/govt/news/hotnews?idx=` + idx + `&mode=view" class="item_cont">
			<div class="cont_box">
				<div class="title_box">` + titleText + `</div>
				<dl>
					<dt class="text_hidden">작성자: </dt><dd>` + author + `</dd>
					<dt class="text_hidden">날짜: </dt><dd>` + date + `</dd>
					<dt>조회</dt><dd>10</dd>
				</dl>
			</div>
		</a>
	</div></div>`
}

func TestExtractPhotoNewsArticle_Success(t *testing.T) {
	c := newTestCrawler()
	s := divSelectionFromHTML(t, newPhotoItem("500", "포토제목500", "홍보팀 홍길동", "2024-03-15"), "div.item")

	article, err := c.extractPhotoNewsArticle(s)

	require.NoError(t, err)
	assert.Equal(t, "500", article.ArticleID)
	assert.Equal(t, "포토제목500", article.Title)
	assert.Equal(t, "홍길동", article.Author) // 마지막 토큰
	assert.Equal(t, 2024, article.CreatedAt.Year())
	assert.Contains(t, article.Link, "idx=500")
}

func TestExtractPhotoNewsArticle_AuthorSingleToken(t *testing.T) {
	c := newTestCrawler()
	s := divSelectionFromHTML(t, newPhotoItem("501", "제목", "주차차량팀", "2024-03-15"), "div.item")

	article, err := c.extractPhotoNewsArticle(s)

	require.NoError(t, err)
	assert.Equal(t, "주차차량팀", article.Author)
}

func TestExtractPhotoNewsArticle_AnchorMissing(t *testing.T) {
	c := newTestCrawler()
	s := divSelectionFromHTML(t, `<div class="board_list"><div class="item"><span>앵커없음</span></div></div>`, "div.item")

	_, err := c.extractPhotoNewsArticle(s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractPhotoNewsArticle_HrefMissing(t *testing.T) {
	c := newTestCrawler()
	s := divSelectionFromHTML(t, `<div class="board_list"><div class="item">
		<a class="item_cont"><div class="cont_box"><div class="title_box">제목</div></div></a>
	</div></div>`, "div.item")

	_, err := c.extractPhotoNewsArticle(s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractPhotoNewsArticle_TitleNodeMissing(t *testing.T) {
	c := newTestCrawler()
	s := divSelectionFromHTML(t, `<div class="board_list"><div class="item">
		<a href="/www/govt/news/hotnews?idx=1&mode=view" class="item_cont">
			<div class="cont_box"><div class="NO_title_box">제목</div></div>
		</a>
	</div></div>`, "div.item")

	_, err := c.extractPhotoNewsArticle(s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractPhotoNewsArticle_MetaCountMismatch(t *testing.T) {
	c := newTestCrawler()
	// dd가 2개 (3개 아님)
	s := divSelectionFromHTML(t, `<div class="board_list"><div class="item">
		<a href="/www/govt/news/hotnews?idx=1&mode=view" class="item_cont">
			<div class="cont_box">
				<div class="title_box">제목</div>
				<dl><dd>홍길동</dd><dd>2024-03-15</dd></dl>
			</div>
		</a>
	</div></div>`, "div.item")

	_, err := c.extractPhotoNewsArticle(s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractPhotoNewsArticle_NewArticleIconRemoved(t *testing.T) {
	c := newTestCrawler()
	s := divSelectionFromHTML(t, `<div class="board_list"><div class="item">
		<a href="/www/govt/news/hotnews?idx=600&mode=view" class="item_cont">
			<div class="cont_box">
				<div class="title_box">
					진짜포토제목<span class="icon_box"><span class="icon_new">새로운글</span></span>
				</div>
				<dl>
					<dd>홍보팀 홍길동</dd><dd>2024-03-15</dd><dd>5</dd>
				</dl>
			</div>
		</a>
	</div></div>`, "div.item")

	article, err := c.extractPhotoNewsArticle(s)

	require.NoError(t, err)
	assert.Equal(t, "진짜포토제목", article.Title)
	assert.NotContains(t, article.Title, "새로운글")
}

// ─────────────────────────────────────────────────────────────────────────────
// extractCardNewsArticle (C)
// ─────────────────────────────────────────────────────────────────────────────

// newCardItem 카드뉴스 <div class="item"> HTML을 생성합니다.
func newCardItem(idx, title, date, author string) string {
	return `<div class="board_list"><div class="item">
		<div class="cont_box">
			<div class="top_util"><ul><li class="share_wrap">
				<div class="board_share_box"><ul class="share_btn">
					<li class="facebook share_btn"><a href="#none" class="share_btn" data-url="/www/govt/news/card_news?idx=` + idx + `&mode=view">페북</a></li>
					<li class="twitter share_btn"><a href="#none" class="share_btn" data-url="/www/govt/news/card_news?idx=` + idx + `&mode=view">트위터</a></li>
					<li class="kakaostory share_btn"><a href="#none" class="share_btn" data-url="/www/govt/news/card_news?idx=` + idx + `&mode=view">카카오</a></li>
					<li class="band share_btn"><a href="#none" class="share_btn" data-url="/www/govt/news/card_news?idx=` + idx + `&mode=view">밴드</a></li>
				</ul></div>
			</li></ul></div>
			<h3>` + title + `</h3>
			<dl>
				<dt class="text_hidden">날짜</dt><dd>` + date + `</dd>
				<dt class="text_hidden">작성자</dt><dd>` + author + `</dd>
			</dl>
		</div>
	</div></div>`
}

func TestExtractCardNewsArticle_Success(t *testing.T) {
	c := newTestCrawler()
	s := divSelectionFromHTML(t, newCardItem("700", "카드제목700", "2024-03-15", "홍길동"), "div.item")

	article, err := c.extractCardNewsArticle(s)

	require.NoError(t, err)
	assert.Equal(t, "700", article.ArticleID)
	assert.Equal(t, "카드제목700", article.Title)
	assert.Equal(t, "홍길동", article.Author)
	assert.Equal(t, 2024, article.CreatedAt.Year())
	assert.Contains(t, article.Link, "https://www.yeosu.go.kr")
	assert.Contains(t, article.Link, "idx=700")
}

func TestExtractCardNewsArticle_MultipleShareBtns_FirstURLUsed(t *testing.T) {
	// 실제 HTML에서 공유 버튼 a 태그가 4개 → 첫 번째 data-url 사용 (정상 케이스)
	c := newTestCrawler()
	s := divSelectionFromHTML(t, newCardItem("800", "4개공유버튼", "2024-03-15", "관리자"), "div.item")

	article, err := c.extractCardNewsArticle(s)

	require.NoError(t, err)
	assert.Equal(t, "800", article.ArticleID)
	assert.Contains(t, article.Link, "idx=800")
}

func TestExtractCardNewsArticle_ShareBtnAnchorMissing(t *testing.T) {
	c := newTestCrawler()
	s := divSelectionFromHTML(t, `<div class="board_list"><div class="item">
		<div class="cont_box">
			<h3>제목</h3>
			<dl><dd>2024-03-15</dd><dd>관리자</dd></dl>
		</div>
	</div></div>`, "div.item")

	_, err := c.extractCardNewsArticle(s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractCardNewsArticle_DataURLMissing(t *testing.T) {
	c := newTestCrawler()
	s := divSelectionFromHTML(t, `<div class="board_list"><div class="item">
		<div class="cont_box">
			<div class="top_util"><ul><li class="share_wrap">
				<div class="board_share_box"><ul class="share_btn">
					<li class="facebook share_btn"><a href="#none" class="share_btn">data-url없음</a></li>
				</ul></div>
			</li></ul></div>
			<h3>제목</h3>
			<dl><dd>2024-03-15</dd><dd>관리자</dd></dl>
		</div>
	</div></div>`, "div.item")

	_, err := c.extractCardNewsArticle(s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractCardNewsArticle_DataURLEmpty(t *testing.T) {
	c := newTestCrawler()
	s := divSelectionFromHTML(t, `<div class="board_list"><div class="item">
		<div class="cont_box">
			<div class="top_util"><ul><li class="share_wrap">
				<div class="board_share_box"><ul class="share_btn">
					<li class="facebook share_btn"><a href="#none" class="share_btn" data-url="">빈URL</a></li>
				</ul></div>
			</li></ul></div>
			<h3>제목</h3>
			<dl><dd>2024-03-15</dd><dd>관리자</dd></dl>
		</div>
	</div></div>`, "div.item")

	_, err := c.extractCardNewsArticle(s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractCardNewsArticle_RelativeDataURL_Normalized(t *testing.T) {
	c := newTestCrawler()
	s := divSelectionFromHTML(t, newCardItem("900", "상대URL테스트", "2024-03-15", "관리자"), "div.item")

	article, err := c.extractCardNewsArticle(s)

	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(article.Link, "https://www.yeosu.go.kr"))
}

func TestExtractCardNewsArticle_TitleNodeMissing(t *testing.T) {
	c := newTestCrawler()
	s := divSelectionFromHTML(t, `<div class="board_list"><div class="item">
		<div class="cont_box">
			<div class="top_util"><ul><li class="share_wrap">
				<div class="board_share_box"><ul class="share_btn">
					<li class="facebook share_btn"><a href="#none" class="share_btn" data-url="/card_news?idx=1&mode=view">페북</a></li>
				</ul></div>
			</li></ul></div>
			<dl><dd>2024-03-15</dd><dd>관리자</dd></dl>
		</div>
	</div></div>`, "div.item")

	_, err := c.extractCardNewsArticle(s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractCardNewsArticle_MetaCountMismatch(t *testing.T) {
	c := newTestCrawler()
	// dd가 1개 (2개 아님)
	s := divSelectionFromHTML(t, `<div class="board_list"><div class="item">
		<div class="cont_box">
			<div class="top_util"><ul><li class="share_wrap">
				<div class="board_share_box"><ul class="share_btn">
					<li class="facebook share_btn"><a href="#none" class="share_btn" data-url="/card_news?idx=1&mode=view">페북</a></li>
				</ul></div>
			</li></ul></div>
			<h3>제목</h3>
			<dl><dd>2024-03-15</dd></dl>
		</div>
	</div></div>`, "div.item")

	_, err := c.extractCardNewsArticle(s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

// ─────────────────────────────────────────────────────────────────────────────
// extractArticleIDFromURL
// ─────────────────────────────────────────────────────────────────────────────

func TestExtractArticleIDFromURL_Success(t *testing.T) {
	c := newTestCrawler()

	id, err := c.extractArticleIDFromURL("https://www.yeosu.go.kr/www/govt/news/notice?idx=12345&mode=view")

	require.NoError(t, err)
	assert.Equal(t, "12345", id)
}

func TestExtractArticleIDFromURL_InvalidURL(t *testing.T) {
	c := newTestCrawler()

	_, err := c.extractArticleIDFromURL("://invalid-url")

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractArticleIDFromURL_IdxParamMissing(t *testing.T) {
	c := newTestCrawler()

	_, err := c.extractArticleIDFromURL("https://www.yeosu.go.kr/www/govt/news/notice?mode=view")

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractArticleIDFromURL_IdxParamEmpty(t *testing.T) {
	c := newTestCrawler()

	_, err := c.extractArticleIDFromURL("https://www.yeosu.go.kr/www/govt/news/notice?idx=&mode=view")

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

// ─────────────────────────────────────────────────────────────────────────────
// crawlArticleContent
// ─────────────────────────────────────────────────────────────────────────────

// makeCrawlerAndArticle crawlArticleContent 테스트용 crawler와 article을 반환합니다.
func makeCrawlerAndArticle(t *testing.T) (*crawler, *fetchermocks.MockFetcher, *feed.Article) {
	t.Helper()
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)
	c := setupTestCrawler(t, f, r, nil)
	article := &feed.Article{
		BoardID:   "notice",
		BoardName: "공지사항",
		ArticleID: "99",
		Title:     "테스트글",
		Author:    "관리자",
		Link:      "https://www.yeosu.go.kr/www/govt/news/notice?idx=99&mode=view",
	}
	return c, f, article
}

func TestCrawlArticleContent_ForbiddenResponse_ReturnsErrContentUnavailable(t *testing.T) {
	c, f, article := makeCrawlerAndArticle(t)
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse("", http.StatusForbidden), nil)

	err := c.crawlArticleContent(context.Background(), article)

	assert.ErrorIs(t, err, provider.ErrContentUnavailable)
}

func TestCrawlArticleContent_UnauthorizedResponse_ReturnsErrContentUnavailable(t *testing.T) {
	c, f, article := makeCrawlerAndArticle(t)
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse("", http.StatusUnauthorized), nil)

	err := c.crawlArticleContent(context.Background(), article)

	assert.ErrorIs(t, err, provider.ErrContentUnavailable)
}

func TestCrawlArticleContent_NetworkError_Propagated(t *testing.T) {
	c, f, article := makeCrawlerAndArticle(t)
	f.On("Do", mock.Anything).Return((*http.Response)(nil), apperrors.New(apperrors.ExecutionFailed, "일시적 네트워크 에러"))

	err := c.crawlArticleContent(context.Background(), article)

	require.Error(t, err)
	assert.NotErrorIs(t, err, provider.ErrContentUnavailable)
}

func TestCrawlArticleContent_ContentNodeMissing_ReturnsErrContentUnavailable(t *testing.T) {
	c, f, article := makeCrawlerAndArticle(t)
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(
		`<html><body><div class="other">본문없음</div></body></html>`,
		http.StatusOK,
	), nil)

	err := c.crawlArticleContent(context.Background(), article)

	assert.ErrorIs(t, err, provider.ErrContentUnavailable)
}

func TestCrawlArticleContent_TextExtracted(t *testing.T) {
	c, f, article := makeCrawlerAndArticle(t)
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(
		`<html><body><div class="contbox"><div class="viewbox"><p>첫 번째 단락</p><p>두 번째 단락</p></div></div></body></html>`,
		http.StatusOK,
	), nil)

	err := c.crawlArticleContent(context.Background(), article)

	require.NoError(t, err)
	assert.Contains(t, article.Content, "첫 번째 단락")
	assert.Contains(t, article.Content, "두 번째 단락")
}

func TestCrawlArticleContent_ImageExtracted_RelativeURL(t *testing.T) {
	c, f, article := makeCrawlerAndArticle(t)
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(
		`<html><body><div class="contbox"><div class="viewbox"><img src="/data/upload/img.jpg" alt="이미지설명"></div></div></body></html>`,
		http.StatusOK,
	), nil)

	err := c.crawlArticleContent(context.Background(), article)

	require.NoError(t, err)
	assert.Contains(t, article.Content, "https://www.yeosu.go.kr/data/upload/img.jpg")
	assert.Contains(t, article.Content, `alt="이미지설명"`)
}

func TestCrawlArticleContent_ImageWithStyle(t *testing.T) {
	c, f, article := makeCrawlerAndArticle(t)
	// 여수시청 게시글은 img에 style 속성이 포함된 경우가 있음
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(
		`<html><body><div class="contbox"><div class="viewbox"><img src="/data/img.jpg" alt="설명" style="width:100%"></div></div></body></html>`,
		http.StatusOK,
	), nil)

	err := c.crawlArticleContent(context.Background(), article)

	require.NoError(t, err)
	assert.Contains(t, article.Content, `style="width:100%"`)
}

func TestCrawlArticleContent_ImageSkipped_Base64Inline(t *testing.T) {
	c, f, article := makeCrawlerAndArticle(t)
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(
		`<html><body><div class="contbox"><div class="viewbox"><p>텍스트</p><img src="data:image/png;base64,iVBORw0KGgo=" alt="base64"></div></div></body></html>`,
		http.StatusOK,
	), nil)

	err := c.crawlArticleContent(context.Background(), article)

	require.NoError(t, err)
	assert.NotContains(t, article.Content, "data:image/")
	assert.Contains(t, article.Content, "텍스트")
}

func TestCrawlArticleContent_ImageSkipped_EmptySrc(t *testing.T) {
	c, f, article := makeCrawlerAndArticle(t)
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(
		`<html><body><div class="contbox"><div class="viewbox"><p>텍스트만</p><img src="" alt="빈src"></div></div></body></html>`,
		http.StatusOK,
	), nil)

	err := c.crawlArticleContent(context.Background(), article)

	require.NoError(t, err)
	assert.NotContains(t, article.Content, "<img")
	assert.Contains(t, article.Content, "텍스트만")
}

func TestCrawlArticleContent_MultipleImages_AppendedInOrder(t *testing.T) {
	c, f, article := makeCrawlerAndArticle(t)
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(
		`<html><body><div class="contbox"><div class="viewbox">
			<p>본문텍스트</p>
			<img src="/data/first.jpg" alt="첫번째">
			<img src="/data/second.jpg" alt="두번째">
		</div></div></body></html>`,
		http.StatusOK,
	), nil)

	err := c.crawlArticleContent(context.Background(), article)

	require.NoError(t, err)
	firstIdx := strings.Index(article.Content, "first.jpg")
	secondIdx := strings.Index(article.Content, "second.jpg")
	assert.True(t, firstIdx < secondIdx, "첫 번째 이미지가 두 번째 이미지보다 앞에 와야 함")
}

func TestCrawlArticleContent_SuccessReturnNil(t *testing.T) {
	c, f, article := makeCrawlerAndArticle(t)
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(
		`<html><body><div class="contbox"><div class="viewbox"><p>정상 본문입니다.</p></div></div></body></html>`,
		http.StatusOK,
	), nil)

	err := c.crawlArticleContent(context.Background(), article)

	assert.NoError(t, err)
	assert.NotEmpty(t, article.Content)
}
