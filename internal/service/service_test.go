package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/darkkaiser/rss-feed-server/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Service 인터페이스 계약(Contract) 테스트
//
// service.Service는 Echo HTTP 서버, 크롤러 등 모든 백그라운드 서비스가 따르는
// 단일 메서드 인터페이스입니다.
//
// 계약 명세:
//   - Start(ctx, wg): 서비스를 시작하고, 종료 시 wg.Done()을 반드시 호출한다.
//   - ctx가 취소되면 서비스는 정상적으로 종료 절차를 밟아야 한다.
//   - 에러가 발생하면 wg.Done()을 호출한 후 에러를 반환해야 한다.
// =============================================================================

// mockService service.Service 인터페이스의 테스트용 최소 구현체입니다.
type mockService struct {
	startCalled bool
	startErr    error
	receivedCtx context.Context
}

// 컴파일 타임에 mockService가 service.Service를 구현하는지 검증합니다.
var _ service.Service = (*mockService)(nil)

func (m *mockService) Start(ctx context.Context, wg *sync.WaitGroup) error {
	m.startCalled = true
	m.receivedCtx = ctx
	defer wg.Done()
	return m.startErr
}

// waitForWG WaitGroup이 제한 시간 내에 해제되는지 확인하는 헬퍼입니다.
func waitForWG(t *testing.T, wg *sync.WaitGroup, timeout time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		// 정상 해제
	case <-time.After(timeout):
		t.Fatal("WaitGroup이 제한 시간 내에 해제되지 않았습니다 — Start에서 wg.Done()이 호출되어야 합니다")
	}
}

// =============================================================================
// Service 인터페이스 계약 명세 테스트
// =============================================================================

func TestService_Interface(t *testing.T) {
	t.Run("Start는 호출 시 WaitGroup을 반드시 Done 처리해야 한다", func(t *testing.T) {
		svc := &mockService{}
		wg := &sync.WaitGroup{}
		wg.Add(1)

		err := svc.Start(context.Background(), wg)

		require.NoError(t, err)
		assert.True(t, svc.startCalled, "Start가 호출되어야 한다")
		waitForWG(t, wg, 100*time.Millisecond)
	})

	t.Run("Start는 에러 발생 시에도 WaitGroup을 Done 처리하고 에러를 반환해야 한다", func(t *testing.T) {
		svc := &mockService{startErr: assert.AnError}
		wg := &sync.WaitGroup{}
		wg.Add(1)

		err := svc.Start(context.Background(), wg)

		assert.ErrorIs(t, err, assert.AnError, "Start는 에러를 그대로 반환해야 한다")
		// 에러가 발생해도 wg.Done()은 호출되어야 한다
		waitForWG(t, wg, 100*time.Millisecond)
	})

	t.Run("Start는 이미 취소된 Context도 패닉 없이 처리해야 한다", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Start 호출 전에 이미 취소된 Context

		svc := &mockService{}
		wg := &sync.WaitGroup{}
		wg.Add(1)
		// t.Cleanup으로 테스트 종료 시 WG leak을 방어합니다.
		t.Cleanup(func() { wg.Wait() })

		assert.NotPanics(t, func() {
			_ = svc.Start(ctx, wg)
		})
		assert.True(t, svc.startCalled, "Start는 취소된 Context로도 호출되어야 한다")
		// 전달된 Context가 올바르게 수신되었는지 확인
		assert.Equal(t, ctx, svc.receivedCtx, "Start에 전달된 Context가 그대로 수신되어야 한다")
	})

	t.Run("Start는 WithDeadline Context가 만료되어도 패닉 없이 처리해야 한다", func(t *testing.T) {
		// 이미 만료된 데드라인을 가진 Context
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()

		svc := &mockService{}
		wg := &sync.WaitGroup{}
		wg.Add(1)
		t.Cleanup(func() { wg.Wait() })

		assert.NotPanics(t, func() {
			_ = svc.Start(ctx, wg)
		})
	})
}
