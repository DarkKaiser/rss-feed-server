package httputil

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/rss-feed-server/internal/service/api/model/response"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Error Handler Tests
// =============================================================================

// LogEntry 로그 검증을 위한 구조체
type LogEntry struct {
	Level         string `json:"level"`
	Message       string `json:"msg"`
	Path          string `json:"path"`
	Method        string `json:"method"`
	StatusCode    int    `json:"status_code"`
	RemoteIP      string `json:"remote_ip"`
	RequestID     string `json:"request_id"`
	ApplicationID string `json:"application_id,omitempty"`
}

// TestErrorHandler_Comprehensive는 커스텀 HTTP 에러 핸들러의 모든 동작을 검증합니다.
//
// 주의: 이 테스트는 pkg/log의 글로벌 상태를 변경하므로 t.Parallel()을 사용할 수 없습니다.
// 반드시 직렬로 실행되어야 합니다.
// TestErrorHandler_Comprehensive는 커스텀 HTTP 에러 핸들러의 모든 동작을 검증합니다.
//
// 주의: 이 테스트는 pkg/log의 글로벌 상태를 변경하므로 t.Parallel()을 사용할 수 없습니다.
// 반드시 직렬로 실행되어야 합니다.
func TestErrorHandler_Comprehensive(t *testing.T) {
	// 로거 캡처 설정
	buf := new(bytes.Buffer)
	setupTestLogger(buf)
	defer restoreLogger()

	tests := []struct {
		name            string
		method          string
		path            string
		err             error
		setupContext    func(c echo.Context, req *http.Request, rec *httptest.ResponseRecorder)
		expectedStatus  int
		expectedJSON    string    // 예상되는 JSON 응답 (문자열)
		expectedLog     *LogEntry // 검증할 로그 필드 (nil이면 로그 검증 건너뜀)
		expectedLogPart string    // 로그에 포함되어야 할 문자열 (메시지 등 단순 확인용)
		expectNoLog     bool      // 로그가 생성되지 않아야 함을 명시
	}{
		{
			name:           "404 Not Found_기본 메시지",
			method:         http.MethodGet,
			err:            echo.NewHTTPError(http.StatusNotFound, "Not Found"),
			expectedStatus: http.StatusNotFound,
			expectedJSON:   `{"result_code":404,"message":"요청한 리소스를 찾을 수 없습니다"}`,
			expectedLog: &LogEntry{
				Level:      "warning",
				Message:    "HTTP 4xx: 클라이언트 요청 오류",
				StatusCode: http.StatusNotFound,
			},
		},
		{
			name:            "404 Not Found_커스텀 메시지 유지",
			method:          http.MethodGet,
			err:             echo.NewHTTPError(http.StatusNotFound, "Custom Check"),
			expectedStatus:  http.StatusNotFound,
			expectedJSON:    `{"result_code":404,"message":"Custom Check"}`,
			expectedLogPart: "클라이언트 요청 오류",
		},
		{
			name:           "405 Method Not Allowed",
			method:         http.MethodPost,
			err:            echo.NewHTTPError(http.StatusMethodNotAllowed, "method not allowed"),
			expectedStatus: http.StatusMethodNotAllowed,
			expectedJSON:   `{"result_code":405,"message":"method not allowed"}`,
			expectedLog: &LogEntry{
				Level:      "warning",
				StatusCode: http.StatusMethodNotAllowed,
			},
		},
		{
			name:           "400 Bad Request_ErrorResponse 타입 메시지",
			method:         http.MethodPost,
			err:            echo.NewHTTPError(http.StatusBadRequest, response.ErrorResponse{Message: "잘못된 요청입니다"}),
			expectedStatus: http.StatusBadRequest,
			expectedJSON:   `{"result_code":400,"message":"잘못된 요청입니다"}`,
			expectedLog: &LogEntry{
				Level:      "warning",
				StatusCode: http.StatusBadRequest,
			},
		},
		{
			name:           "401 Unauthorized_인증 실패",
			method:         http.MethodPost,
			err:            echo.NewHTTPError(http.StatusUnauthorized, "인증이 필요합니다"),
			expectedStatus: http.StatusUnauthorized,
			expectedJSON:   `{"result_code":401,"message":"인증이 필요합니다"}`,
			expectedLog: &LogEntry{
				Level:      "warning",
				StatusCode: http.StatusUnauthorized,
			},
		},
		{
			name:           "500 Internal Server Error_일반 에러",
			method:         http.MethodGet,
			err:            errors.New("database connection failed"),
			expectedStatus: http.StatusInternalServerError,
			expectedJSON:   `{"result_code":500,"message":"내부 서버 오류가 발생했습니다"}`,
			expectedLog: &LogEntry{
				Level:      "error",
				Message:    "HTTP 5xx: 서버 내부 오류",
				StatusCode: http.StatusInternalServerError,
			},
		},
		{
			name:   "로깅 필드 검증_IP 및 RequestID",
			method: http.MethodGet,
			err:    echo.NewHTTPError(http.StatusBadRequest, "Bad Request"),
			setupContext: func(c echo.Context, req *http.Request, rec *httptest.ResponseRecorder) {
				req.RemoteAddr = "192.168.1.100:12345"
				rec.Header().Set(echo.HeaderXRequestID, "test-req-id-123")
			},
			expectedStatus: http.StatusBadRequest,
			expectedJSON:   `{"result_code":400,"message":"Bad Request"}`,
			expectedLog: &LogEntry{
				RemoteIP:  "192.168.1.100",
				RequestID: "test-req-id-123",
			},
		},
		{
			name:           "HEAD 요청_Body 없음",
			method:         http.MethodHead,
			err:            echo.NewHTTPError(http.StatusNotFound, "Not Found"),
			expectedStatus: http.StatusNotFound,
			expectedJSON:   "", // Body가 비어있어야 함
		},
		{
			name:   "이미 응답 커밋됨_작업 중단",
			method: http.MethodGet,
			err:    errors.New("error after write"),
			setupContext: func(c echo.Context, req *http.Request, rec *httptest.ResponseRecorder) {
				c.Response().Committed = true
			},
			expectedStatus: http.StatusOK, // 핸들러가 상태 코드를 덮어쓰지 않아야 함
			expectedJSON:   "",
		},
		// --- 엣지 케이스 추가 ---
		{
			name:           "EdgeCase_HTTPError 메시지 타입 불일치(int)",
			method:         http.MethodGet,
			err:            echo.NewHTTPError(http.StatusBadRequest, 12345), // int 메시지
			expectedStatus: http.StatusBadRequest,
			// 메시지가 string이나 ErrorResponse가 아니면 기본값("내부 서버 오류...")이 유지됨.
			expectedJSON: `{"result_code":400,"message":"내부 서버 오류가 발생했습니다"}`,
			expectedLog: &LogEntry{
				Level: "warning",
			},
		},
		{
			name:           "EdgeCase_Status 3xx (로그 제외)",
			method:         http.MethodGet,
			err:            echo.NewHTTPError(http.StatusFound, "Redirecting"),
			expectedStatus: http.StatusFound,
			expectNoLog:    true, // 400 미만 상태 코드는 로그를 남기지 않음
			expectedJSON:   `{"result_code":302,"message":"Redirecting"}`,
		},
		{
			name:           "EdgeCase_Struct 메시지 처리",
			method:         http.MethodGet,
			err:            echo.NewHTTPError(http.StatusBadRequest, struct{ Details string }{Details: "wrong type"}),
			expectedStatus: http.StatusBadRequest,
			// response.ErrorResponse가 아닌 구조체는 처리되지 않으므로 기본 메시지가 나옴
			expectedJSON: `{"result_code":400,"message":"내부 서버 오류가 발생했습니다"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 초기화 (직렬 실행이므로 루프 내 Reset 필수)
			buf.Reset()
			e := echo.New()
			req := httptest.NewRequest(tt.method, "/", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			// 추가 컨텍스트 설정
			if tt.setupContext != nil {
				tt.setupContext(c, req, rec)
			}

			// 테스트 실행
			ErrorHandler(tt.err, c)

			// 1. 응답 상태 코드 검증
			assert.Equal(t, tt.expectedStatus, rec.Code, "HTTP 상태 코드가 일치해야 합니다")

			// 2. 응답 JSON 검증
			if tt.expectedJSON != "" {
				assert.JSONEq(t, tt.expectedJSON, rec.Body.String(), "응답 JSON이 예상과 일치해야 합니다")
			} else {
				assert.Empty(t, rec.Body.String(), "응답 본문이 비어있어야 합니다")
			}

			// 3. 로그 검증
			if tt.expectNoLog {
				assert.Empty(t, buf.String(), "로그가 생성되지 않아야 합니다")
			} else {
				if tt.expectedLog != nil {
					var logEntry LogEntry
					err := json.Unmarshal(buf.Bytes(), &logEntry)
					require.NoError(t, err, "로그 파싱에 실패했습니다: %s", buf.String())

					if tt.expectedLog.Level != "" {
						assert.Equal(t, tt.expectedLog.Level, logEntry.Level)
					}
					if tt.expectedLog.Message != "" {
						assert.Equal(t, tt.expectedLog.Message, logEntry.Message)
					}
					if tt.expectedLog.StatusCode != 0 {
						assert.Equal(t, tt.expectedLog.StatusCode, logEntry.StatusCode)
					}
					if tt.expectedLog.RemoteIP != "" {
						assert.Equal(t, tt.expectedLog.RemoteIP, logEntry.RemoteIP)
					}
					if tt.expectedLog.RequestID != "" {
						assert.Equal(t, tt.expectedLog.RequestID, logEntry.RequestID)
					}
					if tt.expectedLog.ApplicationID != "" {
						assert.Equal(t, tt.expectedLog.ApplicationID, logEntry.ApplicationID)
					} else {
						assert.Empty(t, logEntry.ApplicationID)
					}
				}

				if tt.expectedLogPart != "" {
					assert.Contains(t, buf.String(), tt.expectedLogPart)
				}
			}
		})
	}
}

// setupTestLogger는 테스트를 위해 로거 출력을 버퍼로 변경합니다.
func setupTestLogger(buf *bytes.Buffer) {
	applog.SetOutput(buf)
	applog.SetFormatter(&applog.JSONFormatter{}) // 로그 파싱이 쉽도록 JSON 포맷 사용
}

// restoreLogger는 로거 출력을 표준 출력으로 복구합니다.
func restoreLogger() {
	applog.SetOutput(applog.StandardLogger().Out)
}
