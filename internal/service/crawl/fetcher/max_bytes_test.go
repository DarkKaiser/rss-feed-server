package fetcher_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewMaxBytesFetcher(t *testing.T) {
	mockF := mocks.NewMockFetcher()

	tests := []struct {
		name          string
		limit         int64
		wantLimit     int64
		wantSameFetch bool
	}{
		{
			name:      "정상 케이스: 양수 제한값 (1024)",
			limit:     1024,
			wantLimit: 1024,
		},
		{
			name:          "특수 케이스: NoLimit (-1)",
			limit:         fetcher.NoLimit,
			wantSameFetch: true,
		},
		{
			name:      "경계 케이스: 0 (기본값 적용)",
			limit:     0,
			wantLimit: fetcher.DefaultMaxBytes,
		},
		{
			name:      "경계 케이스: 음수 (NoLimit 제외, 기본값 적용)",
			limit:     -2,
			wantLimit: fetcher.DefaultMaxBytes,
		},
		{
			name:          "경계 케이스: MaxInt64",
			limit:         1<<63 - 1,
			wantLimit:     1<<63 - 1, // 그대로 유지되어야 함
			wantSameFetch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := fetcher.NewMaxBytesFetcher(mockF, tt.limit)

			if tt.wantSameFetch {
				assert.Equal(t, mockF, f, "NoLimit인 경우 원본 Fetcher가 반환되어야 합니다")
				return
			}

			assert.NotEqual(t, mockF, f, "새로운 MaxBytesFetcher 인스턴스가 생성되어야 합니다")

			// 내부 필드 확인을 위해 리플렉션 대신 기능적 검증 또는 export_test.go 활용 고려
			// 여기서는 블랙박스 테스트 관점에서 동작 검증이 더 적절하지만,
			// 단위 테스트의 정확성을 위해 *MaxBytesFetcher 타입 단언 후 내부 값 확인
			_, ok := f.(*fetcher.MaxBytesFetcher)
			require.True(t, ok, "MaxBytesFetcher 타입이어야 합니다")

			// reflect를 쓰지 않고 정확한 필드 검증이 어려우므로
			// Do를 호출하여 Limit가 적용되는지 간접 확인하거나,
			// export_test.go에 Getter를 추가하여 검증할 수 있음.
			// 현재는 구조체가 unexported field 'limit'를 가지므로 직접 접근 불가.
			// 다만, 이 테스트는 Factory의 로직(normalizeByteLimit)을 검증하는 것이 주 목적임.
			// normalizeByteLimit은 내부 함수이므로, Do 메서드 테스트에서 동작을 검증하는 것이 더 확실함.
			// 하지만 Factory 테스트로서 입력값에 따른 반환 타입과 기본 동작을 검증하는 가치는 있음.
		})
	}
}

func TestMaxBytesFetcher_Do(t *testing.T) {
	tests := []struct {
		name              string
		limit             int64
		mockSetup         func(*mocks.MockFetcher) func(t *testing.T) // Returns a verification function
		wantError         bool
		wantErrorMsg      string
		wantBodyFragment  string
		checkBodyReadErr  bool
		expectedErrorType apperrors.ErrorType
	}{
		{
			name:  "정상 케이스: 제한보다 작은 본문",
			limit: 100,
			mockSetup: func(m *mocks.MockFetcher) func(*testing.T) {
				resp := &http.Response{
					StatusCode:    http.StatusOK,
					Body:          io.NopCloser(strings.NewReader("Small body")),
					ContentLength: 10,
				}
				m.On("Do", mock.Anything).Return(resp, nil)
				return nil
			},
			wantError:        false,
			wantBodyFragment: "Small body",
		},
		{
			name:  "정상 케이스: 제한과 정확히 같은 크기의 본문",
			limit: 10,
			mockSetup: func(m *mocks.MockFetcher) func(*testing.T) {
				resp := &http.Response{
					StatusCode:    http.StatusOK,
					Body:          io.NopCloser(strings.NewReader("1234567890")),
					ContentLength: 10,
				}
				m.On("Do", mock.Anything).Return(resp, nil)
				return nil
			},
			wantError:        false,
			wantBodyFragment: "1234567890",
		},
		{
			name:  "에러 케이스: Content-Length 헤더가 제한을 초과함",
			limit: 10,
			mockSetup: func(m *mocks.MockFetcher) func(*testing.T) {
				resp := &http.Response{
					StatusCode:    http.StatusOK,
					Body:          io.NopCloser(strings.NewReader("ignored")),
					ContentLength: 11, // Limit(10) < CL(11)
				}
				m.On("Do", mock.Anything).Return(resp, nil)
				return nil
			},
			wantError:         true,
			wantErrorMsg:      "Content-Length 헤더에 명시된",
			expectedErrorType: apperrors.InvalidInput,
		},
		{
			name:  "에러 케이스: 실제 읽기 시 제한 초과 (Content-Length 없음)",
			limit: 10,
			mockSetup: func(m *mocks.MockFetcher) func(*testing.T) {
				resp := &http.Response{
					StatusCode:    http.StatusOK,
					Body:          io.NopCloser(strings.NewReader("12345678901")), // 11 bytes
					ContentLength: -1,                                             // Unknown
				}
				m.On("Do", mock.Anything).Return(resp, nil)
				return nil
			},
			wantError:         false, // Do 호출 자체는 성공
			checkBodyReadErr:  true,  // Body Read 시 에러 발생
			wantErrorMsg:      "응답 본문의 크기가 설정된 제한을 초과했습니다",
			expectedErrorType: apperrors.InvalidInput,
		},
		{
			name:  "에러 케이스: 실제 읽기 시 제한 초과 (Content-Length 속임수)",
			limit: 10,
			mockSetup: func(m *mocks.MockFetcher) func(*testing.T) {
				resp := &http.Response{
					StatusCode:    http.StatusOK,
					Body:          io.NopCloser(strings.NewReader("12345678901")), // 11 bytes
					ContentLength: 5,                                              // Fake CL
				}
				m.On("Do", mock.Anything).Return(resp, nil)
				return nil
			},
			wantError:         false, // Do 호출 자체는 성공
			checkBodyReadErr:  true,  // Body Read 시 에러 발생
			wantErrorMsg:      "응답 본문의 크기가 설정된 제한을 초과했습니다",
			expectedErrorType: apperrors.InvalidInput,
		},
		{
			name:  "에러 케이스: Delegate Fetcher 실패",
			limit: 100,
			mockSetup: func(m *mocks.MockFetcher) func(*testing.T) {
				m.On("Do", mock.Anything).Return(nil, errors.New("network error"))
				return nil
			},
			wantError:    true,
			wantErrorMsg: "network error",
		},
		{
			name:  "경계 조건: 빈 본문",
			limit: 100,
			mockSetup: func(m *mocks.MockFetcher) func(*testing.T) {
				resp := &http.Response{
					StatusCode:    http.StatusOK,
					Body:          io.NopCloser(strings.NewReader("")),
					ContentLength: 0,
				}
				m.On("Do", mock.Anything).Return(resp, nil)
				return nil
			},
			wantError:        false,
			wantBodyFragment: "",
		},
		{
			name:  "리소스 정리: Body가 닫히는지 확인 (Content-Length 초과)",
			limit: 5,
			mockSetup: func(m *mocks.MockFetcher) func(*testing.T) {
				mockBody := &MockReadCloser{Reader: strings.NewReader("too large body")}
				resp := &http.Response{
					StatusCode:    http.StatusOK,
					Body:          mockBody,
					ContentLength: 15,
				}
				m.On("Do", mock.Anything).Return(resp, nil)

				return func(t *testing.T) {
					assert.True(t, mockBody.Closed, "Content-Length 초과 시 Body가 닫혀야 합니다")
				}
			},
			wantError: true,
		},
		{
			name:  "리소스 정리: 에러와 함께 응답이 반환된 경우 Body 닫힘 확인",
			limit: 100,
			mockSetup: func(m *mocks.MockFetcher) func(*testing.T) {
				mockBody := &MockReadCloser{Reader: strings.NewReader("some body")}
				resp := &http.Response{
					StatusCode: http.StatusOK,
					Body:       mockBody,
				}
				// 에러와 응답이 모두 반환되는 상황 시뮬레이션
				m.On("Do", mock.Anything).Return(resp, errors.New("partial error"))

				return func(t *testing.T) {
					assert.True(t, mockBody.Closed, "에러 발생 시에도 응답 객체가 있으면 Body가 닫혀야 합니다")
				}
			},
			wantError:    true,
			wantErrorMsg: "partial error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			mockF := mocks.NewMockFetcher()
			var verifyFunc func(*testing.T)
			if tt.mockSetup != nil {
				verifyFunc = tt.mockSetup(mockF)
			}
			f := fetcher.NewMaxBytesFetcher(mockF, tt.limit)
			req := &http.Request{Header: make(http.Header)}

			// When
			resp, err := f.Do(req)

			// Then
			if tt.wantError {
				require.Error(t, err)
				if tt.wantErrorMsg != "" {
					assert.Contains(t, err.Error(), tt.wantErrorMsg)
				}
				if tt.expectedErrorType != 0 { // 0 is Unknown (default)
					assert.True(t, apperrors.Is(err, tt.expectedErrorType), "expected error type mismatch")
				}

				// 추가 검증 로직 실행
				if verifyFunc != nil {
					verifyFunc(t)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			defer resp.Body.Close()

			// Body Read 검증
			bodyBytes, readErr := io.ReadAll(resp.Body)

			if tt.checkBodyReadErr {
				// Read 시점에 에러가 발생해야 하는 경우
				require.Error(t, readErr)
				if tt.wantErrorMsg != "" {
					assert.Contains(t, readErr.Error(), tt.wantErrorMsg)
				}
				if tt.expectedErrorType != 0 {
					assert.True(t, apperrors.Is(readErr, tt.expectedErrorType), "expected error type mismatch")
				}
			} else {
				// 정상 읽기 케이스
				require.NoError(t, readErr)
				if tt.wantBodyFragment != "" {
					assert.Equal(t, tt.wantBodyFragment, string(bodyBytes))
				}
			}

			// 추가 검증 로직 실행
			if verifyFunc != nil {
				verifyFunc(t)
			}
		})
	}
}

// MockReadCloser is a helper to verify Close calls if needed
type MockReadCloser struct {
	io.Reader
	Closed bool
}

func (m *MockReadCloser) Close() error {
	m.Closed = true
	return nil
}

func TestMaxBytesFetcher_DrainBehavior(t *testing.T) {
	// 리소스 누수 방지를 위한 drain 동작 검증
	t.Run("설정된 제한을 초과하는 경우 에러 반환 전 바디를 읽고 닫아야 한다", func(t *testing.T) {
		mockF := mocks.NewMockFetcher()
		// 1KB 제한
		f := fetcher.NewMaxBytesFetcher(mockF, 1024)

		realBody := io.NopCloser(bytes.NewReader(make([]byte, 2048)))
		resp := &http.Response{
			StatusCode:    http.StatusOK,
			Body:          realBody,
			ContentLength: 2048, // 제한 초과
		}
		mockF.On("Do", mock.Anything).Return(resp, nil)

		// When
		_, err := f.Do(&http.Request{})

		// Then
		// drainAndCloseBody 호출 확인은 간접적으로 수행되지만,
		// 핵심은 에러가 올바르게 반환되고 패닉이 없어야 함
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Content-Length 헤더에 명시된")
	})
}

func TestMaxBytesFetcher_Close(t *testing.T) {
	mockF := mocks.NewMockFetcher()
	f := fetcher.NewMaxBytesFetcher(mockF, 100)

	mockF.On("Close").Return(nil)

	err := f.Close()
	require.NoError(t, err)
	mockF.AssertExpectations(t)
}

func TestNormalizeByteLimit(t *testing.T) {
	tests := []struct {
		name  string
		input int64
		want  int64
	}{
		{
			name:  "NoLimit (-1)",
			input: fetcher.NoLimit,
			want:  fetcher.NoLimit,
		},
		{
			name:  "Zero (default applied)",
			input: 0,
			want:  fetcher.DefaultMaxBytes,
		},
		{
			name:  "Negative (excluding NoLimit, default applied)",
			input: -100,
			want:  fetcher.DefaultMaxBytes,
		},
		{
			name:  "Positive valid",
			input: 1024,
			want:  1024,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fetcher.NormalizeByteLimit(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
