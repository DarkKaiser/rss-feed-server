package fetcher_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher/mocks"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestLoggingFetcher_Do tests various scenarios for LoggingFetcher.Do.
//
// 주의: LoggingFetcher는 전역 로거(logrus.StandardLogger)를 사용하므로
// Test Hook을 안전하게 사용하기 위해 t.Parallel()을 사용하지 않고 순차 실행합니다.
func TestLoggingFetcher_Do(t *testing.T) {
	// Setup Log Hook for validaton
	hook := &testHook{}
	logrus.AddHook(hook)
	originalLevel := logrus.GetLevel()
	logrus.SetLevel(logrus.DebugLevel) // Debug 레벨 로그 캡처를 위해 설정

	// Teardown
	defer func() {
		logrus.SetLevel(originalLevel)
		// 훅 제거 (직접 제거 API가 없으므로 Hooks를 초기화하거나 레벨을 조절해야 함)
		// 여기서는 StandardLogger의 훅을 초기화
		logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))
	}()

	tests := []struct {
		name           string
		reqURL         string
		reqMethod      string
		mockSetup      func(*mocks.MockFetcher)
		expectedLevel  logrus.Level
		validateFields func(*testing.T, logrus.Fields)
		expectedError  string
	}{
		{
			name:      "Success Request (Debug Level)",
			reqURL:    "http://example.com/ok",
			reqMethod: http.MethodGet,
			mockSetup: func(m *mocks.MockFetcher) {
				m.On("Do", mock.Anything).Return(&http.Response{
					StatusCode: 200,
					Status:     "200 OK",
				}, nil)
			},
			expectedLevel: logrus.DebugLevel,
			validateFields: func(t *testing.T, f logrus.Fields) {
				assert.Equal(t, "GET", f["method"])
				assert.Equal(t, "http://example.com/ok", f["url"])
				assert.Equal(t, 200, f["status_code"])
				assert.Equal(t, "200 OK", f["status"])
				assert.NotEmpty(t, f["duration"])
				assert.Equal(t, "crawl.fetcher", f["component"])
			},
		},
		{
			name:      "Network Error (Error Level)",
			reqURL:    "http://example.com/error",
			reqMethod: http.MethodPost,
			mockSetup: func(m *mocks.MockFetcher) {
				m.On("Do", mock.Anything).Return(nil, errors.New("network failure"))
			},
			expectedLevel: logrus.ErrorLevel,
			expectedError: "network failure",
			validateFields: func(t *testing.T, f logrus.Fields) {
				assert.Equal(t, "POST", f["method"])
				assert.Equal(t, "network failure", f["error"])
				assert.Equal(t, "crawl.fetcher", f["component"])
			},
		},
		{
			name:      "HTTP 500 Error with Response (Error Level)",
			reqURL:    "http://example.com/500",
			reqMethod: http.MethodGet,
			mockSetup: func(m *mocks.MockFetcher) {
				// 에러와 함께 응답 객체 반환 (상태 코드 로깅 확인)
				m.On("Do", mock.Anything).Return(&http.Response{
					StatusCode: 500,
					Status:     "500 Internal Server Error",
				}, errors.New("server error"))
			},
			expectedLevel: logrus.ErrorLevel,
			expectedError: "server error",
			validateFields: func(t *testing.T, f logrus.Fields) {
				assert.Equal(t, 500, f["status_code"], "에러 발생 시에도 응답이 있으면 상태 코드를 로깅해야 함")
				assert.Equal(t, "500 Internal Server Error", f["status"])
				assert.Equal(t, "server error", f["error"])
			},
		},
		{
			name:      "Sensitive Information Redaction",
			reqURL:    "http://user:password123@example.com/api?token=secret_token&key=value",
			reqMethod: http.MethodDelete,
			mockSetup: func(m *mocks.MockFetcher) {
				m.On("Do", mock.Anything).Return(&http.Response{StatusCode: 204}, nil)
			},
			expectedLevel: logrus.DebugLevel,
			validateFields: func(t *testing.T, f logrus.Fields) {
				urlLog := f["url"].(string)
				assert.NotContains(t, urlLog, "password123", "비밀번호는 로그에 노출되면 안 됩니다")
				assert.NotContains(t, urlLog, "secret_token", "민감한 쿼리 파라미터 값은 노출되면 안 됩니다")
				assert.Contains(t, urlLog, "xxxxx", "마스킹 처리된 문자열이 포함되어야 합니다")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Hook Reset
			hook.reset()

			// Setup Mock
			mockFetcher := new(mocks.MockFetcher)
			if tt.mockSetup != nil {
				tt.mockSetup(mockFetcher)
			}

			// Execution
			f := fetcher.NewLoggingFetcher(mockFetcher)
			req, _ := http.NewRequest(tt.reqMethod, tt.reqURL, nil)

			_, err := f.Do(req)

			// Error Check
			if tt.expectedError != "" {
				assert.EqualError(t, err, tt.expectedError)
			} else {
				assert.NoError(t, err)
			}

			// Log Verification
			require.NotEmpty(t, hook.entries, "로그가 반드시 기록되어야 합니다")
			lastEntry := hook.entries[len(hook.entries)-1]

			assert.Equal(t, tt.expectedLevel, lastEntry.Level, "로그 레벨이 일치해야 합니다")
			if tt.validateFields != nil {
				tt.validateFields(t, lastEntry.Data)
			}

			mockFetcher.AssertExpectations(t)
		})
	}
}

// TestLoggingFetcher_TimeMeasurement 실행 시간 측정 로직이 동작하는지 검증합니다.
func TestLoggingFetcher_TimeMeasurement(t *testing.T) {
	// Setup Log Hook
	hook := &testHook{}
	logrus.AddHook(hook)
	defer logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))
	logrus.SetLevel(logrus.DebugLevel)

	mockFetcher := new(mocks.MockFetcher)
	mockFetcher.On("Do", mock.Anything).Run(func(args mock.Arguments) {
		// 실제 소요 시간 시뮬레이션
		time.Sleep(50 * time.Millisecond)
	}).Return(&http.Response{StatusCode: 200}, nil)

	f := fetcher.NewLoggingFetcher(mockFetcher)
	req, _ := http.NewRequest("GET", "http://example.com/timer", nil)

	f.Do(req)

	require.NotEmpty(t, hook.entries)
	entry := hook.entries[0]
	durationStr, ok := entry.Data["duration"].(string)
	require.True(t, ok, "duration 필드는 문자열이어야 합니다")

	duration, err := time.ParseDuration(durationStr)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, duration, 50*time.Millisecond, "측정된 소요 시간이 시뮬레이션 시간보다 짧을 수 없습니다")
}

// TestLoggingFetcher_Close Close 메서드 위임 검증
func TestLoggingFetcher_Close(t *testing.T) {
	mockFetcher := new(mocks.MockFetcher)
	expectedErr := errors.New("close failed")

	mockFetcher.On("Close").Return(expectedErr)

	f := fetcher.NewLoggingFetcher(mockFetcher)
	err := f.Close()

	assert.Equal(t, expectedErr, err)
	mockFetcher.AssertExpectations(t)
}

// =============================================================================
// Helper
// =============================================================================

type testHook struct {
	entries []*logrus.Entry
}

func (h *testHook) Fire(e *logrus.Entry) error {
	// 엔트리 복사 (Deep Copy가 아니므로 Data 맵 변경 시 주의 필요하나 읽기 전용으로는 충분)
	h.entries = append(h.entries, e)
	return nil
}

func (h *testHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *testHook) reset() {
	h.entries = nil
}
