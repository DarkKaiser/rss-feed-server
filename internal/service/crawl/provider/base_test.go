package provider_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"
)

// =============================================================================
// Mock 구현체
// =============================================================================

// dummyFetcher는 fetcher.Fetcher 인터페이스를 만족하는 더미 객체입니다.
type dummyFetcher struct{}

func (d *dummyFetcher) Close() error                                 { return nil }
func (d *dummyFetcher) Do(req *http.Request) (*http.Response, error) { return nil, nil }

// mockRepository는 feed.Repository 인터페이스를 만족하는 유연한 테스트 전용 객체입니다.
type mockRepository struct {
	SaveArticlesFunc                 func(ctx context.Context, providerID string, articles []*feed.Article) (int, error)
	GetArticlesFunc                  func(ctx context.Context, providerID string, boardIDs []string, limit uint) ([]*feed.Article, error)
	GetCrawlingCursorFunc            func(ctx context.Context, providerID, boardID string) (string, time.Time, error)
	UpsertLatestCrawledArticleIDFunc func(ctx context.Context, providerID, boardID, articleID string) error
}

func (m *mockRepository) SaveArticles(ctx context.Context, providerID string, articles []*feed.Article) (int, error) {
	if m.SaveArticlesFunc != nil {
		return m.SaveArticlesFunc(ctx, providerID, articles)
	}
	return len(articles), nil
}

func (m *mockRepository) GetArticles(ctx context.Context, providerID string, boardIDs []string, limit uint) ([]*feed.Article, error) {
	if m.GetArticlesFunc != nil {
		return m.GetArticlesFunc(ctx, providerID, boardIDs, limit)
	}
	return nil, nil
}

func (m *mockRepository) GetCrawlingCursor(ctx context.Context, providerID, boardID string) (string, time.Time, error) {
	if m.GetCrawlingCursorFunc != nil {
		return m.GetCrawlingCursorFunc(ctx, providerID, boardID)
	}
	return "", time.Time{}, nil
}

func (m *mockRepository) UpsertLatestCrawledArticleID(ctx context.Context, providerID, boardID, articleID string) error {
	if m.UpsertLatestCrawledArticleIDFunc != nil {
		return m.UpsertLatestCrawledArticleIDFunc(ctx, providerID, boardID, articleID)
	}
	return nil
}

// =============================================================================
// A. 인스턴스 생성 및 초기화 검증 
// =============================================================================

func TestNewBase_PanicFetcher(t *testing.T) {
	t.Parallel()

	params := provider.NewCrawlerParams{
		ProviderID: "test-provider",
		Config:     &config.ProviderDetailConfig{ID: "test", Name: "테스트사이트"},
		Fetcher:    nil, // 패닉 유발 트리거
		FeedRepo:   &mockRepository{},
	}

	assert.PanicsWithValue(t, "NewBase: 스크래핑 작업에는 Fetcher 주입이 필수입니다 (ProviderID=test-provider)", func() {
		provider.NewBase(params, 10)
	})
}

func TestNewBase_Success(t *testing.T) {
	t.Parallel()

	cfg := &config.ProviderDetailConfig{
		ID:   "test",
		Name: "테스트사이트",
	}

	params := provider.NewCrawlerParams{
		ProviderID: "test-provider",
		Config:     cfg,
		Fetcher:    &dummyFetcher{},
		FeedRepo:   &mockRepository{},
	}
	
	maxPageCount := 5
	base := provider.NewBase(params, maxPageCount)

	require.NotNil(t, base)
	assert.Equal(t, "test-provider", base.ProviderID())
	assert.Equal(t, cfg, base.Config())
	assert.Equal(t, maxPageCount, base.MaxPageCount())
	assert.NotNil(t, base.Scraper())
	assert.NotNil(t, base.FeedRepo())
	assert.NotNil(t, base.Logger())
}

// =============================================================================
// B. 실행 파이프라인 검증
// =============================================================================

func TestRun_PanicRecovery(t *testing.T) {
	t.Parallel()

	base := provider.NewBase(provider.NewCrawlerParams{
		ProviderID: "test-provider",
		Config:     &config.ProviderDetailConfig{ID: "test", Name: "테스트사이트"},
		Fetcher:    &dummyFetcher{},
	}, 1)

	// 의도적으로 패닉을 발생시키는 함수 주입
	base.SetCrawlArticles(func(ctx context.Context) ([]*feed.Article, map[string]string, string, error) {
		panic("의도된 런타임 패닉")
	})

	// Run이 패닉으로 인해 죽지 않고 정상적으로 리턴되는지 검증
	assert.NotPanics(t, func() {
		base.Run(context.Background())
	})
}

// prepareExecution 검증: CrawlArticlesFunc가 주입되지 않았을 때 패닉 발생 검증
// Run을 간접 호출하여 prepareExecution 시 패닉이 나고 Run 내부 defer에서 이를 복구하는 것을 확인
func TestPrepareExecution_MissingCrawlArticles(t *testing.T) {
	t.Parallel()

	base := provider.NewBase(provider.NewCrawlerParams{
		ProviderID: "test-provider",
		Config:     &config.ProviderDetailConfig{ID: "test", Name: "테스트사이트"},
		Fetcher:    &dummyFetcher{},
	}, 1)

	// SetCrawlArticles 호출 누락

	// Run을 호출하면 prepareExecution 내부에서 패닉이 발생하고, Run의 recover에서 잡힘
	assert.NotPanics(t, func() {
		base.Run(context.Background())
	})
}

func TestExecute_Error(t *testing.T) {
	t.Parallel()

	base := provider.NewBase(provider.NewCrawlerParams{
		ProviderID: "test-provider",
		Config:     &config.ProviderDetailConfig{ID: "test", Name: "테스트사이트"},
		Fetcher:    &dummyFetcher{},
	}, 1)

	expectedErr := errors.New("수집 오류 발생")
	base.SetCrawlArticles(func(ctx context.Context) ([]*feed.Article, map[string]string, string, error) {
		return nil, nil, "테스트 중 발생한 에러", expectedErr
	})

	// mockRepository를 주입하지 않아 finalizeExecution이 불리면 패닉이 날 수 있으나,
	// 에러 맵핑으로 인해 finalizeExecution 로직은 스킵되어야 함.
	assert.NotPanics(t, func() {
		base.Run(context.Background())
	})
}

func TestFinalizeExecution_DBSaveSuccessAndCursorUpdate(t *testing.T) {
	t.Parallel()

	saveCalled := false
	updateCalled := false

	repo := &mockRepository{
		SaveArticlesFunc: func(ctx context.Context, providerID string, articles []*feed.Article) (int, error) {
			saveCalled = true
			return len(articles), nil // 성공 반환
		},
		UpsertLatestCrawledArticleIDFunc: func(ctx context.Context, providerID, boardID, articleID string) error {
			updateCalled = true
			return nil
		},
	}

	base := provider.NewBase(provider.NewCrawlerParams{
		ProviderID: "test-provider",
		Config:     &config.ProviderDetailConfig{ID: "test", Name: "테스트사이트"},
		Fetcher:    &dummyFetcher{},
		FeedRepo:   repo,
	}, 1)

	articles := []*feed.Article{{ArticleID: "1"}}
	cursors := map[string]string{"b1": "1"}

	base.SetCrawlArticles(func(ctx context.Context) ([]*feed.Article, map[string]string, string, error) {
		return articles, cursors, "", nil
	})

	base.Run(context.Background())

	assert.True(t, saveCalled, "SaveArticles가 호출되어야 합니다.")
	assert.True(t, updateCalled, "SaveArticles 성공 시 Cursor가 업데이트되어야 합니다.")
}

func TestFinalizeExecution_DBSaveFailureIgnoresCursorUpdate(t *testing.T) {
	t.Parallel()

	saveCalled := false
	updateCalled := false

	repo := &mockRepository{
		SaveArticlesFunc: func(ctx context.Context, providerID string, articles []*feed.Article) (int, error) {
			saveCalled = true
			return 0, errors.New("DB 저장 실패") // 실패 반환
		},
		UpsertLatestCrawledArticleIDFunc: func(ctx context.Context, providerID, boardID, articleID string) error {
			updateCalled = true
			return nil
		},
	}

	base := provider.NewBase(provider.NewCrawlerParams{
		ProviderID: "test-provider",
		Config:     &config.ProviderDetailConfig{ID: "test", Name: "테스트사이트"},
		Fetcher:    &dummyFetcher{},
		FeedRepo:   repo,
	}, 1)

	articles := []*feed.Article{{ArticleID: "1"}}
	cursors := map[string]string{"b1": "1"}

	base.SetCrawlArticles(func(ctx context.Context) ([]*feed.Article, map[string]string, string, error) {
		return articles, cursors, "", nil
	})

	base.Run(context.Background())

	assert.True(t, saveCalled, "SaveArticles가 호출되어야 합니다.")
	assert.False(t, updateCalled, "SaveArticles 실패 시 Cursor 업데이트는 생략되어야 합니다.")
}

func TestFinalizeExecution_EmptyArticlesCursorUpdate(t *testing.T) {
	t.Parallel()

	saveCalled := false
	updateCalled := false

	repo := &mockRepository{
		SaveArticlesFunc: func(ctx context.Context, providerID string, articles []*feed.Article) (int, error) {
			saveCalled = true
			return 0, nil
		},
		UpsertLatestCrawledArticleIDFunc: func(ctx context.Context, providerID, boardID, articleID string) error {
			updateCalled = true
			return nil
		},
	}

	base := provider.NewBase(provider.NewCrawlerParams{
		ProviderID: "test-provider",
		Config:     &config.ProviderDetailConfig{ID: "test", Name: "테스트사이트"},
		Fetcher:    &dummyFetcher{},
		FeedRepo:   repo,
	}, 1)

	articles := make([]*feed.Article, 0) // 빈 슬라이스 (nil 아님)
	cursors := map[string]string{"b1": "1"}

	base.SetCrawlArticles(func(ctx context.Context) ([]*feed.Article, map[string]string, string, error) {
		return articles, cursors, "", nil
	})

	base.Run(context.Background())

	assert.False(t, saveCalled, "신규 게시글이 없으면 SaveArticles는 호출되지 않아야 합니다.")
	assert.True(t, updateCalled, "신규 게시글이 없어도 Cursor는 업데이트되어야 합니다.")
}

// TestUpdateCursors_EmptyBoardID 치환 검증
func TestUpdateCursors_EmptyBoardIDSubstitution(t *testing.T) {
	t.Parallel()

	var updatedBoardID string

	repo := &mockRepository{
		UpsertLatestCrawledArticleIDFunc: func(ctx context.Context, providerID, boardID, articleID string) error {
			updatedBoardID = boardID
			return nil
		},
	}

	base := provider.NewBase(provider.NewCrawlerParams{
		ProviderID: "test-provider",
		Config:     &config.ProviderDetailConfig{ID: "test", Name: "테스트사이트"},
		Fetcher:    &dummyFetcher{},
		FeedRepo:   repo,
	}, 1)

	articles := make([]*feed.Article, 0) // 0건 (nil 아님)
	cursors := map[string]string{provider.EmptyBoardID: "999"}

	base.SetCrawlArticles(func(ctx context.Context) ([]*feed.Article, map[string]string, string, error) {
		return articles, cursors, "", nil
	})

	base.Run(context.Background())

	assert.Equal(t, "", updatedBoardID, "#empty# 식별자는 빈 문자열로 치환되어야 합니다.")
}

// =============================================================================
// C. 유틸리티 로직 검증
// =============================================================================

func TestMessagef(t *testing.T) {
	t.Parallel()

	base := provider.NewBase(provider.NewCrawlerParams{
		ProviderID: "test-provider",
		Config:     &config.ProviderDetailConfig{ID: "test", Name: "테스트사이트"},
		Fetcher:    &dummyFetcher{},
	}, 1)

	msg := base.Messagef("작업 상황: %s", "문제 없음")
	assert.Equal(t, "테스트사이트('test')의 작업 상황: 문제 없음", msg)
}

func TestReportError_WithNotifyNil(t *testing.T) {
	t.Parallel()

	// NotifyClient가 nil인 경우에도 시스템이 다운되지 않고 정상 반환되어야 함
	base := provider.NewBase(provider.NewCrawlerParams{
		ProviderID:   "test-provider",
		Config:       &config.ProviderDetailConfig{ID: "test", Name: "테스트사이트"},
		Fetcher:      &dummyFetcher{},
		NotifyClient: nil, // 의도적 nil
	}, 1)

	assert.NotPanics(t, func() {
		base.ReportError("강제 에러 메세지", errors.New("테스트 에러"))
	})
	assert.NotPanics(t, func() {
		base.ReportError("에러 없는 단순 경고 메세지", nil)
	})
}

// =============================================================================
// D. 동시성 제어 검증 
// =============================================================================

func TestCrawlArticleContentsConcurrently_Success(t *testing.T) {
	t.Parallel()

	base := provider.NewBase(provider.NewCrawlerParams{
		ProviderID: "test",
		Config:     &config.ProviderDetailConfig{},
		Fetcher:    &dummyFetcher{},
	}, 1)

	articles := []*feed.Article{
		{ArticleID: "1"},
		{ArticleID: "2"},
		{ArticleID: "3"},
	}

	callCount := 0
	err := base.CrawlArticleContentsConcurrently(context.Background(), articles, 2, func(ctx context.Context, article *feed.Article) error {
		callCount++
		article.Content = "테스트 본문"
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 3, callCount, "각 파싱 함수는 게시글 수 만큼 정확히 3번 호출되어야 합니다.")
	
	for _, a := range articles {
		assert.Equal(t, "테스트 본문", a.Content)
	}
}

func TestCrawlArticleContentsConcurrently_ContentUnavailableShortCircuit(t *testing.T) {
	t.Parallel()

	base := provider.NewBase(provider.NewCrawlerParams{
		ProviderID: "test",
		Config:     &config.ProviderDetailConfig{},
		Fetcher:    &dummyFetcher{},
	}, 1)

	articles := []*feed.Article{{ArticleID: "1"}}
	
	callCount := 0
	err := base.CrawlArticleContentsConcurrently(context.Background(), articles, 0, func(ctx context.Context, article *feed.Article) error {
		callCount++
		return provider.ErrContentUnavailable
	})

	assert.NoError(t, err, "부분 실패(권한 부족)는 동시성 파이프라인의 에러로 전파되지 않아야 합니다.")
	assert.Equal(t, 1, callCount, "ErrContentUnavailable 반환 시 무의미한 재시도를 즉시 차단(단락 평가)하여 단 1회만 호출되어야 합니다.")
}

func TestCrawlArticleContentsConcurrently_GoroutinePanicRecovery(t *testing.T) {
	t.Parallel()

	base := provider.NewBase(provider.NewCrawlerParams{
		ProviderID: "test",
		Config:     &config.ProviderDetailConfig{},
		Fetcher:    &dummyFetcher{},
	}, 1)

	articles := []*feed.Article{{ArticleID: "panic"}, {ArticleID: "normal"}}

	err := base.CrawlArticleContentsConcurrently(context.Background(), articles, 0, func(ctx context.Context, article *feed.Article) error {
		if article.ArticleID == "panic" {
			panic("본문 파싱 중 발생한 예측 못한 에러")
		}
		article.Content = "정상 처리 완료"
		return nil
	})

	assert.NoError(t, err, "고루틴 런타임 패닉이 발생해도 전체 크롤링 파이프라인은 복구되어야 합니다.")
	assert.Equal(t, "", articles[0].Content, "패닉이 발생한 게시글은 빈 본문 처리")
	assert.Equal(t, "정상 처리 완료", articles[1].Content, "정상적인 게시글은 끝까지 파싱 완료")
}

func TestCrawlArticleContentsConcurrently_ContextCanceled(t *testing.T) {
	t.Parallel()

	base := provider.NewBase(provider.NewCrawlerParams{
		ProviderID: "test",
		Config:     &config.ProviderDetailConfig{},
		Fetcher:    &dummyFetcher{},
	}, 1)

	articles := []*feed.Article{{ArticleID: "1"}, {ArticleID: "2"}}
	
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 시작부터 컨텍스트 취소 신호

	err := base.CrawlArticleContentsConcurrently(ctx, articles, 0, func(c context.Context, article *feed.Article) error {
		return nil
	})

	assert.ErrorIs(t, err, context.Canceled, "취소된 컨텍스트가 주입되면 동시성 파이프라인이 즉시 오류를 반환해야 합니다.")
}
