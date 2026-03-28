package fetcher_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHTTPStatusError_Error verifies that the Error() method returns a correctly formatted string
// under various combinations of fields.
func TestHTTPStatusError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *fetcher.HTTPStatusError
		expected string
	}{
		{
			name: "Minimal fields",
			err: &fetcher.HTTPStatusError{
				StatusCode: http.StatusNotFound,
				Status:     "404 Not Found",
			},
			expected: "HTTP 404 (404 Not Found)",
		},
		{
			name: "With URL",
			err: &fetcher.HTTPStatusError{
				StatusCode: http.StatusInternalServerError,
				Status:     "500 Internal Server Error",
				URL:        "https://example.com/api",
			},
			expected: "HTTP 500 (500 Internal Server Error) URL: https://example.com/api",
		},
		{
			name: "With BodySnippet",
			err: &fetcher.HTTPStatusError{
				StatusCode:  http.StatusBadRequest,
				Status:      "400 Bad Request",
				BodySnippet: "invalid input",
			},
			expected: "HTTP 400 (400 Bad Request), Body: invalid input",
		},
		{
			name: "With Cause",
			err: &fetcher.HTTPStatusError{
				StatusCode: http.StatusForbidden,
				Status:     "403 Forbidden",
				Cause:      errors.New("access denied"),
			},
			expected: "HTTP 403 (403 Forbidden): access denied",
		},
		{
			name: "With All Fields",
			err: &fetcher.HTTPStatusError{
				StatusCode:  http.StatusTeapot,
				Status:      "418 I'm a teapot",
				URL:         "https://example.com/brew",
				BodySnippet: "short and stout",
				Cause:       errors.New("cannot brew coffee"),
			},
			expected: "HTTP 418 (418 I'm a teapot) URL: https://example.com/brew, Body: short and stout: cannot brew coffee",
		},
		{
			// Edge case: empty status text, nil cause, should not panic or format weirdly
			name: "Empty Status Text",
			err: &fetcher.HTTPStatusError{
				StatusCode: 599,
			},
			expected: "HTTP 599 ()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

// TestHTTPStatusError_Unwrap verifies standard error unwrapping.
func TestHTTPStatusError_Unwrap(t *testing.T) {
	rootCause := errors.New("root cause")

	t.Run("Returns Cause", func(t *testing.T) {
		err := &fetcher.HTTPStatusError{Cause: rootCause}
		assert.Equal(t, rootCause, err.Unwrap())
		assert.Equal(t, rootCause, errors.Unwrap(err))
	})

	t.Run("Returns Nil when no Cause", func(t *testing.T) {
		err := &fetcher.HTTPStatusError{Cause: nil}
		assert.Nil(t, err.Unwrap())
		assert.Nil(t, errors.Unwrap(err))
	})
}

// TestHTTPStatusError_ErrorChaining verifies compatibility with errors.Is and errors.As.
func TestHTTPStatusError_ErrorChaining(t *testing.T) {
	sentinelErr := errors.New("sentinel error")

	err := &fetcher.HTTPStatusError{
		StatusCode: 500,
		Status:     "Internal Server Error",
		Cause:      sentinelErr,
	}

	t.Run("errors.Is matches wrapped error", func(t *testing.T) {
		assert.True(t, errors.Is(err, sentinelErr), "Should match the wrapped sentinel error")
	})

	t.Run("errors.Is fails for unrelated error", func(t *testing.T) {
		assert.False(t, errors.Is(err, errors.New("other")), "Should not match unrelated error")
	})

	t.Run("errors.As extracts HTTPStatusError", func(t *testing.T) {
		var target *fetcher.HTTPStatusError
		assert.True(t, errors.As(err, &target), "Should be castable to *HTTPStatusError")
		assert.Equal(t, 500, target.StatusCode)
		assert.Equal(t, sentinelErr, target.Cause)
	})

	t.Run("errors.As extracts wrapped error type", func(t *testing.T) {
		// Create a custom error type to wrap
		customErr := &CustomError{Msg: "custom"}
		errWithCustom := &fetcher.HTTPStatusError{Cause: customErr}

		var target *CustomError
		assert.True(t, errors.As(errWithCustom, &target), "Should extract wrapped custom error")
		assert.Equal(t, "custom", target.Msg)
	})
}

// TestHTTPStatusError_WrappedInFmtErrorf verifies extraction when HTTPStatusError is wrapped.
func TestHTTPStatusError_WrappedInFmtErrorf(t *testing.T) {
	rootErr := &fetcher.HTTPStatusError{
		StatusCode: 404,
		Status:     "Not Found",
	}
	wrappedErr := fmt.Errorf("context: %w", rootErr)

	// Verify extraction via errors.As
	var extracted *fetcher.HTTPStatusError
	require.True(t, errors.As(wrappedErr, &extracted))
	assert.Equal(t, 404, extracted.StatusCode)
	assert.Equal(t, "Not Found", extracted.Status)
}

// CustomError for testing errors.As deep unwrapping
type CustomError struct {
	Msg string
}

func (e *CustomError) Error() string { return e.Msg }
