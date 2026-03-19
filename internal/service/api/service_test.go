package api

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test Helpers & Mocks
// =============================================================================

// mockFeedRepository feed.Repository 인터페이스의 테스트용 구현체입니다.
type mockFeedRepository struct{}

var _ feed.Repository = (*mockFeedRepository)(nil)

func (m *mockFeedRepository) InsertArticles(_ string, _ []*feed.Article) (int, error) {
	return 0, nil
}

func (m *mockFeedRepository) GetArticles(_ string, _ []string, _ uint) ([]*feed.Article, error) {
	return nil, nil
}

func (m *mockFeedRepository) GetLatestCrawledInfo(_ string, _ string) (string, time.Time, error) {
	return "", time.Time{}, nil
}

func (m *mockFeedRepository) UpdateLatestCrawledArticleID(_ string, _ string, _ string) error {
	return nil
}

// newTestAppConfig 테스트에서 공통으로 사용할 최소 AppConfig를 생성합니다.
// ListenPort=0 으로 설정하여 OS가 빈 포트를 자동 할당하도록 합니다.
func newTestAppConfig() *config.AppConfig {
	return &config.AppConfig{
		WS: config.WSConfig{
			ListenPort: 0,
		},
	}
}

// startAndWaitRunning 서비스를 시작하고, 고루틴이 스케줄링되어 running이 true가 될
// 때까지 짧은 시간 동안 폴링합니다. 테스트에서 race condition을 방지하기 위해 사용합니다.
func startAndWaitRunning(t *testing.T, svc *Service, ctx context.Context, wg *sync.WaitGroup) {
	t.Helper()
	err := svc.Start(ctx, wg)
	require.NoError(t, err)
	// HTTP 서버가 포트에 바인딩될 시간을 확보합니다.
	time.Sleep(50 * time.Millisecond)
}

// =============================================================================
// NewService 테스트
// =============================================================================

func TestNewService(t *testing.T) {
	t.Run("성공: 유효한 인자로 서비스가 생성된다", func(t *testing.T) {
		appConfig := newTestAppConfig()
		repo := &mockFeedRepository{}

		svc := NewService(appConfig, repo, nil)

		require.NotNil(t, svc)
		assert.Equal(t, appConfig, svc.appConfig)
		assert.Equal(t, repo, svc.feedRepo)
		assert.Nil(t, svc.notifyClient)
		assert.False(t, svc.running, "최초 생성 시 running은 false여야 한다")
	})

	t.Run("패닉: appConfig가 nil이면 패닉이 발생한다", func(t *testing.T) {
		assert.Panics(t, func() {
			NewService(nil, &mockFeedRepository{}, nil)
		})
	})

	t.Run("패닉: feedRepo가 nil이면 패닉이 발생한다", func(t *testing.T) {
		assert.Panics(t, func() {
			NewService(newTestAppConfig(), nil, nil)
		})
	})
}

// =============================================================================
// Start 테스트
// =============================================================================

func TestService_Start(t *testing.T) {
	t.Run("성공: 정상 시작 후 running 플래그가 true가 된다", func(t *testing.T) {
		svc := NewService(newTestAppConfig(), &mockFeedRepository{}, nil)
		ctx, cancel := context.WithCancel(context.Background())
		wg := &sync.WaitGroup{}
		wg.Add(1)

		err := svc.Start(ctx, wg)
		require.NoError(t, err)
		assert.True(t, svc.running)

		// 정상 종료하여 고루틴 누수 방지
		cancel()
		wg.Wait()
		assert.False(t, svc.running, "종료 후 running은 false여야 한다")
	})

	t.Run("성공: Context 취소 시 Graceful Shutdown이 shutdownTimeout 이내에 완료된다", func(t *testing.T) {
		svc := NewService(newTestAppConfig(), &mockFeedRepository{}, nil)
		ctx, cancel := context.WithCancel(context.Background())
		wg := &sync.WaitGroup{}
		wg.Add(1)

		startAndWaitRunning(t, svc, ctx, wg)
		assert.True(t, svc.running)

		cancel()

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			assert.False(t, svc.running, "종료 후 running은 false여야 한다")
		case <-time.After(shutdownTimeout + time.Second):
			t.Fatal("서비스가 제한 시간 내에 종료되지 않았습니다")
		}
	})

	t.Run("nil 반환: 서비스가 이미 실행 중인 경우 nil을 반환한다", func(t *testing.T) {
		svc := NewService(newTestAppConfig(), &mockFeedRepository{}, nil)
		ctx, cancel := context.WithCancel(context.Background())
		wg := &sync.WaitGroup{}
		wg.Add(1)

		// 첫 번째 Start
		require.NoError(t, svc.Start(ctx, wg))

		// 두 번째 Start: 이미 running 중이므로 nil 반환 + WG Done() 즉시 호출
		wg.Add(1)
		err := svc.Start(ctx, wg)
		assert.NoError(t, err, "이미 실행 중일 때 error가 아닌 nil을 반환해야 한다")

		cancel()
		wg.Wait()
	})

	t.Run("에러 반환: feedRepo가 nil인 Service를 Start하면 에러가 반환된다", func(t *testing.T) {
		// NewService 패닉을 우회하여 직접 nil 주입
		svc := &Service{
			appConfig: newTestAppConfig(),
			feedRepo:  nil,
		}
		wg := &sync.WaitGroup{}
		wg.Add(1)

		err := svc.Start(context.Background(), wg)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "feed.Repository")
	})
}

// =============================================================================
// handleServerError 테스트
// =============================================================================

func TestService_handleServerError(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		err  error
	}{
		{
			// nil: 정상 종료 신호. 아무 작업도 하지 않고 즉시 반환해야 한다.
			name: "nil 에러: 패닉 없이 정상 반환한다",
			err:  nil,
		},
		{
			// http.ErrServerClosed: Shutdown 호출에 의한 정상 종료.
			// Info 레벨 로그만 기록하고 알림은 전송하지 않아야 한다.
			name: "ErrServerClosed: 패닉 없이 Info 로그만 기록하고 반환한다",
			err:  http.ErrServerClosed,
		},
		{
			// 예상치 못한 에러: Error 레벨 로그를 기록하고, notifyClient가 nil이므로 알림 전송은 생략한다.
			name: "예상치 못한 에러 + notifyClient nil: 패닉 없이 에러 로그만 기록한다",
			err:  assert.AnError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(newTestAppConfig(), &mockFeedRepository{}, nil)
			assert.NotPanics(t, func() {
				svc.handleServerError(ctx, tt.err)
			})
		})
	}
}

// =============================================================================
// waitForShutdown 테스트
// =============================================================================

func TestService_waitForShutdown_ServerDiesFirst(t *testing.T) {
	t.Run("httpServerDone이 먼저 닫히면: Shutdown 없이 cleanup만 수행하고 즉시 반환한다", func(t *testing.T) {
		svc := NewService(newTestAppConfig(), &mockFeedRepository{}, nil)

		// running을 수동으로 true로 설정
		svc.runningMu.Lock()
		svc.running = true
		svc.runningMu.Unlock()

		// 이미 닫힌 httpServerDone 채널과 취소되지 않은 ctx를 전달
		// → select가 즉시 httpServerDone 케이스를 선택하고 cleanup만 수행
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		httpServerDone := make(chan struct{})
		close(httpServerDone)

		// 별도 Echo 서버 없이 waitForShutdown 직접 호출
		// 주의: 이 케이스에서는 e.Shutdown이 호출되지 않으므로 Echo 인스턴스가 불필요
		e := NewEchoServer(ServerConfig{AllowOrigins: []string{"*"}}, views)

		done := make(chan struct{})
		go func() {
			svc.waitForShutdown(ctx, e, httpServerDone)
			close(done)
		}()

		select {
		case <-done:
			// cleanup이 호출되어 running이 false가 되어야 한다
			svc.runningMu.Lock()
			defer svc.runningMu.Unlock()
			assert.False(t, svc.running, "httpServerDone 수신 후 cleanup이 호출되어 running은 false여야 한다")
		case <-time.After(time.Second):
			t.Fatal("waitForShutdown이 제한 시간 내에 반환되지 않았습니다")
		}
	})
}

func TestService_waitForShutdown_GracefulShutdown(t *testing.T) {
	t.Run("Context가 취소되면: Graceful Shutdown 후 cleanup을 수행한다", func(t *testing.T) {
		svc := NewService(newTestAppConfig(), &mockFeedRepository{}, nil)

		// running을 수동으로 true로 설정
		svc.runningMu.Lock()
		svc.running = true
		svc.runningMu.Unlock()

		ctx, cancel := context.WithCancel(context.Background())
		httpServerDone := make(chan struct{})

		// 실제 Echo 서버를 생성하되 포트를 바인딩하지 않은 상태로 Shutdown 호출 가능
		e := NewEchoServer(ServerConfig{AllowOrigins: []string{"*"}}, views)

		done := make(chan struct{})
		go func() {
			svc.waitForShutdown(ctx, e, httpServerDone)
			close(done)
		}()

		// 짧은 시간 후 ctx 취소 및 httpServerDone 신호 전송
		time.Sleep(20 * time.Millisecond)
		cancel()
		// e.Shutdown 이후 httpServerDone이 닫혀야 waitForShutdown이 반환됨
		// 실제 서버 없이 직접 close하여 시뮬레이션
		close(httpServerDone)

		select {
		case <-done:
			svc.runningMu.Lock()
			defer svc.runningMu.Unlock()
			assert.False(t, svc.running, "Graceful Shutdown 후 running은 false여야 한다")
		case <-time.After(shutdownTimeout + time.Second):
			t.Fatal("waitForShutdown이 제한 시간 내에 반환되지 않았습니다")
		}
	})
}

// =============================================================================
// cleanup 테스트
// =============================================================================

func TestService_cleanup(t *testing.T) {
	t.Run("성공: cleanup 호출 시 running 플래그가 false로 초기화된다", func(t *testing.T) {
		svc := NewService(newTestAppConfig(), &mockFeedRepository{}, nil)

		svc.runningMu.Lock()
		svc.running = true
		svc.runningMu.Unlock()

		svc.cleanup()

		svc.runningMu.Lock()
		defer svc.runningMu.Unlock()
		assert.False(t, svc.running, "cleanup 호출 후 running은 false여야 한다")
	})

	t.Run("성공: cleanup은 이미 false인 상태에서도 패닉 없이 실행된다", func(t *testing.T) {
		svc := NewService(newTestAppConfig(), &mockFeedRepository{}, nil)
		assert.False(t, svc.running)
		assert.NotPanics(t, func() {
			svc.cleanup()
		})
	})
}
