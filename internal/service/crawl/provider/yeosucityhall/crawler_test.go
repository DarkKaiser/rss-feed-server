package yeosucityhall

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/darkkaiser/rss-feed-server/internal/config"
	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher"
	fetchermocks "github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher/mocks"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// ─────────────────────────────────────────────────────────────────────────────
// 공통 헬퍼
// ─────────────────────────────────────────────────────────────────────────────

// mockFeedRepo feed.Repository 인터페이스의 Mock 구현체
type mockFeedRepo struct {
	mock.Mock
}

func (m *mockFeedRepo) SaveArticles(ctx context.Context, providerID string, articles []*feed.Article) (int, error) {
	args := m.Called(ctx, providerID, articles)
	return args.Int(0), args.Error(1)
}

func (m *mockFeedRepo) GetArticles(ctx context.Context, providerID string, boardIDs []string, limit uint) ([]*feed.Article, error) {
	args := m.Called(ctx, providerID, boardIDs, limit)
	return args.Get(0).([]*feed.Article), args.Error(1)
}

func (m *mockFeedRepo) GetCrawlingCursor(ctx context.Context, providerID, boardID string) (string, time.Time, error) {
	args := m.Called(ctx, providerID, boardID)
	return args.String(0), args.Get(1).(time.Time), args.Error(2)
}

func (m *mockFeedRepo) UpsertLatestCrawledArticleID(ctx context.Context, providerID, boardID, articleID string) error {
	args := m.Called(ctx, providerID, boardID, articleID)
	return args.Error(0)
}

// setupTestCrawler 공통 테스트용 crawler를 생성합니다.
func setupTestCrawler(t *testing.T, f fetcher.Fetcher, r *mockFeedRepo, boards []*config.BoardConfig) *crawler {
	cfg := &config.ProviderDetailConfig{
		ID:     "yeosu-cityhall-news",
		Name:   "여수시청",
		URL:    "https://www.yeosu.go.kr",
		Boards: boards,
	}
	p := provider.NewCrawlerParams{
		ProviderID:   "yeosu-cityhall-news",
		Config:       cfg,
		Fetcher:      f,
		FeedRepo:     r,
		NotifyClient: nil,
	}
	base := provider.NewBase(p, 3)
	c := &crawler{Base: base}
	c.SetCrawlArticles(c.crawlArticles)
	return c
}

// htmlDetailOK crawlArticleContent용 간단한 정상 상세 페이지 HTML
const htmlDetailOK = `<html><body><div class="contbox"><div class="viewbox"><p>본문 내용</p></div></div></body></html>`

// ─────────────────────────────────────────────────────────────────────────────
// TestNewCrawler_Init
// ─────────────────────────────────────────────────────────────────────────────

func TestNewCrawler_Init(t *testing.T) {
	assert.NotNil(t, boardTypes)
	assert.Contains(t, boardTypes, boardTypeList1)
	assert.Contains(t, boardTypes, boardTypeList2)
	assert.Contains(t, boardTypes, boardTypePhotoNews)
	assert.Contains(t, boardTypes, boardTypeCardNews)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCrawlSingleBoard_UnsupportedBoardType
// ─────────────────────────────────────────────────────────────────────────────

func TestCrawlSingleBoard_UnsupportedBoardType(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)
	b := &config.BoardConfig{ID: "notice", Name: "공지", Type: "UNKNOWN"}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b})

	articles, cursor, msg, err := c.crawlSingleBoard(context.Background(), b)

	assert.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.System))
	assert.NotEmpty(t, msg)
	assert.Empty(t, cursor)
	assert.Nil(t, articles)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCrawlSingleBoard_NetworkFailure_Rollback
// ─────────────────────────────────────────────────────────────────────────────

func TestCrawlSingleBoard_NetworkFailure_Rollback(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)
	b := &config.BoardConfig{ID: "notice", Name: "공지사항", Type: boardTypeList1}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b})

	r.On("GetCrawlingCursor", mock.Anything, "yeosu-cityhall-news", "notice").Return("", time.Time{}, nil)
	f.On("Do", mock.Anything).Return((*http.Response)(nil), errors.New("network timeout"))

	articles, cursor, msg, err := c.crawlSingleBoard(context.Background(), b)

	assert.Error(t, err)
	assert.NotEmpty(t, msg)
	assert.Empty(t, cursor)
	assert.Nil(t, articles)
	r.AssertExpectations(t)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCrawlSingleBoard_DOMStructureChange
// ─────────────────────────────────────────────────────────────────────────────

func TestCrawlSingleBoard_DOMStructureChange(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)
	b := &config.BoardConfig{ID: "notice", Name: "공지사항", Type: boardTypeList1}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b})

	r.On("GetCrawlingCursor", mock.Anything, "yeosu-cityhall-news", "notice").Return("", time.Time{}, nil)
	// #content 자체가 없는 완전히 다른 HTML — CSS 파싱 오류로 판별
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(`<html><body><h1>Nothing</h1></body></html>`, http.StatusOK), nil)

	articles, cursor, msg, err := c.crawlSingleBoard(context.Background(), b)

	assert.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.System))
	assert.Contains(t, msg, "DOM 구조")
	assert.Empty(t, cursor)
	assert.Nil(t, articles)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCrawlSingleBoard_EmptyBoard_List_DataNone
// ─────────────────────────────────────────────────────────────────────────────

func TestCrawlSingleBoard_EmptyBoard_List_DataNone(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)
	b := &config.BoardConfig{ID: "notice", Name: "공지사항", Type: boardTypeList1}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b})

	r.On("GetCrawlingCursor", mock.Anything, "yeosu-cityhall-news", "notice").Return("", time.Time{}, nil)
	// tr 1개, td.data_none — 게시글 없음 안내 메시지 구조
	htmlEmpty := `<html><body><div id="content"><table class="board_basic"><tbody>
		<tr><td class="data_none">등록된 게시물이 없습니다.</td></tr>
	</tbody></table></div></body></html>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlEmpty, http.StatusOK), nil)

	articles, cursor, msg, err := c.crawlSingleBoard(context.Background(), b)

	assert.NoError(t, err)
	assert.Empty(t, msg)
	assert.Empty(t, cursor)
	assert.Empty(t, articles)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCrawlSingleBoard_EmptyBoard_List_OnlyNotice
// ─────────────────────────────────────────────────────────────────────────────

func TestCrawlSingleBoard_EmptyBoard_List_OnlyNotice(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)
	b := &config.BoardConfig{ID: "notice", Name: "공지사항", Type: boardTypeList1}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b})

	r.On("GetCrawlingCursor", mock.Anything, "yeosu-cityhall-news", "notice").Return("", time.Time{}, nil)
	// tr.notice 만 있고 일반 게시글 없음 — 빈 게시판으로 처리
	htmlOnlyNotice := `<html><body><div id="content"><table class="board_basic"><tbody>
		<tr class="notice"><td>공지</td><td class="align_left"><a href="/notice?idx=1" class="basic_cont">공지글</a></td><td>관리자</td><td>2024.01.01</td><td>0</td></tr>
	</tbody></table></div></body></html>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlOnlyNotice, http.StatusOK), nil)

	articles, cursor, msg, err := c.crawlSingleBoard(context.Background(), b)

	assert.NoError(t, err)
	assert.Empty(t, msg)
	assert.Empty(t, cursor)
	assert.Empty(t, articles)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCrawlSingleBoard_EmptyBoard_PhotoNews
// ─────────────────────────────────────────────────────────────────────────────

func TestCrawlSingleBoard_EmptyBoard_PhotoNews(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)
	b := &config.BoardConfig{ID: "hotnews", Name: "여수주요소식", Type: boardTypePhotoNews}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b})

	r.On("GetCrawlingCursor", mock.Anything, "yeosu-cityhall-news", "hotnews").Return("", time.Time{}, nil)
	// 부모 컨테이너(div.board_list_box) 존재 + 게시글 item 없음
	htmlEmpty := `<html><body><div id="content"><div class="board_list_box"><div class="board_list"></div></div></div></body></html>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlEmpty, http.StatusOK), nil)

	articles, cursor, msg, err := c.crawlSingleBoard(context.Background(), b)

	assert.NoError(t, err)
	assert.Empty(t, msg)
	assert.Empty(t, cursor)
	assert.Empty(t, articles)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCrawlSingleBoard_EmptyBoard_CardNews
// ─────────────────────────────────────────────────────────────────────────────

func TestCrawlSingleBoard_EmptyBoard_CardNews(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)
	b := &config.BoardConfig{ID: "card_news", Name: "카드뉴스", Type: boardTypeCardNews}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b})

	r.On("GetCrawlingCursor", mock.Anything, "yeosu-cityhall-news", "card_news").Return("", time.Time{}, nil)
	// 부모 컨테이너 존재 + item 없음
	htmlEmpty := `<html><body><div id="content"><div class="board_list_box"></div></div></body></html>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlEmpty, http.StatusOK), nil)

	articles, cursor, msg, err := c.crawlSingleBoard(context.Background(), b)

	assert.NoError(t, err)
	assert.Empty(t, msg)
	assert.Empty(t, cursor)
	assert.Empty(t, articles)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCrawlSingleBoard_NormalAndCursorStop_List1
// ─────────────────────────────────────────────────────────────────────────────

func TestCrawlSingleBoard_NormalAndCursorStop_List1(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)
	b := &config.BoardConfig{ID: "notice", Name: "공지사항", Type: boardTypeList1}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b})

	// 마지막 커서: 12 → 13, 14만 신규 수집
	r.On("GetCrawlingCursor", mock.Anything, "yeosu-cityhall-news", "notice").Return("12", time.Time{}, nil)

	htmlList := `<html><body><div id="content"><table class="board_basic"><tbody>
		<tr><td>3</td><td class="align_left"><a href="/www/govt/news/notice?idx=14&mode=view" class="basic_cont">새글14</a></td><td>부서A</td><td>2024-03-15</td><td>10</td></tr>
		<tr><td>2</td><td class="align_left"><a href="/www/govt/news/notice?idx=13&mode=view" class="basic_cont">새글13</a></td><td>부서B</td><td>2024-03-14</td><td>5</td></tr>
		<tr><td>1</td><td class="align_left"><a href="/www/govt/news/notice?idx=12&mode=view" class="basic_cont">구글12</a></td><td>부서C</td><td>2024-03-13</td><td>3</td></tr>
	</tbody></table></div></body></html>`
	// 2페이지: data_none → EndOfData 로 페이지 순회 조기 종료
	htmlPage2 := `<html><body><div id="content"><table class="board_basic"><tbody>
		<tr><td class="data_none">등록된 게시물이 없습니다.</td></tr>
	</tbody></table></div></body></html>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlList, http.StatusOK), nil).Once()
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlDetailOK, http.StatusOK), nil).Once()
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlDetailOK, http.StatusOK), nil).Once()
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlPage2, http.StatusOK), nil).Maybe()

	articles, cursor, msg, err := c.crawlSingleBoard(context.Background(), b)

	assert.NoError(t, err)
	assert.Empty(t, msg)
	assert.Equal(t, "14", cursor)
	// 오래된 순으로 정렬 반환 → [13, 14]
	assert.Len(t, articles, 2)
	assert.Equal(t, "13", articles[0].ArticleID)
	assert.Equal(t, "새글13", articles[0].Title)
	assert.Equal(t, "14", articles[1].ArticleID)
	assert.Equal(t, "새글14", articles[1].Title)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCrawlSingleBoard_NormalAndCursorStop_PhotoNews
// ─────────────────────────────────────────────────────────────────────────────

func TestCrawlSingleBoard_NormalAndCursorStop_PhotoNews(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)
	b := &config.BoardConfig{ID: "hotnews", Name: "여수주요소식", Type: boardTypePhotoNews}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b})

	r.On("GetCrawlingCursor", mock.Anything, "yeosu-cityhall-news", "hotnews").Return("100", time.Time{}, nil)

	// 102 신규 / 100 이미 수집됨
	htmlList := `<html><body><div id="content"><div class="board_list_box"><div class="board_list">
		<div class="item">
			<a href="/www/govt/news/hotnews?idx=102&mode=view" class="item_cont">
				<div class="cont_box">
					<div class="title_box">소식102</div>
					<dl>
						<dt class="text_hidden">작성자: </dt><dd>홍보팀 홍길동</dd>
						<dt class="text_hidden">날짜: </dt><dd>2024-03-15</dd>
						<dt>조회</dt><dd>10</dd>
					</dl>
				</div>
			</a>
		</div>
		<div class="item">
			<a href="/www/govt/news/hotnews?idx=100&mode=view" class="item_cont">
				<div class="cont_box">
					<div class="title_box">소식100</div>
					<dl>
						<dt class="text_hidden">작성자: </dt><dd>홍보팀 이순신</dd>
						<dt class="text_hidden">날짜: </dt><dd>2024-03-10</dd>
						<dt>조회</dt><dd>50</dd>
					</dl>
				</div>
			</a>
		</div>
	</div></div></div></body></html>`
	// 2페이지: 부모 컨테이너 있음 + item 없음 → EndOfData
	htmlEmpty := `<html><body><div id="content"><div class="board_list_box"><div class="board_list"></div></div></div></body></html>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlList, http.StatusOK), nil).Once()
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlDetailOK, http.StatusOK), nil).Once()
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlEmpty, http.StatusOK), nil).Maybe()

	articles, cursor, msg, err := c.crawlSingleBoard(context.Background(), b)

	assert.NoError(t, err)
	assert.Empty(t, msg)
	assert.Equal(t, "102", cursor)
	assert.Len(t, articles, 1)
	assert.Equal(t, "102", articles[0].ArticleID)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCrawlSingleBoard_NormalAndCursorStop_CardNews
// ─────────────────────────────────────────────────────────────────────────────

func TestCrawlSingleBoard_NormalAndCursorStop_CardNews(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)
	b := &config.BoardConfig{ID: "card_news", Name: "카드뉴스", Type: boardTypeCardNews}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b})

	r.On("GetCrawlingCursor", mock.Anything, "yeosu-cityhall-news", "card_news").Return("200", time.Time{}, nil)

	// 실제 카드뉴스 HTML 구조: 공유 버튼에 4개 a 태그 / 날짜는 2024-03-15 형식
	htmlCard := `<html><body><div id="content"><div class="board_list_box"><div class="board_list"><div class="board_list"><div class="board_photo"><div class="item_wrap">
		<div class="item">
			<div class="cont_box">
				<div class="top_util"><ul><li class="share_wrap">
					<div class="board_share_box"><ul class="share_btn">
						<li class="facebook share_btn"><a href="#none" class="share_btn" data-url="/www/govt/news/card_news?idx=202&mode=view">페북</a></li>
						<li class="twitter share_btn"><a href="#none" class="share_btn" data-url="/www/govt/news/card_news?idx=202&mode=view">트위터</a></li>
						<li class="kakaostory share_btn"><a href="#none" class="share_btn" data-url="/www/govt/news/card_news?idx=202&mode=view">카카오</a></li>
						<li class="band share_btn"><a href="#none" class="share_btn" data-url="/www/govt/news/card_news?idx=202&mode=view">밴드</a></li>
					</ul></div>
				</li></ul></div>
				<h3>신규카드202</h3>
				<dl>
					<dt class="text_hidden">날짜</dt><dd>2024-03-15</dd>
					<dt class="text_hidden">작성자</dt><dd>홍길동</dd>
				</dl>
			</div>
		</div>
		<div class="item">
			<div class="cont_box">
				<div class="top_util"><ul><li class="share_wrap">
					<div class="board_share_box"><ul class="share_btn">
						<li class="facebook share_btn"><a href="#none" class="share_btn" data-url="/www/govt/news/card_news?idx=200&mode=view">페북</a></li>
					</ul></div>
				</li></ul></div>
				<h3>구카드200</h3>
				<dl>
					<dt class="text_hidden">날짜</dt><dd>2024-03-10</dd>
					<dt class="text_hidden">작성자</dt><dd>이순신</dd>
				</dl>
			</div>
		</div>
	</div></div></div></div></div></div></body></html>`
	// 2페이지: 부모 컨테이너 있음 + item 없음 → EndOfData
	htmlEmpty2 := `<html><body><div id="content"><div class="board_list_box"></div></div></body></html>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlCard, http.StatusOK), nil).Once()
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlDetailOK, http.StatusOK), nil).Once()
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlEmpty2, http.StatusOK), nil).Maybe()

	articles, cursor, msg, err := c.crawlSingleBoard(context.Background(), b)

	assert.NoError(t, err)
	assert.Empty(t, msg)
	assert.Equal(t, "202", cursor)
	assert.Len(t, articles, 1)
	assert.Equal(t, "202", articles[0].ArticleID)
	assert.Equal(t, "신규카드202", articles[0].Title)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCrawlArticles_MultiBoard_PartialError
// ─────────────────────────────────────────────────────────────────────────────

func TestCrawlArticles_MultiBoard_PartialError(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)
	b1 := &config.BoardConfig{ID: "notice", Name: "공지사항", Type: boardTypeList1}
	b2 := &config.BoardConfig{ID: "hotnews", Name: "여수주요소식", Type: boardTypePhotoNews}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b1, b2})

	r.On("GetCrawlingCursor", mock.Anything, "yeosu-cityhall-news", "notice").Return("", time.Time{}, nil)
	r.On("GetCrawlingCursor", mock.Anything, "yeosu-cityhall-news", "hotnews").Return("", time.Time{}, nil)

	// b1: 네트워크 에러
	f.On("Do", mock.Anything).Return((*http.Response)(nil), errors.New("connection refused")).Once()
	// b2: 빈 게시판
	htmlEmpty := `<html><body><div id="content"><div class="board_list_box"><div class="board_list"></div></div></div></body></html>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlEmpty, http.StatusOK), nil).Once()

	// crawlArticles는 개별 게시판 에러를 내부 격리 처리 → 전체는 nil 반환
	articles, cursors, msg, err := c.crawlArticles(context.Background())

	assert.NoError(t, err)
	assert.Empty(t, msg)
	assert.Empty(t, articles)
	assert.Empty(t, cursors)
	r.AssertExpectations(t)
}
