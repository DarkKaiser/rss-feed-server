package scraper

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
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
)

func TestFetchJSON_Comprehensive(t *testing.T) {
	// 로그 검증을 위한 Hook 설정
	hook := test.NewGlobal()

	// 테스트용 구조체 정의
	type User struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	tests := []struct {
		name string
		// Input
		method     string
		url        string
		body       any
		target     any
		header     http.Header
		scraperOpt []Option

		// Mock
		setupMock func(*testing.T, *mocks.MockFetcher)
		ctxSetup  func() (context.Context, context.CancelFunc)

		// Verification
		wantErr     bool
		errType     apperrors.ErrorType
		errContains []string
		assertVal   func(*testing.T, any)
		assertLog   func(*testing.T, *test.Hook)
	}{
		// 1. 정상 동작 (Success Cases)
		{
			name:   "Success: Basic GET",
			method: http.MethodGet,
			url:    "https://api.example.com/users/1",
			target: &User{},
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				resp := mocks.NewMockResponse(`{"id": 1, "name": "Alice"}`, 200)
				resp.Header.Set("Content-Type", "application/json")
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			assertVal: func(t *testing.T, val any) {
				user := val.(*User)
				assert.Equal(t, 1, user.ID)
				assert.Equal(t, "Alice", user.Name)
			},
		},
		{
			name:   "Success: POST with Body",
			method: http.MethodPost,
			url:    "https://api.example.com/users",
			body:   User{Name: "Bob"},
			target: &User{},
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				resp := mocks.NewMockResponse(`{"id": 2, "name": "Bob"}`, 201)
				resp.Header.Set("Content-Type", "application/json")

				m.On("Do", mock.MatchedBy(func(req *http.Request) bool {
					// 요청 본문 확인
					buf := new(bytes.Buffer)
					buf.ReadFrom(req.Body)
					return strings.Contains(buf.String(), `"name":"Bob"`) &&
						req.Method == http.MethodPost &&
						req.Header.Get("Content-Type") == "application/json"
				})).Return(resp, nil)
			},
			assertVal: func(t *testing.T, val any) {
				user := val.(*User)
				assert.Equal(t, 2, user.ID)
				assert.Equal(t, "Bob", user.Name)
			},
		},
		{
			name:   "Success: Vendor Specific Content-Type",
			method: http.MethodGet,
			url:    "https://api.example.com/data",
			target: &User{},
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				resp := mocks.NewMockResponse(`{"id": 1, "name": "Vendor"}`, 200)
				// 표준은 아니지만 JSON 호환 타입
				resp.Header.Set("Content-Type", "application/vnd.api+json")
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			assertVal: func(t *testing.T, val any) {
				user := val.(*User)
				assert.Equal(t, "Vendor", user.Name)
			},
		},
		{
			name:   "Success: Top-Level Array",
			method: http.MethodGet,
			url:    "https://api.example.com/users",
			target: &[]User{},
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				resp := mocks.NewMockResponse(`[{"id": 1, "name": "Alice"}, {"id": 2, "name": "Bob"}]`, 200)
				resp.Header.Set("Content-Type", "application/json")
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			assertVal: func(t *testing.T, val any) {
				users := val.(*[]User)
				assert.Len(t, *users, 2)
				assert.Equal(t, "Alice", (*users)[0].Name)
			},
		},
		{
			name:   "Success: 204 No Content",
			method: http.MethodDelete,
			url:    "https://api.example.com/users/1",
			target: &User{}, // 아무것도 채워지지 않음
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				resp := mocks.NewMockResponse("", 204)
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			assertVal: func(t *testing.T, val any) {
				user := val.(*User)
				assert.Zero(t, user.ID) // 변경 없음
			},
		},
		{
			name:   "Success: Valid JSON with Trailing Whitespace",
			method: http.MethodGet,
			url:    "https://api.example.com/users/whitespace",
			target: &User{},
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				// JSON 뒤에 공백/개행은 허용되어야 함
				resp := mocks.NewMockResponse(`{"id": 1, "name": "Space"}   `+"\n", 200)
				resp.Header.Set("Content-Type", "application/json")
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			assertVal: func(t *testing.T, val any) {
				user := val.(*User)
				assert.Equal(t, "Space", user.Name)
			},
		},

		// 2. 인코딩 및 Content-Type 검증 (Encoding & Validation)
		{
			name:   "Validation: HTML Response (Critical Error)",
			method: http.MethodGet,
			url:    "https://api.example.com/error",
			target: &User{},
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				resp := mocks.NewMockResponse(`<html><body>Error</body></html>`, 200)
				resp.Header.Set("Content-Type", "text/html")
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			wantErr:     true,
			errType:     apperrors.InvalidInput,
			errContains: []string{"응답 형식 오류", "JSON 대신 HTML이 반환되었습니다"},
		},
		{
			name:   "Validation: XHTML Response (Critical Error)",
			method: http.MethodGet,
			url:    "https://api.example.com/xhtml",
			target: &User{},
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				resp := mocks.NewMockResponse(`<!DOCTYPE html>...`, 200)
				resp.Header.Set("Content-Type", "application/xhtml+xml")
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			wantErr:     true,
			errType:     apperrors.InvalidInput,
			errContains: []string{"JSON 대신 HTML이 반환되었습니다"},
		},
		{
			name:   "Warning: Non-Standard Content-Type (text/plain)",
			method: http.MethodGet,
			url:    "https://api.example.com/plain",
			target: &User{},
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				resp := mocks.NewMockResponse(`{"id": 3, "name": "Plain"}`, 200)
				resp.Header.Set("Content-Type", "text/plain") // 잘못된 헤더지만 내용은 JSON
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			assertVal: func(t *testing.T, val any) {
				user := val.(*User)
				assert.Equal(t, "Plain", user.Name)
			},
			assertLog: func(t *testing.T, hook *test.Hook) {
				// 경고 로그 확인
				found := false
				for _, entry := range hook.AllEntries() {
					if entry.Level == logrus.WarnLevel && strings.Contains(entry.Message, "비표준 Content-Type") {
						found = true
						break
					}
				}
				assert.True(t, found, "비표준 Content-Type에 대한 경고 로그가 있어야 합니다")
			},
		},
		{
			name:   "Encoding: EUC-KR Decoding",
			method: http.MethodGet,
			url:    "https://api.example.com/korean",
			target: &User{},
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				// "홍길동" EUC-KR 인코딩: c8 ab b1 e6 b5 bf
				eucKRData := []byte{
					'{', '"', 'i', 'd', '"', ':', '1', ',', '"', 'n', 'a', 'm', 'e', '"', ':', '"',
					0xc8, 0xab, 0xb1, 0xe6, 0xb5, 0xbf, // 홍길동
					'"', '}',
				}
				resp := mocks.NewMockResponse(string(eucKRData), 200)
				resp.Header.Set("Content-Type", "application/json; charset=euc-kr")
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			assertVal: func(t *testing.T, val any) {
				user := val.(*User)
				assert.Equal(t, "홍길동", user.Name)
			},
		},

		// 3. 에러 처리 (Error Handling)
		{
			name:   "Error: Target is Nil",
			method: http.MethodGet,
			url:    "https://api.example.com",
			target: nil,
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				// 호출되지 않음
			},
			wantErr:     true,
			errType:     apperrors.Internal, // Internal System Error로 매핑되어야 함
			errContains: []string{"결과를 저장할 변수가 nil입니다"},
		},
		{
			name:   "Error: Target is Not Pointer",
			method: http.MethodGet,
			url:    "https://api.example.com",
			target: User{}, // 포인터 아님
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				// 호출되지 않음
			},
			wantErr:     true,
			errType:     apperrors.Internal,
			errContains: []string{"포인터여야 합니다"},
		},
		{
			name:   "Error: HTTP 500 Server Error",
			method: http.MethodGet,
			url:    "https://api.example.com/500",
			target: &User{},
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				resp := mocks.NewMockResponse(`{"error": "internal"}`, 500)
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			wantErr:     true,
			errType:     apperrors.Unavailable, // 5xx는 재시도 가능
			errContains: []string{"HTTP 요청 실패", "500"},
		},
		{
			name:   "Error: HTTP 404 Not Found",
			method: http.MethodGet,
			url:    "https://api.example.com/404",
			target: &User{},
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				resp := mocks.NewMockResponse(`{"error": "not found"}`, 404)
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			wantErr:     true,
			errType:     apperrors.ExecutionFailed, // 4xx는 재시도 불필요 (일반적으로)
			errContains: []string{"HTTP 요청 실패", "404"},
		},
		{
			name:   "Error: Network Failure",
			method: http.MethodGet,
			url:    "https://api.example.com",
			target: &User{},
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				m.On("Do", mock.Anything).Return(nil, errors.New("connection refused"))
			},
			wantErr:     true,
			errType:     apperrors.Unavailable,
			errContains: []string{"네트워크 오류"},
		},
		{
			name:   "Error: Context Timeout",
			method: http.MethodGet,
			url:    "https://api.example.com",
			target: &User{},
			ctxSetup: func() (context.Context, context.CancelFunc) {
				// 이미 만료된 컨텍스트 생성
				ctx, cancel := context.WithTimeout(context.Background(), -1*time.Second)
				return ctx, cancel
			},
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				// Fetcher 내부나 호출 전 Context 체크에서 걸림
				// Mock 호출은 발생할 수도 있고 안 할 수도 있음 (구현에 따라 다름)
				// Request 생성 단계에서 걸린다면 Do는 호출되지 않음.
				// 여기서는 executeRequest 내부 createAndSendRequest에서 NewRequestWithContext나
				// Fetcher.Do가 컨텍스트 에러를 반환한다고 가정.
				// 안전하게 Mock을 허용하되 에러 리턴
				m.On("Do", mock.Anything).Return(nil, context.DeadlineExceeded).Maybe()
			},
			wantErr:     true,
			errType:     apperrors.Unknown, // Raw context error returned
			errContains: []string{"context deadline exceeded"},
		},
		{
			name:   "Error: Request Body Truncation (Max Limit)",
			method: http.MethodGet,
			url:    "https://api.example.com/large",
			target: &User{},
			scraperOpt: []Option{
				WithMaxResponseBodySize(10), // 매우 작은 제한
			},
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				// 제한(10 byte)보다 큰 응답
				longJSON := `{"id": 1, "name": "Very Long Name..."}`
				resp := mocks.NewMockResponse(longJSON, 200)
				resp.Header.Set("Content-Type", "application/json")
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			wantErr:     true,
			errType:     apperrors.InvalidInput,
			errContains: []string{"응답 본문 크기 초과"},
		},
		{
			name:   "Error: Malformed JSON (Syntax Error)",
			method: http.MethodGet,
			url:    "https://api.example.com/bad-json",
			target: &User{},
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				badJSON := `{"id": 1, "name": "Broken" ...`
				resp := mocks.NewMockResponse(badJSON, 200)
				resp.Header.Set("Content-Type", "application/json")
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			wantErr:     true,
			errType:     apperrors.ParsingFailed,
			errContains: []string{"JSON 파싱 실패", "구문 오류"},
			assertLog: func(t *testing.T, hook *test.Hook) {
				// Syntax Error 발생 시 주변 문맥이 로그에 남는지 확인
				found := false
				for _, entry := range hook.AllEntries() {
					if entry.Level == logrus.ErrorLevel && strings.Contains(entry.Message, "JSON 데이터 변환 실패") {
						if _, ok := entry.Data["syntax_error_context"]; ok {
							found = true
						}
					}
				}
				assert.True(t, found, "구문 오류 시 로그에 문맥(Context) 정보가 있어야 합니다")
			},
		},
		{
			name:   "Error: Unmarshal Type Error",
			method: http.MethodGet,
			url:    "https://api.example.com/type-error",
			target: &User{}, // ID is int, Name is string
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				typeErrJSON := `{"id": "NotAnInteger", "name": "Bob"}`
				resp := mocks.NewMockResponse(typeErrJSON, 200)
				resp.Header.Set("Content-Type", "application/json")
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			wantErr:     true,
			errType:     apperrors.ParsingFailed,
			errContains: []string{"구문 오류로 인해 응답 데이터를 변환할 수 없습니다"},
		},
		{
			name:   "Error: Extra Data after JSON (Strict Mode)",
			method: http.MethodGet,
			url:    "https://api.example.com/extra",
			target: &User{},
			setupMock: func(t *testing.T, m *mocks.MockFetcher) {
				// 유효한 JSON 뒤에 쓰레기 데이터 존재
				extraJSON := `{"id": 1, "name": "Ok"} GARBAGE`
				resp := mocks.NewMockResponse(extraJSON, 200)
				resp.Header.Set("Content-Type", "application/json")
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			wantErr:     true,
			errType:     apperrors.ParsingFailed,
			errContains: []string{"불필요한 토큰"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hook.Reset()
			mockFetcher := &mocks.MockFetcher{}
			if tt.setupMock != nil {
				tt.setupMock(t, mockFetcher)
			}

			// Context 설정
			var ctx context.Context
			var cancel context.CancelFunc
			if tt.ctxSetup != nil {
				ctx, cancel = tt.ctxSetup()
			} else {
				// 기본 타임아웃
				ctx, cancel = context.WithTimeout(context.Background(), 1*time.Second)
			}
			defer cancel()

			// Scraper 생성
			s := New(mockFetcher, tt.scraperOpt...)

			// Body 준비 (PrepareBody 내부 로직은 별도 테스트되므로 여기선 단순 전달)
			err := s.FetchJSON(ctx, tt.method, tt.url, tt.body, tt.header, tt.target)

			// 에러 검증
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errType != apperrors.Unknown {
					assert.True(t, apperrors.Is(err, tt.errType), "Expected error type %s, got %v", tt.errType, err)
				}
				for _, msg := range tt.errContains {
					assert.Contains(t, err.Error(), msg)
				}
			} else {
				assert.NoError(t, err)
				if tt.assertVal != nil {
					tt.assertVal(t, tt.target)
				}
			}

			// 로그 검증
			if tt.assertLog != nil {
				tt.assertLog(t, hook)
			}

			mockFetcher.AssertExpectations(t)
		})
	}
}

// TestFetchJSON_MarshalError는 JSON 인코딩이 불가능한 Body를 전달했을 때를 검증합니다.
func TestFetchJSON_MarshalError(t *testing.T) {
	mockFetcher := &mocks.MockFetcher{}
	s := New(mockFetcher)

	// 채널은 JSON 마샬링이 불가능함
	body := make(chan int)
	var target map[string]any

	err := s.FetchJSON(context.Background(), "POST", "http://example.com", body, nil, &target)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "JSON 인코딩 실패")
}

// TestVerifyJSONContentType_EdgeCases는 Content-Type 헤더 검증의 다양한 엣지 케이스를 테스트합니다.
func TestVerifyJSONContentType_EdgeCases(t *testing.T) {
	s := &scraper{}
	logger := logrus.NewEntry(logrus.New())

	tests := []struct {
		name        string
		contentType string
		wantErr     bool
	}{
		{"Empty Content-Type", "", false},
		{"Case Insensitive Matches", "Application/JSON", false},
		{"With Parameters", "application/json; charset=utf-8", false},
		{"With Version Parameter", "application/json; v=2", false},
		{"HTML Content-Type", "text/html", true},
		{"HTML with Charset", "text/html; charset=utf-8", true},
		{"XML Content-Type", "application/xml", false}, // JSON은 아니지만 HTML도 아님 -> 경고 로그만 남기고 진행
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{tt.contentType}},
			}
			err := s.verifyJSONContentType(resp, "http://example.com", logger)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestDecodeJSONResponse_CharsetLogic은 다양한 인코딩 상황 및 에러 처리를 검증합니다.
func TestDecodeJSONResponse_CharsetLogic(t *testing.T) {
	s := &scraper{maxResponseBodySize: 1024}
	logger := logrus.NewEntry(logrus.New())

	t.Run("Unsupported Charset Fallback", func(t *testing.T) {
		// 지원되지 않는 Charset -> 원본 바이트로 파싱 시도
		// "Unknown" 이라는 Charset은 없을 것이므로 에러 발생 -> Fallback 작동
		jsonData := []byte(`{"key": "value"}`)
		result := fetchResult{
			Response: &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"application/json; charset=Unknown-Charset-X"}},
				Body:       io.NopCloser(bytes.NewReader(jsonData)),
			},
			Body: jsonData,
		}
		var target map[string]string

		err := s.decodeJSONResponse(context.Background(), result, &target, "http://url", logger)
		assert.NoError(t, err)
		assert.Equal(t, "value", target["key"])
	})

	t.Run("Decode Error with Invalid Charset Data", func(t *testing.T) {
		// 잘못된 인코딩 데이터가 들어왔을 때 디코딩 에러 발생 확인
		// EUC-KR 데이터를 UTF-8 헤더로 명시 -> 디코딩 에러 예상
		eucKRData := []byte{0xc8, 0xab} // "홍" (EUC-KR)
		result := fetchResult{
			Response: &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"application/json; charset=utf-8"}}, // 거짓말 헤더
				Body:       io.NopCloser(bytes.NewReader(eucKRData)),
			},
			Body: eucKRData,
		}
		var target any

		err := s.decodeJSONResponse(context.Background(), result, &target, "http://url", logger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "JSON 파싱 실패")
	})

	t.Run("Context Canceled Error Passthrough", func(t *testing.T) {
		// faultyReader로 읽기 오류를 유발하여 decodeJSONResponse의 charset 변환 에러 경로를 커버합니다.
		result := fetchResult{
			Response: &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(&faultyReader{err: context.DeadlineExceeded}),
			},
			Body: nil,
		}
		var target any

		err := s.decodeJSONResponse(context.Background(), result, &target, "http://url", logger)
		assert.Error(t, err)
	})
}

// TestDecodeJSONResponse_ContextCancel는 JSON 디코딩 중 Trailing Data를 확인하는 단계에서
// 컨텍스트가 취소되는 엣지 케이스를 검증합니다.
func TestDecodeJSONResponse_ContextCancel(t *testing.T) {
	// 1. Mock Context 생성 (Err() 호출 시 제어 가능)
	// 디코딩(Decode)은 성공하고, 그 직후(Token) 호출 시 취소되도록 유도
	callCount := 0
	mockCtx := &MockContext{
		Context: context.Background(),
		errFunc: func() error {
			callCount++
			// 1번째: Decode 내부 Read -> 성공 (nil)
			// 2번째: Token 내부 Read (종료 확인) -> 취소 (Canceled)
			if callCount > 1 {
				return context.Canceled
			}
			return nil
		},
	}

	// 2. 입력 데이터 준비 (유효한 JSON + 공백)
	jsonData := []byte(`{"id": 1}   `)
	result := fetchResult{
		Response: &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(jsonData)),
		},
		Body: jsonData,
	}

	s := &scraper{maxResponseBodySize: 1024}
	var target struct {
		ID int `json:"id"`
	}

	// 3. 실행
	err := s.decodeJSONResponse(mockCtx, result, &target, "http://test.url", applog.WithFields(nil))

	// 4. 검증
	// "불필요한 토큰" 에러가 아니라 "Context Canceled" 에러가 반환되어야 함
	assert.ErrorIs(t, err, context.Canceled)
}

// MockContext는 Context 인터페이스를 모의하여 Err() 동작을 제어합니다.
type MockContext struct {
	context.Context
	errFunc func() error
}

func (m *MockContext) Err() error {
	if m.errFunc != nil {
		return m.errFunc()
	}
	return nil
}

func (m *MockContext) Done() <-chan struct{} {
	return nil
}

// TestDecodeJSONResponse_Infinity is a test case for mathematical constants that are not valid in standard JSON.
// Go's json decoder might handle or reject them depending on usage, but standard JSON doesn't support NaN/Infinity.
func TestDecodeJSONResponse_Numbers(t *testing.T) {
	s := &scraper{maxResponseBodySize: 1024}
	logger := logrus.NewEntry(logrus.New())

	// JSON Spec does not support NaN or Infinity
	// This test ensures we handle them reasonably (usually error in standard library)
	data := []byte(`{"val": NaN}`)
	result := fetchResult{
		Response: &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewReader(data)),
		},
		Body: data,
	}
	var target struct {
		Val float64 `json:"val"`
	}

	err := s.decodeJSONResponse(context.Background(), result, &target, "url", logger)
	assert.Error(t, err) // Expect syntax error
}

func TestFetchJSON_UnsupportedFloat(t *testing.T) {
	mockFetcher := &mocks.MockFetcher{}
	s := New(mockFetcher)

	// Body with NaN (unsupported in JSON)
	body := map[string]float64{"val": math.NaN()}
	var target any

	err := s.FetchJSON(context.Background(), "POST", "http://example.com", body, nil, &target)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "JSON 인코딩 실패")
}
