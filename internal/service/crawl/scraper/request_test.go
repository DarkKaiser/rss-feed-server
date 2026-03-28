package scraper

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher/mocks"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// spyReader is a wrapper for io.ReadCloser that tracks whether Close was called.
type spyReader struct {
	io.Reader
	Closed bool
}

func (s *spyReader) Close() error {
	s.Closed = true
	if c, ok := s.Reader.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// failReader is a helper for testing read errors
type failReader struct{}

func (f *failReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read failed")
}
func (f *failReader) Close() error { return nil }

func TestExecuteRequest_Comprehensive(t *testing.T) {
	// Logrus Hook for log verification
	hook := test.NewGlobal()

	type mockResponse struct {
		statusCode int
		body       string
		header     http.Header
		err        error
		// If set, this reader is used instead of string body
		// useful for testing read errors or tracking Close()
		customBody io.ReadCloser
	}

	tests := []struct {
		name        string
		params      requestParams
		scraperOpt  []Option
		mockResp    mockResponse
		ctxSetup    func() (context.Context, context.CancelFunc)
		wantErr     bool
		errType     apperrors.ErrorType
		errContains []string
		checkResult func(*testing.T, fetchResult)
		checkLog    func(*testing.T, *test.Hook)
		checkMock   func(*testing.T, *mocks.MockFetcher)
	}{
		// 1. Success Scenarios
		{
			name: "Success: Simple GET",
			params: requestParams{
				Method: "GET",
				URL:    "http://example.com",
			},
			mockResp: mockResponse{
				statusCode: 200,
				body:       "success",
				header:     http.Header{"Content-Type": []string{"text/plain"}},
			},
			checkResult: func(t *testing.T, res fetchResult) {
				assert.Equal(t, 200, res.Response.StatusCode)
				assert.Equal(t, "success", string(res.Body))
				assert.False(t, res.IsTruncated)
			},
		},
		{
			name: "Success: Truncated Body (Limit Exceeded)",
			params: requestParams{
				Method: "GET",
				URL:    "http://example.com/large",
			},
			scraperOpt: []Option{WithMaxResponseBodySize(5)}, // Limit 5 bytes
			mockResp: mockResponse{
				statusCode: 200,
				body:       "1234567890", // 10 bytes
			},
			checkResult: func(t *testing.T, res fetchResult) {
				assert.Equal(t, 200, res.Response.StatusCode)
				assert.Len(t, res.Body, 5)
				assert.Equal(t, "12345", string(res.Body))
				assert.True(t, res.IsTruncated)
			},
			checkLog: func(t *testing.T, hook *test.Hook) {
				found := false
				for _, entry := range hook.AllEntries() {
					if entry.Level == logrus.WarnLevel && strings.Contains(entry.Message, "응답 본문 크기 제한 초과") {
						found = true
					}
				}
				assert.True(t, found, "Expected truncation warning log")
			},
		},
		{
			name: "Success: Resource Cleanup (Body Closed)",
			params: requestParams{
				Method: "GET",
				URL:    "http://example.com",
			},
			mockResp: mockResponse{
				statusCode: 200,
				customBody: &spyReader{Reader: io.NopCloser(strings.NewReader("data"))},
			},
			checkResult: func(t *testing.T, res fetchResult) {
				// Result body is buffered
				assert.Equal(t, "data", string(res.Body))
			},
			checkMock: func(t *testing.T, m *mocks.MockFetcher) {
				// We can't easily check the mock here for closing,
				// but we can check if our spyReader was closed.
				// However, `executeRequest` receives the response from mock.
				// We need to access the reader *passed* to mock.
				// Actually, we construct the response in setup.
			},
		},

		// 2. Error Scenarios - Request/Network
		{
			name: "Error: Invalid URL (Request Creation Failed)",
			params: requestParams{
				Method: "GET",
				URL:    "://invalid-url",
			},
			wantErr:     true,
			errType:     apperrors.ExecutionFailed,
			errContains: []string{"HTTP 요청 생성 실패"},
		},
		{
			name: "Error: Network Failure",
			params: requestParams{
				Method: "GET",
				URL:    "http://example.com",
			},
			mockResp: mockResponse{
				err: errors.New("connection refused"),
			},
			wantErr:     true,
			errType:     apperrors.Unavailable,
			errContains: []string{"네트워크 오류"},
		},
		{
			name: "Error: Context Cancelled Pre-Request",
			params: requestParams{
				Method: "GET",
				URL:    "http://example.com",
			},
			ctxSetup: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			mockResp: mockResponse{
				err: context.Canceled,
			},
			wantErr:     true,
			errType:     apperrors.Unavailable,
			errContains: []string{"요청 중단"},
		},

		// 3. Error Scenarios - Validation
		{
			name: "Error: HTTP 404 Not Found",
			params: requestParams{
				Method: "GET",
				URL:    "http://example.com/404",
			},
			mockResp: mockResponse{
				statusCode: 404,
				body:       "Not Found",
			},
			wantErr:     true,
			errType:     apperrors.ExecutionFailed,
			errContains: []string{"404"},
		},
		{
			name: "Error: HTTP 500 Internal Server Error",
			params: requestParams{
				Method: "GET",
				URL:    "http://example.com/500",
			},
			mockResp: mockResponse{
				statusCode: 500,
				body:       "Internal Error",
			},
			wantErr:     true,
			errType:     apperrors.Unavailable,
			errContains: []string{"500"},
		},
		{
			name: "Error: Custom Validator Failed",
			params: requestParams{
				Method: "GET",
				URL:    "http://example.com",
				Validator: func(r *http.Response, l *applog.Entry) error {
					return errors.New("custom validation error")
				},
			},
			mockResp: mockResponse{
				statusCode: 200,
				body:       "ok",
			},
			wantErr:     true,
			errType:     apperrors.ExecutionFailed,
			errContains: []string{"custom validation error"},
		},

		// 4. Error Scenarios - Body Read
		{
			name: "Error: Body Read Failed",
			params: requestParams{
				Method: "GET",
				URL:    "http://example.com",
			},
			mockResp: mockResponse{
				statusCode: 200,
				customBody: io.NopCloser(&failReader{}),
			},
			wantErr:     true,
			errType:     apperrors.Unavailable,
			errContains: []string{"본문 데이터 수신 실패"},
		},
		{
			name: "Error: Context Cancelled During Read",
			params: requestParams{
				Method: "GET",
				URL:    "http://example.com",
			},
			ctxSetup: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				return ctx, cancel
			},
			mockResp: mockResponse{
				statusCode: 200,
				// A body that blocks then can be cancelled?
				// Or use a mock reader that checks context?
				// contextAwareReader handles this. To test it here, we rely on Read failure
				// triggered by context cancellation if we could simulate it.
				// Simpler: mocking read error as context.Canceled
				customBody: io.NopCloser(&faultyReader{err: context.Canceled}),
			},
			wantErr:     true,
			errType:     apperrors.Unavailable, // Should be wrapped as Unavailable
			errContains: []string{"본문 데이터 수신 실패"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hook.Reset()
			mockFetcher := new(mocks.MockFetcher)

			// Prepare Response
			var resp *http.Response
			var spy *spyReader

			if !tt.wantErr || (tt.mockResp.err != nil || tt.mockResp.statusCode != 0) {
				// Only setup mock if we expect a call (invalid URL fails before call)
				if !strings.Contains(tt.name, "Invalid URL") {
					if tt.mockResp.err != nil {
						mockFetcher.On("Do", mock.Anything).Return(nil, tt.mockResp.err)
					} else {
						body := tt.mockResp.customBody
						if body == nil {
							body = io.NopCloser(bytes.NewBufferString(tt.mockResp.body))
						}
						// Wrap in spy to verify Close() if needed
						if s, ok := body.(*spyReader); ok {
							spy = s
						} else {
							spy = &spyReader{Reader: body}
							body = spy
						}

						resp = &http.Response{
							StatusCode: tt.mockResp.statusCode,
							Header:     tt.mockResp.header,
							Body:       body,
						}
						mockFetcher.On("Do", mock.Anything).Return(resp, nil)
					}
				}
			}

			// Setup Scraper
			s := New(mockFetcher, tt.scraperOpt...).(*scraper)

			// Setup Context
			var ctx context.Context
			var cancel context.CancelFunc
			if tt.ctxSetup != nil {
				ctx, cancel = tt.ctxSetup()
			} else {
				ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			}
			defer cancel()

			// Execute
			result, _, err := s.executeRequest(ctx, tt.params)

			// Verification
			if tt.wantErr {
				require.Error(t, err)
				if tt.errType != apperrors.Unknown {
					assert.True(t, apperrors.Is(err, tt.errType), "Expected error type %s, got %v", tt.errType, err)
				}
				for _, msg := range tt.errContains {
					assert.Contains(t, err.Error(), msg)
				}
			} else {
				require.NoError(t, err)
				if tt.checkResult != nil {
					tt.checkResult(t, result)
				}
			}

			if tt.checkLog != nil {
				tt.checkLog(t, hook)
			}

			// Verify Body Closed
			if spy != nil && !strings.Contains(tt.name, "Context Cancelled") {
				// For context cancelled during read, close might happen or not depending on where it failed.
				// But generally executeRequest defers Close.
				assert.True(t, spy.Closed, "Original response body should be closed")
			}
		})
	}
}
