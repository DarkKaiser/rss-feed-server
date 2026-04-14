package navercafe

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	fetchermocks "github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher/mocks"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// ─────────────────────────────────────────────────────────────────────────────
// TestExtractArticle (개별 파싱 기능 테스트)
// ─────────────────────────────────────────────────────────────────────────────

func TestExtractArticle_ReplyRow(t *testing.T) {
	c := setupTestCrawler(t, fetchermocks.NewMockFetcher(), nil, nil)
	htmlReply := `<table><tbody>
		<tr>
			<td id="reply_12345" style="display:none"></td>
		</tr>
	</tbody></table>`

	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(htmlReply))
	sel := doc.Find("tr").First()
	article, err := c.extractArticle(sel)
	assert.NoError(t, err)
	assert.Nil(t, article) // 답글이라 스킵
}

func TestExtractArticle_ValidRow(t *testing.T) {
	c := setupTestCrawler(t, fetchermocks.NewMockFetcher(), nil, nil)
	htmlValid := `<table><tbody>
		<tr>
			<td class="td_date">2024.03.15.</td>
			<td class="td_article">
				<div class="board-name"><a class="link_name" href="https://cafe.naver.com/ArticleList.nhn?search.menuid=100">자유게시판</a></div>
				<div class="board-list"><a class="article" href="https://cafe.naver.com/ArticleRead.nhn?articleid=777">방금 쓴 글제목</a></div>
			</td>
			<td class="td_name"><div class="pers_nick_area"><table><tbody><tr><td class="p-nick"><a class="m-tcol-c">닉네임이지롱</a></td></tr></tbody></table></div></td>
		</tr>
	</tbody></table>`

	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(htmlValid))
	sel := doc.Find("tr").First()

	article, err := c.extractArticle(sel)
	assert.NoError(t, err)
	assert.NotNil(t, article)

	assert.Equal(t, "777", article.ArticleID)
	assert.Equal(t, "100", article.BoardID)
	assert.Equal(t, "자유게시판", article.BoardName)
	assert.Equal(t, "방금 쓴 글제목", article.Title)
	assert.Equal(t, "닉네임이지롱", article.Author)
	assert.Equal(t, "https://cafe.naver.com/testcafe/ArticleRead.nhn?articleid=777&clubid=12345678", article.Link)
	assert.Equal(t, "2024-03-15", article.CreatedAt.Format("2006-01-02")) // 과거날짜 파싱
}

func TestExtractArticle_InvalidMissingTags(t *testing.T) {
	c := setupTestCrawler(t, fetchermocks.NewMockFetcher(), nil, nil)
	htmlInvalid := `<table><tbody><tr><td class="td_date">2024.03.15.</td></tr></tbody></table>`
	
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(htmlInvalid))
	sel := doc.Find("tr").First()

	article, err := c.extractArticle(sel)
	assert.Error(t, err)
	assert.Nil(t, article)
	assert.Contains(t, err.Error(), "식별할 수 없어") // 에러 메시지 내용 검증
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCrawlContentViaAPI (JSON 파싱 및 에러 모의 테스트)
// ─────────────────────────────────────────────────────────────────────────────

func TestCrawlContentViaAPI_Success(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	c := setupTestCrawler(t, f, nil, nil)
	article := &feed.Article{ArticleID: "123", Link: "https://cafe.naver.com/ArticleRead.nhn?articleid=123&clubid=12345678"}

	jsonSuccess := `{
		"result": {
			"article": {
				"writeDate": 1713060000000,
				"contentHtml": "<p>API 본문</p><img src=\"http://example.com/api.jpg\">"
			}
		}
	}`

	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(jsonSuccess, http.StatusOK), nil)

	err := c.crawlContentViaAPI(context.Background(), article)
	assert.NoError(t, err)

	// HTML 파싱 결과물 확인 (이미지가 본문에 결합됨)
	expectedContent := "API 본문\r\n<img src=\"http://example.com/api.jpg\" alt=\"\" style=\"\">"
	assert.Equal(t, expectedContent, article.Content)
	
	// 시간 보정 확인: 1713060000000 밀리초 -> Time
	expectedTime := time.UnixMilli(1713060000000)
	assert.Equal(t, expectedTime.Format(time.RFC3339), article.CreatedAt.Format(time.RFC3339))
}

func TestCrawlContentViaAPI_ParseError(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	c := setupTestCrawler(t, f, nil, nil)
	article := &feed.Article{ArticleID: "123"}

	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse("INVALID JSON {", http.StatusOK), nil)

	err := c.crawlContentViaAPI(context.Background(), article)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "구문 오류")
}

func TestCrawlContentViaAPI_NetworkError(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	c := setupTestCrawler(t, f, nil, nil)
	article := &feed.Article{ArticleID: "123"}

	f.On("Do", mock.Anything).Return((*http.Response)(nil), errors.New("timeout"))

	err := c.crawlContentViaAPI(context.Background(), article)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCrawlContentViaPage (HTML 본문 크롤링 실패/성공 파싱)
// ─────────────────────────────────────────────────────────────────────────────

func TestCrawlContentViaPage_Success(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	c := setupTestCrawler(t, f, nil, nil)
	article := &feed.Article{ArticleID: "123", Link: "https://cafe.naver.com/ArticleRead.nhn?articleid=123&clubid=12345678"}

	htmlPage := `<html><body>
		<h3 class="title_text">제목입니다만</h3>
		<div id="tbody">
			<p>본문내용</p>
			<img src="http://example.com/page.jpg" />
		</div>
	</body></html>`

	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlPage, http.StatusOK), nil)

	err := c.crawlContentViaPage(context.Background(), article)
	assert.NoError(t, err)
	
	expectedContent := "본문내용\r\n<img src=\"http://example.com/page.jpg\" alt=\"\" style=\"\">"
	assert.Equal(t, expectedContent, article.Content)
}

func TestCrawlContentViaPage_FallbackSearchNoDOM(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	c := setupTestCrawler(t, f, nil, nil)
	article := &feed.Article{ArticleID: "123", Link: "https://cafe.naver.com/ArticleRead.nhn?articleid=123&clubid=12345678"}

	// 본문 클래스(.se-main-container 또는 #tbody)가 없는 경우
	htmlEmptyPage := `<html><body>
		<div>다른 레이아웃 게시글 (아마 멤버공개)</div>
	</body></html>`

	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlEmptyPage, http.StatusOK), nil)

	err := c.crawlContentViaPage(context.Background(), article)
	assert.ErrorIs(t, err, provider.ErrContentUnavailable)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCrawlContentViaSearch (폴백 검색 로직 파싱 검증)
// ─────────────────────────────────────────────────────────────────────────────

func TestCrawlContentViaSearch_Success(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	c := setupTestCrawler(t, f, nil, nil)
	article := &feed.Article{
		ArticleID: "123", 
		Link: "https://cafe.naver.com/ArticleRead.nhn?articleid=123&clubid=12345678",
		Title: "검색용 제목",
	}

	// \u200b 문자를 포함한 테스트 문자열
	htmlSearch := `<html><body>
		<div id="cafeArticleResult"><ul>
			<li>
				<a class="total_dsc" href="https://cafe.naver.com/testcafe/123">\u200b이것은 \u200b 검색된 본문 \u200b</a>
				<a class="thumb_single" href="https://cafe.naver.com/testcafe/123"><img src="http://example.com/search.jpg?type=w800" /></a>
			</li>
		</ul></div>
	</body></html>`

	// 실제 유니코드 \u200b 로 치환
	htmlSearch = strings.ReplaceAll(htmlSearch, `\u200b`, "\u200b")

	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlSearch, http.StatusOK), nil)

	err := c.crawlContentViaSearch(context.Background(), article)
	assert.NoError(t, err)
	
	// \u200b 가 제거된 텍스트 확인 및 img 덧붙여진 결과 확인
	expectedContent := "이것은 검색된 본문\r\n<img src=\"http://example.com/search.jpg?type=w800\" alt=\"\" style=\"\">"
	assert.Equal(t, expectedContent, article.Content)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCrawlArticleContent (통합 폴백 전략 순차 연계 검증)
// ─────────────────────────────────────────────────────────────────────────────

func TestCrawlArticleContent_FullFallback(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)
	b := &config.BoardConfig{ID: "100", Name: "자유게시판"}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b})
	article := &feed.Article{ArticleID: "123", Link: "https://cafe.naver.com/ArticleRead.nhn?articleid=123&clubid=12345678"}

	// 1. API: 내부 파싱 에러 발생
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse("INVALID JSON {", http.StatusOK), nil).Once()

	// 2. Page: 본문 태그 누락 발생
	htmlEmptyPage := `<html><body><div>다른 레이아웃</div></body></html>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlEmptyPage, http.StatusOK), nil).Once()

	// 3. Search: 정상 성공
	htmlSearch := `<html><body><div id="cafeArticleResult"><ul><li><a class="total_dsc" href="https://cafe.naver.com/testcafe/123">검색결과본문</a></li></ul></div></body></html>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlSearch, http.StatusOK), nil).Once()

	err := c.crawlArticleContent(context.Background(), article)
	assert.NoError(t, err)
	assert.Equal(t, "검색결과본문", article.Content)
}
