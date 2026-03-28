package fetcher

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	applog "github.com/darkkaiser/notify-server/pkg/log"
)

const (
	// minAllowedRetries 허용 가능한 최소 재시도 횟수입니다. (0: 재시도 안 함)
	minAllowedRetries = 0

	// maxAllowedRetries 허용 가능한 최대 재시도 횟수입니다.
	maxAllowedRetries = 10

	// defaultMaxRetryDelay 사용자가 재시도 대기 시간의 최대값을 지정하지 않았을 때 사용되는 기본값(30초)입니다.
	defaultMaxRetryDelay = 30 * time.Second
)

// RetryFetcher HTTP 요청 실패 시 자동으로 재시도를 수행하는 미들웨어입니다.
//
// 주요 특징:
//   - 지수 백오프(Exponential Backoff): 재시도 간격을 지수적으로 증가시켜 서버 부하를 분산
//   - Jitter: 무작위 지연을 추가하여 동시 다발적인 재시도로 인한 "Thundering Herd" 문제 방지
//   - Retry-After 헤더 지원: 서버가 명시한 재시도 시간을 우선적으로 준수
//   - 컨텍스트 취소 감지: 사용자 요청 취소 시 즉시 재시도 중단
type RetryFetcher struct {
	delegate Fetcher

	// maxRetries 최대 재시도 횟수입니다.
	//
	// 이 값은 normalizeMaxRetries 함수를 통해 정규화되며,
	// minAllowedRetries(0) ~ maxAllowedRetries(10) 사이의 값만 저장됩니다.
	maxRetries int

	// minRetryDelay 재시도 대기 시간의 최소값입니다. (지수 백오프의 시작점)
	//
	// 이 값은 normalizeRetryDelays 함수를 통해 정규화되며, 항상 1초 이상의 값으로 보정됩니다.
	minRetryDelay time.Duration

	// maxRetryDelay 재시도 대기 시간의 최대값입니다. (지수 백오프 증가 시 상한선)
	//
	// 이 값은 normalizeRetryDelays 함수를 통해 정규화되며, 항상 minRetryDelay 이상의 값으로 보정됩니다.
	maxRetryDelay time.Duration
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ Fetcher = (*RetryFetcher)(nil)

// NewRetryFetcher 새로운 RetryFetcher 인스턴스를 생성합니다.
func NewRetryFetcher(delegate Fetcher, maxRetries int, minRetryDelay time.Duration, maxRetryDelay time.Duration) *RetryFetcher {
	// 최대 재시도 횟수 정규화
	maxRetries = normalizeMaxRetries(maxRetries)

	// 재시도 대기 시간 정규화
	minRetryDelay, maxRetryDelay = normalizeRetryDelays(minRetryDelay, maxRetryDelay)

	return &RetryFetcher{
		delegate:      delegate,
		maxRetries:    maxRetries,
		minRetryDelay: minRetryDelay,
		maxRetryDelay: maxRetryDelay,
	}
}

// Do HTTP 요청을 수행하며, 실패 시 설정된 정책에 따라 자동으로 재시도합니다.
//
// 재시도 전략:
//  1. 지수 백오프(Exponential Backoff): 재시도 간격을 지수적으로 증가 (예: 1초 → 2초 → 4초 → 8초 → ...)
//     - 공식: delay = minRetryDelay * 2^(retry-1)
//     - 재시도 대기 시간의 최대값(maxRetryDelay)을 초과하지 않도록 제한
//  2. Full Jitter: 계산된 재시도 대기 시간 범위 내에서 무작위 값 선택 (0 ~ delay)
//     - Thundering Herd 문제 방지: 여러 클라이언트가 동시에 재시도하는 것을 분산
//  3. Retry-After 헤더 우선 처리:
//     - 서버가 Retry-After 헤더를 제공하면 해당 시간을 우선 사용
//     - 단, 재시도 대기 시간의 최대값(maxRetryDelay)을 초과하면 재시도하지 않고 에러 반환
//  4. 멱등성 검증:
//     - GET, HEAD, PUT, DELETE, OPTIONS, TRACE: 재시도 허용
//     - POST, PATCH: 재시도 제외 (데이터 중복 생성/수정 위험)
//  5. 컨텍스트 취소 감지:
//     - 대기 중 컨텍스트가 취소되면 즉시 재시도 중단
//
// 재시도 대상:
//   - 네트워크 오류 (타임아웃, 연결 실패 등)
//   - 5xx 서버 에러 (단, 501/505/511 제외)
//   - 429 Too Many Requests
//   - 408 Request Timeout
//
// 재시도 제외:
//   - 컨텍스트 취소 (context.Canceled)
//   - 4xx 클라이언트 에러 (400, 401, 403, 404 등)
//   - 비즈니스 로직 에러 (apperrors.ExecutionFailed, InvalidInput, Forbidden, NotFound)
//
// 매개변수:
//   - req: 처리할 HTTP 요청 객체
//
// 반환값:
//   - HTTP 응답 객체 (성공 시)
//   - 에러 (최대 재시도 횟수 초과 또는 재시도 불가능한 에러 발생 시)
//
// 주의사항:
//   - 요청 객체의 Body가 있는 경우 반드시 GetBody 필드를 설정해야 합니다.
//   - 비멱등 메서드(POST, PATCH)는 자동으로 재시도가 제외됩니다.
//   - 반환된 응답 객체의 Body는 호출자가 반드시 닫아야 합니다.
func (f *RetryFetcher) Do(req *http.Request) (*http.Response, error) {
	effectiveMaxRetries := f.maxRetries

	// [사전 검증 1] 멱등성 확인
	// 비멱등 메서드(POST, PATCH)는 재시도 시 데이터 중복 생성/수정 위험이 있으므로 재시도 비활성화!!
	isIdempotent := isIdempotentMethod(req.Method)
	if !isIdempotent {
		effectiveMaxRetries = 0
	}

	// [사전 검증 2] 요청 객체의 Body 재구성 가능 여부 확인
	// 재시도 시 요청 객체의 Body를 다시 읽어야 하므로, GetBody가 없으면 데이터 유실 위험이 있습니다.
	// 따라서 재시도 기능만 비활성화하고 요청 처리는 계속 진행합니다.
	if req.Body != nil && req.GetBody == nil && f.maxRetries > 0 {
		applog.WithComponent(component).WithContext(req.Context()).WithFields(applog.Fields{
			"url":            redactURL(req.URL),
			"method":         req.Method,
			"max_retries":    f.maxRetries,
			"content_length": req.ContentLength,
		}).Warn("재시도 비활성화: 요청 본문 재생성 불가 (GetBody nil)")

		effectiveMaxRetries = 0
	}

	var lastErr error           // 마지막 시도에서 발생한 에러
	var lastResp *http.Response // 마지막 시도에서 받은 응답

	// [재시도 루프] 첫 번째 시도와 재시도를 포함하여 최대 `effectiveMaxRetries + 1`회 반복합니다.
	for i := 0; i <= effectiveMaxRetries; i++ {
		// [재시도 대기]
		// 첫 번째 시도(i=0)가 실패한 경우, 다음 시도 전에 일정 시간 대기합니다.
		if i > 0 {
			// [단계 1: 지수 백오프(Exponential Backoff) 계산]
			// 재시도 횟수가 늘어날수록 대기 시간을 2배씩 증가시켜 서버 부하를 줄입니다.
			// 예: 1초 -> 2초 -> 4초 -> 8초 ... (설정된 최소 재시도 대기 시간 ~ 최대 재시도 대기 시간 범위 내)
			delay := f.minRetryDelay * time.Duration(1<<(i-1))
			if delay > f.maxRetryDelay {
				delay = f.maxRetryDelay
			}

			// [단계 2: 지터(Jitter) 적용]
			// 모든 클라이언트가 동시에 재시도하는 것을 방지하기 위해 무작위성을 추가합니다.
			// 0 ~ 계산된 delay 사이의 값을 무작위로 선택합니다.
			if delay > 0 {
				delay = time.Duration(rand.Int64N(int64(delay) + 1))
			}

			// [단계 3: Retry-After 헤더 우선 적용]
			// 서버가 응답 헤더(Retry-After)를 통해 재시도 가능한 시점을 명시한 경우,
			// 계산된 지수 백오프 시간 대신 해당 값을 우선적으로 사용하여 서버의 요청 제어 정책을 준수합니다.
			// 단, 요구된 대기 시간이 설정된 최대 재시도 대기 시간(maxRetryDelay)을 초과하는 경우는,
			// 과도한 지연을 방지하기 위해 재시도를 포기하고 즉시 에러를 반환합니다.
			var retryAfter string
			var explicitDelayFound bool // 서버가 명시한 재시도 대기 시간(Retry-After) 확보 여부

			if lastResp != nil {
				retryAfter = lastResp.Header.Get("Retry-After")
			} else if lastErr != nil {
				// HTTPStatusError에 포함된 헤더에서도 Retry-After 확인
				var statusErr *HTTPStatusError
				if errors.As(lastErr, &statusErr) {
					retryAfter = statusErr.Header.Get("Retry-After")
				}
			}

			if retryAfter != "" {
				if retryAfterDelay, ok := parseRetryAfter(retryAfter); ok {
					// Retry-After 값이 설정된 최대 재시도 대기 시간(maxRetryDelay)을 초과하는 경우입니다.
					// 이를 공격적인 재시도로 간주할 수 있으므로, 요청을 중단하고 에러를 반환합니다.
					if retryAfterDelay > f.maxRetryDelay {
						if lastResp != nil && lastResp.Body != nil {
							// 커넥션 재사용을 위해 응답 객체의 Body를 안전하게 비우고 닫음
							drainAndCloseBody(lastResp.Body)
						}

						return nil, newErrRetryAfterExceeded(retryAfterDelay.String(), f.maxRetryDelay.String())
					}

					// 서버가 명시한 Retry-After 값이 유효하고 정책 범위 내에 있으므로,
					// 앞서 계산한 지수 백오프 및 지터 값을 무시하고 이 값을 최종 대기 시간으로 적용합니다. (0초 가능)
					delay = retryAfterDelay

					// 서버가 명시한 재시도 대기 시간(Retry-After)을 확보했음을 기록 (최소 재시도 대기 시간 보장 로직 건너뛰기 위함)
					explicitDelayFound = true
				}
			}

			// [단계 4: 최소 재시도 대기 시간 보장]
			// 서버가 명시한 재시도 대기 시간(Retry-After)이 없는 경우, 계산된 대기 시간(지터 포함)이 너무 짧으면 서버에 부담이 될 수 있습니다.
			// 따라서 최소한의 대기 시간(1ms 미만인 경우 minRetryDelay로 보정)을 보장하여 너무 빠른 재시도를 방지합니다.
			if !explicitDelayFound {
				if delay < time.Millisecond {
					delay = f.minRetryDelay
				}
			}

			// [단계 5: 로깅]
			// 재시도 대기 시작을 알리는 경고 로그를 출력합니다.
			fields := applog.Fields{
				"url":               redactURL(req.URL),
				"retry":             i,
				"max_retries":       f.maxRetries,
				"remaining_retries": effectiveMaxRetries - i,
				"delay":             delay.String(),
			}

			var retryReason string
			if lastErr != nil {
				fields["error"] = lastErr.Error()

				retryReason = "network_error"
			}
			if lastResp != nil {
				fields["status_code"] = lastResp.StatusCode

				if retryReason == "" {
					retryReason = fmt.Sprintf("status_code_%d", lastResp.StatusCode)
				}
			}
			if retryReason != "" {
				fields["retry_reason"] = retryReason
			}
			if retryAfter != "" {
				fields["retry_after_header"] = retryAfter
			}

			applog.WithComponent(component).
				WithContext(req.Context()).
				WithFields(fields).
				Warn("재시도 대기 중: 일시적 오류로 인해 요청 재시도를 준비합니다")

			// [단계 6: 재시도 대기 및 취소 감지]
			// 계산된 시간만큼 대기하되, 요청이 취소되면 즉시 중단합니다.
			timer := time.NewTimer(delay)

			select {
			case <-req.Context().Done():
				// 타이머를 중지하고 채널을 비움 (리소스 정리)
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}

				// 컨텍스트 취소 시 리소스 정리
				if lastResp != nil && lastResp.Body != nil {
					// 컨텍스트가 취소된 경우, 빠른 반환을 위해 drain 과정을 생략하고 즉시 닫습니다.
					// (커넥션 재사용보다 응답성 우선)
					lastResp.Body.Close()
				}

				return nil, req.Context().Err()

			case <-timer.C:
				// 대기 완료: 설정된 대기 시간이 경과하였으므로, 재시도 로직(Body 재생성 등)을 진행합니다.
			}
		}

		// [요청 본문 재생성] 이전 시도에서 소진된 Body를 다시 읽을 수 있도록 복구
		// GetBody를 통해 새로운 Body를 생성하고, 원본 요청 객체를 변경하지 않기 위해 복제본 사용
		if i > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				// GetBody 실패 시 더 이상 재시도 불가 (데이터 무결성 보호)
				if lastResp != nil && lastResp.Body != nil {
					// 커넥션 재사용을 위해 응답 객체의 Body를 안전하게 비우고 닫음
					drainAndCloseBody(lastResp.Body)
				}

				return nil, newErrGetBodyFailed(err)
			}

			// 원본 요청 객체를 변경하지 않기 위해 복제본 생성 (방어적 프로그래밍)
			req = req.Clone(req.Context())
			req.Body = body
		}

		// [HTTP 요청 실행]
		resp, err := f.delegate.Do(req)
		lastResp = resp

		// [재시도 여부 판단]
		// 에러 유무와 응답 객체의 상태 코드를 기반으로 재시도 수행 여부를 결정합니다.
		//
		// 1. 응답 객체의 상태 코드 검사 (응답이 있는 경우):
		//    - 429 (Too Many Requests), 408 (Request Timeout): 무조건 재시도 대상입니다.
		//    - 5xx (Server Errors): 501, 505, 511을 제외하고 재시도 대상입니다.
		//
		// 2. 에러 검사 (에러가 있는 경우):
		//    - isRetriable 함수를 통해 네트워크 오류나 일시적 장애인지 확인합니다.
		shouldRetry := false

		// 응답 객체의 상태 코드 검사 (응답이 있는 경우)
		// =========================================================================================
		// [주의: 방어적 코드 유지]
		// 현재 미들웨어 체인 구성상 StatusCodeFetcher가 먼저 실행되어 5xx 등의 에러 응답은
		// 이미 error로 변환되어 전달됩니다. (Unreachable Code 가능성 있음)
		//
		// 하지만 추후 미들웨어 순서 변경이나 StatusCodeFetcher가 없는 환경에서의 독립적 사용을
		// 대비하여 아래 로직을 '의도적으로 유지'합니다.
		// =========================================================================================
		if resp != nil {
			if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout {
				shouldRetry = true
			} else if resp.StatusCode >= 500 {
				// 501: Not Implemented, 505: HTTP Version Not Supported, 511: Network Authentication Required는
				// 영구적인 문제이므로 재시도해도 성공할 가능성이 낮음!
				switch resp.StatusCode {
				case http.StatusNotImplemented, http.StatusHTTPVersionNotSupported, http.StatusNetworkAuthenticationRequired:
					shouldRetry = false

				default:
					shouldRetry = true
				}
			}
		}

		// 에러 검사 (에러가 있는 경우)
		if err != nil {
			// [에러 처리 1: 컨텍스트 타임아웃 확인]
			// 전체 요청 제한 시간(Deadline)이 초과된 경우, 재시도를 해도 성공할 수 없으므로 즉시 중단합니다.
			if errors.Is(err, context.DeadlineExceeded) && req.Context().Err() != nil {
				if resp != nil && resp.Body != nil {
					// 컨텍스트 타임아웃 발생 시 빠른 반환을 위해 drain 과정을 생략하고 즉시 닫습니다.
					// (커넥션 재사용보다 응답성 우선)
					resp.Body.Close()
				}

				return nil, err
			}

			// [에러 처리 2: 재시도 가능성 판단]
			// 발생한 에러가 일시적인 오류(네트워크 지연, 서버 과부하 등)인지, 아니면 영구적인 문제인지 확인합니다.
			// isRetriable 함수가 true를 반환해야만 재시도를 수행합니다.
			if !isRetriable(err) {
				if resp != nil && resp.Body != nil {
					if errors.Is(err, context.Canceled) {
						// 컨텍스트가 취소된 경우, 빠른 반환을 위해 drain 과정을 생략하고 즉시 닫습니다.
						// (커넥션 재사용보다 응답성 우선)
						resp.Body.Close()
					} else {
						// 그 외 에러는 커넥션 재사용을 위해 응답 객체의 Body를 안전하게 비우고 닫음
						drainAndCloseBody(resp.Body)
					}
				}

				return nil, err
			}
		} else if !shouldRetry {
			// [재시도 루프 종료]
			// 현재 응답이 재시도 대상이 아니라고 판단되었으므로 결과를 반환하고 종료합니다.
			//
			// 반환되는 경우:
			// 1. 요청이 성공적으로 처리된 경우 (예: 2xx Success)
			// 2. 재시도를 해도 해결되지 않는 영구적인 오류인 경우 (예: 501 Not Implemented)
			return resp, nil
		}

		// [재시도 준비: 상태 저장 및 리소스 정리]
		// 다음 재시도를 수행하기 위해 현재 에러를 저장하고,
		// 커넥션 누수를 방지하기 위해 사용이 끝난 응답 객체의 Body를 닫습니다.
		lastErr = err
		if resp != nil {
			if i == effectiveMaxRetries {
				// [최종 실패: 모든 재시도 횟수 소진]
				// 마지막 재시도까지 실패했으므로, 상세한 디버깅 정보를 포함하여 에러를 반환합니다.
				finalErr := lastErr
				if finalErr == nil {
					// 디버깅 편의를 위해 응답 객체의 Body 일부만 읽어서 에러 객체에 포함시킵니다.
					var bodySnippet string
					if resp.Body != nil {
						lr := io.LimitReader(resp.Body, 4096)
						bodyBytes, _ := io.ReadAll(lr)
						if len(bodyBytes) > 0 {
							bodySnippet = string(bodyBytes)
						}
					}

					// 네트워크 오류는 없었으나, 서버가 재시도 대상 상태 코드(예: 429, 5xx)를 지속적으로 반환하여 실패한 경우입니다.
					// 응답의 상세 정보(상태 코드, 헤더, 본문 등)를 포함한 HTTPStatusError를 생성합니다.
					finalErr = &HTTPStatusError{
						StatusCode:  resp.StatusCode,
						Status:      resp.Status,
						URL:         redactURL(req.URL),
						Header:      redactHeaders(resp.Header),
						BodySnippet: bodySnippet,
						Cause:       ErrMaxRetriesExceeded,
					}
				} else {
					finalErr = newErrMaxRetriesExceeded(finalErr)
				}

				// 커넥션 재사용을 위해 응답 객체의 Body를 안전하게 비우고 닫음
				drainAndCloseBody(resp.Body)

				return nil, finalErr
			}

			// 커넥션 재사용을 위해 응답 객체의 Body를 안전하게 비우고 닫음
			drainAndCloseBody(resp.Body)
		}
	}

	// [최종 실패: 응답 없음]
	// 모든 재시도 횟수를 소진했으나, 서버로부터 응답을 받지 못한 경우(예: 타임아웃, 연결 거부)입니다.
	// 참고: 응답이 있는 실패(예: 5xx 에러)는 루프 내부에서 이미 처리되어 반환되었습니다.
	// 응답이 없는 경우(resp == nil)는 루프가 종료된 후 이 지점에서 에러를 반환합니다.
	return nil, newErrMaxRetriesExceeded(lastErr)
}

func (f *RetryFetcher) Close() error {
	return f.delegate.Close()
}

// normalizeMaxRetries 최대 재시도 횟수를 정규화합니다.
//
// 정규화 규칙:
//   - 허용 범위 미만: 최소값으로 보정
//   - 허용 범위 초과: 최대값으로 제한
//   - 허용 범위 내: 그대로 유지
//
// 동작 방식:
//   - 0: 재시도 안 함
//   - 1~10: 지정된 횟수만큼 재시도
func normalizeMaxRetries(maxRetries int) int {
	// 허용 범위 미만의 값은 최소값으로 보정
	if maxRetries < minAllowedRetries {
		return minAllowedRetries
	}

	// 허용 범위 초과 시 최대값으로 제한 (과도한 재시도로 인한 지연 방지)
	if maxRetries > maxAllowedRetries {
		return maxAllowedRetries
	}

	return maxRetries
}

// normalizeRetryDelays 재시도 대기 시간의 최소값과 최대값을 정규화합니다.
//
// 정규화 규칙:
//   - minRetryDelay 1초 미만: 1초로 보정
//   - maxRetryDelay 0: 기본값(30초)으로 보정
//   - maxRetryDelay < minRetryDelay: minRetryDelay로 보정
//
// 동작 방식:
//   - minRetryDelay: 지수 백오프 시작값
//   - maxRetryDelay: 지수 백오프 상한값
func normalizeRetryDelays(minRetryDelay, maxRetryDelay time.Duration) (time.Duration, time.Duration) {
	if minRetryDelay < time.Second {
		// 너무 짧은 대기 시간(1초 미만)은 서버에 부담을 줄 수 있으므로 1초로 보정
		minRetryDelay = 1 * time.Second
	}

	if maxRetryDelay == 0 {
		// 값을 설정하지 않았거나 0으로 잘못 설정된 경우 기본값 적용
		maxRetryDelay = defaultMaxRetryDelay
	}

	// 최대 재시도 대기 시간(maxRetryDelay)은 최소 재시도 대기 시간(minRetryDelay)보다 작을 수 없음!
	if maxRetryDelay < minRetryDelay {
		// 설정값 오류(max < min) 또는 기본값이 min보다 작은 경우 minRetryDelay로 자동 보정
		maxRetryDelay = minRetryDelay
	}

	return minRetryDelay, maxRetryDelay
}

// isRetriable 발생한 에러가 재시도 가능한 일시적인 오류인지 판단합니다.
//
// 이 함수는 HTTP 요청 실패 시 재시도 여부를 결정하는 핵심 로직입니다.
// 일시적인 네트워크 오류나 서버 과부하는 재시도 대상이지만,
// 클라이언트 에러나 영구적인 설정 오류는 재시도해도 성공할 가능성이 없으므로 제외합니다.
//
// 재시도 대상:
//   - 네트워크 타임아웃 (net.Error.Timeout())
//   - 일시적인 네트워크 연결 오류
//   - 서버 일시적 오류 (apperrors.Unavailable)
//   - 분류되지 않은 일반 에러 (비즈니스 로직 에러가 아닌 경우)
//
// 재시도 제외:
//   - 컨텍스트 취소 (context.Canceled): 사용자의 명시적 취소 의도
//   - SSL/TLS 인증서 오류: 영구적 보안 문제
//   - URL 형식 오류: 잘못된 URL 스킴, 제어 문자 등
//   - 리다이렉트 제한 초과: 무한 리다이렉트 방지
//   - 비즈니스 로직 에러: ExecutionFailed, InvalidInput, Forbidden, NotFound
//
// 매개변수:
//   - err: 판단할 에러 객체
//
// 반환값:
//   - bool: 재시도 가능 여부 (true: 재시도 가능, false: 재시도 불가)
//
// 주의사항:
//   - context.DeadlineExceeded는 net.Error.Timeout()으로 감지되어 재시도 대상으로 분류됨
//   - nil 에러는 재시도 불필요 (false 반환)
func isRetriable(err error) bool {
	if err == nil {
		return false
	}

	// [검사 1] 컨텍스트 취소 확인
	// context.Canceled는 사용자가 명시적으로 요청을 취소한 것이므로 재시도 제외!
	// 주의: context.DeadlineExceeded는 HTTP 클라이언트 타임아웃 시에도 발생하므로,
	// 여기서 처리하지 않고 net.Error 검사 단계에서 확인합니다.
	if errors.Is(err, context.Canceled) {
		return false
	}

	// [검사 2] URL 에러 확인
	// url.Error는 HTTP 요청 과정에서 발생하는 다양한 하위 에러를 래핑합니다.
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		// 재시도해도 해결되지 않는 URL 관련 오류는 즉시 중단합니다.
		switch urlErr.Err.Error() {
		case "stopped after 10 redirects": // http.Client의 기본 리다이렉트 정책(최대 10회) 초과
			return false

		case "invalid control character in URL": // 잘못된 URL 형식
			return false
		}

		// 지원하지 않는 프로토콜 스킴
		// HTTP 클라이언트는 http/https만 지원하므로 재시도 불필요
		if strings.Contains(urlErr.Error(), "unsupported protocol scheme") {
			return false
		}
	}

	// [검사 3] SSL/TLS 인증서 에러 확인
	// 인증서 에러(유효기간 만료, 신뢰할 수 없는 CA 등)는 재시도해도 해결되지 않는 문제로 간주!
	var x509HostnameErr x509.HostnameError                     // 인증서의 호스트명 불일치
	var x509UnknownAuthorityErr x509.UnknownAuthorityError     // 신뢰할 수 없는 인증 기관
	var x509CertificateInvalidErr x509.CertificateInvalidError // 만료되었거나 유효하지 않은 인증서
	if errors.As(err, &x509HostnameErr) || errors.As(err, &x509UnknownAuthorityErr) || errors.As(err, &x509CertificateInvalidErr) {
		return false
	}

	// [검사 4] 네트워크 일시적 오류 확인
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			// 타임아웃은 일시적인 네트워크 지연으로 간주하여 재시도
			return true
		}

		// 타임아웃이 아닌 네트워크 에러라도 일시적일 수 있으므로,
		// 즉시 false를 반환하지 않고 후속 검사(apperrors)로 넘김
	}

	// [검사 5] 서버 측 일시적 오류 확인
	// apperrors.Unavailable은 서버가 일시적으로 요청을 처리할 수 없는 상태를 나타냅니다.
	// (예: 5xx 서버 에러, 429 Too Many Requests, 503 Service Unavailable 등)
	// 단, 501(Not Implemented), 505(HTTP Version Not Supported), 511(Network Authentication Required)은
	// 영구적인 설정 문제이므로 재시도 대상에서 제외합니다.
	if apperrors.Is(err, apperrors.Unavailable) {
		// HTTP 상태 코드를 확인하여 재시도 제외 대상(501, 505, 511)인지 판별!
		var statusErr *HTTPStatusError
		if errors.As(err, &statusErr) {
			switch statusErr.StatusCode {
			case http.StatusNotImplemented, // 501: 서버가 기능 미지원 (영구적)
				http.StatusHTTPVersionNotSupported,       // 505: HTTP 버전 미지원 (영구적)
				http.StatusNetworkAuthenticationRequired: // 511: 네트워크 인증 필요 (사용자 개입 필요)
				return false
			}
		}

		// 서버가 일시적으로 요청을 처리할 수 없는 상태이므로 재시도합니다.
		return true
	}

	// [검사 6] 비즈니스 로직 에러 확인
	// 명확한 비즈니스 로직 에러는 재시도해도 동일한 결과가 나오므로 재시도 제외!
	if apperrors.Is(err, apperrors.ExecutionFailed /* 서버 내부 비즈니스 로직 실패 */) ||
		apperrors.Is(err, apperrors.InvalidInput /* 잘못된 요청 파라미터 (400 Bad Request) */) ||
		apperrors.Is(err, apperrors.Forbidden /* 권한 없음 (403 Forbidden) */) ||
		apperrors.Is(err, apperrors.NotFound /* 리소스 없음 (404 Not Found) */) {
		return false
	}

	// 모든 재시도 제외 조건을 통과했으므로 재시도를 허용합니다.
	// 명확한 실패 사유가 없다면, 일시적 오류(네트워크 문제 등)로 간주하고 재시도합니다.
	// 예: DNS 조회 실패, 연결 거부, 네트워크 단절 등
	return true
}

// isIdempotentMethod 지정된 HTTP 메서드가 멱등한지(재시도가 안전한지) 여부를 반환합니다.
//
// 멱등성(Idempotency)이란 동일한 요청을 여러 번 수행해도 결과가 동일함을 의미합니다.
// 멱등한 메서드는 재시도해도 데이터 중복 생성/수정 위험이 없으므로 안전하게 재시도할 수 있습니다.
//
// 멱등 메서드 (재시도 대상):
//   - GET, HEAD, OPTIONS, TRACE: 서버 상태를 변경하지 않는 안전한 메서드
//   - PUT, DELETE: 여러 번 수행해도 최종 결과가 동일한 메서드
//     · PUT: 동일한 리소스를 동일한 값으로 여러 번 업데이트해도 결과 동일
//     · DELETE: 이미 삭제된 리소스를 다시 삭제해도 결과 동일 (404 또는 성공)
//
// 비멱등 메서드 (재시도 제외):
//   - POST: 새로운 리소스 생성 시 중복 생성 위험
//   - PATCH: 부분 수정 시 중복 적용 위험
//
// 참고: RFC 7231 Section 4.2.2 (Idempotent Methods)
func isIdempotentMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace, http.MethodPut, http.MethodDelete:
		return true

	default:
		return false
	}
}

// parseRetryAfter Retry-After 헤더 값을 파싱하여 대기해야 할 시간을 반환합니다.
//
// Retry-After 헤더는 서버가 클라이언트에게 "언제 다시 요청하면 되는지"를 알려주는 표준 HTTP 헤더입니다.
// 주로 429 Too Many Requests 또는 503 Service Unavailable 응답과 함께 사용됩니다.
//
// 지원 형식 (RFC 7231 Section 7.1.3):
//  1. 초 단위 정수: "120" → 120초 후 재시도
//  2. HTTP-date 형식: "Wed, 21 Oct 2015 07:28:00 GMT" → 해당 시각까지 대기
//
// 매개변수:
//   - value: Retry-After 헤더 값 (예: "60" 또는 "Wed, 21 Oct 2015 07:28:00 GMT")
//
// 반환값:
//   - time.Duration: 대기해야 할 시간 (초 단위 정수)
//   - bool: 파싱 성공 여부 (true: 성공, false: 실패 - 실패 시 duration은 0)
func parseRetryAfter(value string) (time.Duration, bool) {
	if value == "" {
		return 0, false
	}

	// [형식 1] 초 단위 정수 파싱 (예: "120")
	var seconds int
	if _, err := fmt.Sscanf(value, "%d", &seconds); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second, true
	}

	// [형식 2] HTTP-date 형식 파싱 (예: "Wed, 21 Oct 2015 07:28:00 GMT")
	if date, err := http.ParseTime(value); err == nil {
		duration := time.Until(date)
		if duration < 0 {
			// 과거 시간이면 즉시 재시도 (0초 대기)
			// 서버 시간과 클라이언트 시간 차이로 인해 발생 가능
			duration = 0
		}

		return duration, true
	}

	return 0, false
}
