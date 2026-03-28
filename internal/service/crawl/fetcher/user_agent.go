package fetcher

import (
	"math/rand/v2"
	"net/http"
)

// defaultUserAgents 웹 스크래핑 시 차단을 회피하기 위해 사용되는 일반적인 User-Agent 목록입니다.
var defaultUserAgents = []string{
	// Chrome 120 - Windows 10/11 (64비트)
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	// Chrome 120 - macOS Catalina (10.15.7)
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	// Chrome 120 - Linux (64비트)
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	// Firefox 121 - Windows 10/11 (64비트)
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
	// Firefox 121 - macOS Catalina (10.15)
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:121.0) Gecko/20100101 Firefox/121.0",
	// Safari 17.2 - macOS Catalina (10.15.7)
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Safari/605.1.15",
}

// UserAgentFetcher HTTP 요청에 User-Agent를 주입하는 미들웨어입니다.
//
// 주요 기능:
//   - 요청에 User-Agent가 없을 경우에만 랜덤으로 선택하여 주입합니다.
//   - 요청에 User-Agent가 있을 경우에는 수정하지 않고 그대로 전달합니다.
//   - User-Agent를 랜덤으로 선택하여 웹 스크래핑 시 차단을 회피할 수 있습니다.
type UserAgentFetcher struct {
	delegate Fetcher

	// userAgents 랜덤으로 선택할 User-Agent 문자열 목록입니다.
	userAgents []string
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ Fetcher = (*UserAgentFetcher)(nil)

// NewUserAgentFetcher 새로운 UserAgentFetcher 인스턴스를 생성합니다.
func NewUserAgentFetcher(delegate Fetcher, userAgents []string) *UserAgentFetcher {
	return &UserAgentFetcher{
		delegate:   delegate,
		userAgents: userAgents,
	}
}

// Do HTTP 요청을 수행하며, 필요한 경우 User-Agent를 랜덤으로 선택하여 주입합니다.
//
// 매개변수:
//   - req: 처리할 HTTP 요청
//
// 반환값:
//   - HTTP 응답 객체 (성공 시)
//   - 에러 (요청 처리 중 발생한 에러)
//
// 주의사항:
//   - 이 메서드는 원본 요청 객체를 수정하지 않습니다.
//   - User-Agent 주입이 필요한 경우 req.Clone()을 사용하여 복제본을 생성합니다.
//   - 재시도 시에도 동일한 User-Agent를 유지하려면 이 미들웨어를 RetryFetcher보다 상위에 배치해야 합니다.
func (f *UserAgentFetcher) Do(req *http.Request) (*http.Response, error) {
	// 1. 이미 User-Agent가 설정되어 있다면 수정 없이 그대로 전달한다.
	if req.Header.Get("User-Agent") != "" {
		return f.delegate.Do(req)
	}

	// 2. 사용할 User-Agent 목록 결정
	uas := f.userAgents
	if len(uas) == 0 {
		uas = defaultUserAgents
	}

	// 3. 목록에서 랜덤으로 하나 선택한다.
	ua := uas[rand.IntN(len(uas))]

	// 4. User-Agent를 주입한다.
	// 원본 요청을 보호하기 위해 복제한다.
	// 이렇게 하면 호출자의 원본 요청 객체가 변경되지 않습니다.
	clonedReq := req.Clone(req.Context())
	clonedReq.Header.Set("User-Agent", ua)

	return f.delegate.Do(clonedReq)
}

func (f *UserAgentFetcher) Close() error {
	return f.delegate.Close()
}
