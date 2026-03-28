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

	"github.com/PuerkitoBio/goquery"
	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher/mocks"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/transform"
)

// eucKrContent는 문자열을 EUC-KR로 인코딩하여 반환하는 헬퍼 함수입니다.
func eucKrContent(s string) string {
	var buf bytes.Buffer
	w := transform.NewWriter(&buf, korean.EUCKR.NewEncoder())
	w.Write([]byte(s))
	w.Close()
	return buf.String()
}

// faultyReader는 Read 호출 시 에러를 반환하는 Reader입니다.
type faultyReader struct {
	err error
}

func (r *faultyReader) Read(p []byte) (n int, err error) {
	return 0, r.err
}

func TestFetchHTML_Comprehensive(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		url         string
		body        io.Reader
		header      http.Header
		options     []Option
		setupMock   func(*mocks.MockFetcher)
		ctxSetup    func() (context.Context, context.CancelFunc)
		wantErr     bool
		errType     apperrors.ErrorType
		errContains []string
		checkLog    func(*testing.T, *test.Hook)
		validate    func(*testing.T, *goquery.Document)
	}{
		// 1. 정상 동작 (Success Cases)
		{
			name:   "Success: Basic UTF-8 Parsing",
			method: http.MethodGet,
			url:    "http://example.com/utf8",
			setupMock: func(m *mocks.MockFetcher) {
				resp := mocks.NewMockResponse(`<html><body><div class="test">Success</div></body></html>`, 200)
				resp.Header.Set("Content-Type", "text/html; charset=utf-8")
				req, _ := http.NewRequest(http.MethodGet, "http://example.com/utf8", nil)
				resp.Request = req

				m.On("Do", mock.MatchedBy(func(req *http.Request) bool {
					return req.Method == http.MethodGet && req.URL.String() == "http://example.com/utf8" &&
						strings.Contains(req.Header.Get("Accept"), "text/html")
				})).Return(resp, nil)
			},
			validate: func(t *testing.T, doc *goquery.Document) {
				assert.Equal(t, "Success", doc.Find(".test").Text())
			},
		},
		{
			name:   "Success: POST with Custom Header",
			method: http.MethodPost,
			url:    "http://example.com/post",
			body:   strings.NewReader("key=value"),
			header: http.Header{"X-Custom": []string{"MyValue"}},
			setupMock: func(m *mocks.MockFetcher) {
				resp := mocks.NewMockResponse(`<html></html>`, 200)
				req, _ := http.NewRequest(http.MethodPost, "http://example.com/post", strings.NewReader("key=value"))
				resp.Request = req

				m.On("Do", mock.MatchedBy(func(req *http.Request) bool {
					buf := new(bytes.Buffer)
					buf.ReadFrom(req.Body)
					return req.Method == http.MethodPost &&
						req.Header.Get("X-Custom") == "MyValue" &&
						buf.String() == "key=value"
				})).Return(resp, nil)
			},
			validate: func(t *testing.T, doc *goquery.Document) {
				assert.NotNil(t, doc)
			},
		},
		{
			name:   "Success: EUC-KR Encoding (Charset in Header)",
			method: http.MethodGet,
			url:    "http://example.com/euckr",
			setupMock: func(m *mocks.MockFetcher) {
				content := eucKrContent(`<html><body><div class="test">성공</div></body></html>`)
				resp := mocks.NewMockResponse(content, 200)
				resp.Header.Set("Content-Type", "text/html; charset=euc-kr")
				req, _ := http.NewRequest(http.MethodGet, "http://example.com/euckr", nil)
				resp.Request = req
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			validate: func(t *testing.T, doc *goquery.Document) {
				assert.Equal(t, "성공", doc.Find(".test").Text())
			},
		},
		{
			name:   "Success: Base URL Resolution (Redirect)",
			method: http.MethodGet,
			url:    "http://example.com/initial",
			setupMock: func(m *mocks.MockFetcher) {
				resp := mocks.NewMockResponse(`<html><body><a href="/link">Link</a></body></html>`, 200)
				// 리다이렉트가 발생하여 최종 URL이 변경된 상황 시뮬레이션
				finalReq, _ := http.NewRequest(http.MethodGet, "http://example.com/final", nil)
				resp.Request = finalReq
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			validate: func(t *testing.T, doc *goquery.Document) {
				// Base URL이 최종 URL로 설정되어야 함
				assert.Equal(t, "http://example.com/final", doc.Url.String())

				// 상대 경로 링크 확인
				link, exists := doc.Find("a").Attr("href")
				assert.True(t, exists)
				assert.Equal(t, "/link", link)
			},
		},

		// 2. 경고 및 호환성 (Warnings & Compatibility)
		{
			name:   "Warning: Non-Standard Content-Type (image/png)",
			method: http.MethodGet,
			url:    "http://example.com/image",
			setupMock: func(m *mocks.MockFetcher) {
				// 실제 내용은 HTML이지만 Content-Type이 이미지인 경우
				resp := mocks.NewMockResponse(`<html><body>Real HTML</body></html>`, 200)
				resp.Header.Set("Content-Type", "image/png")
				req, _ := http.NewRequest(http.MethodGet, "http://example.com/image", nil)
				resp.Request = req
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			checkLog: func(t *testing.T, hook *test.Hook) {
				found := false
				for _, entry := range hook.AllEntries() {
					if entry.Level == logrus.WarnLevel && strings.Contains(entry.Message, "비표준 Content-Type") {
						found = true
						break
					}
				}
				assert.True(t, found, "비표준 Content-Type에 대한 경고 로그가 있어야 합니다")
			},
			validate: func(t *testing.T, doc *goquery.Document) {
				assert.Equal(t, "Real HTML", doc.Find("body").Text())
			},
		},
		{
			name:   "Success: XHTML Content-Type",
			method: http.MethodGet,
			url:    "http://example.com/xhtml",
			setupMock: func(m *mocks.MockFetcher) {
				resp := mocks.NewMockResponse(`<!DOCTYPE html><html><body>XHTML</body></html>`, 200)
				resp.Header.Set("Content-Type", "application/xhtml+xml")
				req, _ := http.NewRequest(http.MethodGet, "http://example.com/xhtml", nil)
				resp.Request = req
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			validate: func(t *testing.T, doc *goquery.Document) {
				assert.Equal(t, "XHTML", doc.Find("body").Text())
			},
		},

		// 3. 에러 처리 (Error Handling)
		{
			name:   "Error: Network Failure",
			method: http.MethodGet,
			url:    "http://example.com/error",
			setupMock: func(m *mocks.MockFetcher) {
				m.On("Do", mock.Anything).Return(nil, errors.New("network error"))
			},
			wantErr:     true,
			errType:     apperrors.Unavailable,
			errContains: []string{"network error"},
		},
		{
			name:   "Error: HTTP 404 Not Found",
			method: http.MethodGet,
			url:    "http://example.com/404",
			setupMock: func(m *mocks.MockFetcher) {
				resp := mocks.NewMockResponse(`Not Found`, 404)
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			wantErr:     true,
			errType:     apperrors.ExecutionFailed,
			errContains: []string{"404"},
		},
		{
			name:   "Error: HTTP 500 Server Error",
			method: http.MethodGet,
			url:    "http://example.com/500",
			setupMock: func(m *mocks.MockFetcher) {
				resp := mocks.NewMockResponse(`Internal Error`, 500)
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			wantErr:     true,
			errType:     apperrors.Unavailable,
			errContains: []string{"500"},
		},
		{
			name:    "Error: Response Body Too Large",
			method:  http.MethodGet,
			url:     "http://example.com/large",
			options: []Option{WithMaxResponseBodySize(10)}, // 10 바이트 제한
			setupMock: func(m *mocks.MockFetcher) {
				// 제한을 초과하는 본문
				resp := mocks.NewMockResponse("This body is definitely larger than 10 bytes", 200)
				req, _ := http.NewRequest(http.MethodGet, "http://example.com/large", nil)
				resp.Request = req
				m.On("Do", mock.Anything).Return(resp, nil)
			},
			wantErr:     true,
			errType:     apperrors.InvalidInput,
			errContains: []string{"응답 본문 크기 초과"},
		},
		{
			name:    "Error: Request Body Read Failed",
			method:  http.MethodPost,
			url:     "http://example.com/fail-read",
			body:    &faultyReader{err: errors.New("read error")},
			wantErr: true,
			// request_sender.go에서 prepareBody가 에러를 반환
			errType:     apperrors.Unknown, // prepareBody error wrapping depends on implementation, usually Internal or wrapped
			errContains: []string{"read error"},
		},
		{
			name:   "Error: Context Timeout",
			method: http.MethodGet,
			url:    "http://example.com/timeout",
			ctxSetup: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), -1*time.Second) // 즉시 타임아웃
			},
			setupMock: func(m *mocks.MockFetcher) {
				// 타임아웃으로 인해 Fetcher가 호출되지 않거나, 호출되어도 context error 반환
				m.On("Do", mock.Anything).Return(nil, context.DeadlineExceeded).Maybe()
			},
			wantErr:     true,
			errType:     apperrors.Unknown, // Raw context error returned
			errContains: []string{"context deadline exceeded"},
		},
		{
			name:   "Error: Context Canceled during Request",
			method: http.MethodGet,
			url:    "http://example.com/canceled",
			setupMock: func(m *mocks.MockFetcher) {
				m.On("Do", mock.Anything).Return(nil, context.Canceled)
			},
			wantErr:     true,
			errType:     apperrors.Unknown, // Raw context error returned
			errContains: []string{"context canceled"},
		},
		{
			name:   "Error: Invalid URL Creation",
			method: http.MethodGet,
			// \x00 같은 제어문자가 포함된 URL은 net/url.Parse 에서 에러 반환 -> request 생성 실패
			url: "http://example.com/invalid\x00url",
			setupMock: func(m *mocks.MockFetcher) {
				// Do not expect Do to be called
			},
			wantErr:     true,
			errType:     apperrors.ExecutionFailed,
			errContains: []string{"요청 객체 초기화 중 오류 발생"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hook := test.NewGlobal()
			mockFetcher := &mocks.MockFetcher{}
			if tt.setupMock != nil {
				tt.setupMock(mockFetcher)
			}

			// Context 설정
			var ctx context.Context
			var cancel context.CancelFunc
			if tt.ctxSetup != nil {
				ctx, cancel = tt.ctxSetup()
			} else {
				ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			}
			defer cancel()

			// Scraper 생성 및 실행
			s := New(mockFetcher, tt.options...).(*scraper)
			doc, err := s.FetchHTML(ctx, tt.method, tt.url, tt.body, tt.header)

			// 검증
			if tt.wantErr {
				require.Error(t, err)
				if tt.errType != apperrors.Unknown {
					assert.True(t, apperrors.Is(err, tt.errType), "Expected error type %s, got %v", tt.errType, err)
				}
				for _, msg := range tt.errContains {
					assert.Contains(t, err.Error(), msg)
				}
			} else {
				assert.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, doc)
				}
			}

			if tt.checkLog != nil {
				tt.checkLog(t, hook)
			}
			mockFetcher.AssertExpectations(t)
		})
	}
}

func TestFetchHTMLDocument(t *testing.T) {
	// 헬퍼 함수 테스트: FetchHTMLDocument -> FetchHTML(GET)
	mockFetcher := &mocks.MockFetcher{}
	resp := mocks.NewMockResponse("<html></html>", 200)
	resp.Request, _ = http.NewRequest("GET", "http://example.com", nil)
	mockFetcher.On("Do", mock.Anything).Return(resp, nil)

	s := New(mockFetcher)
	doc, err := s.FetchHTMLDocument(context.Background(), "http://example.com", nil)

	assert.NoError(t, err)
	assert.NotNil(t, doc)
	mockFetcher.AssertExpectations(t)
}

func TestParseHTML_Comprehensive(t *testing.T) {
	tests := []struct {
		name        string
		input       io.Reader
		url         string
		contentType string
		options     []Option
		wantErr     bool
		errType     apperrors.ErrorType
		errContains []string
		validate    func(*testing.T, *goquery.Document)
	}{
		{
			name:        "Success: Simple UTF-8",
			input:       strings.NewReader(`<html><head><title>Hello</title></head></html>`),
			contentType: "text/html",
			validate: func(t *testing.T, doc *goquery.Document) {
				assert.Equal(t, "Hello", doc.Find("title").Text())
			},
		},
		{
			name:        "Success: EUC-KR with Meta Tag Detection",
			input:       strings.NewReader(eucKrContent(`<html><head><meta charset="euc-kr"><title>한글</title></head></html>`)),
			contentType: "text/html", // 헤더에 charset 없음, 메타 태그로 감지
			validate: func(t *testing.T, doc *goquery.Document) {
				assert.Equal(t, "한글", doc.Find("title").Text())
			},
		},
		{
			name:        "Success: UTF-8 BOM Handling",
			input:       io.MultiReader(bytes.NewReader([]byte{0xEF, 0xBB, 0xBF}), strings.NewReader(`<html><body>BOM Test</body></html>`)),
			contentType: "text/html",
			validate: func(t *testing.T, doc *goquery.Document) {
				text := doc.Find("body").Text()
				assert.True(t, strings.Contains(text, "BOM Test"))
			},
		},
		{
			name:        "Success: Unknown Encoding Fallback",
			input:       strings.NewReader(`<html><body>Unknown</body></html>`),
			contentType: "text/html; charset=unknown-xyz", // 알 수 없는 인코딩
			validate: func(t *testing.T, doc *goquery.Document) {
				// 원본 그대로 파싱 시도
				assert.Equal(t, "Unknown", doc.Find("body").Text())
			},
		},
		{
			name:    "Error: Nil Reader",
			input:   nil,
			wantErr: true,
			errType: apperrors.Internal, // Internal logic failure
		},
		{
			name: "Error: Size Limit Exceeded (HTML Specific Check)",
			input: func() io.Reader {
				return strings.NewReader(strings.Repeat("A", 20))
			}(),
			options:     []Option{WithMaxResponseBodySize(10)},
			wantErr:     true,
			errType:     apperrors.InvalidInput,
			errContains: []string{"크기 초과"},
		},
		{
			name:        "Error: Read Failed",
			input:       &faultyReader{err: errors.New("read error")},
			wantErr:     true,
			errType:     apperrors.Unavailable,
			errContains: []string{"read error"},
		},
		{
			name:        "Warning: Invalid Base URL Parsing in ParseHTML",
			input:       strings.NewReader(`<html><body>Success</body></html>`),
			url:         "://invalid-url",
			contentType: "text/html",
			validate: func(t *testing.T, doc *goquery.Document) {
				assert.Equal(t, "Success", doc.Find("body").Text())
			},
		},
		{
			name:        "Error: Context Canceled during Parse(LimitReader)",
			input:       &slowReader{}, // Read 중 지연 발생
			contentType: "text/html",
			wantErr:     true,
			errType:     apperrors.Unknown, // context error pass-through
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.options
			if len(opts) == 0 {
				opts = []Option{WithMaxResponseBodySize(1024)}
			}
			s := New(&mocks.MockFetcher{}, opts...).(*scraper)

			// slowReader용 짧은 타임아웃
			ctx := context.Background()
			if tt.name == "Error: Context Canceled during Parse(LimitReader)" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 10*time.Millisecond)
				defer cancel()
			}

			doc, err := s.ParseHTML(ctx, tt.input, tt.url, tt.contentType)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errType != apperrors.Unknown {
					assert.True(t, apperrors.Is(err, tt.errType), "Expected error type %s, got %v", tt.errType, err)
				}
				for _, msg := range tt.errContains {
					assert.Contains(t, err.Error(), msg)
				}
			} else {
				assert.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, doc)
				}
			}
		})
	}
}

func TestVerifyHTMLContentType(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		contentType string
		wantLog     bool
	}{
		{"Standard HTML", 200, "text/html", false},
		{"Standard HTML Charset", 200, "text/html; charset=utf-8", false},
		{"XHTML", 200, "application/xhtml+xml", false},
		{"Warning: JSON", 200, "application/json", true},
		{"Warning: Plain", 200, "text/plain", true},
		{"Warning: Empty", 200, "", true},
		{"Ignore: 204 No Content", 204, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hook := test.NewGlobal()
			s := New(&mocks.MockFetcher{}).(*scraper)
			logger := applog.WithContext(context.Background())

			resp := &http.Response{
				StatusCode: tt.statusCode,
				Header:     http.Header{"Content-Type": []string{tt.contentType}},
			}

			// 에러가 반환되지는 않지만(nil), 로그를 확인
			_ = s.verifyHTMLContentType(resp, "http://test.url", logger)

			found := false
			for _, entry := range hook.AllEntries() {
				if entry.Level == logrus.WarnLevel && strings.Contains(entry.Message, "비표준 Content-Type") {
					found = true
				}
			}
			assert.Equal(t, tt.wantLog, found)
		})
	}
}

func TestParseHTML_ContextCancel(t *testing.T) {
	// 대용량 데이터로 파싱 시간을 늘리고 중간에 취소
	hugeHTML := "<html><body>" + strings.Repeat("<div>test</div>", 100000) + "</body></html>"
	reader := strings.NewReader(hugeHTML)

	// 즉시 취소되는 컨텍스트
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := New(&mocks.MockFetcher{}).(*scraper)

	_, err := s.ParseHTML(ctx, reader, "", "")
	require.Error(t, err)

	// 취소 에러 확인 (Unavailable 혹은 Canceled)
	assert.True(t, apperrors.Is(err, apperrors.Unavailable) || errors.Is(err, context.Canceled),
		"Expected context cancellation error, got %v", err)
}
