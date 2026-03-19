package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Panic Recovery 미들웨어 테스트
// =============================================================================

// TestPanicRecovery_Table은 패닉 복구 미들웨어의 동작을 검증합니다.
//
// 검증 항목:
//   - 문자열(string) 패닉 복구 및 로깅
//   - 에러(error) 패닉 복구 및 로깅
//   - Request ID가 포함된 패닉 로깅
//   - 스택 트레이스 포함 여부 확인
//   - HTTP 500 상태 코드 응답 확인
func TestPanicRecovery_Table(t *testing.T) {
	// Setup: 로거 출력을 캡처하기 위한 설정
	setupLogger := func(t *testing.T) *bytes.Buffer {
		t.Helper()
		var buf bytes.Buffer
		applog.SetOutput(&buf)
		applog.SetFormatter(&applog.JSONFormatter{}) // JSON 포맷터 사용

		originalOut := applog.StandardLogger().Out
		t.Cleanup(func() {
			applog.SetOutput(originalOut)
		})
		return &buf
	}

	tests := []struct {
		name         string
		panicPayload any
		requestID    string
		verifyLog    func(*testing.T, map[string]interface{})
	}{
		{
			name:         "성공: 문자열 패닉 복구",
			panicPayload: "치명적인 오류 발생",
			verifyLog: func(t *testing.T, entry map[string]interface{}) {
				msg, ok := entry["msg"].(string)
				assert.True(t, ok)
				assert.Equal(t, "패닉 복구: 예기치 못한 오류가 발생하여 안전하게 복구했습니다", msg)

				errorField, ok := entry["error"].(string) // 문자열 패닉은 fmt.Sprintf로 변환됨
				assert.True(t, ok)
				assert.Contains(t, errorField, "치명적인 오류 발생")

				stack, ok := entry["stack"].(string)
				assert.True(t, ok)
				assert.NotEmpty(t, stack, "스택 트레이스가 포함되어야 합니다")
			},
		},
		{
			name:         "성공: 에러 객체 패닉 복구",
			panicPayload: fmt.Errorf("데이터베이스 연결 실패"),
			verifyLog: func(t *testing.T, entry map[string]interface{}) {
				errorField, ok := entry["error"].(string)
				assert.True(t, ok)
				assert.Contains(t, errorField, "데이터베이스 연결 실패")
			},
		},
		{
			name:         "성공: 정수형(Int) 패닉 복구",
			panicPayload: 12345,
			verifyLog: func(t *testing.T, entry map[string]interface{}) {
				errorField, ok := entry["error"].(string)
				assert.True(t, ok)
				assert.Contains(t, errorField, "12345")
			},
		},
		{
			name:         "성공: 구조체 패닉 복구",
			panicPayload: struct{ Code int }{Code: 500},
			verifyLog: func(t *testing.T, entry map[string]interface{}) {
				errorField, ok := entry["error"].(string)
				assert.True(t, ok)
				assert.Contains(t, errorField, "{500}")
			},
		},
		{
			name:         "성공: Request ID 포함 패닉 로깅",
			panicPayload: "알 수 없는 오류",
			requestID:    "req-123456",
			verifyLog: func(t *testing.T, entry map[string]interface{}) {
				reqID, ok := entry["request_id"].(string)
				assert.True(t, ok)
				assert.Equal(t, "req-123456", reqID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := setupLogger(t)

			e := echo.New()
			// PanicRecovery 미들웨어 등록
			e.Use(PanicRecovery())

			// Request ID 설정을 위한 미들웨어 (테스트용)
			if tt.requestID != "" {
				e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
					return func(c echo.Context) error {
						c.Response().Header().Set(echo.HeaderXRequestID, tt.requestID)
						return next(c)
					}
				})
			}

			// 패닉을 유발하는 핸들러 등록
			e.GET("/panic", func(c echo.Context) error {
				panic(tt.panicPayload)
			})

			// HTTP 요청 생성
			req := httptest.NewRequest(http.MethodGet, "/panic", nil)
			rec := httptest.NewRecorder()

			// 미들웨어 체인 실행
			e.ServeHTTP(rec, req)

			// 1. 상태 코드 검증 (패닉 복구 후 500 에러 반환)
			assert.Equal(t, http.StatusInternalServerError, rec.Code)

			// 2. 로그 파싱 및 검증
			require.Greater(t, buf.Len(), 0, "로그가 기록되어야 합니다")

			var logEntry map[string]interface{}
			err := json.Unmarshal(buf.Bytes(), &logEntry)
			assert.NoError(t, err, "JSON 로그 파싱 실패")

			// 공통 필드 검증 (unexported 상수 직접 검증은 불가하지만 값으로 검증)
			assert.Equal(t, "api.middleware.panic_recovery", logEntry["component"])
			assert.Equal(t, "error", logEntry["level"])

			// 케이스별 상세 검증
			if tt.verifyLog != nil {
				tt.verifyLog(t, logEntry)
			}
		})
	}
}

// TestPanicRecovery_MiddlewareReturn은 미들웨어 생성 함수를 검증합니다.
func TestPanicRecovery_MiddlewareReturn(t *testing.T) {
	middleware := PanicRecovery()
	assert.NotNil(t, middleware, "미들웨어 함수는 nil이 아니어야 합니다")
}

// TestNewErrPanicRecovered_Conversion_Table은 패닉 값 변환 로직을 검증합니다.
func TestNewErrPanicRecovered_Conversion_Table(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name:     "문자열 변환",
			input:    "critical error",
			expected: "critical error",
		},
		{
			name:     "정수형 변환",
			input:    12345,
			expected: "12345",
		},
		{
			name:     "구조체 변환",
			input:    struct{ ID int }{ID: 1},
			expected: "{1}",
		},
		{
			name:     "nil 변환",
			input:    nil,
			expected: "<nil>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewErrPanicRecovered(tt.input)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expected)
		})
	}
}
