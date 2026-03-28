package fetcher_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher/mocks"
	"github.com/stretchr/testify/assert"
)

func TestDrainAndCloseBody(t *testing.T) {
	// 64KB보다 약간 더 큰 데이터 생성
	largeDataSize := fetcher.MaxDrainBytes + 1024
	largeData := make([]byte, largeDataSize)

	tests := []struct {
		name          string
		body          *mocks.MockReadCloser
		expectedClose bool
		expectRead    bool
	}{
		{
			name:          "Nil Body - 안전하게 처리",
			body:          nil,
			expectedClose: false,
			expectRead:    false,
		},
		{
			name:          "Small Body (< maxDrainBytes) - 읽고 닫기",
			body:          mocks.NewMockReadCloserBytes([]byte("small data")),
			expectedClose: true,
			expectRead:    true,
		},
		{
			name:          "Exact Boundary Body (= maxDrainBytes) - 읽고 닫기",
			body:          mocks.NewMockReadCloserBytes(make([]byte, fetcher.MaxDrainBytes)),
			expectedClose: true,
			expectRead:    true,
		},
		{
			name:          "Large Body (> maxDrainBytes) - 제한만큼만 읽고 닫기",
			body:          mocks.NewMockReadCloserBytes(largeData),
			expectedClose: true,
			expectRead:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// mockReadCloser가 nil인 경우를 처리하기 위해 인터페이스로 캐스팅
			var rc io.ReadCloser
			if tt.body != nil {
				rc = tt.body
			}

			fetcher.DrainAndCloseBody(rc)

			if tt.body != nil {
				// MockReadCloser uses atomic counter
				assert.Equal(t, tt.expectedClose, tt.body.GetCloseCount() > 0, "Close() 호출 여부가 일치해야 합니다")

				if tt.expectRead {
					assert.True(t, tt.body.WasRead(), "데이터를 읽으려 시도했어야 합니다")
				}
			}
		})
	}
}

func TestDrainAndCloseBody_ErrorScenarios(t *testing.T) {
	t.Run("Reader에서 에러 반환 시에도 Close는 호출되어야 함", func(t *testing.T) {
		expectedErr := errors.New("read error")

		// 로컬 Mock 대신 testify Mock 사용 (더 정교한 제어 가능)
		// 하지만 MockReadCloser 헬퍼가 있다면 그걸 활용
		// Read 호출 시 에러 반환하도록 설정 (내부 구현에 따라 설정 방식이 다를 수 있으나, 여기선 간단히 헬퍼 확장 없이 로컬 타입 사용)

		// *mocks.MockReadCloser는 bytes.Buffer 기반이라 강제 에러가 어려울 수 있음.
		// 따라서 간단한 stub 사용
		stub := &stubReadCloser{
			readErr: expectedErr,
		}

		fetcher.DrainAndCloseBody(stub)

		assert.True(t, stub.closed, "에러가 발생해도 Body는 반드시 닫혀야 합니다")
	})

	t.Run("이미 닫힌 Body (Read 시 에러) - 안전하게 처리", func(t *testing.T) {
		// 이미 닫힌 바디는 Read 시 에러를 반환함
		stub := &stubReadCloser{
			readErr: io.ErrClosedPipe, // 또는 http.ErrBodyReadAfterClose
		}

		// 패닉 없이 실행되어야 함
		assert.NotPanics(t, func() {
			fetcher.DrainAndCloseBody(stub)
		})

		assert.True(t, stub.closed, "이미 닫힌 바디라도 함수 종료 시 Close가 (다시) 호출됨 (idempotent 가정)")
	})
}

func TestDrainAndCloseBody_AllocationOptimization(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping allocation test in short mode")
	}

	data := make([]byte, 1024)

	// 메모리 할당 횟수 측정
	// sync.Pool을 사용하므로, 반복 실행 시 할당 횟수가 매우 적어야 함 (이상적으로는 0에 수렴)
	allocs := testing.AllocsPerRun(100, func() {
		rc := io.NopCloser(bytes.NewReader(data))
		fetcher.DrainAndCloseBody(rc)
	})

	// 허용 가능한 할당 횟수:
	// - io.LimitReader 구조체 생성 등 소량의 할당은 발생할 수 있음
	// - 하지만 32KB 버퍼를 매번 make() 한다면 할당 횟수나 바이트가 큼 (AllocsPerRun은 횟수 측정)
	// - 엄격하게 5회 미만으로 잡음 (sync.Pool 미사용 시 더 높게 나옴)
	assert.Less(t, allocs, 5.0, "메모리 할당 횟수가 너무 높습니다. sync.Pool이 제대로 동작하지 않을 수 있습니다.")
}

// stubReadCloser: 에러 테스트를 위한 간단한 Stub
type stubReadCloser struct {
	readErr error
	closed  bool
}

func (s *stubReadCloser) Read(p []byte) (n int, err error) {
	if s.readErr != nil {
		return 0, s.readErr
	}
	return 0, io.EOF
}

func (s *stubReadCloser) Close() error {
	s.closed = true
	return nil
}
