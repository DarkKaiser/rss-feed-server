package ssangbonges

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

// mockFeedRepo is a mock for feed.Repository
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

func setupTestCrawler(t *testing.T, f fetcher.Fetcher, r *mockFeedRepo, bTypes []*config.BoardConfig) *crawler {
	cfg := &config.ProviderDetailConfig{
		ID:     "testsid",
		Name:   "테스트 사이트",
		URL:    "http://test.local",
		Boards: bTypes,
	}

	p := provider.NewCrawlerParams{
		ProviderID:   "ssangbonges",
		Config:       cfg,
		Fetcher:      f,
		FeedRepo:     r,
		NotifyClient: nil,
	}

	base := provider.NewBase(p, 2)
	c := &crawler{Base: base}
	c.SetCrawlArticles(c.crawlArticles)
	return c
}

func TestNewCrawler_Init(t *testing.T) {
	assert.NotNil(t, boardTypes)
	assert.Contains(t, boardTypes, boardTypeList1)
}

func TestCrawlSingleBoard_NetworkFailure_Rollback(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)

	b := &config.BoardConfig{ID: "123", Name: "테스트게시판", Type: boardTypeList1}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b})

	// DB 의 커서 응답
	r.On("GetCrawlingCursor", mock.Anything, "ssangbonges", "123").Return("", time.Time{}, nil)

	// Fetcher 호출 시 네트워크 에러 발생 조작
	expectedErr := errors.New("network timeout")
	f.On("Do", mock.Anything).Return((*http.Response)(nil), expectedErr)

	ctx := context.Background()
	articles, cursor, msg, err := c.crawlSingleBoard(ctx, b)

	assert.Error(t, err)
	assert.Equal(t, "테스트 사이트('testsid')의 '테스트게시판' 게시판의 1번 페이지 목록을 불러오지 못했습니다.", msg)
	assert.Empty(t, cursor)
	assert.Nil(t, articles)

	r.AssertExpectations(t)
}

func TestCrawlSingleBoard_DOMStructureChange(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)

	b := &config.BoardConfig{ID: "123", Name: "테스트게시판", Type: boardTypeList1}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b})

	r.On("GetCrawlingCursor", mock.Anything, "ssangbonges", "123").Return("", time.Time{}, nil)

	// 컨테이너조차 없는 엉뚱한 HTML 반환
	dummyHTML := `<body><div><h1>Nothing here</h1></div></body>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(dummyHTML, http.StatusOK), nil)

	ctx := context.Background()
	articles, cursor, msg, err := c.crawlSingleBoard(ctx, b)

	assert.Error(t, err)
	assert.True(t, apperrors.Is(err, apperrors.System))
	assert.Contains(t, msg, "DOM 구조가 변경되어")
	assert.Empty(t, cursor)
	assert.Nil(t, articles)
}

func TestCrawlSingleBoard_NormalAndCursorStop(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)

	b := &config.BoardConfig{ID: "123", Name: "테스트게시판", Type: boardTypeList1}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b})

	// DB에 마지막 저장된 커서가 10 이라고 가정
	r.On("GetCrawlingCursor", mock.Anything, "ssangbonges", "123").Return("10", time.Time{}, nil)

	// HTML 조작: 12, 11 은 신규, 10 은 이미 존재 (커서 중단 발동), 고정 공지 1개 포함
	dummyHTML := `
	<div class="subContent"><div class="bbs_ListA"><table><tbody>
		<tr><td class="mPre">공지</td><td class="bbs_tit"><a data-id="999">공지글</a></td><td>작성자 UserX</td><td>등록일 2024.01.01.</td><td></td></tr>
		<tr><td>1</td><td class="bbs_tit"><a data-id="12">새글12</a></td><td>작성자 UserA</td><td>등록일 2024.01.02.</td><td></td></tr>
		<tr><td>2</td><td class="bbs_tit"><a data-id="11">새글11</a></td><td>작성자 UserB</td><td>등록일 2024.01.02.</td><td></td></tr>
		<tr><td>3</td><td class="bbs_tit"><a data-id="10">구글10</a></td><td>작성자 UserC</td><td>등록일 2024.01.01.</td><td></td></tr>
	</tbody></table></div></div>
	`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(dummyHTML, http.StatusOK), nil)

	ctx := context.Background()
	articles, cursor, msg, err := c.crawlSingleBoard(ctx, b)

	assert.NoError(t, err)
	assert.Empty(t, msg)
	assert.Equal(t, "12", cursor) // 수집된 최고 신규 커서는 12

	// 오래된 글부터 최신 글 순서로 쌓이므로 [11, 12] 순서여야 함. 공지는 제외됨 (td.mPre)
	assert.Len(t, articles, 2)
	assert.Equal(t, "11", articles[0].ArticleID)
	assert.Equal(t, "새글11", articles[0].Title)
	assert.Equal(t, "12", articles[1].ArticleID)
	assert.Equal(t, "새글12", articles[1].Title)
}

func TestCrawlArticles_Routing(t *testing.T) {
	f := fetchermocks.NewMockFetcher()
	r := new(mockFeedRepo)

	b1 := &config.BoardConfig{ID: "111", Name: "빈게시판", Type: boardTypeList1}
	b2 := &config.BoardConfig{ID: "222", Name: "정상", Type: boardTypeList1}
	c := setupTestCrawler(t, f, r, []*config.BoardConfig{b1, b2})

	r.On("GetCrawlingCursor", mock.Anything, "ssangbonges", "111").Return("", time.Time{}, nil)
	r.On("GetCrawlingCursor", mock.Anything, "ssangbonges", "222").Return("", time.Time{}, nil)

	// 게시판 1: 컨테이너는 있지만 게시글이 0개 (빈 게시판 케이스)
	htmlEmpty := `<div class="subContent"><div class="bbs_ListA"><table><tbody></tbody></table></div></div>`
	
	// 게시판 2: 정상 글 1건
	htmlNormal := `
	<div class="subContent"><div class="bbs_ListA"><table><tbody>
		<tr><td>1</td><td class="bbs_tit"><a data-id="55">글55</a></td><td>작성자 UserA</td><td>등록일 2024.01.02.</td><td></td></tr>
	</tbody></table></div></div>
	`



	// To handle POST body safely in testify mock without consuming `req.Body`:
	// Since both are called in order (b1 then b2), we can just use `f.On("Do", ...).Return(...).Once()` for each.
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlEmpty, http.StatusOK), nil).Once()
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlNormal, http.StatusOK), nil).Once()

	// crawlArticleContent() 호출 시 추가 FetchHTML 이 발생함 (상세페이지 조회 - 이것도 POST로 body에 담김)
	htmlDetail := `<div class="bbs_ViewA"><ul class="bbsV_data"><li>작성자UserA</li><li></li><li></li></ul><div class="bbsV_cont"><p>상세테스트</p></div></div>`
	f.On("Do", mock.Anything).Return(fetchermocks.NewMockResponse(htmlDetail, http.StatusOK), nil).Maybe()

	ctx := context.Background()
	articles, cursors, msg, err := c.crawlArticles(ctx)

	assert.NoError(t, err)
	assert.Empty(t, msg)
	assert.Len(t, articles, 1)
	assert.Equal(t, "55", articles[0].ArticleID)

	assert.Contains(t, cursors, "222")
	assert.Equal(t, "55", cursors["222"])
	assert.NotContains(t, cursors, "111") // 수집된 내역 없으면 커서 맵에 남지 않음

	r.AssertExpectations(t)
}
