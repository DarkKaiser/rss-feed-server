package ssangbonges

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

// newTestCrawler 공통 파서 테스트용 crawler를 생성합니다.
// 실제 HTTP 요청이 필요한 경우 f에 MockFetcher를 주입하고, 그렇지 않으면 nil을 넘기면 됩니다.
func newTestCrawler(f ...*fetchermocks.MockFetcher) *crawler {
	var fetcher *fetchermocks.MockFetcher
	if len(f) > 0 {
		fetcher = f[0]
	} else {
		fetcher = fetchermocks.NewMockFetcher()
	}

	repo := new(mockFeedRepo)
	return setupTestCrawler(nil, fetcher, repo, nil)
}

// selectionFromHTML `<table><tbody><tr>...</tr></tbody></table>` 형태의 HTML에서
// 첫 번째 <tr> 행의 goquery.Selection을 반환합니다.
func selectionFromHTML(t *testing.T, rowHTML string) *goquery.Selection {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rowHTML))
	require.NoError(t, err)
	return doc.Find("tr").First()
}

// liSelectionFromHTML `<ul>...</ul>` 안의 첫 번째 <li>의 goquery.Selection을 반환합니다.
func liSelectionFromHTML(t *testing.T, liHTML string) *goquery.Selection {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(liHTML))
	require.NoError(t, err)
	return doc.Find("li").First()
}

// ─────────────────────────────────────────────────────────────────────────────
// extractArticle (분기 라우팅 테스트)
// ─────────────────────────────────────────────────────────────────────────────

func TestExtractArticle_UnsupportedBoardType(t *testing.T) {
	c := newTestCrawler()
	s := selectionFromHTML(t, `<table><tbody><tr><td>dummy</td></tr></tbody></table>`)

	_, err := c.extractArticle("123", "UNKNOWN_TYPE", "/template/#{board_id}", s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.System))
}

func TestExtractArticle_RoutesToList1(t *testing.T) {
	c := newTestCrawler()
	rowHTML := `<table><tbody><tr>
		<td>1</td>
		<td class="bbs_tit"><a data-id="100">제목A</a></td>
		<td>작성자 홍길동</td>
		<td>등록일 2024.03.15.</td>
		<td>조회수</td>
	</tr></tbody></table>`
	s := selectionFromHTML(t, rowHTML)

	article, err := c.extractArticle("123", boardTypeList1, boardTypes[boardTypeList1].detailURLTemplate, s)

	require.NoError(t, err)
	assert.Equal(t, "100", article.ArticleID)
	assert.Equal(t, "제목A", article.Title)
}

func TestExtractArticle_RoutesToPhoto1(t *testing.T) {
	c := newTestCrawler()
	liHTML := `<ul><li>
		<a class="selectNttInfo" title="사진제목" data-param="200">
			<p class="txt">
				<span class="date">2024.03.15.</span>
				<span class="date">조회 10</span>
			</p>
		</a>
	</li></ul>`
	s := liSelectionFromHTML(t, liHTML)

	article, err := c.extractArticle("456", boardTypePhoto1, boardTypes[boardTypePhoto1].detailURLTemplate, s)

	require.NoError(t, err)
	assert.Equal(t, "200", article.ArticleID)
	assert.Equal(t, "사진제목", article.Title)
}

// ─────────────────────────────────────────────────────────────────────────────
// extractList1Article
// ─────────────────────────────────────────────────────────────────────────────

// newList1Row 일반 목록형 게시판의 <tr> HTML을 생성합니다.
func newList1Row(dataID, title, author, date string) string {
	return `<table><tbody><tr>
		<td>번호</td>
		<td class="bbs_tit"><a data-id="` + dataID + `">` + title + `</a></td>
		<td>` + author + `</td>
		<td>` + date + `</td>
		<td>조회</td>
	</tr></tbody></table>`
}

func TestExtractList1Article_Success(t *testing.T) {
	c := newTestCrawler()
	s := selectionFromHTML(t, newList1Row("9999", "공지 테스트", "작성자 홍길동", "등록일 2024.03.15."))

	article, err := c.extractList1Article("BID01", boardTypes[boardTypeList1].detailURLTemplate, s)

	require.NoError(t, err)
	assert.Equal(t, "9999", article.ArticleID)
	assert.Equal(t, "공지 테스트", article.Title)
	assert.Equal(t, "홍길동", article.Author)
	assert.Equal(
		t,
		"http://test.local/ys-ssangbong_es/na/ntt/selectNttInfo.do?mi=BID01&bbsId=BID01&nttSn=9999",
		article.Link,
	)
	assert.Equal(t, 2024, article.CreatedAt.Year())
	assert.Equal(t, 3, int(article.CreatedAt.Month()))
	assert.Equal(t, 15, article.CreatedAt.Day())
}

func TestExtractList1Article_TitleAnchorMissing(t *testing.T) {
	// td.bbs_tit > a 가 없는 HTML
	c := newTestCrawler()
	s := selectionFromHTML(t, `<table><tbody><tr><td>번호</td><td class="bbs_tit"><span>앵커없음</span></td><td>작성자 A</td><td>등록일 2024.01.01.</td><td></td></tr></tbody></table>`)

	_, err := c.extractList1Article("BID", boardTypes[boardTypeList1].detailURLTemplate, s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractList1Article_TitleAnchorMultiple(t *testing.T) {
	// td.bbs_tit > a 가 2개 이상인 HTML — 레이아웃 변경 케이스
	c := newTestCrawler()
	s := selectionFromHTML(t, `<table><tbody><tr>
		<td class="bbs_tit"><a data-id="1">첫번째</a><a data-id="2">두번째</a></td>
		<td>작성자 A</td><td>등록일 2024.01.01.</td><td></td>
	</tr></tbody></table>`)

	_, err := c.extractList1Article("BID", boardTypes[boardTypeList1].detailURLTemplate, s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractList1Article_ArticleIDMissing(t *testing.T) {
	// data-id 속성이 없는 앵커
	c := newTestCrawler()
	s := selectionFromHTML(t, `<table><tbody><tr>
		<td class="bbs_tit"><a>ID없음</a></td>
		<td>작성자 A</td><td>등록일 2024.01.01.</td><td></td>
	</tr></tbody></table>`)

	_, err := c.extractList1Article("BID", boardTypes[boardTypeList1].detailURLTemplate, s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractList1Article_ArticleIDEmpty(t *testing.T) {
	// data-id 가 있지만 빈 값인 앵커
	c := newTestCrawler()
	s := selectionFromHTML(t, `<table><tbody><tr>
		<td class="bbs_tit"><a data-id="">빈ID</a></td>
		<td>작성자 A</td><td>등록일 2024.01.01.</td><td></td>
	</tr></tbody></table>`)

	_, err := c.extractList1Article("BID", boardTypes[boardTypeList1].detailURLTemplate, s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractList1Article_TooFewCells(t *testing.T) {
	// td 가 4개 미만 (3개)
	c := newTestCrawler()
	s := selectionFromHTML(t, `<table><tbody><tr>
		<td class="bbs_tit"><a data-id="1">제목</a></td>
		<td>작성자 A</td>
		<td>등록일 2024.01.01.</td>
	</tr></tbody></table>`)

	_, err := c.extractList1Article("BID", boardTypes[boardTypeList1].detailURLTemplate, s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractList1Article_AuthorPrefixMissing(t *testing.T) {
	// 작성자 셀에 "작성자" 접두어가 없는 경우
	c := newTestCrawler()
	s := selectionFromHTML(t, newList1Row("1", "제목", "홍길동", "등록일 2024.01.01."))

	_, err := c.extractList1Article("BID", boardTypes[boardTypeList1].detailURLTemplate, s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractList1Article_DatePrefixMissing(t *testing.T) {
	// 등록일 셀에 "등록일" 접두어가 없는 경우
	c := newTestCrawler()
	s := selectionFromHTML(t, newList1Row("1", "제목", "작성자 홍길동", "2024.01.01."))

	_, err := c.extractList1Article("BID", boardTypes[boardTypeList1].detailURLTemplate, s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractList1Article_DateFormatDot(t *testing.T) {
	// 등록일이 "등록일 2024.03.01." 형식 → 정상 파싱 확인
	c := newTestCrawler()
	s := selectionFromHTML(t, newList1Row("42", "도트날짜", "작성자 박지성", "등록일 2024.03.01."))

	article, err := c.extractList1Article("BID", boardTypes[boardTypeList1].detailURLTemplate, s)

	require.NoError(t, err)
	assert.Equal(t, 2024, article.CreatedAt.Year())
	assert.Equal(t, 3, int(article.CreatedAt.Month()))
	assert.Equal(t, 1, article.CreatedAt.Day())
}

func TestExtractList1Article_LinkAssembly(t *testing.T) {
	// 링크가 "BaseURL + detailURLTemplate(boardID 치환) + &nttSn=ArticleID" 형식으로 조립되는지 확인
	c := newTestCrawler()
	s := selectionFromHTML(t, newList1Row("777", "링크확인", "작성자 이순신", "등록일 2024.01.01."))

	article, err := c.extractList1Article("MYBOARD", boardTypes[boardTypeList1].detailURLTemplate, s)

	require.NoError(t, err)
	assert.Equal(
		t,
		"http://test.local/ys-ssangbong_es/na/ntt/selectNttInfo.do?mi=MYBOARD&bbsId=MYBOARD&nttSn=777",
		article.Link,
	)
}

// ─────────────────────────────────────────────────────────────────────────────
// extractPhoto1Article
// ─────────────────────────────────────────────────────────────────────────────

// buildPhoto1Li 포토 게시판 <li> HTML을 간편하게 조합합니다.
func buildPhoto1Li(boardID, dataParam, titleAttr string, extraSpanCount int, imgStyle string) string {
	var sb strings.Builder
	sb.WriteString(`<ul><li>`)
	sb.WriteString(`<div class="img"><span style="` + imgStyle + `"></span></div>`)
	sb.WriteString(`<a class="selectNttInfo" title="` + titleAttr + `" data-param="` + dataParam + `">`)
	sb.WriteString(`<p class="txt">`)
	for i := 0; i < extraSpanCount; i++ {
		sb.WriteString(`<span class="date">2024.03.15.</span>`)
	}
	sb.WriteString(`</p></a>`)
	sb.WriteString(`</li></ul>`)
	return sb.String()
}

func TestExtractPhoto1Article_Success_NormalBoard(t *testing.T) {
	// 일반 포토 게시판 (학교앨범이 아닌 경우) — 작성자/본문은 상세 페이지에서 가져오므로 빈 문자열
	c := newTestCrawler()
	s := liSelectionFromHTML(t, buildPhoto1Li("99", "500", "사진제목", 2, ""))

	article, err := c.extractPhoto1Article("99", boardTypes[boardTypePhoto1].detailURLTemplate, s)

	require.NoError(t, err)
	assert.Equal(t, "500", article.ArticleID)
	assert.Equal(t, "사진제목", article.Title)
	assert.Empty(t, article.Author)  // 상세 페이지에서 보완
	assert.Empty(t, article.Content) // 상세 페이지에서 보완
	assert.Equal(t, 2024, article.CreatedAt.Year())
}

func TestExtractPhoto1Article_Success_SchoolAlbum_WithThumbnail(t *testing.T) {
	// 학교앨범 게시판 — 썸네일 이미지를 Content로, 작성자 "쌍봉초등학교"로 고정
	c := newTestCrawler()
	imgStyle := "background-image:url(/data/upload/thumbnail.jpg);"
	liHTML := `<ul><li>
		<div class="img"><span style="` + imgStyle + `"></span></div>
		<a class="selectNttInfo" title="앨범사진" data-param="111">
			<p class="txt">
				<span class="date">2024.06.01.</span>
				<span class="date">조회 5</span>
			</p>
		</a>
	</li></ul>`
	s := liSelectionFromHTML(t, liHTML)

	article, err := c.extractPhoto1Article(boardIDSchoolAlbum, boardTypes[boardTypePhoto1].detailURLTemplate, s)

	require.NoError(t, err)
	assert.Equal(t, "111", article.ArticleID)
	assert.Equal(t, "쌍봉초등학교", article.Author)
	assert.NotEmpty(t, article.Content)
	assert.Contains(t, article.Content, `<img src=`)
	assert.Contains(t, article.Content, "thumbnail.jpg")
	assert.Contains(t, article.Content, `alt="앨범사진"`)
}

func TestExtractPhoto1Article_Success_SchoolAlbum_NoThumbnail(t *testing.T) {
	// 학교앨범 게시판 — imgSpan이 없어 썸네일 없음. Content 빈 문자열. 작성자만 고정
	c := newTestCrawler()
	liHTML := `<ul><li>
		<a class="selectNttInfo" title="앨범제목없음" data-param="222">
			<p class="txt">
				<span class="date">2024.06.01.</span>
				<span class="date">조회 1</span>
			</p>
		</a>
	</li></ul>`
	s := liSelectionFromHTML(t, liHTML)

	article, err := c.extractPhoto1Article(boardIDSchoolAlbum, boardTypes[boardTypePhoto1].detailURLTemplate, s)

	require.NoError(t, err)
	assert.Equal(t, "쌍봉초등학교", article.Author)
	assert.Empty(t, article.Content)
}

func TestExtractPhoto1Article_AnchorMissing(t *testing.T) {
	// a.selectNttInfo 가 없는 경우
	c := newTestCrawler()
	s := liSelectionFromHTML(t, `<ul><li><span>no anchor</span></li></ul>`)

	_, err := c.extractPhoto1Article("99", boardTypes[boardTypePhoto1].detailURLTemplate, s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractPhoto1Article_TitleAttrMissing(t *testing.T) {
	// title 속성이 없는 앵커
	c := newTestCrawler()
	liHTML := `<ul><li>
		<a class="selectNttInfo" data-param="300">
			<p class="txt"><span class="date">2024.01.01.</span><span class="date">조회 1</span></p>
		</a>
	</li></ul>`
	s := liSelectionFromHTML(t, liHTML)

	_, err := c.extractPhoto1Article("99", boardTypes[boardTypePhoto1].detailURLTemplate, s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractPhoto1Article_TitleAttrEmpty(t *testing.T) {
	// title 속성이 빈 문자열
	c := newTestCrawler()
	liHTML := `<ul><li>
		<a class="selectNttInfo" title="" data-param="300">
			<p class="txt"><span class="date">2024.01.01.</span><span class="date">조회 1</span></p>
		</a>
	</li></ul>`
	s := liSelectionFromHTML(t, liHTML)

	_, err := c.extractPhoto1Article("99", boardTypes[boardTypePhoto1].detailURLTemplate, s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractPhoto1Article_DataParamMissing(t *testing.T) {
	// data-param 속성이 없는 앵커
	c := newTestCrawler()
	liHTML := `<ul><li>
		<a class="selectNttInfo" title="제목">
			<p class="txt"><span class="date">2024.01.01.</span><span class="date">조회 1</span></p>
		</a>
	</li></ul>`
	s := liSelectionFromHTML(t, liHTML)

	_, err := c.extractPhoto1Article("99", boardTypes[boardTypePhoto1].detailURLTemplate, s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractPhoto1Article_DateSpanCountMismatch(t *testing.T) {
	// span.date 개수가 2개가 아닌 경우 (1개)
	c := newTestCrawler()
	liHTML := `<ul><li>
		<a class="selectNttInfo" title="제목" data-param="300">
			<p class="txt"><span class="date">2024.01.01.</span></p>
		</a>
	</li></ul>`
	s := liSelectionFromHTML(t, liHTML)

	_, err := c.extractPhoto1Article("99", boardTypes[boardTypePhoto1].detailURLTemplate, s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractPhoto1Article_DateSpanEmpty(t *testing.T) {
	// 첫 번째 span.date 텍스트가 비어있는 경우
	c := newTestCrawler()
	liHTML := `<ul><li>
		<a class="selectNttInfo" title="제목" data-param="400">
			<p class="txt"><span class="date"></span><span class="date">조회 10</span></p>
		</a>
	</li></ul>`
	s := liSelectionFromHTML(t, liHTML)

	_, err := c.extractPhoto1Article("99", boardTypes[boardTypePhoto1].detailURLTemplate, s)

	require.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.ParsingFailed))
}

func TestExtractPhoto1Article_ThumbnailURLWithQuotes(t *testing.T) {
	// CSS url에 작은따옴표가 감싸인 케이스: url('/path/img.jpg')
	c := newTestCrawler()
	imgStyle := "background-image:url('/data/upload/thumbnail.jpg');"
	liHTML := `<ul><li>
		<div class="img"><span style="` + imgStyle + `"></span></div>
		<a class="selectNttInfo" title="따옴표테스트" data-param="555">
			<p class="txt">
				<span class="date">2024.03.01.</span>
				<span class="date">조회 3</span>
			</p>
		</a>
	</li></ul>`
	s := liSelectionFromHTML(t, liHTML)

	article, err := c.extractPhoto1Article(boardIDSchoolAlbum, boardTypes[boardTypePhoto1].detailURLTemplate, s)

	require.NoError(t, err)
	assert.Contains(t, article.Content, "thumbnail.jpg")
	// 따옴표가 URL에 포함된 채로 들어가면 안됨
	assert.NotContains(t, article.Content, "'")
}

// ─────────────────────────────────────────────────────────────────────────────
// crawlArticleContent
// ─────────────────────────────────────────────────────────────────────────────

// makeCrawlerForContent MockFetcher 기반 크롤러와 테스트용 article을 리턴합니다.
func makeCrawlerForContent(t *testing.T) (*crawler, *fetchermocks.MockFetcher, *feed.Article) {
	t.Helper()
	f := fetchermocks.NewMockFetcher()
	repo := new(mockFeedRepo)
	c := setupTestCrawler(t, f, repo, nil)

	article := &feed.Article{
		BoardID:   "222",
		BoardName: "테스트게시판",
		ArticleID: "99",
		Title:     "테스트글",
		Link:      "http://test.local/ys-ssangbong_es/na/ntt/selectNttInfo.do?mi=222&bbsId=222&nttSn=99",
	}
	return c, f, article
}

func TestCrawlArticleContent_EarlyReturn_ContentAlreadySet(t *testing.T) {
	// article.Content가 이미 설정된 경우 — HTTP 요청 없이 nil 반환
	c, f, article := makeCrawlerForContent(t)
	article.Content = "미리채워진본문"

	err := c.crawlArticleContent(context.Background(), article)

	assert.NoError(t, err)
	f.AssertNotCalled(t, "Do")
}

func TestCrawlArticleContent_ForbiddenResponse_ReturnsErrContentUnavailable(t *testing.T) {
	// HTTP 403 Forbidden 응답 → ErrContentUnavailable 반환
	c, f, article := makeCrawlerForContent(t)
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse("", http.StatusForbidden), nil)

	err := c.crawlArticleContent(context.Background(), article)

	assert.ErrorIs(t, err, provider.ErrContentUnavailable)
}

func TestCrawlArticleContent_UnauthorizedResponse_ReturnsErrContentUnavailable(t *testing.T) {
	// HTTP 401 Unauthorized 응답 → ErrContentUnavailable 반환
	c, f, article := makeCrawlerForContent(t)
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse("", http.StatusUnauthorized), nil)

	err := c.crawlArticleContent(context.Background(), article)

	assert.ErrorIs(t, err, provider.ErrContentUnavailable)
}

func TestCrawlArticleContent_NetworkError_PropagatesError(t *testing.T) {
	// 네트워크 에러 → 그대로 전파 (재시도 대상)
	c, f, article := makeCrawlerForContent(t)
	netErr := apperrors.New(apperrors.ExecutionFailed, "일시적 네트워크 에러")
	f.On("Do", mock.Anything).Return((*http.Response)(nil), netErr)

	err := c.crawlArticleContent(context.Background(), article)

	require.Error(t, err)
	assert.NotErrorIs(t, err, provider.ErrContentUnavailable)
}

func TestCrawlArticleContent_ContentNodeMissing_ReturnsErrContentUnavailable(t *testing.T) {
	// div.bbsV_cont 가 없는 HTML → ErrContentUnavailable
	c, f, article := makeCrawlerForContent(t)
	html := `<div class="bbs_ViewA">
		<ul class="bbsV_data"><li>작성자 홍길동</li><li>등록일</li><li>조회</li></ul>
		<div class="no_content">본문컨테이너없음</div>
	</div>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(html, http.StatusOK), nil)

	err := c.crawlArticleContent(context.Background(), article)

	assert.ErrorIs(t, err, provider.ErrContentUnavailable)
}

func TestCrawlArticleContent_AuthorExtracted_FromDetailPage(t *testing.T) {
	// 포토 게시판 케이스: article.Author가 빈 문자열 → 상세 페이지에서 작성자를 추출
	c, f, article := makeCrawlerForContent(t)
	article.Author = "" // 포토 게시판은 목록에 작성자가 없음

	html := `<div class="bbs_ViewA">
		<ul class="bbsV_data">
			<li>작성자 이순신</li>
			<li>등록일 2024.03.15.</li>
			<li>조회수 42</li>
		</ul>
		<div class="bbsV_cont"><p>본문 텍스트</p></div>
	</div>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(html, http.StatusOK), nil)

	err := c.crawlArticleContent(context.Background(), article)

	assert.NoError(t, err)
	assert.Equal(t, "이순신", article.Author)
}

func TestCrawlArticleContent_AuthorPrefixMismatch_FallbackToSchoolName(t *testing.T) {
	// li(0) 텍스트가 "작성자" 접두어로 시작하지 않으면 "쌍봉초등학교"로 대체
	c, f, article := makeCrawlerForContent(t)
	article.Author = ""

	html := `<div class="bbs_ViewA">
		<ul class="bbsV_data">
			<li>이름 이순신</li>
			<li>등록일 2024.03.15.</li>
			<li>조회수 1</li>
		</ul>
		<div class="bbsV_cont"><p>본문</p></div>
	</div>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(html, http.StatusOK), nil)

	err := c.crawlArticleContent(context.Background(), article)

	assert.NoError(t, err)
	assert.Equal(t, "쌍봉초등학교", article.Author)
}

func TestCrawlArticleContent_AuthorMetaMissing_FallbackToSchoolName(t *testing.T) {
	// li 개수가 3개가 아닌 경우 "쌍봉초등학교"로 대체
	c, f, article := makeCrawlerForContent(t)
	article.Author = ""

	html := `<div class="bbs_ViewA">
		<ul class="bbsV_data">
			<li>작성자 이순신</li>
		</ul>
		<div class="bbsV_cont"><p>본문</p></div>
	</div>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(html, http.StatusOK), nil)

	err := c.crawlArticleContent(context.Background(), article)

	assert.NoError(t, err)
	assert.Equal(t, "쌍봉초등학교", article.Author)
}

func TestCrawlArticleContent_AuthorPreserved_WhenAlreadySet(t *testing.T) {
	// article.Author가 이미 설정된 경우(일반 목록형 게시판) — 상세 페이지의 작성자 블록을 무시해야 함
	c, f, article := makeCrawlerForContent(t)
	article.Author = "미리설정된작성자"

	html := `<div class="bbs_ViewA">
		<ul class="bbsV_data">
			<li>작성자 다른사람</li>
			<li>등록일 2024.01.01.</li>
			<li>조회수 1</li>
		</ul>
		<div class="bbsV_cont"><p>본문내용</p></div>
	</div>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(html, http.StatusOK), nil)

	err := c.crawlArticleContent(context.Background(), article)

	assert.NoError(t, err)
	assert.Equal(t, "미리설정된작성자", article.Author) // 덮어쓰면 안 됨
}

func TestCrawlArticleContent_ContentText_Extracted(t *testing.T) {
	// 본문 텍스트가 정상적으로 추출되는지 확인
	c, f, article := makeCrawlerForContent(t)
	article.Author = "이순신"

	html := `<div class="bbs_ViewA">
		<div class="bbsV_cont">
			<p>첫 번째 단락</p>
			<p>두 번째 단락</p>
		</div>
	</div>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(html, http.StatusOK), nil)

	err := c.crawlArticleContent(context.Background(), article)

	assert.NoError(t, err)
	assert.Contains(t, article.Content, "첫 번째 단락")
	assert.Contains(t, article.Content, "두 번째 단락")
}

func TestCrawlArticleContent_ContentText_PlainTextNodeIncluded(t *testing.T) {
	// Contents()가 태그 없는 순수 텍스트 노드도 수집하는지 확인 (Children()이었다면 누락됨)
	c, f, article := makeCrawlerForContent(t)
	article.Author = "이순신"

	// "태그없는줄"은 텍스트 노드(TextNode)이므로 Children()에서는 수집되지 않음
	html := `<div class="bbs_ViewA">
		<div class="bbsV_cont">태그없는줄
			<p>태그있는줄</p>
		</div>
	</div>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(html, http.StatusOK), nil)

	err := c.crawlArticleContent(context.Background(), article)

	assert.NoError(t, err)
	assert.Contains(t, article.Content, "태그없는줄")
	assert.Contains(t, article.Content, "태그있는줄")
}

func TestCrawlArticleContent_ImageExtracted_AbsoluteURL(t *testing.T) {
	// 절대 경로 img src → 그대로 사용
	c, f, article := makeCrawlerForContent(t)
	article.Author = "이순신"

	html := `<div class="bbs_ViewA">
		<div class="bbsV_cont">
			<img src="https://cdn.example.com/img.jpg" alt="이미지설명">
		</div>
	</div>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(html, http.StatusOK), nil)

	err := c.crawlArticleContent(context.Background(), article)

	assert.NoError(t, err)
	assert.Contains(t, article.Content, `src="https://cdn.example.com/img.jpg"`)
	assert.Contains(t, article.Content, `alt="이미지설명"`)
}

func TestCrawlArticleContent_ImageExtracted_RelativeURL(t *testing.T) {
	// 상대 경로 img src → article.Link 기준으로 절대 URL로 변환
	c, f, article := makeCrawlerForContent(t)
	article.Author = "이순신"

	html := `<div class="bbs_ViewA">
		<div class="bbsV_cont">
			<img src="/data/upload/image.jpg" alt="상대경로이미지">
		</div>
	</div>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(html, http.StatusOK), nil)

	err := c.crawlArticleContent(context.Background(), article)

	assert.NoError(t, err)
	assert.Contains(t, article.Content, "http://test.local/data/upload/image.jpg")
}

func TestCrawlArticleContent_ImageSkipped_Base64Inline(t *testing.T) {
	// data:image/ 형태의 Base64 인라인 이미지는 수집에서 제외
	c, f, article := makeCrawlerForContent(t)
	article.Author = "이순신"

	html := `<div class="bbs_ViewA">
		<div class="bbsV_cont">
			<p>텍스트</p>
			<img src="data:image/png;base64,iVBORw0KGgo=" alt="base64이미지">
		</div>
	</div>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(html, http.StatusOK), nil)

	err := c.crawlArticleContent(context.Background(), article)

	assert.NoError(t, err)
	assert.NotContains(t, article.Content, "data:image/")
	assert.Contains(t, article.Content, "텍스트") // 텍스트는 포함되어야 함
}

func TestCrawlArticleContent_MultipleImages_AppendedInOrder(t *testing.T) {
	// 이미지가 여러 개인 경우 순서대로 추가되어야 함
	c, f, article := makeCrawlerForContent(t)
	article.Author = "이순신"

	html := `<div class="bbs_ViewA">
		<div class="bbsV_cont">
			<p>본문텍스트</p>
			<img src="/img/first.jpg" alt="첫번째">
			<img src="/img/second.jpg" alt="두번째">
		</div>
	</div>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(html, http.StatusOK), nil)

	err := c.crawlArticleContent(context.Background(), article)

	assert.NoError(t, err)
	firstIdx := strings.Index(article.Content, "first.jpg")
	secondIdx := strings.Index(article.Content, "second.jpg")
	assert.True(t, firstIdx < secondIdx, "첫 번째 이미지가 두 번째 이미지보다 앞에 와야 함")
}

func TestCrawlArticleContent_SuccessReturnNil(t *testing.T) {
	// 정상 시나리오 — nil 반환
	c, f, article := makeCrawlerForContent(t)

	html := `<div class="bbs_ViewA">
		<ul class="bbsV_data">
			<li>작성자 홍길동</li>
			<li>등록일 2024.03.15.</li>
			<li>조회수 10</li>
		</ul>
		<div class="bbsV_cont"><p>정상 본문입니다.</p></div>
	</div>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(html, http.StatusOK), nil)

	err := c.crawlArticleContent(context.Background(), article)

	assert.NoError(t, err)
	assert.NotEmpty(t, article.Content)
	assert.Equal(t, "홍길동", article.Author)
}

// TestCrawlArticleContent_ImageSrcEmpty_Skipped
// src 속성이 빈 문자열인 img 태그는 Content에 추가되지 않아야 합니다.
func TestCrawlArticleContent_ImageSrcEmpty_Skipped(t *testing.T) {
	c, f, article := makeCrawlerForContent(t)
	article.Author = "이순신"

	html := `<div class="bbs_ViewA">
		<div class="bbsV_cont">
			<p>텍스트만</p>
			<img src="" alt="빈src">
		</div>
	</div>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(html, http.StatusOK), nil)

	err := c.crawlArticleContent(context.Background(), article)

	assert.NoError(t, err)
	assert.NotContains(t, article.Content, `<img`)
	assert.Contains(t, article.Content, "텍스트만")
}
