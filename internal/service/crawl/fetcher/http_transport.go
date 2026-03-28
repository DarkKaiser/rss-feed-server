package fetcher

import (
	"container/list"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	applog "github.com/darkkaiser/notify-server/pkg/log"
)

var (
	// defaultTransport 전역 Transport입니다.
	//
	// 목적:
	//   - 싱글톤(Singleton) 패턴으로 동작하여 모든 기본 Fetcher가 공유합니다.
	//   - 프록시나 특수 설정이 필요 없는 일반적인 HTTP 요청에 사용됩니다.
	//   - 연결 풀을 공유하여 TCP 핸드셰이크 비용을 최소화합니다.
	//
	// 성능 최적화:
	//   - HTTP Keep-Alive로 연결 재사용
	//   - 연결 풀링으로 핸드셰이크 오버헤드 감소
	//   - 여러 Fetcher가 동일한 연결 풀 공유
	defaultTransport = &http.Transport{
		// 프록시 설정
		// 환경 변수(HTTP_PROXY, HTTPS_PROXY)에서 프록시 서버 주소를 자동으로 읽어옵니다.
		// 환경 변수가 설정되어 있지 않으면 프록시를 거치지 않고 직접 연결합니다.
		Proxy: http.ProxyFromEnvironment,

		// TCP 연결 수립: 30초 타임아웃, Keep-Alive로 연결 재사용
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,

		// 전체 유휴 연결의 최대 개수 (기본값: 100)
		MaxIdleConns: defaultMaxIdleConns,

		// TLS 핸드셰이크 타임아웃 (기본값: 10초)
		TLSHandshakeTimeout: defaultTLSHandshakeTimeout,

		// 유휴 연결 타임아웃 (기본값: 90초)
		IdleConnTimeout: defaultIdleConnTimeout,
	}

	// transportCache 설정별로 Transport를 캐싱하는 저장소입니다.
	//
	// 목적:
	//   - 동일한 설정을 가진 Fetcher들이 Transport를 공유하여 리소스를 절약합니다.
	//   - TCP 연결 풀을 재사용하여 핸드셰이크 비용을 줄이고 성능을 향상시킵니다.
	//
	// 캐시 구조:
	//   - 키: transportCacheKey (프록시 URL, 타임아웃, 연결 풀 설정의 조합)
	//   - 값: *list.Element (transportCacheLRU의 노드, 실제 데이터는 transportCacheEntry)
	//
	// 관리 정책:
	//   - LRU(Least Recently Used): 오래 사용되지 않은 항목부터 제거
	//   - 최대 크기: defaultMaxTransportCacheSize (기본 100개)
	//   - 스마트 퇴출: 프록시 설정 항목을 우선 제거하여 일반 연결 성능 보호
	//
	// 성능 최적화:
	//   - Lazy LRU Update: 10회 접근마다 한 번씩만 LRU 갱신 (Lock 경합 90% 감소)
	//   - Double-Check Locking: 경합 상황에서 중복 생성 방지
	transportCache = make(map[transportCacheKey]*list.Element)

	// transportCacheLRU Transport 캐시의 LRU(Least Recently Used) 순서를 관리하는 이중 연결 리스트입니다.
	//
	// 역할:
	//   - 캐시 항목의 사용 빈도를 추적하여 퇴출(Eviction) 대상을 결정합니다.
	//   - 최근 사용된 항목일수록 리스트 앞쪽에 위치하여 캐시에 오래 유지됩니다.
	//
	// 동작 방식:
	//   - Front (맨 앞): 가장 최근에 사용된 항목 (MRU - Most Recently Used)
	//   - Back (맨 뒤): 가장 오래전에 사용된 항목 (LRU - Least Recently Used)
	//   - 캐시가 가득 차면 Back부터 제거하여 공간 확보
	//
	// transportCache와의 관계:
	//   - transportCache: 빠른 조회를 위한 해시맵 (키 → 리스트 노드)
	//   - transportCacheLRU: 퇴출 순서 관리를 위한 연결 리스트 (사용 순서 추적)
	transportCacheLRU = list.New()

	// transportCacheMu transportCache와 transportCacheLRU의 동시성을 제어하는 뮤텍스입니다.
	transportCacheMu sync.RWMutex
)

// transportConfig Transport 생성 시 적용할 설정을 전달하는 구조체입니다.
//
// 필드별 설명:
//
//  1. 프록시 설정 (proxyURL *string)
//     - nil: 기본 설정을 따릅니다.
//     └ · 기본 Transport(전역/공유): 환경 변수(HTTP_PROXY, HTTPS_PROXY)를 사용합니다.
//     └ · 외부 Transport(주입된 경우): 기존에 설정된 Proxy 정책을 그대로 유지합니다.
//     - URL: 지정된 프록시 서버 사용 (예: "http://proxy:8080")
//     - NoProxy(또는 "DIRECT") 또는 빈 문자열(""): 프록시 비활성화 (환경 변수 무시, 직접 연결)
//
//  2. 연결 풀 관리 (maxIdleConns, maxIdleConnsPerHost, maxConnsPerHost *int)
//     - nil: 시스템 기본값
//     - 0: 무제한 (단, maxIdleConnsPerHost는 Go 기본값 2)
//     - 양수: 지정된 개수로 제한
//
//  3. 네트워크 타임아웃 (tlsHandshakeTimeout, responseHeaderTimeout, idleConnTimeout *time.Duration)
//     - nil: 시스템 기본값
//     - 0: 타임아웃 없음 (무한 대기, 주의 필요)
//     - 양수: 지정된 시간으로 제한
//
// 변환 및 정규화:
//
//	이 구조체는 ToCacheKey() 메서드를 통해 transportCacheKey로 변환됩니다.
//	변환 과정에서 다음과 같은 정규화가 수행됩니다:
//
//	  1. nil 값은 각 필드의 시스템 기본값으로 채워집니다.
//	  2. 포인터는 역참조되어 값 타입으로 변환됩니다.
//	  3. 동일한 의도를 가진 설정들이 같은 캐시 키를 생성하도록 보장합니다.
type transportConfig struct {
	// 프록시 설정
	proxyURL *string

	// 연결 풀 관리
	maxIdleConns        *int
	maxIdleConnsPerHost *int
	maxConnsPerHost     *int

	// 네트워크 타임아웃
	tlsHandshakeTimeout   *time.Duration
	responseHeaderTimeout *time.Duration
	idleConnTimeout       *time.Duration
}

// ToCacheKey 포인터 기반 설정을 값 기반 캐시 키로 변환(정규화)합니다.
//
// 정규화 규칙:
//
//  1. nil 포인터: 각 필드의 시스템 기본값으로 변환
//     - 예: maxIdleConns가 nil → 100으로 설정
//     - 예: tlsHandshakeTimeout이 nil → 10초로 설정
//
//  2. 값이 있는 포인터: 역참조하여 실제 값 사용
//     - 예: maxIdleConns가 &200 → 200 사용
//
//  3. 특수 케이스 (nil일 때 0 또는 빈 문자열로 정규화):
//     - proxyURL: 빈 문자열로 정규화 (환경 변수 또는 기본 설정 사용)
//     - maxIdleConnsPerHost: 0으로 정규화 (Go 기본값 2 사용)
//     - maxConnsPerHost: 0으로 정규화 (무제한)
//     - responseHeaderTimeout: 0으로 정규화 (무제한)
//
// 반환값:
//
//	transportCacheKey: 정규화된 캐시 키 (모든 필드가 값 타입으로 변환됨)
func (c transportConfig) ToCacheKey() transportCacheKey {
	key := transportCacheKey{}

	// 1. 프록시 설정
	if c.proxyURL != nil {
		if *c.proxyURL == NoProxy || *c.proxyURL == "" {
			// NoProxy 또는 빈 문자열("")은 모두 "프록시 비활성화"를 의미합니다.
			// 캐시 키에서는 NoProxy 상수로 통일하여 동일한 설정으로 인식되도록 합니다.
			key.proxyURL = NoProxy
		} else {
			key.proxyURL = *c.proxyURL
		}
	} else {
		// nil인 경우: 빈 문자열("")로 저장되며, 환경 변수 또는 기본 설정을 따릅니다.
	}

	// 2. 연결 풀 관리
	if c.maxIdleConns != nil {
		key.maxIdleConns = *c.maxIdleConns
	} else {
		// nil인 경우: 시스템 기본값(100)을 사용합니다.
		key.maxIdleConns = defaultMaxIdleConns
	}

	if c.maxIdleConnsPerHost != nil {
		key.maxIdleConnsPerHost = *c.maxIdleConnsPerHost
	} else {
		// nil인 경우: 0으로 저장하여 Go의 기본값(2)을 사용합니다.
	}

	if c.maxConnsPerHost != nil {
		key.maxConnsPerHost = *c.maxConnsPerHost
	} else {
		// nil인 경우: 0으로 저장하여 무제한 연결을 허용합니다.
	}

	// 3. 네트워크 타임아웃
	if c.tlsHandshakeTimeout != nil {
		key.tlsHandshakeTimeout = *c.tlsHandshakeTimeout
	} else {
		// nil인 경우: 시스템 기본값(10초)을 사용합니다.
		key.tlsHandshakeTimeout = defaultTLSHandshakeTimeout
	}

	if c.responseHeaderTimeout != nil {
		key.responseHeaderTimeout = *c.responseHeaderTimeout
	} else {
		// nil인 경우: 0으로 저장하여 타임아웃 없음(무한 대기)을 의미합니다.
	}

	if c.idleConnTimeout != nil {
		key.idleConnTimeout = *c.idleConnTimeout
	} else {
		// nil인 경우: 시스템 기본값(90초)을 사용합니다.
		key.idleConnTimeout = defaultIdleConnTimeout
	}

	return key
}

// transportCacheKey Transport 캐시의 식별자입니다.
//
// 목적:
//   - 동일한 설정을 가진 Fetcher들이 같은 Transport를 공유할 수 있도록 합니다.
//   - 모든 필드가 일치하면 기존 Transport를 재사용하여 리소스를 절약합니다.
//
// transportConfig와의 관계:
//   - transportConfig.ToCacheKey() 메서드를 통해 생성됩니다.
//   - transportConfig의 포인터 필드들이 값 타입으로 정규화되어 이 구조체에 저장됩니다.
//   - 필드 순서는 transportConfig와 동일하게 유지됩니다 (Proxy -> Pool -> Timeouts).
//
// 캐시 키 비교:
//   - Go의 구조체 비교 연산자(==)를 사용하여 모든 필드를 자동으로 비교합니다.
//   - 하나라도 다르면 별도의 Transport가 생성됩니다.
type transportCacheKey struct {
	// 프록시 설정
	proxyURL string

	// 연결 풀 관리
	maxIdleConns        int
	maxIdleConnsPerHost int
	maxConnsPerHost     int

	// 네트워크 타임아웃
	tlsHandshakeTimeout   time.Duration
	responseHeaderTimeout time.Duration
	idleConnTimeout       time.Duration
}

// transportCacheEntry Transport 캐시에 저장되는 항목입니다.
//
// 구성 요소:
//   - key: 캐시 식별자 (설정 조합)
//   - transport: 실제 HTTP Transport 객체
//   - accessCount: Lazy LRU Update를 위한 접근 카운터
//
// Lazy LRU Update 최적화:
//   - 문제: 매번 접근 시 LRU 갱신하면 Write Lock으로 인한 경합 발생
//   - 해결: 10회 접근마다 한 번씩만 LRU 위치 갱신
//   - 효과: Lock 경합 90% 감소, 캐시 성능 향상
//
// 생명주기:
//   - 생성: getSharedTransport()에서 새 Transport 생성 시
//   - 갱신: 10회 접근마다 LRU 리스트 맨 앞으로 이동
//   - 제거: 캐시 용량 초과 시 LRU 정책에 따라 퇴출
type transportCacheEntry struct {
	key         transportCacheKey
	transport   *http.Transport
	accessCount atomic.Int64
}

// newTransport 사용자가 설정한 옵션에 맞춰 격리된(Isolated) Transport 인스턴스를 생성합니다.
//
// 이 함수는 제공된 Transport를 복제한 후, 사용자가 지정한 설정(프록시, 타임아웃, 연결 풀 등)을
// 적용하여 독립적인 Transport를 생성합니다. "격리된"이란 이 Transport가 다른 Fetcher와 공유되지 않고
// 특정 Fetcher 전용으로 사용됨을 의미합니다.
//
// 처리 흐름:
//
//  1. Transport 복제:
//     - baseTr이 제공되면 해당 Transport를 복제하고, nil이면 defaultTransport를 복제합니다.
//     - 복제를 통해 원본 Transport의 설정을 보존하면서 독립적인 인스턴스를 생성합니다.
//
//  2. 프록시 설정:
//     - 프록시 URL 문자열을 파싱하여 *url.URL로 변환합니다.
//     - 파싱 실패 시 비밀번호를 마스킹한 에러를 반환하여 로그 노출을 방지합니다.
//
//  3. 연결 풀 관리:
//     - MaxIdleConns: 전체 유휴 연결 최대 개수 (모든 호스트 통합)
//     - MaxIdleConnsPerHost: 호스트당 유휴 연결 최대 개수 (Keep-Alive 효율 제어)
//     - MaxConnsPerHost: 호스트당 최대 연결 개수 (동시 연결 제한)
//
//  4. 네트워크 타임아웃:
//     - TLSHandshakeTimeout: TLS 핸드셰이크 완료 대기 시간
//     - ResponseHeaderTimeout: 응답 헤더 수신 대기 시간
//     - IdleConnTimeout: 유휴 연결 유지 시간 (연결 풀 관리)
//
// 매개변수:
//   - baseTr: 복제할 Transport (nil이면 defaultTransport 사용)
//   - cfg: 적용할 설정을 담은 설정 객체 (포인터 기반)
//
// 반환값:
//   - *http.Transport: 사용자 설정이 적용된 격리된 Transport 인스턴스
//   - error: 프록시 URL 파싱 실패 시 에러 (비밀번호는 마스킹되어 안전하게 반환됨)
//
// 보안 고려사항:
//   - 프록시 URL 파싱 실패 시, 에러 메시지에서 비밀번호를 자동으로 마스킹하여
//     로그나 에러 추적 시스템에 민감한 정보가 노출되지 않도록 보호합니다.
func newTransport(baseTr *http.Transport, cfg transportConfig) (*http.Transport, error) {
	// 1단계: 제공된 Transport 복제
	var newTr *http.Transport
	if baseTr != nil {
		newTr = baseTr.Clone()
	} else {
		newTr = defaultTransport.Clone()
	}

	// 2단계: 프록시 설정
	if cfg.proxyURL != nil {
		if *cfg.proxyURL == NoProxy || *cfg.proxyURL == "" {
			// 프록시 비활성화:
			// NoProxy("DIRECT") 또는 빈 문자열("")이 설정된 경우, 프록시를 완전히 비활성화합니다.
			//
			// [주의] Transport.Proxy를 nil로 설정하면 Go 기본 동작에 따라 환경 변수(HTTP_PROXY 등)를 참조하게 됩니다.
			// 따라서 환경 변수마저 무시하고 강제로 Direct 연결을 하려면, 항상 nil을 반환하는 함수를 할당해야 합니다.
			// http.ProxyURL(nil)이 바로 이러한 함수(항상 nil 반환)를 생성해줍니다.
			newTr.Proxy = http.ProxyURL(nil)
		} else {
			// 프록시 URL 설정:
			// - 제공된 URL을 파싱하여 프록시 서버로 설정합니다.
			proxyURL, err := url.Parse(*cfg.proxyURL)
			if err != nil {
				// 보안 처리: 파싱 실패 시 URL의 비밀번호를 마스킹하여 로그 노출을 방지합니다.
				redactedURL := redactRawURL(*cfg.proxyURL)

				return nil, newErrInvalidProxyURL(redactedURL)
			}

			newTr.Proxy = http.ProxyURL(proxyURL)
		}
	}

	// 3단계: 연결 풀 관리
	if cfg.maxIdleConns != nil {
		newTr.MaxIdleConns = normalizeMaxIdleConns(*cfg.maxIdleConns)
	}
	if cfg.maxIdleConnsPerHost != nil {
		newTr.MaxIdleConnsPerHost = normalizeMaxIdleConnsPerHost(*cfg.maxIdleConnsPerHost)
	}
	if cfg.maxConnsPerHost != nil {
		newTr.MaxConnsPerHost = normalizeMaxConnsPerHost(*cfg.maxConnsPerHost)
	}

	// 4단계: 네트워크 타임아웃
	if cfg.tlsHandshakeTimeout != nil {
		newTr.TLSHandshakeTimeout = normalizeTLSHandshakeTimeout(*cfg.tlsHandshakeTimeout)
	}
	if cfg.responseHeaderTimeout != nil {
		newTr.ResponseHeaderTimeout = normalizeResponseHeaderTimeout(*cfg.responseHeaderTimeout)
	}
	if cfg.idleConnTimeout != nil {
		newTr.IdleConnTimeout = normalizeIdleConnTimeout(*cfg.idleConnTimeout)
	}

	return newTr, nil
}

// getSharedTransport 주어진 설정에 맞는 공유 Transport를 반환하거나, 없으면 새로 생성합니다.
//
// 이 함수는 Transport 재사용을 통해 HTTP 연결 성능을 최적화하는 핵심 메커니즘입니다.
// 동일한 설정(프록시, 타임아웃, TLS 등)을 가진 여러 Fetcher 인스턴스들이 하나의 Transport를 공유함으로써
// TCP 연결 풀링의 효율성을 극대화하고, 불필요한 메모리 할당을 방지합니다.
//
// 적용된 최적화 기법:
//
//  1. Lazy LRU Update (지연 갱신):
//     - 캐시 히트 시 매번 LRU 순서를 갱신하지 않고, 10번 접근마다 한 번만 갱신합니다.
//     - Write Lock 획득 빈도를 90% 줄여 동시성 경합을 대폭 감소시킵니다.
//     - 약간의 LRU 정확도를 희생하는 대신, 처리량(throughput)을 크게 향상시킵니다.
//
//  2. Double-Check Locking (이중 확인 잠금):
//     - 캐시 미스 시 Read Lock → Write Lock 전환 과정에서 발생할 수 있는 중복 생성을 방지합니다.
//     - 여러 고루틴이 동시에 같은 키로 접근할 때, 하나만 실제로 생성하고 나머지는 대기 후 재사용합니다.
//
//  3. 스마트 퇴출 (Smart Eviction):
//     - 캐시가 가득 찼을 때, 프록시가 설정된 항목을 우선적으로 제거합니다.
//     - 프록시 요청은 일반적으로 빈도가 낮고, 일반 요청의 성능이 더 중요하기 때문입니다.
//     - 이를 통해 대다수 사용자에게 영향을 주는 일반 요청의 캐시 히트율을 보호합니다.
//
// 매개변수:
//   - cfg: Transport 설정을 담은 설정 객체 (포인터 기반)
//
// 반환값:
//   - *http.Transport: 재사용 가능한 공유 Transport 객체
//   - error: Transport 생성 실패 시 에러 (예: 잘못된 프록시 URL)
func getSharedTransport(cfg transportConfig) (*http.Transport, error) {
	// 0단계: 캐시 키 변환
	key := cfg.ToCacheKey()

	// 1단계: 캐시 조회
	// 읽기 잠금(RLock)을 사용하여 여러 고루틴이 동시에 캐시를 조회할 수 있도록 합니다.
	// 이는 읽기 작업이 빈번한 캐시 조회 성능을 최적화합니다.
	transportCacheMu.RLock()
	el, ok := transportCache[key]
	if ok {
		// 캐시 히트(Cache Hit): 요청한 설정과 일치하는 Transport를 발견했습니다.
		entry := el.Value.(*transportCacheEntry)
		tr := entry.transport

		// Lazy LRU Update (지연된 LRU 갱신) 최적화:
		// 매번 접근할 때마다 LRU 리스트를 갱신하면 쓰기 잠금(Lock)이 필요하여 경합(Contention)이 발생합니다.
		// 이를 방지하기 위해 10번 접근할 때마다 한 번씩만 리스트 위치를 갱신하여 동시성 성능을 극대화합니다.
		accessCount := entry.accessCount.Add(1)

		// 10번째 접근이면서, 해당 항목이 리스트의 맨 앞에 있지 않은 경우에만 갱신을 시도합니다.
		if accessCount%10 == 0 && transportCacheLRU.Front() != el {
			// 읽기 잠금을 해제하고 쓰기 잠금을 획득하여 리스트 변경을 준비합니다.
			transportCacheMu.RUnlock()
			transportCacheMu.Lock()

			// 이중 확인:
			// 잠금을 교체하는 짧은 순간에 다른 고루틴이 해당 항목을 제거하거나 변경했을 수 있습니다.
			// 데이터의 일관성을 보장하기 위해 항목의 존재 여부를 다시 확인합니다.
			if el, ok = transportCache[key]; ok {
				// 항목을 리스트의 맨 앞으로 이동시켜 '최근 사용됨'으로 표시합니다.
				transportCacheLRU.MoveToFront(el)

				transportCacheMu.Unlock()

				return el.Value.(*transportCacheEntry).transport, nil
			}

			// 경합 패배: 잠금 교체 중에 항목이 제거되었습니다.
			// 기존에 조회한 Transport(tr)는 이미 닫혔거나 유효하지 않을 수 있으므로 반환하지 않습니다.
			// 대신, 아래(2단계)의 새로운 Transport 생성 단계로 넘어갑니다.
			transportCacheMu.Unlock()
		} else {
			// 아직 10번째 접근이 아니거나, 이미 리스트의 맨 앞에 있다면 읽기 잠금만 해제하고 즉시 반환합니다.
			transportCacheMu.RUnlock()

			return tr, nil
		}
	} else {
		// 캐시 미스(Cache Miss): 캐시 조회 실패 시, 읽기 잠금을 해제하고 생성 단계로 진행합니다.
		transportCacheMu.RUnlock()
	}

	// 2단계: 새로운 Transport 생성
	// 캐시에 없는 경우, 요청된 설정에 맞춰 새로운 Transport 인스턴스를 생성합니다.
	// (이 단계는 잠금 없이 수행되어 다른 고루틴을 차단하지 않습니다)
	newTr, err := newTransport(nil, cfg)
	if err != nil {
		return nil, err
	}

	// 3단계: 캐시에 등록 (Write Lock 활용)
	// 생성된 Transport를 공유 캐시에 등록하기 위해 쓰기 잠금을 획득합니다.
	transportCacheMu.Lock()
	defer transportCacheMu.Unlock()

	// 생성 후 재확인:
	// Transport를 생성하는 동안(2단계), 다른 고루틴이 동일한 설정으로 먼저 캐시에 등록했을 수 있습니다.
	// 중복 생성을 방지하고 리소스를 절약하기 위해 캐시를 다시 한 번 확인합니다.
	if el, ok := transportCache[key]; ok {
		// 경합 패배: 다른 고루틴이 먼저 등록했습니다.
		// 방금 생성한 newTr은 불필요하므로 즉시 정리(Close)합니다.
		newTr.CloseIdleConnections()

		// 먼저 등록된 기존 항목의 LRU 순위를 갱신하고 반환합니다.
		transportCacheLRU.MoveToFront(el)

		return el.Value.(*transportCacheEntry).transport, nil
	}

	// 4단계: 캐시 용량 관리 (Eviction 정책)
	// 캐시가 최대 크기에 도달한 경우, 오래된 항목을 제거하여 공간을 확보합니다.
	if transportCacheLRU.Len() >= defaultMaxTransportCacheSize {
		// '스마트 퇴출(Smart Eviction)' 전략:
		// 프록시를 사용하는 Transport는 리소스를 많이 소모하거나 덜 중요할 가능성이 높다고 가정하여,
		// 일반 연결(Direct)의 성능을 보호하기 위해 프록시 설정이 있는 항목을 우선적으로 제거 대상으로 삼습니다.

		var evictEl *list.Element

		// 가장 오래된 항목부터 탐색을 시작하여 프록시 설정이 있는 항목을 찾습니다.
		// (성능을 위해 최대 10개까지만 검사합니다)
		curr := transportCacheLRU.Back()
		for i := 0; i < 10 && curr != nil; i++ {
			if curr.Value.(*transportCacheEntry).key.proxyURL != "" {
				evictEl = curr
				break
			}
			curr = curr.Prev()
		}

		// 프록시 설정이 있는 항목을 찾지 못한 경우, 가장 오래된(LRU) 항목을 제거 대상으로 선정합니다.
		if evictEl == nil {
			evictEl = transportCacheLRU.Back()
		}

		// 선정된 항목을 캐시와 리스트에서 영구적으로 제거하고, 관련 리소스(연결 풀 등)를 정리합니다.
		entry := evictEl.Value.(*transportCacheEntry)
		entry.transport.CloseIdleConnections()
		delete(transportCache, entry.key)
		transportCacheLRU.Remove(evictEl)
	}

	// 5단계: 최종 등록
	// 새로운 Transport를 리스트의 맨 앞에 추가하여 가장 최근에 사용된 것으로 표시합니다.
	el = transportCacheLRU.PushFront(&transportCacheEntry{
		key:       key,
		transport: newTr,
	})
	transportCache[key] = el

	return newTr, nil
}

// setupTransport HTTPFetcher의 설정에 맞춰 최적의 Transport를 선택하고 구성합니다.
//
// 이 함수는 HTTPClient 초기화 과정에서 호출되며, 성능과 리소스 효율성을 고려하여 다음과 같이 동작합니다:
//
// 처리 흐름:
//
//  1. 설정 유무 확인: 사용자가 옵션(기본값과 다른 값이 아닌, 명시적인 설정값)을 제공했는지 봅니다.
//
//  2. 기본 동작:
//     - 명시적인 설정이 없다면 현재 설정된 Transport(전역 또는 외부 주입)를 변형 없이 그대로 사용합니다.
//     - 이를 통해 불필요한 객체 생성을 피하고 메모리를 절약합니다.
//
//  3. 사용자 정의 동작:
//     - 명시적인 설정이 있다면 두 가지 방식으로 처리합니다:
//     a) 외부 주입 Transport: `configureTransportFromExternal` 호출 (CoW 전략 적용)
//     b) 전역 Transport 사용 중: `configureTransportFromOptions` 호출 (캐시 또는 격리 생성)
//
// 제약사항:
//   - `*http.Transport` 타입만 설정 변경이 가능합니다.
//   - 그 외의 커스텀 `RoundTripper`에 옵션을 적용하려 하면 에러(ErrUnsupportedTransport)를 반환합니다.
//
// 반환값:
//   - error: Transport 초기화 실패 시 에러 (예: 잘못된 프록시 URL)
func (f *HTTPFetcher) setupTransport() error {
	// 1단계: 현재 Transport를 그대로 사용할 수 있는지 확인합니다.
	// needsCustomTransport()는 사용자가 설정한 옵션(타임아웃, 프록시 등)이 명시적으로 존재하는지 검사합니다.
	// 사용자가 설정한 옵션이 존재하지 않는다면(nil), 현재 Transport(defaultTransport 또는 외부 주입 Transport)를 그대로 사용합니다.
	if !f.needsCustomTransport() {
		// 커스터마이징이 불필요하므로 현재 Transport를 유지합니다.
		return nil
	}

	// 2단계: 현재 Transport가 전역 Transport인 경우, 사용자가 설정한 옵션을 반영한 새로운 Transport를 생성합니다.
	// 이 경우는 Fetcher 생성 시 별도의 Transport가 주입되지 않았고,
	// 사용자가 설정한 옵션(타임아웃, 프록시 등)을 적용해야 하는 상황입니다.
	if f.client.Transport == defaultTransport {
		// 사용자가 설정한 옵션을 기반으로 새로운 Transport를 구성합니다.
		return f.configureTransportFromOptions()
	}

	// 3단계: 외부에서 주입된 Transport를 처리합니다.
	// 외부에서 주입된 Transport의 설정을 최대한 보존하면서, 사용자가 설정한 옵션만 선택적으로 덮어쓰는 방식으로 동작합니다.
	// 이를 통해 외부 설정과 사용자가 설정한 옵션을 조화롭게 병합합니다.
	if tr, ok := f.client.Transport.(*http.Transport); ok {
		// 외부 Transport를 복제한 후, 사용자가 설정한 옵션을 적용한 새로운 Transport를 생성합니다.
		return f.configureTransportFromExternal(tr)
	}

	// 4단계: 커스텀 RoundTripper 감지
	// *http.Transport가 아닌 다른 타입(예: 테스트용 모의 객체)은 설정 변경이 불가능합니다.
	return ErrUnsupportedTransport
}

// needsCustomTransport 사용자가 설정한 옵션이 명시적으로 존재하는지 확인하여 새로운 Transport의 생성이 필요한지 판단합니다.
//
// 판단 기준 (다음 중 하나라도 명시적인 설정값(nil 아님)이 존재하면 true 반환):
//  1. 캐싱 설정 (disableTransportCaching)
//  2. 프록시 설정 (proxyURL)
//  3. 연결 풀 관리 (maxIdleConns, maxIdleConnsPerHost, maxConnsPerHost)
//  4. 네트워크 타임아웃 (tlsHandshakeTimeout, responseHeaderTimeout, idleConnTimeout)
//
// 반환값:
//   - true: 사용자 설정이 존재하므로 이를 반영한 새로운 Transport를 생성해야 함
//   - false: 사용자 설정이 없으므로(모두 nil) 현재 Transport를 변형 없이 그대로 사용 가능
func (f *HTTPFetcher) needsCustomTransport() bool {
	return f.disableTransportCaching ||
		f.proxyURL != nil ||
		f.maxIdleConns != nil ||
		f.maxIdleConnsPerHost != nil ||
		f.maxConnsPerHost != nil ||
		f.tlsHandshakeTimeout != nil ||
		f.responseHeaderTimeout != nil ||
		f.idleConnTimeout != nil
}

// configureTransportFromOptions 사용자가 설정한 옵션을 기반으로 최적의 Transport를 구성합니다.
//
// 목적:
//   - 사용자가 지정한 프록시, 타임아웃, 연결 풀 설정을 바탕으로 가장 효율적인 Transport를 선택하거나 생성합니다.
//   - 캐시 정책에 따라 격리 모드 또는 공유 모드로 동작하여 리소스 효율성을 최적화합니다.
//
// 처리 흐름:
//
//  1. 설정 키 생성:
//     - 사용자가 설정한 옵션(f.*)을 transportConfig 구조체로 변환합니다.
//     - 각 포인터 필드를 직접 매핑하여 사용자 의도(설정 유무)를 정확히 전달합니다.
//
//  2. 운영 모드 선택:
//
//     a) 격리 모드 (disableTransportCaching = true):
//     - 이 Fetcher 전용의 독립적인 Transport를 생성합니다.
//     - 다른 Fetcher와 완전히 격리되어 리소스를 공유하지 않습니다.
//
//     b) 공유 모드 (disableTransportCaching = false, 기본값):
//     - 동일한 설정을 가진 Fetcher끼리 Transport를 공유합니다.
//     - TCP 연결 풀을 재사용하여 메모리와 핸드셰이크 비용을 절약합니다.
//
// 반환값:
//   - error: Transport 생성 실패 시 에러 (예: 잘못된 프록시 URL)
func (f *HTTPFetcher) configureTransportFromOptions() error {
	// Transport 설정 객체 생성
	cfg := f.toTransportConfig()

	// 운영 모드 선택
	if f.disableTransportCaching {
		// 격리 모드:
		// - 이 Fetcher 전용의 독립적인 Transport를 생성합니다.
		// - defaultTransport를 복제한 후 사용자 설정을 적용합니다.
		// - 다른 Fetcher와 연결 풀을 공유하지 않습니다.
		newTr, err := newTransport(nil, cfg)
		if err != nil {
			return newErrIsolatedTransportCreateFailed(err)
		}

		f.client.Transport = newTr

		// 격리 모드에서 생성한 Transport는 이 Fetcher가 독점적으로 소유합니다.
		f.ownsTransport = true

		return nil
	}

	// 공유 모드 (기본값):
	// - 동일한 설정을 가진 Fetcher끼리 Transport를 공유합니다.
	// - 캐시에서 기존 Transport를 찾거나, 없으면 새로 생성하여 캐시에 등록합니다.
	// - TCP 연결 풀을 재사용하여 메모리와 핸드셰이크 비용을 절약합니다.
	tr, err := getSharedTransport(cfg)
	if err != nil {
		return newErrSharedTransportCreateFailed(err)
	}

	f.client.Transport = tr

	// 공유 Transport는 캐시가 소유하므로, 개별 Fetcher는 소유권을 갖지 않습니다.
	f.ownsTransport = false

	return nil
}

// configureTransportFromExternal 외부에서 주입된 Transport에 사용자 설정을 적용하여 새로운 Transport를 구성합니다.
//
// 목적:
//   - WithTransport 옵션으로 외부에서 주입된 Transport에 사용자가 설정한 옵션을 안전하게 적용합니다.
//   - Copy-on-Write(CoW) 전략을 사용하여 원본 Transport의 손상을 방지합니다.
//   - 불필요한 복제를 방지하여 리소스를 절약합니다.
//
// 처리 흐름:
//
//  1. 변경 필요성 검사:
//     - 사용자의 명시적 설정이 원본 Transport의 설정과 이미 일치하는지 확인합니다.
//     - 일치한다면 복제 없이 원본을 그대로 사용하여 리소스를 절약합니다.
//
//  2. 안전한 복제 및 적용 (Copy-on-Write):
//     - 설정 변경이 필요하다면 원본을 복제한 후 사용자가 설정한 옵션을 선택적으로 적용합니다.
//     - 복제된 Transport는 격리 모드로 전환되어 Close() 시 안전하게 정리됩니다.
//
// 매개변수:
//   - tr: 외부에서 주입된 Transport (WithTransport 옵션으로 주입됨)
//
// 반환값:
//   - error: 프록시 URL 파싱 실패 시 에러
func (f *HTTPFetcher) configureTransportFromExternal(tr *http.Transport) error {
	// 복제 필요성 검사
	// 외부에서 주입된 Transport의 현재 설정이 사용자가 요청한 설정과 이미 일치하는지 확인합니다.
	// 일치한다면 복제 없이 원본을 그대로 사용하여 불필요한 메모리 사용과 처리 비용을 절약합니다.
	if !f.shouldCloneTransport(tr) {
		// 외부에서 주입된 Transport를 그대로 사용하므로 소유권은 외부에 있습니다.
		f.ownsTransport = false

		return nil
	}

	// ⚠️ 성능 경고 로그
	//
	// WithTransport + 다른 Transport 설정 옵션 동시 사용 시 성능 경고:
	//   - 원본 보호를 위해 Transport 복제 → 매번 새로운 커넥션 풀 생성 (캐싱 비활성화)
	//   - 해결: WithTransport 없이 옵션만 사용 또는 모든 설정 완료 후 Transport 주입
	//   - 로그 필드로 어떤 설정이 복제를 유발했는지 추적
	fields := applog.Fields{}
	if f.proxyURL != nil {
		fields["proxy_changed"] = true
	}
	if f.maxIdleConns != nil && tr.MaxIdleConns != *f.maxIdleConns {
		fields["max_idle_conns_changed"] = true
	}
	if f.maxIdleConnsPerHost != nil && tr.MaxIdleConnsPerHost != *f.maxIdleConnsPerHost {
		fields["max_idle_conns_per_host_changed"] = true
	}
	if f.maxConnsPerHost != nil && tr.MaxConnsPerHost != *f.maxConnsPerHost {
		fields["max_conns_per_host_changed"] = true
	}
	if f.tlsHandshakeTimeout != nil && tr.TLSHandshakeTimeout != *f.tlsHandshakeTimeout {
		fields["tls_handshake_timeout_changed"] = true
	}
	if f.responseHeaderTimeout != nil && tr.ResponseHeaderTimeout != *f.responseHeaderTimeout {
		fields["response_header_timeout_changed"] = true
	}
	if f.idleConnTimeout != nil && tr.IdleConnTimeout != *f.idleConnTimeout {
		fields["idle_conn_timeout_changed"] = true
	}
	applog.WithComponent(component).
		WithFields(fields).
		Warn("외부 Transport 주입 시 추가 설정 옵션 적용으로 인한 성능 저하 감지: " +
			"Copy-on-Write 전략에 따라 Transport가 복제되어 커넥션 풀 재사용이 불가능합니다. " +
			"권장 사항: 내부 캐싱 활용을 위해 WithTransport 옵션 제거 또는 사전 구성된 Transport 주입")

	// Transport 설정 객체 생성
	cfg := f.toTransportConfig()

	// Copy-on-Write (CoW) 전략 적용
	// 외부에서 주입된 Transport를 복제하고, 복제본에만 사용자 설정을 적용합니다.
	// 이를 통해 원본 Transport는 변경되지 않고 보호되며, 새로운 독립적인 Transport가 생성됩니다.
	newTr, err := newTransport(tr, cfg)
	if err != nil {
		return newErrIsolatedTransportCreateFailed(err)
	}

	// 격리 모드로 전환
	// 복제된 Transport는 다른 Fetcher와 공유되지 않는 격리된 리소스입니다.
	// Close() 호출 시 안전하게 정리할 수 있도록 캐싱을 비활성화합니다.
	f.disableTransportCaching = true

	f.client.Transport = newTr

	// 복제된 Transport는 이 Fetcher가 생성했으므로 소유권을 가집니다.
	f.ownsTransport = true

	return nil
}

// toTransportConfig HTTPFetcher의 Transport 관련 설정을 transportConfig 구조체로 변환합니다.
//
// 이 함수는 HTTPFetcher에 설정된 프록시, 연결 풀, 타임아웃 등의 옵션을 transportConfig 형식으로 변환하여
// newTransport 함수에 전달할 수 있도록 준비합니다.
//
// 동작 방식:
//   - 각 설정 필드를 1:1로 매핑하며, 포인터를 그대로 전달합니다.
//   - nil 값은 "설정 안 함"을 의미하며, non-nil 값은 "사용자가 명시적으로 설정함"을 의미합니다.
//   - 값의 정규화(유효성 검증, 기본값 적용 등)는 이 함수에서 수행하지 않고, newTransport에서 처리합니다.
func (f *HTTPFetcher) toTransportConfig() transportConfig {
	cfg := transportConfig{}

	// 프록시 설정
	if f.proxyURL != nil {
		cfg.proxyURL = f.proxyURL
	}

	// 연결 풀 관리
	if f.maxIdleConns != nil {
		cfg.maxIdleConns = f.maxIdleConns
	}
	if f.maxIdleConnsPerHost != nil {
		cfg.maxIdleConnsPerHost = f.maxIdleConnsPerHost
	}
	if f.maxConnsPerHost != nil {
		cfg.maxConnsPerHost = f.maxConnsPerHost
	}

	// 네트워크 타임아웃
	if f.tlsHandshakeTimeout != nil {
		cfg.tlsHandshakeTimeout = f.tlsHandshakeTimeout
	}
	if f.responseHeaderTimeout != nil {
		cfg.responseHeaderTimeout = f.responseHeaderTimeout
	}
	if f.idleConnTimeout != nil {
		cfg.idleConnTimeout = f.idleConnTimeout
	}

	return cfg
}

// shouldCloneTransport 외부에서 주입된 Transport를 복제해야 하는지 판단합니다.
//
// 목적:
//   - 외부에서 주입된 Transport의 설정과 사용자가 설정한 옵션을 비교하여 복제 필요 여부를 결정합니다.
//   - 불필요한 복제를 방지하여 메모리 사용량을 최소화하고 연결 풀 재사용을 극대화합니다.
//
// 판단 기준:
//   - 사용자가 설정한 옵션(f.*)과 외부에서 주입된 Transport의 현재 설정(tr.*)을 비교합니다.
//   - 값이 서로 다르다면 true를 반환하여 복제 및 설정 적용을 수행합니다.
//   - 값이 같다면(또는 사용자가 설정한 옵션이 없다면) 복제 없이 기존 Transport를 재사용합니다.
//
// 비교 항목:
//   - 프록시 서버 (proxyURL)
//   - 연결 풀 관리 (maxIdleConns, maxIdleConnsPerHost, maxConnsPerHost)
//   - 네트워크 타임아웃 (tlsHandshakeTimeout, responseHeaderTimeout, idleConnTimeout)
//
// 매개변수:
//   - tr: 외부에서 주입된 Transport (WithTransport 옵션으로 주입됨)
//
// 반환값:
//   - true: 복제 필요 (설정이 다름)
//   - false: 복제 불필요 (설정이 일치하거나 사용자가 변경을 요청하지 않음)
func (f *HTTPFetcher) shouldCloneTransport(tr *http.Transport) bool {
	return f.proxyURL != nil ||
		(f.maxIdleConns != nil && tr.MaxIdleConns != *f.maxIdleConns) ||
		(f.maxIdleConnsPerHost != nil && tr.MaxIdleConnsPerHost != *f.maxIdleConnsPerHost) ||
		(f.maxConnsPerHost != nil && tr.MaxConnsPerHost != *f.maxConnsPerHost) ||
		(f.tlsHandshakeTimeout != nil && tr.TLSHandshakeTimeout != *f.tlsHandshakeTimeout) ||
		(f.responseHeaderTimeout != nil && tr.ResponseHeaderTimeout != *f.responseHeaderTimeout) ||
		(f.idleConnTimeout != nil && tr.IdleConnTimeout != *f.idleConnTimeout)
}
