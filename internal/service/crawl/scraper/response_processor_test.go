package scraper

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher/mocks"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/transform"
)

// =================================================================================
// Test Group 1: ValidateResponse
// =================================================================================

func TestValidateResponse_StatusCodes(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		body        string
		wantErr     bool
		errType     apperrors.ErrorType
		errContains []string
	}{
		{
			name:       "Success - 200 OK",
			statusCode: http.StatusOK,
		},
		{
			name:       "Success - 201 Created",
			statusCode: http.StatusCreated,
		},
		{
			name:       "Success - 204 No Content",
			statusCode: http.StatusNoContent,
		},
		{
			name:        "Error - 400 Bad Request",
			statusCode:  http.StatusBadRequest,
			body:        "Bad Request From Server",
			wantErr:     true,
			errType:     apperrors.ExecutionFailed,
			errContains: []string{"HTTP 요청 실패", "400", "Bad Request From Server"},
		},
		{
			name:        "Error - 404 Not Found",
			statusCode:  http.StatusNotFound,
			wantErr:     true,
			errType:     apperrors.ExecutionFailed,
			errContains: []string{"HTTP 요청 실패", "404"},
		},
		{
			name:        "Error - 429 Too Many Requests (Retryable)",
			statusCode:  http.StatusTooManyRequests,
			wantErr:     true,
			errType:     apperrors.Unavailable,
			errContains: []string{"HTTP 요청 실패", "429"},
		},
		{
			name:        "Error - 500 Internal Server Error (Retryable)",
			statusCode:  http.StatusInternalServerError,
			body:        "Server Panic",
			wantErr:     true,
			errType:     apperrors.Unavailable,
			errContains: []string{"HTTP 요청 실패", "500", "Server Panic"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			mockFetcher := new(mocks.MockFetcher)
			s := New(mockFetcher).(*scraper)
			logger := applog.WithContext(context.Background())

			resp := &http.Response{
				StatusCode: tt.statusCode,
				Status:     http.StatusText(tt.statusCode),
				Body:       io.NopCloser(strings.NewReader(tt.body)),
				Request:    &http.Request{URL: nil},
				Header:     make(http.Header),
			}

			// Act
			err := s.validateResponse(resp, requestParams{}, logger)

			// Assert
			if tt.wantErr {
				require.Error(t, err)
				if tt.errType != apperrors.Unknown {
					assert.True(t, apperrors.Is(err, tt.errType), "Expected error type %s", tt.errType)
				}
				for _, msg := range tt.errContains {
					assert.Contains(t, err.Error(), msg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateResponse_CustomValidator(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		body        string
		validator   func(*http.Response, *applog.Entry) error
		wantErr     bool
		errContains []string
	}{
		{
			name:       "Success - Validator Passes",
			statusCode: http.StatusOK,
			validator: func(resp *http.Response, logger *applog.Entry) error {
				return nil
			},
			wantErr: false,
		},
		{
			name:       "Error - Validator Fails",
			statusCode: http.StatusOK,
			body:       "Invalid Content",
			validator: func(resp *http.Response, logger *applog.Entry) error {
				return errors.New("custom validation error")
			},
			wantErr:     true,
			errContains: []string{"응답 검증 실패", "custom validation error", "Invalid Content"},
		},
		{
			name:       "Error - Validator Fails (Body Read Error handled gracefully)",
			statusCode: http.StatusOK,
			// Body reading failure simulation requires mocking ReadCloser, hard to trigger with just strings.NewReader.
			// Instead, we verify that validator error is returned even if preview generation fails.
			validator: func(resp *http.Response, logger *applog.Entry) error {
				return errors.New("fail")
			},
			wantErr:     true,
			errContains: []string{"fail"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			s := New(new(mocks.MockFetcher)).(*scraper)
			logger := applog.WithContext(context.Background())

			resp := &http.Response{
				StatusCode: tt.statusCode,
				Body:       io.NopCloser(strings.NewReader(tt.body)),
				Request:    &http.Request{URL: nil},
				Header:     make(http.Header),
			}

			params := requestParams{Validator: tt.validator}

			// Act
			err := s.validateResponse(resp, params, logger)

			// Assert
			if tt.wantErr {
				require.Error(t, err)
				for _, msg := range tt.errContains {
					assert.Contains(t, err.Error(), msg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateResponse_Callback(t *testing.T) {
	// Arrange
	callbackCalled := false
	s := New(new(mocks.MockFetcher), WithResponseCallback(func(resp *http.Response) {
		callbackCalled = true
		// Validate Deep Copy Safety
		// 1. Body should be NoBody
		assert.Equal(t, http.NoBody, resp.Body, "Callback should receive NoBody")
		// 2. Request should be nil
		assert.Nil(t, resp.Request, "Callback should receive nil Request")
		// 3. Header modification should not affect original (though hard to verify side effect here without original check after)
		resp.Header.Set("X-Modified", "True")
	})).(*scraper)
	logger := applog.WithContext(context.Background())

	originalHeader := http.Header{"X-Original": []string{"True"}}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("body")),
		Request:    &http.Request{URL: nil},
		Header:     originalHeader,
	}

	// Act
	err := s.validateResponse(resp, requestParams{}, logger)

	// Assert
	assert.NoError(t, err)
	assert.True(t, callbackCalled, "Response callback should be called")
	assert.Equal(t, "True", originalHeader.Get("X-Original"))
	assert.Empty(t, originalHeader.Get("X-Modified"), "Callback specific modifications should not leak to original header")
}

// =================================================================================
// Test Group 2: ReadResponseBodyWithLimit
// =================================================================================

func TestReadResponseBodyWithLimit(t *testing.T) {
	tests := []struct {
		name          string
		bodyContent   string
		maxSize       int64
		wantContent   string
		wantTruncated bool
	}{
		{
			name:          "Success - Under Limit",
			bodyContent:   "12345",
			maxSize:       10,
			wantContent:   "12345",
			wantTruncated: false,
		},
		{
			name:          "Success - Exact Limit",
			bodyContent:   "1234567890",
			maxSize:       10,
			wantContent:   "1234567890",
			wantTruncated: false,
		},
		{
			name:          "Success - Over Limit (Truncated)",
			bodyContent:   "12345678901",
			maxSize:       10,
			wantContent:   "1234567890",
			wantTruncated: true,
		},
		{
			name:          "Success - Empty Body",
			bodyContent:   "",
			maxSize:       10,
			wantContent:   "",
			wantTruncated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			s := New(new(mocks.MockFetcher), WithMaxResponseBodySize(tt.maxSize)).(*scraper)
			resp := &http.Response{
				Body:       io.NopCloser(strings.NewReader(tt.bodyContent)),
				StatusCode: http.StatusOK, // Assuming 200 OK unless specified
			}

			// Act
			content, truncated, err := s.readResponseBodyWithLimit(context.Background(), resp)

			// Assert
			require.NoError(t, err)
			assert.Equal(t, tt.wantTruncated, truncated)
			assert.Equal(t, tt.wantContent, string(content))
		})
	}
}

func TestReadResponseBodyWithLimit_ContextCancel(t *testing.T) {
	// Arrange
	s := New(new(mocks.MockFetcher)).(*scraper)
	ctx, cancel := context.WithCancel(context.Background())
	resp := &http.Response{
		Body:       io.NopCloser(strings.NewReader("some body")),
		StatusCode: http.StatusOK,
	}

	// Act - Cancel before read
	cancel()
	_, _, err := s.readResponseBodyWithLimit(ctx, resp)

	// Assert
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestReadResponseBodyWithLimit_NoContent(t *testing.T) {
	// Arrange
	s := New(new(mocks.MockFetcher)).(*scraper)
	resp := &http.Response{
		StatusCode: http.StatusNoContent,
		Body:       io.NopCloser(strings.NewReader("")),
	}

	// Act
	content, truncated, err := s.readResponseBodyWithLimit(context.Background(), resp)

	// Assert
	require.NoError(t, err)
	assert.False(t, truncated)
	assert.Nil(t, content)
}

// =================================================================================
// Test Group 3: ReadErrorResponseBody & PreviewBody
// =================================================================================

func TestReadErrorResponseBody(t *testing.T) {
	eucKrData, _ := io.ReadAll(transform.NewReader(strings.NewReader("한글"), korean.EUCKR.NewEncoder()))

	tests := []struct {
		name        string
		body        []byte
		contentType string
		want        string
	}{
		{
			name: "UTF-8 Body",
			body: []byte("Error Message"),
			want: "Error Message",
		},
		{
			name:        "EUC-KR Body with Charset",
			body:        eucKrData,
			contentType: "text/plain; charset=euc-kr",
			want:        "한글",
		},
		{
			name: "Truncation check (Over 1KB)",
			body: append(bytes.Repeat([]byte("a"), 1024), []byte("bc")...),
			want: strings.Repeat("a", 1024),
		},
		{
			name: "UTF-8 Sanitation (Invalid Sequence)",
			// Invalid UTF-8 sequence (0xFF is never valid in UTF-8)
			body:        []byte("Valid" + string([]byte{0xFF}) + "End"),
			contentType: "text/plain; charset=utf-8",
			want:        "Valid\ufffdEnd", // charset decoder replaces invalid bytes with U+FFFD
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			s := New(new(mocks.MockFetcher)).(*scraper)
			resp := &http.Response{
				Body:   io.NopCloser(bytes.NewReader(tt.body)),
				Header: http.Header{"Content-Type": []string{tt.contentType}},
			}

			// Act
			got, err := s.readErrorResponseBody(resp)

			// Assert
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("Read Error", func(t *testing.T) {
		s := New(new(mocks.MockFetcher)).(*scraper)
		resp := &http.Response{
			Body:   io.NopCloser(&faultyReader{err: errors.New("read error in error body")}),
			Header: http.Header{"Content-Type": []string{"text/plain"}},
		}

		got, err := s.readErrorResponseBody(resp)

		// Assert: Error should be returned
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "read error in error body")
		assert.Empty(t, got)
	})
}

func TestPreviewBody(t *testing.T) {
	eucKrData, _ := io.ReadAll(transform.NewReader(strings.NewReader("한글"), korean.EUCKR.NewEncoder()))

	tests := []struct {
		name        string
		body        []byte
		contentType string
		want        string
		wantPrefix  bool // Exact match or Prefix match
	}{
		{
			name: "Empty Body",
			body: []byte{},
			want: "",
		},
		{
			name: "Short UTF-8",
			body: []byte("Hello World"),
			want: "Hello World",
		},
		{
			name:        "EUC-KR Conversion",
			body:        eucKrData,
			contentType: "text/html; charset=euc-kr",
			want:        "한글",
		},
		{
			name:       "Binary Data Detection (Null Byte)",
			body:       []byte{0x00, 0x01, 0x02, 0x03},
			want:       "[바이너리 데이터]",
			wantPrefix: true,
		},
		{
			name:       "Binary Data Detection (Control Char)",
			body:       []byte{'H', 'i', 0x07}, // Bell char
			want:       "[바이너리 데이터]",
			wantPrefix: true,
		},
		{
			name: "Whitespace Control Chars (Allowed)",
			body: []byte("Line1\nLine2\tTabbed\rCarriage"),
			want: "Line1\nLine2\tTabbed\rCarriage",
		},
		{
			name: "Truncation with Ellipsis",
			body: bytes.Repeat([]byte("a"), 2000),
			want: strings.Repeat("a", 1024) + "...(생략됨)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			s := New(new(mocks.MockFetcher)).(*scraper)

			// Act
			got := s.previewBody(tt.body, tt.contentType)

			// Assert
			if tt.wantPrefix {
				assert.True(t, strings.HasPrefix(got, tt.want), "Expected prefix %q, got %q", tt.want, got)
			} else {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

// =================================================================================
// Test Group 4: Helper Functions
// =================================================================================

func TestIsCommonContentTypes(t *testing.T) {
	t.Run("isUTF8ContentType", func(t *testing.T) {
		assert.True(t, isUTF8ContentType("text/html; charset=utf-8"))
		assert.True(t, isUTF8ContentType("application/json; charset=UTF-8"))
		assert.True(t, isUTF8ContentType("text/plain; CHARSET=utf-8")) // Case insensitive
		assert.False(t, isUTF8ContentType("text/html; charset=euc-kr"))
		assert.False(t, isUTF8ContentType("image/png"))
		assert.False(t, isUTF8ContentType(""))
	})

	t.Run("isHTMLContentType", func(t *testing.T) {
		assert.True(t, isHTMLContentType("text/html"))
		assert.True(t, isHTMLContentType("text/html; charset=utf-8"))
		assert.True(t, isHTMLContentType("application/xhtml+xml"))
		assert.True(t, isHTMLContentType("TEXT/HTML")) // Case insensitive
		assert.False(t, isHTMLContentType("application/json"))
		assert.False(t, isHTMLContentType("text/plain"))
		assert.False(t, isHTMLContentType(""))
	})
}
