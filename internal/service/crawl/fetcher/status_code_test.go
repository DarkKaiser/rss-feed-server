package fetcher_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestStatusCodeFetcher_Do 상태 코드 검증 로직을 시나리오별로 테스트합니다.
func TestStatusCodeFetcher_Do(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		allowedStatusCodes []int
		delegateResponse   *http.Response
		delegateError      error
		expectedError      bool
		expectedErrType    apperrors.ErrorType
		expectedSnippet    string
	}{
		{
			name:             "Success: 200 OK (Default)",
			delegateResponse: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString("success"))},
			expectedError:    false,
		},
		{
			name:               "Success: 201 Created (Custom Config)",
			allowedStatusCodes: []int{http.StatusCreated},
			delegateResponse:   &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(bytes.NewBufferString("created"))},
			expectedError:      false,
		},
		{
			name:             "Fail: 404 Not Found (Default)",
			delegateResponse: &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(bytes.NewBufferString("page not found"))},
			expectedError:    true,
			expectedErrType:  apperrors.NotFound,
			expectedSnippet:  "page not found",
		},
		{
			name:             "Fail: 500 Internal Server Error",
			delegateResponse: &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(bytes.NewBufferString("server error"))},
			expectedError:    true,
			expectedErrType:  apperrors.Unavailable,
			expectedSnippet:  "server error",
		},
		{
			name:             "Fail: 403 Forbidden",
			delegateResponse: &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(bytes.NewBufferString("access denied"))},
			expectedError:    true,
			expectedErrType:  apperrors.Forbidden,
			expectedSnippet:  "access denied",
		},
		{
			name:             "Fail: 429 Too Many Requests",
			delegateResponse: &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(bytes.NewBufferString("rate limit"))},
			expectedError:    true,
			expectedErrType:  apperrors.Unavailable,
			expectedSnippet:  "rate limit",
		},
		{
			name:          "Fail: Delegate Error",
			delegateError: errors.New("network error"),
			expectedError: true,
		},
		{
			name: "Fail: Delegate Error with Partial Response (Resource Cleanup)",
			delegateResponse: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("partial content")),
			},
			delegateError: errors.New("read error"),
			expectedError: true,
		},
	}

	for _, tt := range tests {
		tt := tt // 캡처링
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel() // 병렬 실행

			// Mock 설정
			mockFetcher := mocks.NewMockFetcher()
			// Mock은 요청 받은 Response/Error를 그대로 반환
			mockFetcher.On("Do", mock.Anything).Return(tt.delegateResponse, tt.delegateError)

			// Fetcher 생성
			var f *fetcher.StatusCodeFetcher
			if tt.allowedStatusCodes != nil {
				f = fetcher.NewStatusCodeFetcherWithOptions(mockFetcher, tt.allowedStatusCodes...)
			} else {
				f = fetcher.NewStatusCodeFetcher(mockFetcher)
			}

			// 요청 실행
			req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
			resp, err := f.Do(req)

			// 검증
			if tt.expectedError {
				require.Error(t, err)
				if tt.expectedErrType != apperrors.Unknown {
					assert.True(t, apperrors.Is(err, tt.expectedErrType), "에러 타입이 일치해야 합니다. got: %v", err)
				}
				if tt.expectedSnippet != "" {
					assert.Contains(t, err.Error(), tt.expectedSnippet)
				}
				assert.Nil(t, resp, "에러 발생 시 응답은 nil이어야 합니다")
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.delegateResponse != nil {
					assert.Equal(t, tt.delegateResponse.StatusCode, resp.StatusCode)
				}
			}
		})
	}
}

// TestStatusCodeFetcher_ResourceSafety 상태 코드 검증 실패 시 Body 닫힘 여부를 안전하게 확인합니다.
func TestStatusCodeFetcher_ResourceSafety(t *testing.T) {
	t.Parallel()

	t.Run("Status Check Fail -> Body closed", func(t *testing.T) {
		mockBody := mocks.NewMockReadCloser("error content")
		mockFetcher := mocks.NewMockFetcher()
		mockFetcher.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       mockBody,
		}, nil)

		f := fetcher.NewStatusCodeFetcher(mockFetcher)
		req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
		resp, err := f.Do(req)

		assert.Error(t, err)
		assert.Nil(t, resp)
		assert.Greater(t, mockBody.GetCloseCount(), int64(0), "에러 발생 시 Body는 닫혀야 합니다")
	})

	t.Run("Delegate Error -> Body closed", func(t *testing.T) {
		mockBody := mocks.NewMockReadCloser("partial")
		mockFetcher := mocks.NewMockFetcher()
		mockFetcher.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       mockBody,
		}, errors.New("network error"))

		f := fetcher.NewStatusCodeFetcher(mockFetcher)
		req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
		resp, err := f.Do(req)

		assert.Error(t, err)
		assert.Nil(t, resp)
		assert.Greater(t, mockBody.GetCloseCount(), int64(0), "Delegate 에러 시에도 Body는 닫혀야 합니다")
	})

	t.Run("Success -> Body open", func(t *testing.T) {
		mockBody := mocks.NewMockReadCloser("success")
		mockFetcher := mocks.NewMockFetcher()
		mockFetcher.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       mockBody,
		}, nil)

		f := fetcher.NewStatusCodeFetcher(mockFetcher)
		req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
		resp, err := f.Do(req)

		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, int64(0), mockBody.GetCloseCount(), "성공 시에는 Body가 열려 있어야 합니다")

		// 테스트 종료 후 명시적 닫기
		resp.Body.Close()
	})
}

// TestStatusCodeFetcher_Close Close 메서드 위임 검증
func TestStatusCodeFetcher_Close(t *testing.T) {
	t.Parallel()

	mockFetcher := mocks.NewMockFetcher()
	expectedErr := errors.New("close error")
	mockFetcher.On("Close").Return(expectedErr)

	f := fetcher.NewStatusCodeFetcher(mockFetcher)
	err := f.Close()

	assert.Equal(t, expectedErr, err)
	mockFetcher.AssertExpectations(t)
}
