package middleware

import (
	"fmt"
	"sync"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/labstack/echo/v4"
	"golang.org/x/time/rate"
)

// componentRateLimit 속도 제한 미들웨어의 로깅용 컴포넌트 이름
const componentRateLimit = "api.middleware.rate_limit"

const (
	// maxIPRateLimiters 서버가 메모리에 유지할 수 있는 최대 고유 IP 주소(Rate Limiter 인스턴스)의 수입니다.
	// 10,000개로 제한함으로써, 대량의 IP를 이용한 DDoS 공격이나 과도한 트래픽 유입 시 메모리 사용량이 무한정 증가하는 것을 방지합니다.
	// 이 임계값에 도달하면 간이 LRU(Least Recently Used)와 유사한 전략(Go Map의 무작위 순회 특성 활용)을 통해 기존 항목을 축출하여 새로운 요청을 수용합니다.
	maxIPRateLimiters = 10000

	// retryAfter RFC 7231, Section 7.1.3에 정의된 HTTP 헤더 필드입니다.
	// 클라이언트에게 요청이 제한(Throttling)되었음을 알리고, 서비스 가용성을 위해 언제 후속 요청을 시도해야 하는지(Backpressure)를 명시적으로 지시하는 데 사용됩니다.
	retryAfter = "Retry-After"

	// retryAfterSeconds Rate Limit 임계값 초과 시 클라이언트에게 제안하는 기본 대기 시간(초)입니다.
	// "1"초라는 짧은 지연 시간을 설정하여, 클라이언트가 즉시 재시도(Busy Waiting)하는 것을 방지하면서도 서비스 응답성을 최대한 유지하도록 설계되었습니다.
	// 필요에 따라 지수 백오프(Exponential Backoff) 등의 다동적인 전략을 적용하기 전 단계의 고정형 백오프(Fixed Backoff) 값으로 동작합니다.
	retryAfterSeconds = "1"
)

// ipRateLimiter IP 주소별 Rate Limiter를 관리하는 구조체입니다.
//
// Token Bucket 알고리즘을 사용하여 IP별로 독립적인 요청 제한을 적용합니다.
//
// 동시성 안전성:
//   - sync.RWMutex로 여러 고루틴에서 안전하게 접근 가능
//   - 읽기 작업은 RLock, 쓰기 작업은 Lock으로 최적화
//
// 메모리 관리:
//   - 최대 10,000개 IP 추적 (maxIPRateLimiters)
//   - 제한 초과 시 맵에서 랜덤하게 하나 제거 (Go Map 순회 특성 활용)
type ipRateLimiter struct {
	mu       sync.RWMutex
	limiters map[string]*rate.Limiter
	rate     rate.Limit // 초당 허용 요청 수
	burst    int        // 버스트 허용량
}

// newIPRateLimiter 새로운 IP 기반 Rate Limiter를 생성합니다.
//
// Parameters:
//   - requestsPerSecond: 초당 허용 요청 수 (예: 20)
//   - burst: 버스트 허용량 (예: 40)
func newIPRateLimiter(requestsPerSecond int, burst int) *ipRateLimiter {
	return &ipRateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rate:     rate.Limit(requestsPerSecond),
		burst:    burst,
	}
}

// getLimiter 특정 IP의 Rate Limiter를 반환합니다. 없으면 새로 생성합니다.
//
// 동시성 안전하며, Double-Checked Locking 패턴을 사용하여 성능을 최적화합니다.
func (i *ipRateLimiter) getLimiter(ip string) *rate.Limiter {
	// 1. 읽기 락으로 먼저 확인 (성능 최적화)
	i.mu.RLock()
	limiter, exists := i.limiters[ip]
	i.mu.RUnlock()

	if exists {
		return limiter
	}

	// 2. 쓰기 락으로 생성
	i.mu.Lock()
	defer i.mu.Unlock()

	// Double-check: 다른 고루틴이 이미 생성했을 수 있음
	limiter, exists = i.limiters[ip]
	if exists {
		return limiter
	}

	// 3. 메모리 보호: 최대 개수 초과 시 하나 제거
	if len(i.limiters) >= maxIPRateLimiters {
		// Go Map 순회는 랜덤이므로 간이 LRU 효과
		for oldIP := range i.limiters {
			delete(i.limiters, oldIP)
			break
		}
	}

	// 4. 새 Limiter 생성 및 저장
	limiter = rate.NewLimiter(i.rate, i.burst)
	i.limiters[ip] = limiter

	return limiter
}

// RateLimit IP 기반 Rate Limiting 미들웨어를 반환합니다.
//
// Token Bucket 알고리즘을 사용하여 IP별로 요청 속도를 제한합니다.
// 제한 초과 시 HTTP 429 (Too Many Requests)를 반환하고 Retry-After 헤더를 포함합니다.
//
// Parameters:
//   - requestsPerSecond: 초당 허용 요청 수 (양수, 예: 20)
//   - burst: 버스트 허용량 (양수, 예: 40)
//
// Token Bucket 알고리즘:
//   - Rate: 초당 토큰 생성 속도 (requestsPerSecond)
//   - Burst: 버킷 크기 (burst), 최대 저장 가능한 토큰 수
//   - 요청마다 토큰 1개 소비, 부족 시 요청 거부
//
// 사용 예시:
//
//	e := echo.New()
//	e.Use(middleware.RateLimit(20, 40)) // 초당 20 요청, 버스트 40
//
// 주의사항:
//   - 메모리 기반 저장소 (서버 재시작 시 초기화)
//   - 다중 서버 환경에서는 서버별로 독립적인 제한 적용
//
// Panics:
//   - requestsPerSecond 또는 burst가 0 이하인 경우
func RateLimit(requestsPerSecond int, burst int) echo.MiddlewareFunc {
	if requestsPerSecond <= 0 {
		panic(fmt.Sprintf("RateLimit: requestsPerSecond는 양수여야 합니다 (현재값: %d)", requestsPerSecond))
	}
	if burst <= 0 {
		panic(fmt.Sprintf("RateLimit: burst는 양수여야 합니다 (현재값: %d)", burst))
	}

	limiter := newIPRateLimiter(requestsPerSecond, burst)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// 1. 클라이언트 IP 추출
			ip := c.RealIP()

			// 2. IP별 Limiter 가져오기
			ipLimiter := limiter.getLimiter(ip)

			// 3. Rate Limit 확인
			if !ipLimiter.Allow() {
				// 제한 초과 로깅
				applog.WithComponentAndFields(componentRateLimit, applog.Fields{
					"remote_ip": ip,
					"path":      c.Request().URL.Path,
					"method":    c.Request().Method,
				}).Warn("요청 차단: 속도 제한(Rate Limit)을 초과하였습니다")

				// Retry-After 헤더 설정 (1초 후 재시도 권장)
				c.Response().Header().Set(retryAfter, retryAfterSeconds)

				return ErrRateLimitExceeded
			}

			// 4. 다음 핸들러 실행
			return next(c)
		}
	}
}
