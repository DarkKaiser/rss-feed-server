package navercafe

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
// 공통 헬퍼 & Mock
// ─────────────────────────────────────────────────────────────────────────────

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

func setupTestCrawler(t *testing.T, f fetcher.Fetcher, r *mockFeedRepo, boards []*config.BoardConfig) *crawler {
	cfg := &config.ProviderDetailConfig{
		ID:     "navercafe-test",
		Name:   "네이버카페",
		URL:    "https://cafe.naver.com/testcafe",
		Boards: boards,
		Data: map[string]any{
			"club_id":                "12345678",
			"crawling_delay_minutes": 40,
		},
	}
	p := provider.NewCrawlerParams{
		ProviderID:   "navercafe-test",
		Config:       cfg,
		Fetcher:      f,
		FeedRepo:     r,
		NotifyClient: nil,
	}
	base := provider.NewBase(p, 3)
	c := &crawler{
		Base:                 base,
		clubID:               "12345678",
		crawlingDelayMinutes: 40,
	}
	c.SetCrawlArticles(c.crawlArticles)
	return c
}

const htmlNoticeOnly = `<html><body><div class="article-board"><table><tbody>
	<tr class="board-notice">
		<td class="td_date">10:00</td>
		<td class="td_article">공지</td>
	</tr>
</tbody></table></div></body></html>`

const htmlDetailOK = `<html><body>
	<h3 class="title_text">상세본문_크롤링임시제목</h3>
	<div class="se-main-container">
		<p class="se-text-paragraph">이것은 본문 내용입니다.</p>
	</div>
</body></html>`

// ─────────────────────────────────────────────────────────────────────────────
// TestNewCrawler_Init
// ─────────────────────────────────────────────────────────────────────────────

func TestNewCrawler_Init(t *testing.T) {
	params := provider.NewCrawlerParams{
		ProviderID: "navercafe-test",
		Fetcher:    fetchermocks.NewMockFetcher(),
		Config: &config.ProviderDetailConfig{
			Data: map[string]any{"club_id": "99999", "crawling_delay_minutes": 30},
		},
	}
	cr, err := newCrawler(params)
	assert.NoError(t, err)
	assert.NotNil(t, cr)

	cInst, ok := cr.(*crawler)
	assert.True(t, ok)
	assert.Equal(t, "99999", cInst.clubID)
	assert.Equal(t, 30, cInst.crawlingDelayMinutes)
}

func TestNewCrawler_Init_ParseError(t *testing.T) {
	params := provider.NewCrawlerParams{
		ProviderID: "navercafe-test",
		Fetcher:    fetchermocks.NewMockFetcher(),
		Config: &config.ProviderDetailConfig{
			Data: map[string]any{}, // club_id 누락 파싱 에러
		},
	}
	cr, err := newCrawler(params)
	assert.Error(t, err)
	assert.Nil(t, cr)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCrawlArticles_FetchError_Rollback
// ─────────────────────────────────────────────────────────────────────────────

func TestCrawlArticles_FetchError_Rollback(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)
	b := &config.BoardConfig{ID: "100", Name: "자유게시판"}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b})

	r.On("GetCrawlingCursor", mock.Anything, "navercafe-test", "").Return("", time.Time{}, nil)
	f.On("Do", mock.Anything).Return((*http.Response)(nil), errors.New("network timeout"))

	articles, cursors, msg, err := c.crawlArticles(context.Background())

	assert.Error(t, err)
	assert.NotEmpty(t, msg) // 롤백이 요구되는 에러 발생 시 msg가 반환됨
	assert.Nil(t, articles)
	assert.Nil(t, cursors)
	r.AssertExpectations(t)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCrawlArticles_EmptyBoard
// ─────────────────────────────────────────────────────────────────────────────

func TestCrawlArticles_EmptyBoard(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)
	b := &config.BoardConfig{ID: "100", Name: "자유게시판"}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b})

	r.On("GetCrawlingCursor", mock.Anything, "navercafe-test", "").Return("", time.Time{}, nil)
	// 아무런 트리가 없는 예외상황
	htmlEmpty := `<html><body><div class="article-board"><table><tbody></tbody></table></div></body></html>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlEmpty, http.StatusOK), nil)

	articles, cursors, msg, err := c.crawlArticles(context.Background())

	assert.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.System))
	assert.Contains(t, msg, "DOM 구조")
	assert.Nil(t, articles)
	assert.Nil(t, cursors)
}

func TestCrawlArticles_OnlyNoticeBoard(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)
	b := &config.BoardConfig{ID: "100", Name: "자유게시판"}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b})

	r.On("GetCrawlingCursor", mock.Anything, "navercafe-test", "").Return("", time.Time{}, nil)
	// 공지사항만 있고 게시글 리스트가 없음 = 정상적인 빈 게시판
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlNoticeOnly, http.StatusOK), nil)

	articles, cursors, msg, err := c.crawlArticles(context.Background())

	assert.NoError(t, err)
	assert.Empty(t, msg)
	assert.Empty(t, articles)
	assert.Empty(t, cursors)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCrawlArticles_Pagination_And_CursorStop
// ─────────────────────────────────────────────────────────────────────────────

func TestCrawlArticles_Pagination_And_CursorStop(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)
	b := &config.BoardConfig{ID: "100", Name: "자유게시판"}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b})

	// 수집된 기존 최신 커서 12 -> 13, 14 수집
	// 날짜는 00:00:00 처리(딜레이 면제)를 위해 모두 과거 혹은 40분 이전 시간으로 배정
	r.On("GetCrawlingCursor", mock.Anything, "navercafe-test", "").Return("12", time.Time{}, nil)

	htmlList := `<html><body><div class="article-board"><table><tbody>
		<tr>
			<td class="td_date">2024.03.15.</td>
			<td class="td_article">
				<div class="board-name"><a class="link_name" href="?search.menuid=100">자유게시판</a></div>
				<div class="board-list"><a class="article" href="?articleid=14">새글14</a></div>
			</td>
			<td class="td_name"><div class="pers_nick_area"><table><tbody><tr><td class="p-nick"><a class="m-tcol-c">홍길동</a></td></tr></tbody></table></div></td>
		</tr>
		<tr>
			<td class="td_date">2024.03.14.</td>
			<td class="td_article">
				<div class="board-name"><a class="link_name" href="?search.menuid=100">자유게시판</a></div>
				<div class="board-list"><a class="article" href="?articleid=13">새글13</a></div>
			</td>
			<td class="td_name"><div class="pers_nick_area"><table><tbody><tr><td class="p-nick"><a class="m-tcol-c">이순신</a></td></tr></tbody></table></div></td>
		</tr>
		<tr>
			<td class="td_date">2024.03.13.</td>
			<td class="td_article">
				<div class="board-name"><a class="link_name" href="?search.menuid=100">자유게시판</a></div>
				<div class="board-list"><a class="article" href="?articleid=12">구글12</a></div>
			</td>
			<td class="td_name"><div class="pers_nick_area"><table><tbody><tr><td class="p-nick"><a class="m-tcol-c">박혁거세</a></td></tr></tbody></table></div></td>
		</tr>
	</tbody></table></div></body></html>`

	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlList, http.StatusOK), nil).Once()
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlDetailOK, http.StatusOK), nil).Maybe()

	articles, cursors, msg, err := c.crawlArticles(context.Background())

	assert.NoError(t, err)
	assert.Empty(t, msg)
	assert.Len(t, articles, 2)

	// 역순 정렬 확인 (오래된 순)
	assert.Equal(t, "13", articles[0].ArticleID)
	assert.Equal(t, "14", articles[1].ArticleID)
	assert.Equal(t, "14", cursors[provider.EmptyBoardID])
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCrawlArticles_DelayCutoff
// ─────────────────────────────────────────────────────────────────────────────

func TestCrawlArticles_DelayCutoff(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)
	b := &config.BoardConfig{ID: "100", Name: "자유게시판"}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b})

	r.On("GetCrawlingCursor", mock.Anything, "navercafe-test", "").Return("12", time.Time{}, nil)

	// 현재 시간 기준 딜레이 통과/미통과 항목 작성
	// c.crawlingDelayMinutes = 40 이므로 10분 전에 쓰인 새 글은 딜레이 미통과로 Skip!
	recentTimeStr := time.Now().Add(-10 * time.Minute).Format("15:04")
	pastTimeStr := time.Now().Add(-50 * time.Minute).Format("15:04")

	htmlList := `<html><body><div class="article-board"><table><tbody>
		<tr>
			<td class="td_date">` + recentTimeStr + `</td>
			<td class="td_article">
				<div class="board-name"><a class="link_name" href="?search.menuid=100">자유게시판</a></div>
				<div class="board-list"><a class="article" href="?articleid=15">최신글15 (딜레이 스킵)</a></div>
			</td>
			<td class="td_name"><div class="pers_nick_area"><table><tbody><tr><td class="p-nick"><a class="m-tcol-c">홍길동</a></td></tr></tbody></table></div></td>
		</tr>
		<tr>
			<td class="td_date">` + pastTimeStr + `</td>
			<td class="td_article">
				<div class="board-name"><a class="link_name" href="?search.menuid=100">자유게시판</a></div>
				<div class="board-list"><a class="article" href="?articleid=14">과거글14 (통과)</a></div>
			</td>
			<td class="td_name"><div class="pers_nick_area"><table><tbody><tr><td class="p-nick"><a class="m-tcol-c">이순신</a></td></tr></tbody></table></div></td>
		</tr>
		<tr>
			<td class="td_date">2024.03.13.</td>
			<td class="td_article">
				<div class="board-name"><a class="link_name" href="?search.menuid=100">자유게시판</a></div>
				<div class="board-list"><a class="article" href="?articleid=12">구글12</a></div>
			</td>
			<td class="td_name"><div class="pers_nick_area"><table><tbody><tr><td class="p-nick"><a class="m-tcol-c">박혁거세</a></td></tr></tbody></table></div></td>
		</tr>
	</tbody></table></div></body></html>`

	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlList, http.StatusOK), nil).Once()
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlDetailOK, http.StatusOK), nil).Maybe()

	articles, cursors, msg, err := c.crawlArticles(context.Background())

	assert.NoError(t, err)
	assert.Empty(t, msg)
	assert.Len(t, articles, 1)

	// ArticleID 15는 수집되지 않아야 함 (딜레이 필터)
	assert.Equal(t, "14", articles[0].ArticleID)
	// 신규 커서 역시 14까지만 업데이트 되어야 함
	assert.Equal(t, "14", cursors[provider.EmptyBoardID])
}


