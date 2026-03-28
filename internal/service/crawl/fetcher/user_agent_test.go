package fetcher_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestUserAgentFetcher_Do_Scenarios Fetcher.Do 메서드의 핵심 동작 시나리오를 검증합니다.
// 테이블 기반 테스트를 통해 다양한 상황을 체계적으로 검증합니다.
func TestUserAgentFetcher_Do_Scenarios(t *testing.T) {
	t.Parallel()

	customUAs := []string{"Custom/1.0", "Custom/2.0"}

	type reqValidator func(*testing.T, *http.Request)

	tests := []struct {
		name        string
		userAgents  []string
		existingUA  string
		setupMock   func(*mocks.MockFetcher)
		validator   reqValidator
		expectError bool
		description string
	}{
		{
			name:        "Preserve Existing UA",
			userAgents:  customUAs,
			existingUA:  "Original/1.0",
			description: "이미 User-Agent 헤더가 존재하면 랜덤 주입 옵션이 켜져 있어도 원본을 그대로 유지해야 합니다.",
			setupMock: func(m *mocks.MockFetcher) {
				m.On("Do", mock.MatchedBy(func(req *http.Request) bool {
					return req.Header.Get("User-Agent") == "Original/1.0"
				})).Return(&http.Response{StatusCode: 200}, nil)
			},
		},
		{
			name:        "Inject Custom UA",
			userAgents:  customUAs,
			existingUA:  "",
			description: "User-Agent가 없고 커스텀 목록이 제공되면 커스텀 목록 중 하나를 선택해 주입해야 합니다.",
			setupMock: func(m *mocks.MockFetcher) {
				m.On("Do", mock.MatchedBy(func(req *http.Request) bool {
					ua := req.Header.Get("User-Agent")
					for _, allowed := range customUAs {
						if ua == allowed {
							return true
						}
					}
					return false
				})).Return(&http.Response{StatusCode: 200}, nil)
			},
		},
		{
			name:        "Inject Default UA",
			userAgents:  nil, // 커스텀 목록 없음 -> 기본값 사용
			existingUA:  "",
			description: "커스텀 목록이 비어 있으면 패키지 내장 Default 목록을 사용해야 합니다.",
			setupMock: func(m *mocks.MockFetcher) {
				m.On("Do", mock.MatchedBy(func(req *http.Request) bool {
					ua := req.Header.Get("User-Agent")
					// Default 목록(Chrome, Firefox 등)은 일반적으로 "Mozilla"로 시작합니다.
					return ua != "" && strings.HasPrefix(ua, "Mozilla")
				})).Return(&http.Response{StatusCode: 200}, nil)
			},
		},
		{
			name:        "Delegate Error Propagation",
			userAgents:  customUAs,
			existingUA:  "",
			description: "내부 Fetcher에서 발생한 에러는 가감 없이 그대로 호출자에게 반환되어야 합니다.",
			setupMock: func(m *mocks.MockFetcher) {
				m.On("Do", mock.Anything).Return(nil, errors.New("network error"))
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockFetcher := new(mocks.MockFetcher)
			if tt.setupMock != nil {
				tt.setupMock(mockFetcher)
			}

			f := fetcher.NewUserAgentFetcher(mockFetcher, tt.userAgents)
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", nil)
			if tt.existingUA != "" {
				req.Header.Set("User-Agent", tt.existingUA)
			}

			_, err := f.Do(req)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			mockFetcher.AssertExpectations(t)
		})
	}
}

// TestUserAgentFetcher_Immutability 요청 객체의 불변성(Immutability)을 검증합니다.
// UserAgentFetcher는 원본 요청을 변경하지 않고 반드시 복제본(Clone)을 수정해야 합니다.
func TestUserAgentFetcher_Immutability(t *testing.T) {
	t.Parallel()

	mockFetcher := new(mocks.MockFetcher)
	// Mock은 변경된 UA를 가진 복제본을 전달받아야 함
	mockFetcher.On("Do", mock.MatchedBy(func(req *http.Request) bool {
		return req.Header.Get("User-Agent") != ""
	})).Return(&http.Response{StatusCode: 200}, nil)

	f := fetcher.NewUserAgentFetcher(mockFetcher, []string{"MyUA"})

	// 원본 요청 생성
	originalReq, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.Empty(t, originalReq.Header.Get("User-Agent"), "테스트 초기값 검증 실패")

	// 실행
	_, err := f.Do(originalReq)
	require.NoError(t, err)

	// 검증: 원본 요청 객체는 여전히 UA가 없어야 함 (수정되지 않음)
	assert.Empty(t, originalReq.Header.Get("User-Agent"), "Do 메서드는 원본 요청 객체를 수정해서는 안 됩니다 (req.Clone 사용 필수)")

	mockFetcher.AssertExpectations(t)
}

// TestUserAgentFetcher_Close Close 메서드의 위임(Delegation) 동작을 검증합니다.
func TestUserAgentFetcher_Close(t *testing.T) {
	t.Parallel()

	mockFetcher := new(mocks.MockFetcher)
	expectedErr := errors.New("close error")

	// Close 호출 시 내부 Fetcher의 Close도 호출되어야 함
	mockFetcher.On("Close").Return(expectedErr)

	f := fetcher.NewUserAgentFetcher(mockFetcher, nil)
	err := f.Close()

	assert.Equal(t, expectedErr, err, "Close 호출 결과가 올바르게 반환되어야 합니다")
	mockFetcher.AssertExpectations(t)
}

// TestUserAgentFetcher_RandomDistribution 랜덤 선택의 분포를 통계적으로 검증합니다.
func TestUserAgentFetcher_RandomDistribution(t *testing.T) {
	t.Parallel()

	mockFetcher := new(mocks.MockFetcher)
	customUAs := []string{"UA_A", "UA_B", "UA_C", "UA_D"}

	var mu sync.Mutex
	counts := make(map[string]int)

	// 호출될 때마다 UA를 카운트
	mockFetcher.On("Do", mock.Anything).Run(func(args mock.Arguments) {
		req := args.Get(0).(*http.Request)
		ua := req.Header.Get("User-Agent")

		mu.Lock()
		counts[ua]++
		mu.Unlock()
	}).Return(&http.Response{StatusCode: 200}, nil)

	f := fetcher.NewUserAgentFetcher(mockFetcher, customUAs)

	// 충분한 표본 수집 (4개 옵션 * 100회 = 400회)
	// 반복 횟수가 적으면 확률적 통계가 빗나갈 수 있으므로 넉넉하게 설정
	iterations := 400
	var wg sync.WaitGroup
	wg.Add(iterations)

	for i := 0; i < iterations; i++ {
		go func() {
			defer wg.Done()
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", nil)
			_, _ = f.Do(req)
		}()
	}
	wg.Wait()

	// 검증 1: 모든 후보 UA가 최소 1번 이상 선택되었는지 확인
	assert.Equal(t, len(customUAs), len(counts), "설정된 모든 User-Agent가 최소 한 번은 선택되어야 합니다.")

	// 검증 2: 분포가 고른지 확인 (극단적인 한쪽 쏠림 방지)
	// 기대값: 100회. 허용 오차를 넉넉하게 두어 false positive 방지 (최소 10회 이상)
	minThreshold := 10
	for _, ua := range customUAs {
		count := counts[ua]
		assert.GreaterOrEqual(t, count, minThreshold,
			"User-Agent %q의 선택 빈도(%d)가 너무 낮습니다. 랜덤 로직을 점검하세요.", ua, count)
	}
}

// BenchmarkUserAgentFetcher_Do UserAgentFetcher의 성능을 측정합니다.
// 요청 복제(Clone)와 헤더 주입의 오버헤드를 확인합니다.
func BenchmarkUserAgentFetcher_Do(b *testing.B) {
	mockFetcher := new(mocks.MockFetcher)
	mockFetcher.On("Do", mock.Anything).Return(&http.Response{StatusCode: 200}, nil)

	// 긴 UA 목록을 사용하여 random 선택 비용도 포함
	userAgents := make([]string, 100)
	for i := 0; i < 100; i++ {
		userAgents[i] = "Benchmark-User-Agent-" + string(rune(i))
	}

	f := fetcher.NewUserAgentFetcher(mockFetcher, userAgents)
	req, _ := http.NewRequest(http.MethodGet, "http://benchmark.com", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = f.Do(req)
	}
}

// BenchmarkUserAgentFetcher_Do_Parallel UserAgentFetcher의 병렬 처리 성능을 측정합니다.
// 락 경합(UserAgentFetcher는 락이 없으므로 rand 소스의 경합)을 확인합니다.
func BenchmarkUserAgentFetcher_Do_Parallel(b *testing.B) {
	mockFetcher := new(mocks.MockFetcher)
	mockFetcher.On("Do", mock.Anything).Return(&http.Response{StatusCode: 200}, nil)

	userAgents := []string{"UA1", "UA2", "UA3"}
	f := fetcher.NewUserAgentFetcher(mockFetcher, userAgents)
	req, _ := http.NewRequest(http.MethodGet, "http://benchmark.com", nil)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = f.Do(req)
		}
	})
}
