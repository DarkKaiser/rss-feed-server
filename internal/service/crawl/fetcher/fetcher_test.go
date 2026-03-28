package fetcher_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestGet_Scenarios는 Fetcher.Get 헬퍼 함수의 다양한 성공/실패 시나리오를 검증합니다.
func TestGet_Scenarios(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		url           string
		ctx           func() context.Context // 각 테스트마다 독립된 컨텍스트 생성
		mockSetup     func(*mocks.MockFetcher)
		checkResponse func(*testing.T, *http.Response)
		expectedError string
	}{
		{
			name: "Success (Status OK)",
			url:  "https://example.com/ok",
			ctx:  func() context.Context { return context.Background() },
			mockSetup: func(m *mocks.MockFetcher) {
				m.On("Do", mock.MatchedBy(func(req *http.Request) bool {
					return req.Method == http.MethodGet && req.URL.String() == "https://example.com/ok"
				})).Return(&http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("ok body")),
				}, nil)
			},
			checkResponse: func(t *testing.T, resp *http.Response) {
				assert.Equal(t, http.StatusOK, resp.StatusCode)
				body, _ := io.ReadAll(resp.Body)
				assert.Equal(t, "ok body", string(body))
			},
		},
		{
			name: "Invalid URL Scheme",
			url:  "://invalid-url",
			ctx:  func() context.Context { return context.Background() },
			mockSetup: func(m *mocks.MockFetcher) {
				// URL 검증에서 실패해야 하므로 Do는 호출되지 않음
			},
			expectedError: "missing protocol scheme",
		},
		{
			name: "Context Already Canceled",
			url:  "https://example.com/timeout",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel() // 이미 취소된 컨텍스트 주입
				return ctx
			},
			mockSetup: func(m *mocks.MockFetcher) {
				// 취소된 컨텍스트로 요청 시 Do가 호출될 경우를 대비
				m.On("Do", mock.MatchedBy(func(req *http.Request) bool {
					return req.Context().Err() != nil
				})).Return(nil, context.Canceled)
			},
			expectedError: "context canceled",
		},
		{
			name: "Fetcher Returns Error (Network Failure)",
			url:  "https://example.com/error",
			ctx:  func() context.Context { return context.Background() },
			mockSetup: func(m *mocks.MockFetcher) {
				m.On("Do", mock.Anything).Return(nil, errors.New("network failure"))
			},
			expectedError: "network failure",
		},
		{
			name: "Fetcher Error with Response Body (Should Drain and Close)",
			url:  "https://example.com/500",
			ctx:  func() context.Context { return context.Background() },
			mockSetup: func(m *mocks.MockFetcher) {
				// mocks.MockReadCloser 생성
				mockBody := mocks.NewMockReadCloser("")

				m.On("Do", mock.Anything).Return(&http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       mockBody,
				}, errors.New("request failed"))
			},
			checkResponse: func(t *testing.T, resp *http.Response) {
				// 에러 반환 시 resp는 nil 체크 안 함
			},
			expectedError: "request failed",
		},
	}

	for _, tt := range tests {
		tt := tt // 캡처링 방지
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel() // 개별 테스트 케이스의 병렬 실행 허용

			// mocks 패키지의 표준 MockFetcher 사용
			m := mocks.NewMockFetcher()
			// Mock 객체에 현재 테스트 컨텍스트 주입 (AssertExpectations 등을 위해)
			m.Test(t)

			if tt.mockSetup != nil {
				tt.mockSetup(m)
			}

			// 테스트 실행
			resp, err := fetcher.Get(tt.ctx(), m, tt.url)

			if tt.expectedError != "" {
				// 에러 검증
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Nil(t, resp)
			} else {
				// 성공 검증
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.checkResponse != nil {
					tt.checkResponse(t, resp)
				}

				// 호출자가 Body를 닫아야 함
				if resp.Body != nil {
					resp.Body.Close()
				}
			}

			m.AssertExpectations(t)
		})
	}
}

// TestGet_DrainBehavior는 에러 발생 시 응답 Body를 안전하게 비우고 닫는 로직을 집중 검증합니다.
func TestGet_DrainBehavior(t *testing.T) {
	t.Parallel()

	// 1. 작은 응답 (64KB 미만): 전체를 읽고 닫아야 함
	t.Run("Small Body (<64KB) Should Be Fully Drained", func(t *testing.T) {
		m := mocks.NewMockFetcher()
		m.Test(t)
		content := bytes.Repeat([]byte("a"), 1024) // 1KB

		mockBody := mocks.NewMockReadCloserBytes(content)

		m.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       mockBody,
		}, errors.New("upstream error"))

		_, err := fetcher.Get(context.Background(), m, "https://example.com")
		assert.Error(t, err)

		assert.True(t, mockBody.WasRead(), "데이터를 읽었어야 합니다")
		assert.Greater(t, mockBody.GetCloseCount(), int64(0), "Body는 반드시 닫혀야 합니다")
		m.AssertExpectations(t)
	})

	// 2. 큰 응답 (64KB 초과): 64KB까지만 읽고 닫아야 함 (DoS 방지)
	t.Run("Large Body (>64KB) Should Be Drained Up To Limit", func(t *testing.T) {
		m := mocks.NewMockFetcher()
		m.Test(t)
		// 64KB + 1byte 크기
		largeData := make([]byte, 64*1024+1)
		mockBody := mocks.NewMockReadCloserBytes(largeData)

		m.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       mockBody,
		}, errors.New("upstream error"))

		_, err := fetcher.Get(context.Background(), m, "https://example.com")
		assert.Error(t, err)

		assert.True(t, mockBody.WasRead(), "데이터를 읽었어야 합니다")
		assert.Greater(t, mockBody.GetCloseCount(), int64(0), "Body는 반드시 닫혀야 합니다")
		m.AssertExpectations(t)
	})

	// 3. Nil Body: 패닉 없이 안전하게 처리되어야 함
	t.Run("Nil Body Should Be Handled Gracefully", func(t *testing.T) {
		m := mocks.NewMockFetcher()
		m.Test(t)
		m.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       nil, // Body가 없음
		}, errors.New("error with nil body"))

		assert.NotPanics(t, func() {
			_, err := fetcher.Get(context.Background(), m, "https://example.com")
			assert.Error(t, err)
		}, "Nil Body 처리는 패닉을 유발하지 않아야 합니다")

		m.AssertExpectations(t)
	})
}

// TestGet_ContextPropagation Context 값이 올바르게 전파되는지 검증합니다.
func TestGet_ContextPropagation(t *testing.T) {
	t.Parallel()

	type ctxKey string
	const key ctxKey = "request-id"
	const val = "req-12345"

	ctx := context.WithValue(context.Background(), key, val)

	m := mocks.NewMockFetcher()
	m.Test(t)
	m.On("Do", mock.MatchedBy(func(req *http.Request) bool {
		// 전파된 컨텍스트 값 확인
		retrieved := req.Context().Value(key)
		return retrieved == val
	})).Return(&http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil)

	resp, err := fetcher.Get(ctx, m, "https://example.com")
	require.NoError(t, err)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}

	m.AssertExpectations(t)
}

// TestFetcher_Close_Propagation verifies that the Close() method call is correctly propagated
// through the entire middleware chain to the underlying HTTPFetcher.
func TestFetcher_Close_Propagation(t *testing.T) {
	// 1. Create a mock delegate that tracks Close calls
	mockDelegate := mocks.NewMockFetcher()

	// We expect Close() to be called exactly once
	mockDelegate.On("Close").Return(nil).Once()

	// 2. Build the middleware chain around the mock
	// We manually construct the chain to ensure all middleware are included
	// Order (outer -> inner):
	// Logging -> UserAgent -> Retry -> MimeType -> StatusCode -> MaxBytes -> Delegate

	var f fetcher.Fetcher = mockDelegate

	f = fetcher.NewMaxBytesFetcher(f, 1024)
	f = fetcher.NewStatusCodeFetcher(f)
	f = fetcher.NewMimeTypeFetcher(f, []string{"application/json"}, false)
	f = fetcher.NewRetryFetcher(f, 3, time.Second, 5*time.Second)
	f = fetcher.NewUserAgentFetcher(f, nil)
	f = fetcher.NewLoggingFetcher(f)

	// 3. Call Close() on the outermost fetcher
	err := f.Close()

	// 4. Verify assertions
	assert.NoError(t, err)
	mockDelegate.AssertExpectations(t)
}

// TestFetcher_Close_ErrorPropagation verifies that errors returned by Close()
// are propagated up the chain.
func TestFetcher_Close_ErrorPropagation(t *testing.T) {
	expectedErr := errors.New("close failed")

	mockDelegate := mocks.NewMockFetcher()
	mockDelegate.On("Close").Return(expectedErr).Once()

	f := fetcher.NewLoggingFetcher(mockDelegate)

	err := f.Close()

	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
	mockDelegate.AssertExpectations(t)
}
