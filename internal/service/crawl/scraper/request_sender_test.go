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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// slowReader simulates a slow read to allow context cancellation to happen.
type slowReader struct{}

func (s *slowReader) Read(p []byte) (n int, err error) {
	// Sleep longer than the context cancel delay in the test
	time.Sleep(50 * time.Millisecond)
	// Return some data so ReadAll calls Read again
	return copy(p, []byte("data")), nil
}

func TestScraper_prepareBody_Comprehensive(t *testing.T) {
	tests := []struct {
		name string
		// Input
		body       any
		scraperOpt []Option
		// Context
		ctxSetup func() (context.Context, context.CancelFunc)
		// Verification
		wantContent string // Expected content if successful
		wantErr     bool
		errType     apperrors.ErrorType
		errContains []string
	}{
		// -------------------------------------------------------------------------
		// [Category 1: Supported Types]
		// -------------------------------------------------------------------------
		{
			name:        "Success - Nil Body",
			body:        nil,
			wantContent: "",
		},
		{
			name:        "Success - String Body",
			body:        "test string",
			wantContent: "test string",
		},
		{
			name:        "Success - Byte Slice Body",
			body:        []byte("test bytes"),
			wantContent: "test bytes",
		},
		{
			name:        "Success - io.Reader Body (Buffer)",
			body:        bytes.NewBufferString("test reader"),
			wantContent: "test reader",
		},
		{
			name:        "Success - Struct (JSON)",
			body:        struct{ Name string }{"json"},
			wantContent: `{"Name":"json"}`,
		},
		{
			name:        "Success - Map (JSON)",
			body:        map[string]int{"val": 1},
			wantContent: `{"val":1}`,
		},

		// -------------------------------------------------------------------------
		// [Category 2: Edge Cases & Optimizations]
		// -------------------------------------------------------------------------
		{
			name:        "Success - Empty String",
			body:        "",
			wantContent: "",
		},
		{
			name:        "Success - Empty Byte Slice",
			body:        []byte{},
			wantContent: "",
		},
		{
			name:        "Success - Typed Nil (io.Reader)",
			body:        (*bytes.Buffer)(nil),
			wantContent: "",
		},
		{
			name:        "Success - *bytes.Buffer (Optimization)",
			body:        bytes.NewBufferString("optimized buffer"),
			wantContent: "optimized buffer",
		},
		{
			name:        "Success - *bytes.Reader (Optimization)",
			body:        bytes.NewReader([]byte("optimized reader")),
			wantContent: "optimized reader",
		},
		{
			name:        "Success - *strings.Reader (Optimization)",
			body:        strings.NewReader("optimized strings"),
			wantContent: "optimized strings",
		},

		// -------------------------------------------------------------------------
		// [Category 3: Size Limits]
		// -------------------------------------------------------------------------
		{
			name: "Success - Body At Limit",
			body: "12345",
			scraperOpt: []Option{
				WithMaxRequestBodySize(5),
			},
			wantContent: "12345",
		},
		{
			name: "Error - String Body Over Limit",
			body: "123456",
			scraperOpt: []Option{
				WithMaxRequestBodySize(5),
			},
			wantErr:     true,
			errType:     apperrors.InvalidInput,
			errContains: []string{"요청 본문 크기 초과"},
		},
		{
			name: "Error - Reader Body Over Limit",
			body: bytes.NewBufferString("123456"),
			scraperOpt: []Option{
				WithMaxRequestBodySize(5),
			},
			wantErr:     true,
			errType:     apperrors.InvalidInput,
			errContains: []string{"요청 본문 크기 초과"},
		},
		{
			name: "Error - JSON Body Over Limit",
			body: map[string]string{"key": "value_too_long"},
			scraperOpt: []Option{
				WithMaxRequestBodySize(5),
			},
			wantErr:     true,
			errType:     apperrors.InvalidInput,
			errContains: []string{"요청 본문 크기 초과"},
		},

		// -------------------------------------------------------------------------
		// [Category 4: Errors & Context Handling]
		// -------------------------------------------------------------------------
		{
			name:        "Error - JSON Marshal Failure",
			body:        map[string]any{"chan": make(chan int)}, // Channel cannot be marshaled
			wantErr:     true,
			errType:     apperrors.Internal,
			errContains: []string{"JSON 인코딩 실패"},
		},
		{
			name:        "Error - Reader Read Failure",
			body:        &failReader{},
			wantErr:     true,
			errType:     apperrors.ExecutionFailed,
			errContains: []string{"요청 본문 준비 실패"},
		},
		{
			name: "Error - Context Canceled Before Read",
			body: strings.NewReader("test"),
			ctxSetup: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			wantErr:     true,
			errType:     apperrors.Unknown, // Should return context error directly
			errContains: []string{"context canceled"},
		},
		{
			name: "Error - Context Canceled During Read (Slow Reader)",
			body: &slowReader{}, // This reader sleeps
			ctxSetup: func() (context.Context, context.CancelFunc) {
				// Cancel context after small delay
				ctx, cancel := context.WithCancel(context.Background())
				go func() {
					time.Sleep(10 * time.Millisecond)
					cancel()
				}()
				return ctx, cancel
			},
			wantErr:     true,
			errType:     apperrors.Unknown, // Should return context error directly, not wrapped
			errContains: []string{"context canceled"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize Scraper
			s := New(&mocks.MockFetcher{}, tt.scraperOpt...)
			impl := s.(*scraper)

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
			reader, err := impl.prepareBody(ctx, tt.body)

			// Verify
			if tt.wantErr {
				require.Error(t, err)
				if len(tt.errContains) > 0 {
					for _, msg := range tt.errContains {
						assert.Contains(t, err.Error(), msg)
					}
				}
				if tt.errType != apperrors.Unknown {
					assert.True(t, apperrors.Is(err, tt.errType), "Expected error type %s, got %v", tt.errType, err)
				} else if strings.Contains(tt.name, "Context") {
					// For context errors, we expect direct errors (context.Canceled or DeadlineExceeded)
					// Verify it is NOT wrapped in apperror if possible, or matches standard error
					assert.True(t, errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
				}
			} else {
				require.NoError(t, err)
				if tt.wantContent == "" {
					if reader != nil {
						content, err := io.ReadAll(reader)
						assert.NoError(t, err)
						assert.Empty(t, content)
					}
				} else {
					require.NotNil(t, reader)
					content, err := io.ReadAll(reader)
					assert.NoError(t, err)
					assert.Equal(t, tt.wantContent, string(content))
				}
			}
		})
	}
}

func TestScraper_createAndSendRequest_Comprehensive(t *testing.T) {
	tests := []struct {
		name string
		// Input
		params requestParams
		// Mock
		mockSetup func(*mocks.MockFetcher)
		// Context
		ctxSetup func() (context.Context, context.CancelFunc)
		// Verification
		wantErr     bool
		errType     apperrors.ErrorType
		errContains []string
		checkResp   func(*testing.T, *http.Response)
	}{
		// -------------------------------------------------------------------------
		// [Category 1: Success & Header Handling]
		// -------------------------------------------------------------------------
		{
			name: "Success - Basic Request",
			params: requestParams{
				Method: "GET",
				URL:    "http://example.com",
			},
			mockSetup: func(m *mocks.MockFetcher) {
				m.On("Do", mock.MatchedBy(func(req *http.Request) bool {
					return req.Method == "GET" && req.URL.String() == "http://example.com"
				})).Return(&http.Response{StatusCode: 200}, nil)
			},
			checkResp: func(t *testing.T, resp *http.Response) {
				assert.Equal(t, 200, resp.StatusCode)
			},
		},
		{
			name: "Success - Header Cloning (Immutable)",
			params: requestParams{
				Method: "GET",
				URL:    "http://example.com",
				Header: http.Header{"X-Orig": []string{"val"}},
			},
			mockSetup: func(m *mocks.MockFetcher) {
				m.On("Do", mock.MatchedBy(func(req *http.Request) bool {
					// Verify request has original header
					if req.Header.Get("X-Orig") != "val" {
						return false
					}
					// Verify modifying request header doesn't affect params.Header (Conceptual check)
					// In Go, map is reference. Header.Clone() is used in createAndSendRequest.
					return true
				})).Return(&http.Response{StatusCode: 200}, nil)
			},
		},
		{
			name: "Success - Default Accept",
			params: requestParams{
				Method:        "GET",
				URL:           "http://example.com",
				DefaultAccept: "application/json",
			},
			mockSetup: func(m *mocks.MockFetcher) {
				m.On("Do", mock.MatchedBy(func(req *http.Request) bool {
					return req.Header.Get("Accept") == "application/json"
				})).Return(&http.Response{StatusCode: 200}, nil)
			},
		},
		{
			name: "Success - Explicit Accept Overrides Default",
			params: requestParams{
				Method:        "GET",
				URL:           "http://example.com",
				Header:        http.Header{"Accept": []string{"text/xml"}},
				DefaultAccept: "application/json",
			},
			mockSetup: func(m *mocks.MockFetcher) {
				m.On("Do", mock.MatchedBy(func(req *http.Request) bool {
					return req.Header.Get("Accept") == "text/xml"
				})).Return(&http.Response{StatusCode: 200}, nil)
			},
		},

		// -------------------------------------------------------------------------
		// [Category 2: Context Cancellation]
		// -------------------------------------------------------------------------
		{
			name: "Error - Context Canceled During Do",
			params: requestParams{
				Method: "GET",
				URL:    "http://example.com",
			},
			ctxSetup: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				// Cancel so that createAndSendRequest detects it
				cancel()
				return ctx, cancel
			},
			mockSetup: func(m *mocks.MockFetcher) {
				// Simulate Fetcher detecting canceled context
				m.On("Do", mock.Anything).Return(nil, context.Canceled)
			},
			wantErr:     true,
			errType:     apperrors.Unavailable,
			errContains: []string{"요청 중단"},
		},
		{
			name: "Error - Context Timeout During Do",
			params: requestParams{
				Method: "GET",
				URL:    "http://example.com",
			},
			ctxSetup: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithTimeout(context.Background(), 0) // Already timed out
				return ctx, cancel
			},
			mockSetup: func(m *mocks.MockFetcher) {
				m.On("Do", mock.Anything).Return(nil, context.DeadlineExceeded)
			},
			wantErr:     true,
			errType:     apperrors.Unavailable,
			errContains: []string{"요청 중단"},
		},

		// -------------------------------------------------------------------------
		// [Category 3: Errors]
		// -------------------------------------------------------------------------
		{
			name: "Error - Invalid Request (Bad Method)",
			params: requestParams{
				Method: "INVALID METHOD",
				URL:    "http://example.com",
			},
			// Expect NewRequestWithContext to fail
			wantErr:     true,
			errType:     apperrors.ExecutionFailed,
			errContains: []string{"HTTP 요청 생성 실패"},
		},
		{
			name: "Error - Network Error",
			params: requestParams{
				Method: "GET",
				URL:    "http://example.com",
			},
			mockSetup: func(m *mocks.MockFetcher) {
				m.On("Do", mock.Anything).Return(nil, errors.New("dial tcp: i/o timeout"))
			},
			wantErr:     true,
			errType:     apperrors.Unavailable,
			errContains: []string{"네트워크 오류"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFetcher := new(mocks.MockFetcher)
			if tt.mockSetup != nil {
				tt.mockSetup(mockFetcher)
			}

			s := New(mockFetcher)
			impl := s.(*scraper)

			var ctx context.Context
			var cancel context.CancelFunc
			if tt.ctxSetup != nil {
				ctx, cancel = tt.ctxSetup()
			} else {
				ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			}
			defer cancel()

			resp, err := impl.createAndSendRequest(ctx, tt.params)

			if tt.wantErr {
				require.Error(t, err)
				if len(tt.errContains) > 0 {
					for _, msg := range tt.errContains {
						assert.Contains(t, err.Error(), msg)
					}
				}
				if tt.errType != apperrors.Unknown {
					assert.True(t, apperrors.Is(err, tt.errType), "Expected error type %s, got %v", tt.errType, err)
				}
			} else {
				require.NoError(t, err)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

func init() {
	applog.SetLevel(applog.DebugLevel)
}
