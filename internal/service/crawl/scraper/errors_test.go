package scraper

import (
	"context"
	"errors"
	"testing"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/stretchr/testify/assert"
)

// TestScraper_Errors verifies all error constructors and variables in errors.go.
// It ensures that error types, messages, and wrapped causes are correctly handled.
func TestScraper_Errors(t *testing.T) {
	// Common test variables
	dummyURL := "http://example.com"
	dummyCause := errors.New("original error")

	t.Run("JSON Errors", func(t *testing.T) {
		tests := []struct {
			name        string
			err         error
			wantType    apperrors.ErrorType
			wantMsg     []string
			wantWrapped error
		}{
			{
				name:        "newErrJSONParseFailed with context",
				err:         newErrJSONParseFailed(dummyCause, dummyURL, 10, "near this"),
				wantType:    apperrors.ParsingFailed,
				wantMsg:     []string{"JSON 파싱 실패", dummyURL, "오류 위치: 10", "near this"},
				wantWrapped: dummyCause,
			},
			{
				name:        "newErrJSONParseFailed without context",
				err:         newErrJSONParseFailed(dummyCause, dummyURL, 0, ""),
				wantType:    apperrors.ParsingFailed,
				wantMsg:     []string{"JSON 파싱 실패", dummyURL},
				wantWrapped: dummyCause,
			},
			{
				name:     "newErrJSONUnexpectedToken",
				err:      newErrJSONUnexpectedToken(dummyURL),
				wantType: apperrors.ParsingFailed,
				wantMsg:  []string{"불필요한 토큰", dummyURL},
			},
			{
				name:     "newErrUnexpectedHTMLResponse",
				err:      newErrUnexpectedHTMLResponse(dummyURL, "text/html"),
				wantType: apperrors.InvalidInput,
				wantMsg:  []string{"JSON 대신 HTML", dummyURL, "text/html"},
			},
		}
		runErrorTests(t, tests)
	})

	t.Run("HTML Errors", func(t *testing.T) {
		tests := []struct {
			name        string
			err         error
			wantType    apperrors.ErrorType
			wantMsg     []string
			wantWrapped error
		}{
			{
				name:        "newErrHTMLParseFailed",
				err:         newErrHTMLParseFailed(dummyCause, dummyURL),
				wantType:    apperrors.ParsingFailed,
				wantMsg:     []string{"HTML 파싱 실패", dummyURL},
				wantWrapped: dummyCause,
			},
			{
				name:     "NewErrHTMLStructureChanged with URL",
				err:      NewErrHTMLStructureChanged(dummyURL, "changed layout"),
				wantType: apperrors.ExecutionFailed,
				wantMsg:  []string{"HTML 구조 변경", "changed layout", dummyURL},
			},
			{
				name:     "NewErrHTMLStructureChanged without URL",
				err:      NewErrHTMLStructureChanged("", "changed layout"),
				wantType: apperrors.ExecutionFailed,
				wantMsg:  []string{"HTML 구조 변경", "changed layout"},
			},
			{
				name:        "newErrReadHTMLInput",
				err:         newErrReadHTMLInput(dummyCause),
				wantType:    apperrors.Unavailable,
				wantMsg:     []string{"HTML 입력 데이터 읽기 실패"},
				wantWrapped: dummyCause,
			},
		}
		runErrorTests(t, tests)
	})

	t.Run("HTTP and Network Errors", func(t *testing.T) {
		tests := []struct {
			name        string
			err         error
			wantType    apperrors.ErrorType
			wantMsg     []string
			wantWrapped error
		}{
			// newErrHTTPRequestFailed - 4xx
			{
				name:        "newErrHTTPRequestFailed - 400 Bad Request",
				err:         newErrHTTPRequestFailed(dummyCause, dummyURL, 400, "bad request error"),
				wantType:    apperrors.ExecutionFailed,
				wantMsg:     []string{"HTTP 요청 실패", "400", dummyURL, "bad request error"},
				wantWrapped: dummyCause,
			},
			{
				name:        "newErrHTTPRequestFailed - 404 Not Found",
				err:         newErrHTTPRequestFailed(dummyCause, dummyURL, 404, ""),
				wantType:    apperrors.ExecutionFailed,
				wantMsg:     []string{"HTTP 요청 실패", "404", dummyURL},
				wantWrapped: dummyCause,
			},
			// newErrHTTPRequestFailed - Retryable 4xx
			{
				name:        "newErrHTTPRequestFailed - 408 Timeout",
				err:         newErrHTTPRequestFailed(dummyCause, dummyURL, 408, ""),
				wantType:    apperrors.Unavailable,
				wantMsg:     []string{"HTTP 요청 실패", "408"},
				wantWrapped: dummyCause,
			},
			{
				name:        "newErrHTTPRequestFailed - 429 Too Many Requests",
				err:         newErrHTTPRequestFailed(dummyCause, dummyURL, 429, ""),
				wantType:    apperrors.Unavailable,
				wantMsg:     []string{"HTTP 요청 실패", "429"},
				wantWrapped: dummyCause,
			},
			// newErrHTTPRequestFailed - 5xx
			{
				name:        "newErrHTTPRequestFailed - 500 Internal Server Error",
				err:         newErrHTTPRequestFailed(dummyCause, dummyURL, 500, ""),
				wantType:    apperrors.Unavailable,
				wantMsg:     []string{"HTTP 요청 실패", "500"},
				wantWrapped: dummyCause,
			},
			{
				name:        "newErrHTTPRequestFailed - 502 Bad Gateway",
				err:         newErrHTTPRequestFailed(dummyCause, dummyURL, 502, ""),
				wantType:    apperrors.Unavailable,
				wantMsg:     []string{"HTTP 요청 실패", "502"},
				wantWrapped: dummyCause,
			},
			// newErrHTTPRequestFailed - Others
			{
				name:        "newErrHTTPRequestFailed - 302 Found (Unexpected)",
				err:         newErrHTTPRequestFailed(dummyCause, dummyURL, 302, ""),
				wantType:    apperrors.Unavailable,
				wantMsg:     []string{"HTTP 요청 실패", "302"},
				wantWrapped: dummyCause,
			},

			// Other HTTP Errors
			{
				name:        "newErrCreateHTTPRequest",
				err:         newErrCreateHTTPRequest(dummyCause, dummyURL),
				wantType:    apperrors.ExecutionFailed,
				wantMsg:     []string{"HTTP 요청 생성 실패", dummyURL},
				wantWrapped: dummyCause,
			},
			{
				name:        "newErrNetworkError",
				err:         newErrNetworkError(dummyCause, dummyURL),
				wantType:    apperrors.Unavailable,
				wantMsg:     []string{"네트워크 오류", dummyURL},
				wantWrapped: dummyCause,
			},
			{
				name:        "newErrHTTPRequestCanceled",
				err:         newErrHTTPRequestCanceled(context.Canceled, dummyURL),
				wantType:    apperrors.Unavailable,
				wantMsg:     []string{"요청 중단", dummyURL},
				wantWrapped: context.Canceled,
			},
			{
				name:     "ErrContextCanceled",
				err:      ErrContextCanceled,
				wantType: apperrors.Unavailable,
				wantMsg:  []string{"작업 중단", "컨텍스트 취소"},
			},
		}
		runErrorTests(t, tests)
	})

	t.Run("Body Processing Errors", func(t *testing.T) {
		tests := []struct {
			name        string
			err         error
			wantType    apperrors.ErrorType
			wantMsg     []string
			wantWrapped error
		}{
			{
				name:        "newErrPrepareRequestBody",
				err:         newErrPrepareRequestBody(dummyCause),
				wantType:    apperrors.ExecutionFailed,
				wantMsg:     []string{"요청 본문 준비 실패"},
				wantWrapped: dummyCause,
			},
			{
				name:        "newErrEncodeJSONBody",
				err:         newErrEncodeJSONBody(dummyCause),
				wantType:    apperrors.Internal,
				wantMsg:     []string{"요청 본문 JSON 인코딩 실패"},
				wantWrapped: dummyCause,
			},
			{
				name:        "newErrReadResponseBody",
				err:         newErrReadResponseBody(dummyCause),
				wantType:    apperrors.Unavailable,
				wantMsg:     []string{"응답 본문 데이터 수신 실패"},
				wantWrapped: dummyCause,
			},
		}
		runErrorTests(t, tests)
	})

	t.Run("Size Limit Errors", func(t *testing.T) {
		tests := []struct {
			name        string
			err         error
			wantType    apperrors.ErrorType
			wantMsg     []string
			wantWrapped error
		}{
			{
				name:     "newErrRequestBodySizeLimitExceeded",
				err:      newErrRequestBodySizeLimitExceeded(1024, "application/json"),
				wantType: apperrors.InvalidInput,
				wantMsg:  []string{"요청 본문 크기 초과", "1024", "application/json"},
			},
			{
				name:     "newErrResponseBodySizeLimitExceeded",
				err:      newErrResponseBodySizeLimitExceeded(2048, dummyURL, "text/html"),
				wantType: apperrors.InvalidInput,
				wantMsg:  []string{"응답 본문 크기 초과", "2048", dummyURL, "text/html"},
			},
			{
				name:     "newErrInputDataSizeLimitExceeded",
				err:      newErrInputDataSizeLimitExceeded(4096, "HTML"),
				wantType: apperrors.InvalidInput,
				wantMsg:  []string{"입력 데이터 크기 초과", "4096", "HTML"},
			},
		}
		runErrorTests(t, tests)
	})

	t.Run("Input Validation Errors", func(t *testing.T) {
		tests := []struct {
			name        string
			err         error
			wantType    apperrors.ErrorType
			wantMsg     []string
			wantWrapped error
		}{
			{
				name:     "ErrDecodeTargetNil",
				err:      ErrDecodeTargetNil,
				wantType: apperrors.Internal,
				wantMsg:  []string{"JSON 디코딩 실패", "변수가 nil"},
			},
			{
				name:     "newErrDecodeTargetInvalidType",
				err:      newErrDecodeTargetInvalidType(123),
				wantType: apperrors.Internal,
				wantMsg:  []string{"JSON 디코딩 실패", "int"},
			},
			{
				name:     "ErrInputReaderNil",
				err:      ErrInputReaderNil,
				wantType: apperrors.Internal,
				wantMsg:  []string{"HTML 파싱 실패", "스트림이 nil"},
			},
			{
				name:     "ErrInputReaderTypedNil",
				err:      ErrInputReaderTypedNil,
				wantType: apperrors.Internal,
				wantMsg:  []string{"HTML 파싱 실패", "Typed Nil"},
			},
		}
		runErrorTests(t, tests)
	})

	t.Run("Response Validation Errors", func(t *testing.T) {
		t.Run("Wraps Regular Error", func(t *testing.T) {
			err := newErrValidationFailed(dummyCause, dummyURL, "preview")
			assert.True(t, apperrors.Is(err, apperrors.ExecutionFailed))
			assert.Contains(t, err.Error(), "응답 검증 실패")
			assert.Contains(t, err.Error(), dummyURL)
			assert.Contains(t, err.Error(), "preview")
			assert.ErrorIs(t, err, dummyCause)
		})

		t.Run("Preserves AppError Type", func(t *testing.T) {
			unavailableErr := apperrors.New(apperrors.Unavailable, "server busy")
			err := newErrValidationFailed(unavailableErr, dummyURL, "")
			assert.True(t, apperrors.Is(err, apperrors.Unavailable), "Should preserve Unavailable type")
			assert.Contains(t, err.Error(), "응답 검증 실패")
			assert.ErrorIs(t, err, unavailableErr)
		})
	})
}

// Helper for running error tests
func runErrorTests(t *testing.T, tests []struct {
	name        string
	err         error
	wantType    apperrors.ErrorType
	wantMsg     []string
	wantWrapped error
}) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Error(t, tt.err)

			// Verify Type
			if tt.wantType != apperrors.Unknown {
				assert.True(t, apperrors.Is(tt.err, tt.wantType), "Expected error type %s, got err: %v", tt.wantType, tt.err)
			}

			// Verify Message
			for _, msg := range tt.wantMsg {
				assert.Contains(t, tt.err.Error(), msg)
			}

			// Verify Wrapped Error
			if tt.wantWrapped != nil {
				assert.ErrorIs(t, tt.err, tt.wantWrapped)
			}
		})
	}
}
