package fetcher

import (
	"time"
)

// Config Fetcher 체인을 구성하기 위한 모든 설정 옵션을 정의하는 구조체입니다.
type Config struct {
	// ========================================
	// 프록시 설정
	// ========================================

	// ProxyURL 프록시 URL입니다.
	//
	// 설정 규칙:
	//   - nil: 기본 설정을 따릅니다.
	//     · 일반적인 경우: 환경 변수(HTTP_PROXY, HTTPS_PROXY)를 사용합니다.
	//     · HTTP 클라이언트를 직접 주입한 경우: 해당 클라이언트의 기존 Proxy 정책을 그대로 유지합니다.
	//   - URL: 지정된 프록시 서버 사용 (예: "http://proxy:8080")
	//   - NoProxy(또는 "DIRECT") 또는 빈 문자열(""): 프록시 비활성화 (환경 변수 무시, 직접 연결)
	ProxyURL *string

	// ========================================
	// HTTP 클라이언트 동작
	// ========================================

	// MaxRedirects HTTP 클라이언트의 최대 리다이렉트(3xx) 횟수입니다.
	//
	// 설정 규칙:
	//   - nil: 설정하지 않음 (HTTPFetcher 기본값 사용)
	//   - 0: 리다이렉트 허용 안 함
	//   - 양수: 지정된 횟수만큼 리다이렉트 허용
	//   - 음수: 기본값으로 보정
	MaxRedirects *int

	// EnableUserAgentRandomization 요청마다 User-Agent를 랜덤으로 변경할지 여부를 제어합니다.
	//
	// 설정 값:
	//   - false (기본값): 기능 비활성화 (원본 요청의 User-Agent 사용)
	//   - true: 기능 활성화 (UserAgents 또는 내장 목록에서 랜덤 선택, 봇 차단 우회에 유용)
	EnableUserAgentRandomization bool

	// UserAgents User-Agent를 랜덤으로 선택하여 주입할 때 사용할 User-Agent 문자열 목록입니다.
	//
	// 설정 값:
	//   - nil/빈 슬라이스: 내장된 User-Agent 목록에서 랜덤으로 선택하여 주입
	//   - 값 지정: 지정된 목록에서 랜덤으로 선택하여 주입
	UserAgents []string

	// ========================================
	// 연결 풀(Connection Pool) 관리
	// ========================================

	// MaxIdleConns 전체 유휴 연결의 최대 개수입니다.
	// 모든 호스트에 대해 유지할 수 있는 유휴 연결의 최대 개수를 제한합니다.
	//
	// 설정 규칙:
	//   - nil: 설정하지 않음 (HTTPFetcher 기본값 사용)
	//   - 0: 무제한
	//   - 양수: 지정된 개수로 제한
	//   - 음수: 기본값으로 보정
	MaxIdleConns *int

	// MaxIdleConnsPerHost 호스트당 유휴 연결의 최대 개수입니다.
	//
	// 설정 규칙:
	//   - nil: 설정하지 않음 (HTTPFetcher 기본값 사용)
	//   - 0: net/http가 기본값 2로 해석
	//   - 양수: 지정된 개수로 제한
	//   - 음수: 기본값으로 보정
	MaxIdleConnsPerHost *int

	// MaxConnsPerHost 호스트당 최대 연결 개수입니다.
	// 동일한 호스트에 대해 동시에 유지할 수 있는 최대 연결 개수를 제한합니다.
	//
	// 설정 규칙:
	//   - nil: 설정하지 않음 (HTTPFetcher 기본값 사용)
	//   - 0: 무제한
	//   - 양수: 지정된 개수로 제한
	//   - 음수: 기본값으로 보정
	MaxConnsPerHost *int

	// ========================================
	// 네트워크 타임아웃
	// ========================================

	// Timeout HTTP 요청 전체에 대한 타임아웃입니다.
	// 요청 전송부터 응답 본문(Body)까지 모두 읽는 전체 과정에 대한 제한 시간입니다.
	//
	// 설정 규칙:
	//   - nil: 설정하지 않음 (HTTPFetcher 기본값 사용)
	//   - 0: 타임아웃 없음 (무한 대기)
	//   - 양수: 지정된 시간으로 제한
	//   - 음수: 기본값으로 보정
	Timeout *time.Duration

	// TLSHandshakeTimeout TLS 핸드셰이크 타임아웃입니다.
	// TLS 연결 수립 과정에서 핸드셰이크가 완료되기까지 허용되는 최대 시간입니다.
	//
	// 설정 규칙:
	//   - nil: 설정하지 않음 (HTTPFetcher 기본값 사용)
	//   - 0: 타임아웃 없음 (무한 대기)
	//   - 양수: 지정된 시간으로 제한
	//   - 음수: 기본값으로 보정
	TLSHandshakeTimeout *time.Duration

	// ResponseHeaderTimeout HTTP 응답 헤더 대기 타임아웃입니다.
	// 요청 전송 후 서버로부터 응답 헤더를 받을 때까지 허용되는 최대 시간입니다.
	// 본문(Body) 데이터 수신 시간은 포함되지 않습니다.
	//
	// 설정 규칙:
	//   - nil: 설정하지 않음 (HTTPFetcher 기본값 사용)
	//   - 0: 타임아웃 없음 (무한 대기)
	//   - 양수: 지정된 시간으로 제한
	//   - 음수: 타임아웃 없음(0)으로 보정
	ResponseHeaderTimeout *time.Duration

	// IdleConnTimeout 유휴 연결 타임아웃입니다.
	// 연결 풀에서 사용되지 않는 연결이 닫히기 전까지 유지되는 최대 시간입니다.
	//
	// 설정 규칙:
	//   - nil: 설정하지 않음 (HTTPFetcher 기본값 사용)
	//   - 0: 제한 없음 (연결 무기한 유지)
	//   - 양수: 지정된 시간 후 유휴 연결 종료
	//   - 음수: 기본값으로 보정
	IdleConnTimeout *time.Duration

	// ========================================
	// 재시도(Retry) 정책
	// ========================================

	// MaxRetries 최대 재시도 횟수입니다.
	//
	// 설정 규칙:
	//   - nil: 기본값으로 보정
	//   - 0: 재시도 안 함
	//   - 양수: 실패 시(5xx 에러 또는 네트워크 오류 등) 지정된 횟수만큼 재시도
	//   - 보정: 최소값(minAllowedRetries) 미만은 최소값으로, 최대값(maxAllowedRetries) 초과는 최대값으로 보정
	MaxRetries *int

	// MinRetryDelay 재시도 대기 시간의 최소값입니다.
	//
	// 설정 규칙:
	//   - nil: 기본값으로 보정
	//   - 1초 미만: 서버 부하 방지를 위해 최소 시간(1초)으로 보정
	//   - 1초 이상: 별도의 보정 없이 설정값을 그대로 적용
	MinRetryDelay *time.Duration

	// MaxRetryDelay 재시도 대기 시간의 최대값입니다.
	//
	// 설정 규칙:
	//   - nil: 기본값으로 보정
	//   - 0: 기본값으로 보정
	//   - MinRetryDelay 미만: 최대 재시도 대기 시간은 최소 재시도 대기 시간보다 작을 수 없으므로 MinRetryDelay로 보정
	//   - 그 외: 지수 백오프가 진행되더라도 재시도 대기 시간이 이 설정값을 초과하지 않도록 제한
	MaxRetryDelay *time.Duration

	// ========================================
	// 응답 검증 및 제한
	// ========================================

	// MaxBytes HTTP 응답 본문의 최대 허용 크기입니다. (단위: 바이트)
	//
	// 설정 규칙:
	//   - nil: 기본값으로 보정
	//   - NoLimit(-1): 크기 제한을 적용하지 않음 (주의: 메모리 고갈 위험 있음)
	//   - 0 이하: 유효하지 않은 값으로 간주하여 기본값으로 보정
	//   - 양수: 지정된 크기만큼 HTTP 응답 본문의 허용 크기를 제한
	MaxBytes *int64

	// DisableStatusCodeValidation HTTP 응답 상태 코드 검증 사용 여부를 제어합니다.
	//
	// 설정 값:
	//   - false (기본값): 검증 활성화 (200 OK 또는 AllowedStatusCodes만 허용)
	//   - true: 검증 비활성화 (모든 상태 코드 허용, 커스텀 에러 처리가 필요한 경우)
	DisableStatusCodeValidation bool

	// AllowedStatusCodes 허용할 HTTP 응답 상태 코드 목록입니다.
	//
	// 설정 값:
	//   - nil/빈 슬라이스: 200 OK만 허용
	//   - 값 지정: 지정된 코드들만 허용
	AllowedStatusCodes []int

	// AllowedMimeTypes 허용할 HTTP 응답의 MIME 타입 목록입니다.
	//
	// 설정 값:
	//   - nil/빈 슬라이스: MIME 타입 검증 생략
	//   - 값 지정: "text/html" 같이 파라미터를 제외한 순수 MIME 타입만 허용 (대소문자 구분 안 함)
	AllowedMimeTypes []string

	// ========================================
	// 미들웨어 체인 구성
	// ========================================

	// DisableLogging HTTP 요청/응답 로깅 사용 여부를 제어합니다.
	//
	// 설정 값:
	//   - false (기본값): 로깅 활성화 (URL, 상태 코드, 실행 시간 등을 기록)
	//   - true: 로깅 비활성화 (성능 향상 또는 민감한 정보 보호가 필요한 경우)
	DisableLogging bool

	// DisableTransportCaching Transport 캐싱 사용 여부를 제어합니다.
	//
	// 설정 값:
	//   - false (기본값/권장): 캐시 활성화 (성능 최적화)
	//   - true: 캐시 비활성화 (테스트 또는 완전한 격리가 필요한 경우)
	DisableTransportCaching bool
}

// applyDefaults Config의 설정값들을 검증하고, 미설정되거나 유효하지 않은 값을 안전한 기본값으로 보정합니다.
func (cfg *Config) applyDefaults() {
	// 최대 리다이렉트 횟수를 정규화합니다.
	if cfg.MaxRedirects != nil {
		normalizePtr(&cfg.MaxRedirects, defaultMaxRedirects, normalizeMaxRedirects)
	}

	// 전체 유휴 연결의 최대 개수를 정규화합니다.
	if cfg.MaxIdleConns != nil {
		normalizePtr(&cfg.MaxIdleConns, defaultMaxIdleConns, normalizeMaxIdleConns)
	}

	// 호스트당 유휴 연결의 최대 개수를 정규화합니다.
	if cfg.MaxIdleConnsPerHost != nil {
		normalizePtr(&cfg.MaxIdleConnsPerHost, 0, normalizeMaxIdleConnsPerHost)
	}

	// 호스트당 최대 연결 개수를 정규화합니다.
	if cfg.MaxConnsPerHost != nil {
		normalizePtr(&cfg.MaxConnsPerHost, 0, normalizeMaxConnsPerHost)
	}

	// HTTP 요청 전체에 대한 타임아웃을 정규화합니다.
	if cfg.Timeout != nil {
		normalizePtr(&cfg.Timeout, defaultTimeout, normalizeTimeout)
	}

	// TLS 핸드셰이크 타임아웃을 정규화합니다.
	if cfg.TLSHandshakeTimeout != nil {
		normalizePtr(&cfg.TLSHandshakeTimeout, defaultTLSHandshakeTimeout, normalizeTLSHandshakeTimeout)
	}

	// HTTP 응답 헤더 대기 타임아웃을 정규화합니다.
	if cfg.ResponseHeaderTimeout != nil {
		normalizePtr(&cfg.ResponseHeaderTimeout, 0, normalizeResponseHeaderTimeout)
	}

	// 유휴 연결 타임아웃을 정규화합니다.
	if cfg.IdleConnTimeout != nil {
		normalizePtr(&cfg.IdleConnTimeout, defaultIdleConnTimeout, normalizeIdleConnTimeout)
	}

	// 최대 재시도 횟수를 정규화합니다.
	normalizePtr(&cfg.MaxRetries, 0, normalizeMaxRetries)

	// 재시도 대기 시간의 최소값과 최대값을 정규화합니다.
	normalizePtrPair(&cfg.MinRetryDelay, &cfg.MaxRetryDelay, 1*time.Second, defaultMaxRetryDelay, normalizeRetryDelays)

	// HTTP 응답 본문의 최대 허용 크기를 정규화합니다.
	normalizePtr(&cfg.MaxBytes, defaultMaxBytes, normalizeByteLimit)
}

// New 주요 설정값(재시도 횟수, 지연 시간, 본문 크기 제한)만으로 빠르고 간편하게 Fetcher를 생성합니다.
//
// 이 함수는 내부적으로 Config를 생성하고 applyDefaults()를 통해 안전한 기본값을 적용한 후,
// NewFromConfig()를 호출하여 최적화된 Fetcher 체인을 구성합니다.
//
// 복잡한 설정(타임아웃, 프록시, 유효성 검사 등)이 필요한 경우에는 NewFromConfig 함수를 직접 사용하는 것을 권장합니다.
//
// 매개변수:
//   - maxRetries: 최대 재시도 횟수 (권장: 0~10회, 범위 초과 시 내부 정책에 따라 자동 보정)
//   - minRetryDelay: 최소 재시도 대기 시간 (최소: 1초, 서버 부하 방지를 위해 1초 미만은 1초로 자동 보정)
//   - maxBytes: 응답 본문의 최대 허용 크기 (0: 기본값 사용, -1: 제한 없음, 양수: 지정된 바이트로 제한)
//   - opts: HTTPFetcher 추가 설정 옵션 (예: WithTimeout, WithProxy)
//
// 반환값:
//   - Fetcher: 구성된 Fetcher 체인 인터페이스
func New(maxRetries int, minRetryDelay time.Duration, maxBytes int64, opts ...Option) Fetcher {
	config := Config{
		MaxRetries:    &maxRetries,
		MinRetryDelay: &minRetryDelay,

		MaxBytes: &maxBytes,
	}
	config.applyDefaults()

	return NewFromConfig(config, opts...)
}

// NewFromConfig Config 기반 옵션과 추가 옵션을 기반으로 최적화된 Fetcher 실행 체인을 생성합니다.
//
// Fetcher 체인은 책임 연쇄 패턴(Chain of Responsibility)을 따르며, 다음과 같은 순서로 미들웨어가 구성됩니다 (바깥쪽 -> 안쪽):
//
//  1. [관찰] LoggingFetcher    (최외곽): 모든 시도와 지연을 포함한 전체 요청 생애주기를 기록합니다.
//  2. [보조] UserAgentFetcher  (보조): 각 요청에 매번 새로운 User-Agent를 부여합니다.
//  3. [제어] RetryFetcher      (핵심): 실패 시 지수 백오프 전략에 따라 재시도를 총괄 제어합니다.
//  4. [검증] MimeTypeFetcher   (검증): 서버가 반환한 Content-Type의 유효성을 검사합니다.
//  5. [검증] StatusCodeFetcher (검증): HTTP 응답 상태 코드의 유효성을 검사합니다.
//  6. [제한] MaxBytesFetcher   (보호): 응답 본문의 크기를 실시간으로 감시하여 메모리 고갈을 방지합니다.
//  7. [전송] HTTPFetcher       (최내곽): 최하단에서 실제 네트워크 I/O 및 패킷 전송을 담당합니다.
//
// 설계 의도:
//   - LoggingFetcher는 재시도를 포함한 전체 흐름을 기록하기 위해 가장 바깥에 위치합니다.
//   - RetryFetcher는 하위 검증 로직(상태 코드, MimeType) 실패 시에도 재시도를 수행해야 하므로 검증 미들웨어보다 바깥에 위치합니다.
//   - 검증 로직(StatusCode, MimeType)은 각 시도(Attempt)마다 수행되어야 하므로 RetryFetcher 안쪽에 위치합니다.
//
// 매개변수:
//   - cfg: Fetcher 체인 구성을 위한 상세 설정값
//   - opts: HTTPFetcher 내부 동작을 제어하기 위한 추가 옵션
//
// 반환값:
//   - Fetcher: 구성된 Fetcher 체인 인터페이스
func NewFromConfig(cfg Config, opts ...Option) Fetcher {
	cfg.applyDefaults()

	// ========================================
	// 0단계: Config 기반 옵션 및 추가 옵션 통합
	// ========================================
	var mergedOpts []Option

	// 프록시 URL 설정
	if cfg.ProxyURL != nil {
		mergedOpts = append(mergedOpts, WithProxy(*cfg.ProxyURL))
	}

	// HTTP 클라이언트의 최대 리다이렉트(3xx) 횟수 설정
	if cfg.MaxRedirects != nil {
		mergedOpts = append(mergedOpts, WithMaxRedirects(*cfg.MaxRedirects))
	}

	// 전체 유휴 연결의 최대 개수 설정
	if cfg.MaxIdleConns != nil {
		mergedOpts = append(mergedOpts, WithMaxIdleConns(*cfg.MaxIdleConns))
	}

	// 호스트당 유휴 연결의 최대 개수 설정
	if cfg.MaxIdleConnsPerHost != nil {
		mergedOpts = append(mergedOpts, WithMaxIdleConnsPerHost(*cfg.MaxIdleConnsPerHost))
	}

	// 호스트당 최대 연결 개수 설정
	if cfg.MaxConnsPerHost != nil {
		mergedOpts = append(mergedOpts, WithMaxConnsPerHost(*cfg.MaxConnsPerHost))
	}

	// HTTP 요청 전체에 대한 타임아웃 설정
	if cfg.Timeout != nil {
		mergedOpts = append(mergedOpts, WithTimeout(*cfg.Timeout))
	}

	// TLS 핸드셰이크 타임아웃 설정
	if cfg.TLSHandshakeTimeout != nil {
		mergedOpts = append(mergedOpts, WithTLSHandshakeTimeout(*cfg.TLSHandshakeTimeout))
	}

	// HTTP 응답 헤더 대기 타임아웃 설정
	if cfg.ResponseHeaderTimeout != nil {
		mergedOpts = append(mergedOpts, WithResponseHeaderTimeout(*cfg.ResponseHeaderTimeout))
	}

	// 유휴 연결 타임아웃 설정
	if cfg.IdleConnTimeout != nil {
		mergedOpts = append(mergedOpts, WithIdleConnTimeout(*cfg.IdleConnTimeout))
	}

	// Transport 캐싱 사용 여부 설정
	mergedOpts = append(mergedOpts, WithDisableTransportCaching(cfg.DisableTransportCaching))

	// 추가 옵션을 마지막에 추가하여 Config 기반 옵션을 덮어쓸 수 있도록 함!!
	mergedOpts = append(mergedOpts, opts...)

	// ========================================
	// 1단계: 기본 HTTPFetcher 생성 (체인의 가장 안쪽)
	// ========================================
	var f Fetcher = NewHTTPFetcher(mergedOpts...)

	// ========================================
	// 2단계: HTTP 응답 본문의 크기 제한 미들웨어
	// ========================================
	f = NewMaxBytesFetcher(f, *cfg.MaxBytes)

	// ========================================
	// 3단계: HTTP 응답 상태 코드 검증 미들웨어
	// ========================================
	if !cfg.DisableStatusCodeValidation {
		if len(cfg.AllowedStatusCodes) > 0 {
			// 성공으로 간주할 상태 코드를 사용자가 명시한 경우
			f = NewStatusCodeFetcherWithOptions(f, cfg.AllowedStatusCodes...)
		} else {
			// 기본값: 200 OK만 허용
			f = NewStatusCodeFetcher(f)
		}
	}

	// ========================================
	// 4단계: HTTP 응답 MIME 타입 검증 미들웨어
	// ========================================
	if len(cfg.AllowedMimeTypes) > 0 {
		f = NewMimeTypeFetcher(f, cfg.AllowedMimeTypes, true)
	}

	// ========================================
	// 5단계: HTTP 요청 재시도 수행 미들웨어
	// ========================================
	f = NewRetryFetcher(f, *cfg.MaxRetries, *cfg.MinRetryDelay, *cfg.MaxRetryDelay)

	// ========================================
	// 6단계: User-Agent 주입 미들웨어
	// ========================================
	// RetryFetcher 바깥에 위치하여 재시도 시에도 동일한 User-Agent를 유지합니다.
	if cfg.EnableUserAgentRandomization {
		f = NewUserAgentFetcher(f, cfg.UserAgents)
	}

	// ========================================
	// 7단계: 로깅 미들웨어 (체인의 가장 바깥쪽)
	// ========================================
	// 가장 바깥쪽에 위치하여 모든 미들웨어의 동작을 포함한 전체 과정을 로깅
	if !cfg.DisableLogging {
		f = NewLoggingFetcher(f)
	}

	return f
}

// normalizePtr 포인터 필드의 값을 안전하게 꺼내어 정규화(보정)한 뒤, 다시 포인터에 담아주는 제네릭 헬퍼 함수입니다.
//
// 이 함수는 Config의 선택적 필드(포인터 타입)들을 일관된 방식으로 초기화하고 검증할 때 사용됩니다.
//
// 작동 방식:
//  1. Nil 안전성: 입력받은 포인터가 nil인 경우, 시스템 전체의 일관성을 위해 제공된 기본값(defaultValue)을 사용합니다.
//  2. 값 정규화: 사용자가 정의한 로직(normalizer)을 호출하여 값이 유효한 범위 내에 있는지 검증하고 필요시 보정합니다.
//  3. 불변성 유지: 기존 메모리 값을 직접 수정하는 대신, 정규화된 새로운 결과값의 주소를 할당하여 부수 효과를 최소화합니다.
func normalizePtr[T any](ptr **T, defaultValue T, normalizer func(T) T) {
	var val T
	if *ptr != nil {
		val = **ptr
	} else {
		val = defaultValue
	}

	result := normalizer(val)

	*ptr = &result
}

// normalizePtrPair 서로 논리적으로 결합된 두 개의 포인터 필드 값을 함께 정규화하는 제네릭 헬퍼 함수입니다.
//
// 주로 '최소값'과 '최대값'처럼 하나의 설정이 다른 설정에 영향을 미치는(상호 의존적인) 관계를 처리할 때 유용합니다.
//
// 작동 방식:
//  1. 개별 기본값 적용: 두 포인터가 각각 nil인 경우를 처리하여 모두 유효한 값을 가지도록 준비합니다.
//  2. 원자적(Atomic) 상호 보정: 두 값을 동시에 normalizer에 전달하여, 논리적 모순(예: 최소값이 최대값보다 큰 경우)이 없는지 확인하고 교정합니다.
//  3. 일관된 상태 보장: 보정된 두 결과를 동시에 각 포인터 필드에 업데이트하여 설정의 일관성을 즉각적으로 확보합니다.
func normalizePtrPair[T any](ptr1 **T, ptr2 **T, defaultValue1, defaultValue2 T, normalizer func(T, T) (T, T)) {
	var val1, val2 T
	if *ptr1 != nil {
		val1 = **ptr1
	} else {
		val1 = defaultValue1
	}
	if *ptr2 != nil {
		val2 = **ptr2
	} else {
		val2 = defaultValue2
	}

	result1, result2 := normalizer(val1, val2)

	*ptr1 = &result1
	*ptr2 = &result2
}
