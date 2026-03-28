package fetcher_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher/mocks" // Import updated mocks
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckResponseStatus_StatusCodeMapping verifies that HTTP status codes are correctly mapped to domain errors.
func TestCheckResponseStatus_StatusCodeMapping(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantError  apperrors.ErrorType
		wantStatus string
	}{
		// Success (Should not be called in error path usually, but for completeness)
		{"200 OK", http.StatusOK, apperrors.Unknown, "OK"},

		// Client Errors (4xx)
		{"400 Bad Request", http.StatusBadRequest, apperrors.InvalidInput, "Bad Request"},
		{"401 Unauthorized", http.StatusUnauthorized, apperrors.Forbidden, "Unauthorized"},
		{"403 Forbidden", http.StatusForbidden, apperrors.Forbidden, "Forbidden"},
		{"404 Not Found", http.StatusNotFound, apperrors.NotFound, "Not Found"},
		{"405 Method Not Allowed", http.StatusMethodNotAllowed, apperrors.ExecutionFailed, "Method Not Allowed"},
		{"408 Request Timeout", http.StatusRequestTimeout, apperrors.Unavailable, "Request Timeout"},
		{"429 Too Many Requests", http.StatusTooManyRequests, apperrors.Unavailable, "Too Many Requests"},

		// Server Errors (5xx)
		{"500 Internal Server Error", http.StatusInternalServerError, apperrors.Unavailable, "Internal Server Error"},
		{"502 Bad Gateway", http.StatusBadGateway, apperrors.Unavailable, "Bad Gateway"},
		{"503 Service Unavailable", http.StatusServiceUnavailable, apperrors.Unavailable, "Service Unavailable"},
		{"504 Gateway Timeout", http.StatusGatewayTimeout, apperrors.Unavailable, "Gateway Timeout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.statusCode,
				Status:     http.StatusText(tt.statusCode),
				Request: &http.Request{
					URL: &url.URL{Scheme: "https", Host: "example.com", Path: "/"},
				},
				Body: io.NopCloser(bytes.NewBufferString("")),
			}

			// Force error by not allowing any status codes (empty allowed list checks 200 OK only)
			// But for 200 OK case, it returns nil.
			err := fetcher.CheckResponseStatus(resp)

			if tt.statusCode == http.StatusOK {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.True(t, apperrors.Is(err, tt.wantError), "expected error type %v, got %v", tt.wantError, err)

				var statusErr *fetcher.HTTPStatusError
				if assert.ErrorAs(t, err, &statusErr) {
					assert.Equal(t, tt.statusCode, statusErr.StatusCode)
					assert.Equal(t, tt.wantStatus, statusErr.Status)
				}
			}
		})
	}
}

// TestCheckResponseStatus_AllowedCodes verifies valid vs invalid status code logic.
func TestCheckResponseStatus_AllowedCodes(t *testing.T) {
	resp404 := &http.Response{
		StatusCode: http.StatusNotFound,
		Status:     "404 Not Found",
		Body:       io.NopCloser(bytes.NewBufferString("")),
	}
	resp200 := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(bytes.NewBufferString("")),
	}

	t.Run("Default (Empty list implies 200 only)", func(t *testing.T) {
		assert.Error(t, fetcher.CheckResponseStatus(resp404))
		assert.NoError(t, fetcher.CheckResponseStatus(resp200))
	})

	t.Run("Explicit Allow List", func(t *testing.T) {
		// Allow 404
		assert.NoError(t, fetcher.CheckResponseStatus(resp404, http.StatusNotFound))

		// 200 is NOT implicitly allowed if list is provided
		assert.Error(t, fetcher.CheckResponseStatus(resp200, http.StatusNotFound))
	})

	t.Run("Multiple Allowed codes", func(t *testing.T) {
		assert.NoError(t, fetcher.CheckResponseStatus(resp404, http.StatusOK, http.StatusNotFound))
		assert.NoError(t, fetcher.CheckResponseStatus(resp200, http.StatusOK, http.StatusNotFound))
	})
}

// TestCheckResponseStatus_BodyHandling verifies body snippet capture, limiting, and reconstruction.
func TestCheckResponseStatus_BodyHandling(t *testing.T) {
	t.Run("Snippet Limitation (Max 4KB)", func(t *testing.T) {
		longBody := strings.Repeat("A", 5000)
		resp := &http.Response{
			StatusCode: http.StatusInternalServerError,
			Status:     "500 Error",
			Body:       io.NopCloser(bytes.NewBufferString(longBody)),
		}

		err := fetcher.CheckResponseStatus(resp) // Default reconstruct=true
		require.Error(t, err)

		var statusErr *fetcher.HTTPStatusError
		require.ErrorAs(t, err, &statusErr)

		assert.Len(t, statusErr.BodySnippet, 4096, "Body snippet should be capped at 4096 bytes")
		assert.Equal(t, strings.Repeat("A", 4096), statusErr.BodySnippet)
	})

	t.Run("Reconstruction (Default)", func(t *testing.T) {
		originalBody := "some useful error details"
		resp := &http.Response{
			StatusCode: http.StatusBadRequest,
			Status:     "400 Bad Request",
			Body:       io.NopCloser(bytes.NewBufferString(originalBody)),
		}

		err := fetcher.CheckResponseStatus(resp)
		require.Error(t, err)

		// Verify we can still read the full body from resp.Body
		read, readErr := io.ReadAll(resp.Body)
		require.NoError(t, readErr)
		assert.Equal(t, originalBody, string(read))
	})

	t.Run("No Reconstruction", func(t *testing.T) {
		originalBody := "transient error"
		resp := &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Status:     "503 Service Unavailable",
			Body:       io.NopCloser(bytes.NewBufferString(originalBody)),
		}

		err := fetcher.CheckResponseStatusWithoutReconstruct(resp)
		require.Error(t, err)

		var statusErr *fetcher.HTTPStatusError
		require.ErrorAs(t, err, &statusErr)
		assert.Equal(t, originalBody, statusErr.BodySnippet)

		// Verify body is consumed
		remaining, _ := io.ReadAll(resp.Body)
		assert.Empty(t, remaining, "Body should be consumed and not reconstructed")
	})

	t.Run("Nil Body", func(t *testing.T) {
		resp := &http.Response{
			StatusCode: http.StatusInternalServerError,
			Status:     "500 Error",
			Body:       nil,
		}

		err := fetcher.CheckResponseStatus(resp)
		require.Error(t, err)
		var statusErr *fetcher.HTTPStatusError
		require.ErrorAs(t, err, &statusErr)
		assert.Empty(t, statusErr.BodySnippet)
	})

	t.Run("Body Read Error", func(t *testing.T) {
		// Use shared MockReadCloser from mocks package with injected Read error
		mockBody := mocks.NewMockReadCloser("")
		mockBody.ReadErr = errors.New("read failed")

		resp := &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       mockBody,
		}

		err := fetcher.CheckResponseStatus(resp)
		require.Error(t, err)

		// If read fails, snippet should be empty (or partial) and no panic
		var statusErr *fetcher.HTTPStatusError
		require.ErrorAs(t, err, &statusErr)
		assert.Empty(t, statusErr.BodySnippet)
	})
}

// TestCheckResponseStatus_RedactionIntegration verifies that sensitive information is redacted.
func TestCheckResponseStatus_RedactionIntegration(t *testing.T) {
	t.Run("URL Redaction", func(t *testing.T) {
		u, _ := url.Parse("https://api.example.com/sensitive?token=secret123&public=ok")
		resp := &http.Response{
			StatusCode: http.StatusUnauthorized,
			Status:     "401 Unauthorized",
			Request:    &http.Request{URL: u},
			Body:       io.NopCloser(bytes.NewBufferString("")),
		}

		err := fetcher.CheckResponseStatus(resp)
		require.Error(t, err)

		var statusErr *fetcher.HTTPStatusError
		require.ErrorAs(t, err, &statusErr)

		assert.Contains(t, statusErr.URL, "token=xxxxx")
		assert.NotContains(t, statusErr.URL, "secret123")
		assert.Contains(t, statusErr.URL, "public=ok")
	})

	t.Run("Header Redaction", func(t *testing.T) {
		resp := &http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Header: http.Header{
				"Authorization": []string{"Bearer secret_token"},
				"Cookie":        []string{"session=s3cr3t"},
				"Content-Type":  []string{"application/json"},
			},
			Body: io.NopCloser(bytes.NewBufferString("")),
		}

		err := fetcher.CheckResponseStatus(resp)
		require.Error(t, err)

		var statusErr *fetcher.HTTPStatusError
		require.ErrorAs(t, err, &statusErr)

		assert.Equal(t, "***", statusErr.Header.Get("Authorization"))
		assert.Equal(t, "***", statusErr.Header.Get("Cookie"))
		assert.Equal(t, "application/json", statusErr.Header.Get("Content-Type"))
	})
}

// TestCheckResponseStatus_BodyClosePreservation ensures that the reconstructed body
// still calls the original Closer.
func TestCheckResponseStatus_BodyClosePreservation(t *testing.T) {
	// Use shared MockReadCloser from mocks package
	mockBody := mocks.NewMockReadCloser("test")

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       mockBody,
	}

	// Trigger error with reconstruction
	err := fetcher.CheckResponseStatus(resp)
	require.Error(t, err)

	// User reads new body then closes it
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	assert.Equal(t, int64(1), mockBody.GetCloseCount(), "Original body's Close method must be called")
}
