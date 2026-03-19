package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test Helpers & Setup
// =============================================================================

// captureLogs는 테스트 동안 발생하는 로거 출력을 캡처합니다.
// 테스트 종료 시(Cleanup) 자동으로 원래 상태로 복구됩니다.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	buf := new(bytes.Buffer)
	originalOut := applog.StandardLogger().Out
	originalFormatter := applog.StandardLogger().Formatter
	originalLevel := applog.StandardLogger().Level

	// 테스트용 설정: 버퍼 출력 및 쉬운 파싱을 위한 JSON 포맷
	applog.SetOutput(buf)
	applog.SetFormatter(&applog.JSONFormatter{})
	applog.SetLevel(applog.DebugLevel)

	t.Cleanup(func() {
		applog.SetOutput(originalOut)
		applog.SetFormatter(originalFormatter)
		applog.SetLevel(originalLevel)
	})

	return buf
}

// parseLogEntry는 버퍼에 기록된 마지막 JSON 로그를 파싱합니다.
// 여러 로그가 있을 경우 마지막 로그를 반환하며, 로그가 없으면 테스트를 실패시킵니다.
func parseLastLogEntry(t *testing.T, buf *bytes.Buffer) map[string]interface{} {
	t.Helper()

	output := buf.String()
	require.NotEmpty(t, output, "로그가 기록되지 않았습니다")

	lines := strings.TrimSpace(output)
	logLines := strings.Split(lines, "\n")
	lastLine := logLines[len(logLines)-1]

	var entry map[string]interface{}
	err := json.Unmarshal([]byte(lastLine), &entry)
	require.NoError(t, err, "로그 파싱 실패: %s", lastLine)

	return entry
}

// =============================================================================
// Main Test Suite
// =============================================================================

func TestHTTPLogger(t *testing.T) {
	// 공통 테스트 케이스 정의
	tests := []struct {
		name         string
		setupRequest func() (*http.Request, *httptest.ResponseRecorder)
		handler      echo.HandlerFunc
		verify       func(t *testing.T, rec *httptest.ResponseRecorder, logEntry map[string]interface{})
		expectPanic  bool // Panic 발생 여부
	}{
		{
			name: "Basic GET Request",
			setupRequest: func() (*http.Request, *httptest.ResponseRecorder) {
				req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
				req.Header.Set("User-Agent", "TestAgent/1.0")
				req.RemoteAddr = "1.2.3.4:12345" // RealIP 테스트용
				return req, httptest.NewRecorder()
			},
			handler: func(c echo.Context) error {
				return c.String(http.StatusOK, "Hello, World!")
			},
			verify: func(t *testing.T, rec *httptest.ResponseRecorder, logEntry map[string]interface{}) {
				// 1. 응답 검증
				assert.Equal(t, http.StatusOK, rec.Code)
				assert.Equal(t, "Hello, World!", rec.Body.String())

				// 2. 로그 필드 검증
				assert.Equal(t, "HTTP 요청", logEntry["msg"])
				assert.Equal(t, "GET", logEntry["method"])
				assert.Equal(t, "/api/test", logEntry["path"])
				assert.Equal(t, "1.2.3.4", logEntry["remote_ip"])
				assert.Equal(t, "TestAgent/1.0", logEntry["user_agent"])
				assert.Equal(t, float64(http.StatusOK), logEntry["status"]) // JSON unmarshals numbers as float64
				assert.NotEmpty(t, logEntry["latency"])
				assert.NotEmpty(t, logEntry["time_rfc3339"])
			},
		},
		{
			name: "Status Code Error (400)",
			setupRequest: func() (*http.Request, *httptest.ResponseRecorder) {
				return httptest.NewRequest(http.MethodPost, "/api/error", nil), httptest.NewRecorder()
			},
			handler: func(c echo.Context) error {
				return echo.NewHTTPError(http.StatusBadRequest, "Invalid Request")
			},
			verify: func(t *testing.T, rec *httptest.ResponseRecorder, logEntry map[string]interface{}) {
				// 핸들러가 에러를 리턴하더라도 미들웨어 체인 상에서는 nil로 처리되고
				// status code가 업데이트되었는지 확인
				assert.Equal(t, float64(http.StatusBadRequest), logEntry["status"])
				assert.Equal(t, "POST", logEntry["method"])
			},
		},
		{
			name: "Content-Length Logging",
			setupRequest: func() (*http.Request, *httptest.ResponseRecorder) {
				req := httptest.NewRequest(http.MethodPost, "/api/upload", nil)
				req.Header.Set(echo.HeaderContentLength, "1024")
				return req, httptest.NewRecorder()
			},
			handler: func(c echo.Context) error {
				return c.NoContent(http.StatusOK)
			},
			verify: func(t *testing.T, rec *httptest.ResponseRecorder, logEntry map[string]interface{}) {
				assert.Equal(t, "1024", logEntry["bytes_in"])
			},
		},
		{
			name: "Sensitive Query Param Masking",
			setupRequest: func() (*http.Request, *httptest.ResponseRecorder) {
				req := httptest.NewRequest(http.MethodGet, "/api/auth?app_key=secret-key&id=user1", nil)
				return req, httptest.NewRecorder()
			},
			handler: func(c echo.Context) error {
				return c.NoContent(http.StatusOK)
			},
			verify: func(t *testing.T, rec *httptest.ResponseRecorder, logEntry map[string]interface{}) {
				uri := logEntry["uri"].(string)
				// 마스킹 검증
				assert.Contains(t, uri, "app_key=secr%2A%2A%2A") // URL Encoded '***'
				assert.Contains(t, uri, "id=user1")
				assert.NotContains(t, uri, "secret-key")
			},
		},
		{
			name: "Panic Logging with Defer (Critical)",
			setupRequest: func() (*http.Request, *httptest.ResponseRecorder) {
				return httptest.NewRequest(http.MethodGet, "/api/panic", nil), httptest.NewRecorder()
			},
			handler: func(c echo.Context) error {
				panic("unexpected error detected")
			},
			expectPanic: true,
			verify: func(t *testing.T, rec *httptest.ResponseRecorder, logEntry map[string]interface{}) {
				// Panic 발생 시에도 로그가 남아야 함 (defer 사용 효과)
				// 주의: Panic 발생 시점의 Status는 초기값(200)일 수 있음.
				// 중요한 건 로그가 *존재한다*는 점과 요청 정보(Path, IP 등)가 남는다는 점임.
				assert.Equal(t, "/api/panic", logEntry["path"])
				assert.Equal(t, "GET", logEntry["method"])
				// Panic 상황에서도 지연 시간 측정은 완료되어야 함
				assert.NotEmpty(t, logEntry["latency"])
			},
		},
		// --- [NEW] Added Test Cases ---
		{
			name: "Empty Path Normalization",
			setupRequest: func() (*http.Request, *httptest.ResponseRecorder) {
				req := httptest.NewRequest(http.MethodGet, "/", nil) // Valid initial URL
				req.URL.Path = ""                                    // Explicitly set empty to test normalization
				return req, httptest.NewRecorder()
			},
			handler: func(c echo.Context) error {
				return c.NoContent(http.StatusOK)
			},
			verify: func(t *testing.T, rec *httptest.ResponseRecorder, logEntry map[string]interface{}) {
				assert.Equal(t, "/", logEntry["path"], "빈 경로는 '/'로 정규화되어야 합니다")
			},
		},
		{
			name: "Full Request Fields (Referer, Request ID)",
			setupRequest: func() (*http.Request, *httptest.ResponseRecorder) {
				req := httptest.NewRequest(http.MethodGet, "/api/full", nil)
				req.Header.Set("Referer", "http://example.com")
				rec := httptest.NewRecorder()
				rec.Header().Set(echo.HeaderXRequestID, "req-12345") // Response Header에 Request ID 설정 시뮬레이션
				return req, rec
			},
			handler: func(c echo.Context) error {
				return c.NoContent(http.StatusOK)
			},
			verify: func(t *testing.T, rec *httptest.ResponseRecorder, logEntry map[string]interface{}) {
				assert.Equal(t, "http://example.com", logEntry["referer"])
				assert.Equal(t, "req-12345", logEntry["request_id"])
				assert.NotEmpty(t, logEntry["host"])
				assert.NotEmpty(t, logEntry["protocol"])
			},
		},
		{
			name: "Bytes Out Verification",
			setupRequest: func() (*http.Request, *httptest.ResponseRecorder) {
				return httptest.NewRequest(http.MethodGet, "/api/data", nil), httptest.NewRecorder()
			},
			handler: func(c echo.Context) error {
				return c.String(http.StatusOK, "12345") // 5 bytes
			},
			verify: func(t *testing.T, rec *httptest.ResponseRecorder, logEntry map[string]interface{}) {
				assert.Equal(t, "5", logEntry["bytes_out"])
			},
		},
		{
			name: "Latency Human Readable Format",
			setupRequest: func() (*http.Request, *httptest.ResponseRecorder) {
				return httptest.NewRequest(http.MethodGet, "/api/slow", nil), httptest.NewRecorder()
			},
			handler: func(c echo.Context) error {
				return c.NoContent(http.StatusOK)
			},
			verify: func(t *testing.T, rec *httptest.ResponseRecorder, logEntry map[string]interface{}) {
				latencyHuman, ok := logEntry["latency_human"].(string)
				assert.True(t, ok)
				assert.NotEmpty(t, latencyHuman)
				// Go time.Duration string format check (e.g., "100µs", "1.2ms")
				// 단순하게 숫자로 끝나지 않고 단위가 포함되어 있는지 확인
				assert.False(t, strings.HasSuffix(latencyHuman, "000"), "단위(ms, µs 등)가 포함되어야 합니다")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 1. Setup
			buf := captureLogs(t)
			e := echo.New()
			req, rec := tt.setupRequest()
			c := e.NewContext(req, rec)

			// 2. Execution
			// 미들웨어 체인 구성: HTTPLogger -> Handler
			middleware := HTTPLogger()
			chain := middleware(tt.handler)

			if tt.expectPanic {
				// Panic 기대 시 verify 로직을 defer 안에서 수행
				defer func() {
					_ = recover() // Panic 복구 (테스트 중단 방지)
					logEntry := parseLastLogEntry(t, buf)
					tt.verify(t, rec, logEntry)
				}()
			}

			// 핸들러 실행
			err := chain(c)

			// Panic이 아니면 여기서 검증
			if !tt.expectPanic {
				assert.NoError(t, err) // HTTPLogger는 에러를 삼키고(log) nil 반환함 (내부 구현 따름)
				logEntry := parseLastLogEntry(t, buf)
				tt.verify(t, rec, logEntry)
			}
		})
	}
}

// =============================================================================
// Unit Tests for Helper Functions
// =============================================================================

func TestMaskSensitiveQueryParams_Unit(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Masks App Key",
			input:    "/api?app_key=verysecretkey",
			expected: "/api?app_key=very%2A%2A%2Atkey",
		},
		{
			name:     "Masks Password",
			input:    "/login?password=mypassword123",
			expected: "/login?password=mypa%2A%2A%2Ad123",
		},
		{
			name:     "Preserves Other Params",
			input:    "/search?query=hello&sort=desc",
			expected: "/search?query=hello&sort=desc",
		},
		{
			name:     "Mixed Params",
			input:    "/auth?id=123&app_key=secret",
			expected: "/auth?app_key=secr%2A%2A%2A&id=123", // Query 순서는 바뀔 수 있음 (URL Encode 특성)
		},
		{
			name:     "Invalid URI",
			input:    "://invalid-uri",
			expected: "://invalid-uri", // 원본 반환
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskSensitiveQueryParams(tt.input)

			if tt.name == "Mixed Params" {
				// Query param 순서 보장이 안되므로 포함 여부로 검증
				assert.Contains(t, result, "app_key=secr%2A%2A%2A")
				assert.Contains(t, result, "id=123")
			} else {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
