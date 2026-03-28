package fetcher

import (
	"net/http"
	"time"
)

// Option HTTPFetcher의 설정을 변경하기 위한 함수 타입입니다.
//
// Functional Options 패턴을 사용하여 선택적 매개변수를 유연하게 설정할 수 있습니다.
// 각 Option 함수는 HTTPFetcher의 특정 필드를 수정합니다.
type Option func(*HTTPFetcher)

// WithProxy 프록시 URL을 설정합니다.
//
// 모든 HTTP/HTTPS 요청이 지정된 프록시 서버를 통해 전송됩니다.
//
// 매개변수:
//   - proxyURL: 프록시 URL
//     · URL: 지정된 프록시 서버 사용 (예: "http://proxy:8080")
//     · NoProxy(또는 "DIRECT") 또는 빈 문자열(""): 프록시 비활성화 (환경 변수 무시, 직접 연결)
func WithProxy(proxyURL string) Option {
	return func(h *HTTPFetcher) {
		h.proxyURL = &proxyURL
	}
}

// WithMaxRedirects HTTP 클라이언트의 최대 리다이렉트 횟수를 설정합니다.
//
// 기본적으로 Go HTTP 클라이언트는 최대 10번까지 리다이렉트를 따라갑니다.
// 이 옵션으로 제한을 변경할 수 있으며, 리다이렉트 시 Referer 헤더를 자동으로 설정합니다.
//
// 매개변수:
//   - max: 최대 리다이렉트 횟수
//     · 0: 리다이렉트 허용 안 함
//     · 양수: 지정된 횟수만큼 리다이렉트 허용
//     · 음수: 기본값으로 보정
func WithMaxRedirects(max int) Option {
	// 최대 리다이렉트 횟수를 정규화합니다.
	max = normalizeMaxRedirects(max)

	return func(h *HTTPFetcher) {
		h.client.CheckRedirect = newCheckRedirectPolicy(max)
	}
}

// WithUserAgent 기본 User-Agent를 설정합니다.
//
// 이 옵션으로 설정한 User-Agent는 요청 헤더에 User-Agent가 없을 때만 자동으로 추가됩니다.
// 요청 헤더에 이미 User-Agent가 설정되어 있으면 그 값이 우선적으로 사용됩니다.
//
// 매개변수:
//   - ua: User-Agent 문자열 (예: "MyBot/1.0", "Mozilla/5.0 ...")
func WithUserAgent(ua string) Option {
	return func(h *HTTPFetcher) {
		h.defaultUA = ua
	}
}

// WithMaxIdleConns 전체 유휴 연결의 최대 개수를 설정합니다.
//
// 매개변수:
//   - max: 전체 유휴 연결의 최대 개수
//     · 0: 무제한
//     · 양수: 지정된 개수로 제한
//     · 음수: 기본값으로 보정
func WithMaxIdleConns(max int) Option {
	// 전체 유휴 연결의 최대 개수를 정규화합니다.
	max = normalizeMaxIdleConns(max)

	return func(h *HTTPFetcher) {
		h.maxIdleConns = &max
	}
}

// WithMaxIdleConnsPerHost 호스트당 유휴 연결의 최대 개수를 설정합니다.
//
// 매개변수:
//   - max: 호스트당 유휴 연결의 최대 개수
//     · 0: net/http가 기본값 2로 해석
//     · 양수: 지정된 개수로 제한
//     · 음수: 기본값으로 보정
func WithMaxIdleConnsPerHost(max int) Option {
	// 호스트당 유휴 연결의 최대 개수를 정규화합니다.
	max = normalizeMaxIdleConnsPerHost(max)

	return func(h *HTTPFetcher) {
		h.maxIdleConnsPerHost = &max
	}
}

// WithMaxConnsPerHost 호스트당 최대 연결 개수를 설정합니다.
//
// 매개변수:
//   - max: 호스트당 최대 연결 개수
//     · 0: 무제한
//     · 양수: 지정된 개수로 제한
//     · 음수: 기본값으로 보정
func WithMaxConnsPerHost(max int) Option {
	// 호스트당 최대 연결 개수를 정규화합니다.
	max = normalizeMaxConnsPerHost(max)

	return func(h *HTTPFetcher) {
		h.maxConnsPerHost = &max
	}
}

// WithTimeout HTTP 요청 전체에 대한 타임아웃을 설정합니다.
//
// 이 타임아웃은 요청 시작부터 응답 완료까지의 전체 시간을 제한합니다.
// DNS 조회, 연결, TLS 핸드셰이크, 응답 헤더, 응답 본문 읽기 등 모든 단계를 포함합니다.
//
// 매개변수:
//   - timeout: 요청 전체에 대한 타임아웃 (예: 30*time.Second)
//     · 0: 타임아웃 없음 (무한 대기)
//     · 양수: 지정된 시간으로 제한
//     · 음수: 기본값으로 보정
func WithTimeout(timeout time.Duration) Option {
	// HTTP 요청 전체에 대한 타임아웃을 정규화합니다.
	timeout = normalizeTimeout(timeout)

	return func(h *HTTPFetcher) {
		h.client.Timeout = timeout
	}
}

// WithTLSHandshakeTimeout TLS 핸드셰이크 타임아웃을 설정합니다.
//
// HTTPS 연결 시 SSL/TLS 협상에 허용되는 최대 시간입니다.
// 네트워크가 느리거나 서버 부하가 높을 때 타임아웃이 발생할 수 있습니다.
//
// 매개변수:
//   - timeout: TLS 핸드셰이크 타임아웃
//     · 0: 타임아웃 없음 (무한 대기)
//     · 양수: 지정된 시간으로 제한
//     · 음수: 기본값으로 보정
func WithTLSHandshakeTimeout(timeout time.Duration) Option {
	// TLS 핸드셰이크 타임아웃을 정규화합니다.
	timeout = normalizeTLSHandshakeTimeout(timeout)

	return func(h *HTTPFetcher) {
		h.tlsHandshakeTimeout = &timeout
	}
}

// WithResponseHeaderTimeout HTTP 응답 헤더 대기 타임아웃을 설정합니다.
//
// 이 타임아웃은 요청을 보낸 후 응답 헤더를 받을 때까지의 시간을 제한합니다.
// 응답 본문 읽기 시간은 포함되지 않으므로, 전체 타임아웃(WithTimeout)과 함께 사용하세요.
//
// 매개변수:
//   - timeout: 응답 헤더 대기 타임아웃
//     · 0: 타임아웃 없음 (무한 대기)
//     · 양수: 지정된 시간으로 제한
//     · 음수: 0으로 보정
func WithResponseHeaderTimeout(timeout time.Duration) Option {
	// HTTP 응답 헤더 대기 타임아웃을 정규화합니다.
	timeout = normalizeResponseHeaderTimeout(timeout)

	return func(h *HTTPFetcher) {
		h.responseHeaderTimeout = &timeout
	}
}

// WithIdleConnTimeout 유휴 연결 타임아웃을 설정합니다.
//
// 사용되지 않는 연결을 유지할 최대 시간입니다.
// 이 시간이 지나면 연결이 자동으로 닫히고 풀에서 제거됩니다.
//
// 매개변수:
//   - timeout: 유휴 연결 타임아웃
//     · 0: 제한 없음 (연결이 무기한 유지)
//     · 양수: 지정된 시간 후 연결 종료
//     · 음수: 기본값으로 보정
func WithIdleConnTimeout(timeout time.Duration) Option {
	// 유휴 연결 타임아웃을 정규화합니다.
	timeout = normalizeIdleConnTimeout(timeout)

	return func(h *HTTPFetcher) {
		h.idleConnTimeout = &timeout
	}
}

// WithDisableTransportCaching Transport 캐싱 사용 여부를 설정합니다.
//
// 기본적으로 동일한 설정의 요청들은 Transport를 공유하여 성능을 최적화합니다.
// 캐싱을 비활성화하면 매번 새로운 Transport를 생성하여 완전히 격리된 환경을 제공합니다.
//
// 매개변수:
//   - disable: Transport 캐싱 비활성화 여부
//     · false (기본값): 캐시 사용 (성능 최적화)
//     · true: 캐시 비활성화 (격리된 환경, 테스트에 유용)
//
// 주의사항:
//   - 캐시 비활성화 시 성능 저하 및 메모리 사용량 증가 가능
func WithDisableTransportCaching(disable bool) Option {
	return func(h *HTTPFetcher) {
		h.disableTransportCaching = disable
	}
}

// WithTransport HTTP 클라이언트의 Transport를 직접 설정합니다.
//
// 이 옵션은 고급 사용자를 위한 것으로, 표준 옵션으로 제공되지 않는 특수한 Transport 설정이 필요할 때 사용합니다.
// 예를 들어, 커스텀 Dialer, 특수한 TLS 설정, 또는 테스트용 모의(Mock) Transport를 주입할 수 있습니다.
//
// ⚠️ 중요: 커넥션 풀링 성능 저하 주의
//
// WithTransport와 다른 Transport 설정 옵션(WithProxy, WithMaxIdleConns, WithTLSHandshakeTimeout 등)을
// 함께 사용하면 매번 새로운 커넥션 풀이 생성되어 성능이 크게 저하됩니다.
//
// 권장 사용법:
//   - Transport 설정이 필요하면 WithTransport 없이 옵션만 사용 (내부 캐싱 활용)
//   - WithTransport 사용 시 모든 설정을 완료한 Transport를 주입
//
// 참고: WithTimeout은 http.Client 설정이므로 이 제약에 해당하지 않습니다.
//
// 매개변수:
//   - transport: 사용할 http.RoundTripper 구현체 (일반적으로 *http.Transport 또는 테스트용 Mock)
//
// 주의사항:
//   - *http.Transport가 아닌 RoundTripper를 제공하면서 Transport 관련 옵션을 함께 사용하면,
//     Do() 호출 시 ErrUnsupportedTransport 에러가 반환됩니다.
//   - 옵션 없이 사용하면 제공된 RoundTripper를 그대로 사용하며 정상 동작합니다.
//   - Transport 캐싱이 비활성화되므로 성능 최적화 효과가 감소할 수 있습니다.
//   - 일반적인 경우에는 표준 옵션(WithProxy, WithMaxIdleConns 등)을 사용하는 것을 권장합니다.
//
// 타입별 동작 방식:
//
//  1. *http.Transport 타입 (일반적인 경우):
//     - 원본을 복제한 후, 사용자가 설정한 Transport 관련 옵션들을 선택적으로 덮어씁니다.
//     - WithProxy, WithMaxIdleConns 등의 옵션이 정상적으로 적용됩니다.
//
//  2. 다른 RoundTripper 타입 (Mock 등):
//     a) Transport 관련 옵션을 사용하지 않은 경우:
//     제공된 객체를 그대로 사용하며 정상 동작합니다.
//     이 경우 needsCustomTransport()가 false를 반환하여 setupTransport()가 조기 종료됩니다.
//
//     b) Transport 관련 옵션(WithProxy, WithMaxIdleConns 등)을 함께 사용한 경우:
//     NewHTTPFetcher()는 성공하지만, Do() 호출 시 ErrUnsupportedTransport 에러를 반환합니다.
//     이는 *http.Transport가 아닌 타입은 내부 설정(프록시, 타임아웃 등)을 변경할 수 없기 때문입니다.
//     setupTransport()에서 타입 검사 후 에러를 initErr에 저장하며, Do() 실행 시 이 에러가 반환됩니다.
//
// 캐싱 비활성화 이유:
//   - 외부에서 주입된 Transport는 소유권과 생명주기를 fetcher가 제어할 수 없습니다.
//   - 다른 곳에서도 동일한 Transport를 사용 중일 수 있어, 캐시 관리 로직이 리소스를 정리하면 예상치 못한 부작용이 발생할 수 있습니다.
//   - 따라서 격리 모드로 동작하여 사용자가 직접 Transport의 생명주기를 관리하도록 합니다.
func WithTransport(transport http.RoundTripper) Option {
	return func(h *HTTPFetcher) {
		h.client.Transport = transport

		// 외부에서 주입된 Transport는 소유권이 불명확하므로 캐시를 비활성화하여 격리 모드로 동작합니다.
		h.disableTransportCaching = true

		// 외부에서 주입된 Transport는 Fetcher가 소유하지 않으므로 Close() 시 닫지 않습니다.
		h.ownsTransport = false
	}
}

// WithCookieJar HTTP 클라이언트에 쿠키 관리자(CookieJar)를 설정합니다.
//
// CookieJar를 설정하면 HTTP 응답의 Set-Cookie 헤더를 자동으로 저장하고,
// 동일한 도메인에 대한 후속 요청에 쿠키를 자동으로 포함합니다.
//
// 매개변수:
//   - jar: http.CookieJar 구현체 (예: cookiejar.New(nil))
//
// 사용 예시:
//
//	jar, _ := cookiejar.New(nil)
//	fetcher := NewHTTPFetcher(WithCookieJar(jar))
func WithCookieJar(jar http.CookieJar) Option {
	return func(h *HTTPFetcher) {
		h.client.Jar = jar
	}
}
