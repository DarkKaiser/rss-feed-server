package fetcher_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestMimeTypeFetcher_Do(t *testing.T) {
	// 공통 테스트 데이터
	urlStr := "http://example.com"
	req := httptest.NewRequest(http.MethodGet, urlStr, nil)

	tests := []struct {
		name               string
		allowedMimeTypes   []string
		allowEmptyMimeType bool
		responseHeader     string // Delegate가 반환할 Content-Type 헤더
		delegateErr        error  // Delegate가 반환할 에러
		expectedErr        error  // 기대하는 에러 (sentinel error 체크용)
		errContains        string // 에러 메시지에 포함되어야 할 문자열
	}{
		// =================================================================
		// 1. 정상 케이스 (허용)
		// =================================================================
		{
			name:             "허용된 Content-Type (HTML) - 정상 통과",
			allowedMimeTypes: []string{"text/html"},
			responseHeader:   "text/html; charset=utf-8",
		},
		{
			name:             "허용된 Content-Type (JSON) - 정상 통과",
			allowedMimeTypes: []string{"text/html", "application/json"},
			responseHeader:   "application/json",
		},
		{
			name:             "대소문자 무시 (허용 목록이 대문자) - 정상 통과",
			allowedMimeTypes: []string{"Text/HTML"},
			responseHeader:   "text/html",
		},
		{
			name:             "대소문자 무시 (응답 헤더가 대문자) - 정상 통과",
			allowedMimeTypes: []string{"text/html"},
			responseHeader:   "Text/HTML; charset=UTF-8",
		},
		{
			name:             "파라미터가 복잡한 Content-Type - 정상 통과",
			allowedMimeTypes: []string{"multipart/form-data"},
			responseHeader:   "multipart/form-data; boundary=something; charset=utf-8",
		},
		{
			name:             "비표준 헤더 폴백 처리 (세미콜론 형식 오류) - 정상 통과",
			allowedMimeTypes: []string{"text/html"},
			// 파싱에는 실패하지만, 폴백 로직이 앞부분("text/html")을 추출하여 허용
			responseHeader: "text/html; invalid-parameter",
		},
		{
			name:             "공백이 포함된 헤더 (TrimSpace) - 정상 통과",
			allowedMimeTypes: []string{"text/html"},
			responseHeader:   " text/html ; charset=utf-8 ",
		},
		{
			name:               "Content-Type 없음 (allowEmpty=true) - 정상 통과",
			allowedMimeTypes:   []string{"text/html"},
			allowEmptyMimeType: true,
			responseHeader:     "",
		},
		{
			name:             "빈 allowedMimeTypes (모든 타입 허용) - HTML",
			allowedMimeTypes: []string{},
			responseHeader:   "text/html",
		},
		{
			name:             "빈 allowedMimeTypes (모든 타입 허용) - ZIP",
			allowedMimeTypes: []string{},
			responseHeader:   "application/zip",
		},

		// =================================================================
		// 2. 비정상 케이스 (거부)
		// =================================================================
		{
			name:             "허용되지 않은 Content-Type (ZIP) - 거부",
			allowedMimeTypes: []string{"text/html"},
			responseHeader:   "application/zip",
			errContains:      "지원하지 않는 미디어 타입입니다",
		},
		{
			name:             "허용되지 않은 Content-Type (HTML 아님) - 거부",
			allowedMimeTypes: []string{"application/json"},
			responseHeader:   "text/html",
			errContains:      "지원하지 않는 미디어 타입입니다",
		},
		{
			name:             "Strict Mode: Prefix match should FAIL - 거부",
			allowedMimeTypes: []string{"text/plain"},
			// 접두사는 같지만, 정확히 일치하지 않으므로 거부되어야 함
			responseHeader: "text/plain-custom",
			errContains:    "지원하지 않는 미디어 타입입니다",
		},
		{
			name:             "완전 잘못된 Content-Type - 거부",
			allowedMimeTypes: []string{"application/json"},
			responseHeader:   "INVALID-TYPE ;;;",
			errContains:      "지원하지 않는 미디어 타입입니다",
		},
		{
			name:               "Content-Type 없음 (allowEmpty=false) - 거부",
			allowedMimeTypes:   []string{"text/html"},
			allowEmptyMimeType: false,
			responseHeader:     "",
			expectedErr:        fetcher.ErrMissingResponseContentType,
		},

		// =================================================================
		// 3. Delegate 에러
		// =================================================================
		{
			name:             "Delegate Fetcher 에러 발생",
			allowedMimeTypes: []string{"text/html"},
			delegateErr:      errors.New("network failure"),
			errContains:      "network failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mock 설정
			mockFetcher := mocks.NewMockFetcher()
			response := &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewBufferString("body content")),
			}
			if tt.responseHeader != "" {
				response.Header.Set("Content-Type", tt.responseHeader)
			}

			// Delegate 동작 정의
			if tt.delegateErr != nil {
				mockFetcher.On("Do", mock.Anything).Return(nil, tt.delegateErr)
			} else {
				mockFetcher.On("Do", mock.Anything).Return(response, nil)
			}

			// MimeTypeFetcher 생성 및 실행
			f := fetcher.NewMimeTypeFetcher(mockFetcher, tt.allowedMimeTypes, tt.allowEmptyMimeType)
			resp, err := f.Do(req)

			// 검증
			if tt.expectedErr != nil || tt.errContains != "" || tt.delegateErr != nil {
				// 에러가 발생해야 하는 경우
				require.Error(t, err)
				assert.Nil(t, resp)

				if tt.expectedErr != nil {
					assert.ErrorIs(t, err, tt.expectedErr, "예상된 에러 타입과 다릅니다")
				}
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains, "에러 메시지에 예상된 문자열이 포함되지 않았습니다")
				}
			} else {
				// 성공해야 하는 경우
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, response, resp)

				// 리소스 정리 (성공 케이스는 호출자가 닫을 책임이 있음)
				_ = resp.Body.Close()
			}

			// Mock 호출 검증
			mockFetcher.AssertExpectations(t)
		})
	}
}

func TestMimeTypeFetcher_ResourceManagement(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)

	t.Run("검증 실패 시 Body를 비우고 닫아야 함 (drainAndCloseBody)", func(t *testing.T) {
		// Mock Body 생성 (Close, Read 호출 추적)
		mockBody := mocks.NewMockReadCloser("some content to drain")

		mockFetcher := mocks.NewMockFetcher()
		response := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/pdf"}}, // 비허용 타입
			Body:       mockBody,
		}
		mockFetcher.On("Do", mock.Anything).Return(response, nil)

		f := fetcher.NewMimeTypeFetcher(mockFetcher, []string{"text/html"}, false)
		resp, err := f.Do(req)

		// 검증
		assert.Error(t, err)
		assert.Nil(t, resp)

		// 리소스 관리 검증
		assert.Equal(t, int64(1), mockBody.GetCloseCount(), "검증 실패 시 Body.Close()가 호출되어야 함")
		assert.True(t, mockBody.WasRead(), "커넥션 재사용을 위해 Body 데이터를 읽어야(drain) 함")
	})

	t.Run("Delegate 에러 시 응답 객체가 있으면 Body를 닫아야 함", func(t *testing.T) {
		mockBody := mocks.NewMockReadCloser("dangling body")
		mockFetcher := mocks.NewMockFetcher()

		// 에러와 함께 응답이 오는 드문 경우 (예: 일부 리다이렉트 에러 등)
		response := &http.Response{Body: mockBody}
		mockFetcher.On("Do", mock.Anything).Return(response, errors.New("underlying error"))

		f := fetcher.NewMimeTypeFetcher(mockFetcher, []string{"text/html"}, false)
		resp, err := f.Do(req)

		assert.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, int64(1), mockBody.GetCloseCount())
	})

	t.Run("Content-Type 누락(거부) 시 Body를 닫아야 함", func(t *testing.T) {
		mockBody := mocks.NewMockReadCloser("content")
		mockFetcher := mocks.NewMockFetcher()

		response := &http.Response{
			Header: make(http.Header), // Content-Type 없음
			Body:   mockBody,
		}
		mockFetcher.On("Do", mock.Anything).Return(response, nil)

		f := fetcher.NewMimeTypeFetcher(mockFetcher, []string{"text/html"}, false)
		resp, err := f.Do(req)

		assert.ErrorIs(t, err, fetcher.ErrMissingResponseContentType)
		assert.Nil(t, resp)
		assert.Equal(t, int64(1), mockBody.GetCloseCount())
		assert.True(t, mockBody.WasRead())
	})
}

func TestMimeTypeFetcher_Close(t *testing.T) {
	// Delegate의 Close 메서드가 호출되는지 검증
	mockFetcher := mocks.NewMockFetcher()
	mockFetcher.On("Close").Return(nil)

	f := fetcher.NewMimeTypeFetcher(mockFetcher, nil, true)
	err := f.Close()

	assert.NoError(t, err)
	mockFetcher.AssertExpectations(t)
}

func TestMimeTypeFetcher_ContextPassThrough(t *testing.T) {
	// 컨텍스트가 Delegate로 잘 전달되는지 검증
	type ctxKey string
	key := ctxKey("requestID")
	val := "req-123"

	ctx := context.WithValue(context.Background(), key, val)
	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil).WithContext(ctx)

	mockFetcher := mocks.NewMockFetcher()

	// 매처(Matcher)를 사용하여 컨텍스트 값 검증
	mockFetcher.On("Do", mock.MatchedBy(func(r *http.Request) bool {
		return r.Context().Value(key) == val
	})).Return(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       io.NopCloser(bytes.NewBufferString("ok")),
	}, nil)

	f := fetcher.NewMimeTypeFetcher(mockFetcher, []string{"text/html"}, true)
	resp, err := f.Do(req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	resp.Body.Close()

	mockFetcher.AssertExpectations(t)
}
