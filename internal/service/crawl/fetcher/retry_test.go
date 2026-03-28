package fetcher_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// blockingReader blocks on Read until the context is canceled.
// Used for testing cancellation behavior.
type blockingReader struct {
	ctx context.Context
}

func (r *blockingReader) Read(p []byte) (n int, err error) {
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}

func (r *blockingReader) Close() error {
	return nil
}

// SpyBody is a mock ReadCloser that records Close calls.
type SpyBody struct {
	io.Reader
	Closed bool
}

func (s *SpyBody) Close() error {
	s.Closed = true
	return nil
}

func TestRetryFetcher_Do(t *testing.T) {
	dummyURL := "http://example.com"
	errNetwork := errors.New("dial tcp: i/o timeout")

	t.Run("Scenarios", func(t *testing.T) {
		tests := []struct {
			name              string
			method            string
			maxRetries        int
			minRetryDelay     time.Duration
			setupMock         func(*mocks.MockFetcher)
			setupReq          func(*http.Request) *http.Request
			wantErr           bool
			errCheck          func(error) bool
			expectedCallCount int
		}{
			{
				name:          "Success on first attempt",
				method:        http.MethodGet,
				maxRetries:    3,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("ok", 200), nil).Once()
				},
				expectedCallCount: 1,
			},
			{
				name:          "Success after transient failure (500)",
				method:        http.MethodGet,
				maxRetries:    3,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("error", 500), nil).Once()
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("ok", 200), nil).Once()
				},
				expectedCallCount: 2,
			},
			{
				name:          "Success after transient failure (429)",
				method:        http.MethodGet,
				maxRetries:    3,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("wait", 429), nil).Once()
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("ok", 200), nil).Once()
				},
				expectedCallCount: 2,
			},
			{
				name:          "Success after transient failure (408 Request Timeout)",
				method:        http.MethodGet,
				maxRetries:    3,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("timeout", 408), nil).Once()
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("ok", 200), nil).Once()
				},
				expectedCallCount: 2,
			},
			{
				name:          "Success after network error",
				method:        http.MethodGet,
				maxRetries:    3,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					m.On("Do", mock.Anything).Return(nil, errNetwork).Once()
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("ok", 200), nil).Once()
				},
				expectedCallCount: 2,
			},
			{
				name:          "Max retries exceeded (Status Codes)",
				method:        http.MethodGet,
				maxRetries:    2,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					// 1 initial + 2 retries = 3 calls
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("error", 500), nil).Times(3)
				},
				wantErr: true,
				errCheck: func(err error) bool {
					return errors.Is(err, fetcher.ErrMaxRetriesExceeded)
				},
				expectedCallCount: 3,
			},
			{
				name:          "Max retries exceeded (Network Errors)",
				method:        http.MethodGet,
				maxRetries:    2,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					m.On("Do", mock.Anything).Return(nil, errNetwork).Times(3)
				},
				wantErr: true,
				errCheck: func(err error) bool {
					// Should be wrapped in ErrMaxRetriesExceeded -> Unavailable
					// The net error is wrapped in Unavailable by the retry logic (or fallback)
					// Let's check for the net error inside
					return errors.Is(err, errNetwork) && apperrors.Is(err, apperrors.Unavailable)
				},
				expectedCallCount: 3,
			},
			{
				name:          "Non-retriable Status Code (404 Not Found)",
				method:        http.MethodGet,
				maxRetries:    3,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("not found", 404), nil).Once()
				},
				expectedCallCount: 1,
			},
			{
				name:          "Non-retriable Status Code (501 Not Implemented)",
				method:        http.MethodGet,
				maxRetries:    3,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("not implemented", 501), nil).Once()
				},
				expectedCallCount: 1,
			},
			{
				name:          "Non-retriable Error (Context Canceled)",
				method:        http.MethodGet,
				maxRetries:    3,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					m.On("Do", mock.Anything).Return(nil, context.Canceled).Once()
				},
				wantErr: true,
				errCheck: func(err error) bool {
					return errors.Is(err, context.Canceled)
				},
				expectedCallCount: 1,
			},
			{
				name:          "Non-retriable method (POST) - No Retry",
				method:        http.MethodPost,
				maxRetries:    3,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("error", 500), nil).Once()
				},
				wantErr: true,
				errCheck: func(err error) bool {
					// Should fail immediately
					return errors.Is(err, fetcher.ErrMaxRetriesExceeded)
				},
				expectedCallCount: 1,
			},
			// [New Scenario: GetBody Missing (Retry Disabled)]
			// If Body is present but GetBody is nil, retry should be disabled.
			{
				name:          "GetBody Missing (Retry Disabled)",
				method:        http.MethodPut,
				maxRetries:    3,
				minRetryDelay: time.Millisecond,
				setupReq: func(req *http.Request) *http.Request {
					req.Body = io.NopCloser(strings.NewReader("payload"))
					req.GetBody = nil // Explicitly nil
					return req
				},
				setupMock: func(m *mocks.MockFetcher) {
					// Should only try once because retry is disabled
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("error", 500), nil).Once()
				},
				wantErr: true,
				errCheck: func(err error) bool {
					return errors.Is(err, fetcher.ErrMaxRetriesExceeded)
				},
				expectedCallCount: 1,
			},
			// [New Scenario: GetBody Failure]
			// If GetBody returns error, retry loop should abort.
			{
				name:          "GetBody Failure (Abort Retry)",
				method:        http.MethodPut,
				maxRetries:    3,
				minRetryDelay: time.Millisecond,
				setupReq: func(req *http.Request) *http.Request {
					req.Body = io.NopCloser(strings.NewReader("payload"))
					req.GetBody = func() (io.ReadCloser, error) {
						return nil, errors.New("get body failed")
					}
					return req
				},
				setupMock: func(m *mocks.MockFetcher) {
					// 1st call fails
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("error", 500), nil).Once()
					// Then retry logic calls GetBody, fails, and aborts before 2nd Do
				},
				wantErr: true,
				errCheck: func(err error) bool {
					// Should return ErrGetBodyFailed
					// We check if error string contains "get body" (lowercase, matching the error created in setupReq)
					return strings.Contains(err.Error(), "get body")
				},
				expectedCallCount: 1,
			},
			// [New Scenario: Retry-After Exceeded]
			{
				name:          "Retry-After Exceeded (Server Too Demanding)",
				method:        http.MethodGet,
				maxRetries:    3,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					resp := mocks.NewMockResponse("wait long", 429)
					resp.Header.Set("Retry-After", "3600") // 1 hour
					m.On("Do", mock.Anything).Return(resp, nil).Once()
				},
				wantErr: true,
				errCheck: func(err error) bool {
					return apperrors.Is(err, apperrors.Unavailable) && strings.Contains(err.Error(), "초과하여 재시도가 중단되었습니다")
				},
				expectedCallCount: 1,
			},
			{
				name:          "Retry-After: 0 should bypass minRetryDelay",
				method:        http.MethodGet,
				maxRetries:    1,
				minRetryDelay: 1 * time.Hour, // Logic should ignore this if RA=0
				setupMock: func(m *mocks.MockFetcher) {
					resp := mocks.NewMockResponse("wait", 429)
					resp.Header.Set("Retry-After", "0")
					m.On("Do", mock.Anything).Return(resp, nil).Once()
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("ok", 200), nil).Once()
				},
				expectedCallCount: 2,
			},
			{
				name:          "Retry-After exceeds MaxRetryDelay (Seconds format)",
				method:        http.MethodGet,
				maxRetries:    3,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					resp := mocks.NewMockResponse("wait", 429)
					resp.Header.Set("Retry-After", "10") // 10 seconds
					m.On("Do", mock.Anything).Return(resp, nil).Once()
				},
				wantErr: true,
				errCheck: func(err error) bool {
					// We check for the Korean error message parts
					return strings.Contains(err.Error(), "재시도 대기 시간") && strings.Contains(err.Error(), "초과")
				},
				expectedCallCount: 1, // Start -> 429 (Retry-After big) -> Stop
			},
			{
				name:          "Retry-After exceeds MaxRetryDelay (Date format)",
				method:        http.MethodGet,
				maxRetries:    3,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					resp := mocks.NewMockResponse("wait", 429)
					// Set Retry-After to 1 hour in the future
					future := time.Now().UTC().Add(1 * time.Hour).Format(http.TimeFormat)
					resp.Header.Set("Retry-After", future)
					m.On("Do", mock.Anything).Return(resp, nil).Once()
				},
				wantErr: true,
				errCheck: func(err error) bool {
					return strings.Contains(err.Error(), "재시도 대기 시간") && strings.Contains(err.Error(), "초과")
				},
				expectedCallCount: 1,
			},
			{
				name:          "Retry-After Valid (Date format)",
				method:        http.MethodGet,
				maxRetries:    3,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					resp := mocks.NewMockResponse("wait", 429)
					// Set Retry-After to 1 second in the future (within limit)
					// Note: Time synchronization in tests is tricky, but we assume 1s is safe within 1s deadline.
					future := time.Now().UTC().Add(1 * time.Second).Format(http.TimeFormat)
					resp.Header.Set("Retry-After", future)
					m.On("Do", mock.Anything).Return(resp, nil).Once()
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("ok", 200), nil).Once()
				},
				expectedCallCount: 2,
			},
			{
				name:          "Context Deadline Exceeded during delegate call - No Retry",
				method:        http.MethodGet,
				maxRetries:    3,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					m.On("Do", mock.Anything).Return(nil, context.DeadlineExceeded).Once()
				},
				setupReq: func(req *http.Request) *http.Request {
					ctx, cancel := context.WithDeadline(req.Context(), time.Now().Add(-1*time.Hour)) // Already expired
					cancel()                                                                         // Clean up context immediately since we only need the expired state
					return req.WithContext(ctx)
				},
				wantErr: true,
				errCheck: func(err error) bool {
					return errors.Is(err, context.DeadlineExceeded)
				},
				expectedCallCount: 1,
			},
			{
				name:          "Retry on 502 Bad Gateway",
				method:        http.MethodGet,
				maxRetries:    1,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("bad gateway", 502), nil).Once()
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("ok", 200), nil).Once()
				},
				expectedCallCount: 2,
			},
			{
				name:          "Retry on 503 Service Unavailable",
				method:        http.MethodGet,
				maxRetries:    1,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("unavailable", 503), nil).Once()
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("ok", 200), nil).Once()
				},
				expectedCallCount: 2,
			},
			{
				name:          "Retry on 504 Gateway Timeout",
				method:        http.MethodGet,
				maxRetries:    1,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("timeout", 504), nil).Once()
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("ok", 200), nil).Once()
				},
				expectedCallCount: 2,
			},
			{
				name:          "Non-retriable Status (505 Version Not Supported)",
				method:        http.MethodGet,
				maxRetries:    3,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("version not supported", 505), nil).Once()
				},
				expectedCallCount: 1,
			},
			{
				name:          "Non-retriable Status (511 Network Auth Required)",
				method:        http.MethodGet,
				maxRetries:    3,
				minRetryDelay: time.Millisecond,
				setupMock: func(m *mocks.MockFetcher) {
					m.On("Do", mock.Anything).Return(mocks.NewMockResponse("auth required", 511), nil).Once()
				},
				expectedCallCount: 1,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mockFetcher := &mocks.MockFetcher{}
				tt.setupMock(mockFetcher)

				f := fetcher.NewRetryFetcher(mockFetcher, tt.maxRetries, tt.minRetryDelay, 1*time.Second)

				req, _ := http.NewRequest(tt.method, dummyURL, nil)
				if tt.method == http.MethodPost {
					req.GetBody = func() (io.ReadCloser, error) {
						return io.NopCloser(strings.NewReader("")), nil
					}
				}

				if tt.setupReq != nil {
					req = tt.setupReq(req)
				}

				resp, err := f.Do(req)

				if tt.wantErr {
					require.Error(t, err)
					if tt.errCheck != nil {
						assert.True(t, tt.errCheck(err), "Error validation failed: %v", err)
					}
				} else {
					require.NoError(t, err)
				}

				if resp != nil {
					resp.Body.Close()
				}

				mockFetcher.AssertNumberOfCalls(t, "Do", tt.expectedCallCount)
			})
		}
	})

	t.Run("Retry-After Preference", func(t *testing.T) {
		mockFetcher := &mocks.MockFetcher{}

		// First response: 429 with Retry-After: 1 (second)
		// Current minDelay is 10ms. Without header, backoff would be small.
		// With header, it should wait at least 1s.
		// We can't verify exact timing easily in unit test without mocking time,
		// but we can ensure it parses and processes it by checking logic path via result.
		// Detailed timing verification is done in integration/manual tests or by trusting logic.
		// Here we verify it DOES respect the retry flow.
		resp := mocks.NewMockResponse("wait", 429)
		resp.Header.Set("Retry-After", "1")

		mockFetcher.On("Do", mock.Anything).Return(resp, nil).Once()
		mockFetcher.On("Do", mock.Anything).Return(mocks.NewMockResponse("ok", 200), nil).Once()

		f := fetcher.NewRetryFetcher(mockFetcher, 2, 10*time.Millisecond, 5*time.Second)

		req, _ := http.NewRequest(http.MethodGet, dummyURL, nil)
		// To speed up test, actual sleeping is annoying.
		// However, since we can't inject a fake clock into `Do`, we might skip strict timing check
		// or use a very small Retry-After for functional check.
		// Let's use "0" or "0.01" if supported? Standard says seconds.
		// Let's assume logic test is sufficient and just check it retries.
		// For deterministic cancellation test below, we use Retry-After to FORCE a wait.

		_, err := f.Do(req)
		assert.NoError(t, err)
		mockFetcher.AssertNumberOfCalls(t, "Do", 2)
	})
}

// TestRetryFetcher_Cancellation validates that the fetcher respects context cancellation immediately.
func TestRetryFetcher_Cancellation(t *testing.T) {
	t.Run("Cancel during backoff wait", func(t *testing.T) {
		mockFetcher := &mocks.MockFetcher{}

		// Use Retry-After to force a long wait (2s), ensuring we're in the "sleep" phase
		// when we cancel. Jitter makes default backoff non-deterministic.
		resp := mocks.NewMockResponse("wait", 429)
		resp.Header.Set("Retry-After", "2")

		mockFetcher.On("Do", mock.Anything).Return(resp, nil).Once()

		f := fetcher.NewRetryFetcher(mockFetcher, 3, 100*time.Millisecond, 5*time.Second)

		ctx, cancel := context.WithCancel(context.Background())
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)

		// Trigger cancellation shortly after Do starts
		time.AfterFunc(50*time.Millisecond, cancel)

		start := time.Now()
		_, err := f.Do(req)
		duration := time.Since(start)

		require.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled))
		// Should return way faster than the 2s Retry-After
		assert.Less(t, duration, 500*time.Millisecond)
	})

	t.Run("Cancel during response body draining (Non-blocking)", func(t *testing.T) {
		// This verifies the fix for "Blocking on Cancellation".
		// We provide a body that blocks forever on Read.
		// Cancellation should trigger Immediate Close instead of Draining.

		mockDelegate := &mocks.MockFetcher{}

		// Create a body that blocks on Read until context done
		readerCtx, readerCancel := context.WithCancel(context.Background())
		defer readerCancel()
		blockBody := &blockingReader{ctx: readerCtx}

		response := &http.Response{
			StatusCode: 200, // Status irrelevant if error returned
			Body:       blockBody,
		}

		// Delegate returns this blocking body AND a cancellation/timeout error
		// Simulating a case where we caught a timeout but still got a responsive stream that hangs?
		// Or simply the context is done.
		mockDelegate.On("Do", mock.Anything).Return(response, context.Canceled).Once()

		f := fetcher.NewRetryFetcher(mockDelegate, 3, 10*time.Millisecond, 1*time.Second)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)

		start := time.Now()
		_, err := f.Do(req)
		duration := time.Since(start)

		require.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled))

		// If it tried to drain blockBody, it would hang until test timeout.
		// It should be immediate.
		assert.Less(t, duration, 100*time.Millisecond)
	})
}

// TestRetryFetcher_GetBody validates body reconstruction logic for retries.
func TestRetryFetcher_GetBody(t *testing.T) {
	t.Run("GetBody failure aborts retries", func(t *testing.T) {
		mockFetcher := &mocks.MockFetcher{}
		// First failure to trigger attempt to get body
		mockFetcher.On("Do", mock.Anything).Return(mocks.NewMockResponse("fail", 500), nil).Once()

		f := fetcher.NewRetryFetcher(mockFetcher, 3, time.Millisecond, time.Millisecond)

		req, _ := http.NewRequest(http.MethodGet, "http://example.com", strings.NewReader("body"))
		req.GetBody = func() (io.ReadCloser, error) {
			return nil, errors.New("getBody failed")
		}

		_, err := f.Do(req)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "getBody failed")
		// Should stop after 1 attempt (Initial attempt succeeds in calling Do, but retry fails at GetBody)
		mockFetcher.AssertNumberOfCalls(t, "Do", 1)
	})

	t.Run("Missing GetBody disables retries for Body requests", func(t *testing.T) {
		mockFetcher := &mocks.MockFetcher{}
		// Fails with retriable error
		mockFetcher.On("Do", mock.Anything).Return(mocks.NewMockResponse("error", 500), nil).Once()

		f := fetcher.NewRetryFetcher(mockFetcher, 3, time.Millisecond, time.Millisecond)

		req, _ := http.NewRequest(http.MethodPost, "http://example.com", strings.NewReader("data"))
		req.GetBody = nil // Explicitly nil

		_, err := f.Do(req)

		// Returns error because it couldn't retry
		require.Error(t, err)
		assert.True(t, errors.Is(err, fetcher.ErrMaxRetriesExceeded))
		mockFetcher.AssertNumberOfCalls(t, "Do", 1)
	})

	t.Run("GetBody is called on retry", func(t *testing.T) {
		mockFetcher := &mocks.MockFetcher{}
		// Fail once, then succeed
		mockFetcher.On("Do", mock.Anything).Return(mocks.NewMockResponse("fail", 500), nil).Once()
		mockFetcher.On("Do", mock.Anything).Return(mocks.NewMockResponse("success", 200), nil).Once()

		f := fetcher.NewRetryFetcher(mockFetcher, 3, time.Millisecond, time.Millisecond)

		req, _ := http.NewRequest(http.MethodGet, "http://example.com", strings.NewReader("body"))

		getBodyCallCount := 0
		req.GetBody = func() (io.ReadCloser, error) {
			getBodyCallCount++
			return io.NopCloser(strings.NewReader("body")), nil
		}

		_, err := f.Do(req)

		require.NoError(t, err)
		mockFetcher.AssertNumberOfCalls(t, "Do", 2)
		// GetBody should be called exactly once (to prepare for the retry)
		// The initial request uses the body provided to NewRequest (or GetBody if Client uses it, but here we pass req to Do).
		// RetryFetcher calls GetBody ONLY when it needs to retry.
		assert.Equal(t, 1, getBodyCallCount)
	})
	t.Run("Previous response body is closed on retry", func(t *testing.T) {
		mockFetcher := &mocks.MockFetcher{}

		// 1st response: 500 with a body that needs closing
		resp1 := mocks.NewMockResponse("error", 500)
		spyBody1 := &SpyBody{Reader: strings.NewReader("error body"), Closed: false}
		resp1.Body = spyBody1

		// 2nd response: 200 OK
		resp2 := mocks.NewMockResponse("ok", 200)
		spyBody2 := &SpyBody{Reader: strings.NewReader("ok body"), Closed: false}
		resp2.Body = spyBody2

		mockFetcher.On("Do", mock.Anything).Return(resp1, nil).Once()
		mockFetcher.On("Do", mock.Anything).Return(resp2, nil).Once()

		f := fetcher.NewRetryFetcher(mockFetcher, 2, time.Millisecond, time.Second)
		req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)

		resp, err := f.Do(req)

		require.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode)

		// Assert that the FIRST failed response body was closed
		assert.True(t, spyBody1.Closed, "First response body should be closed before retry")

		// The second response is returned to caller, so it might not be closed yet unless caller closes it.
		// We shouldn't assert spyBody2.Closed here unless we close `resp.Body`.
		resp.Body.Close()
		assert.True(t, spyBody2.Closed)
	})
}
