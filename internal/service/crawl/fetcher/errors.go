package fetcher

import (
	"fmt"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
)

// HTTP 응답 검증 관련 에러

// newErrUnexpectedHTTPStatus HTTP 상태 코드 검증 과정에서 허용하지 않는 상태 코드가 확인되었을 때 반환하는 에러를 생성합니다.
func newErrUnexpectedHTTPStatus(errType apperrors.ErrorType, status, urlStr string) error {
	return apperrors.New(errType, fmt.Sprintf("HTTP 요청을 처리하는 과정에서 실패하였습니다 (상태 코드: %s, URL: %s)", status, urlStr))
}

// RetryFetcher 관련 에러

// newErrGetBodyFailed 재시도 수행 전 http.Request.GetBody를 호출하여 요청 본문을 복구하는 과정에서 오류가 발생했을 때 반환하는 에러를 생성합니다.
func newErrGetBodyFailed(err error) error {
	return apperrors.Wrap(err, apperrors.InvalidInput, "재시도를 위한 요청 본문 재생성 과정에서 오류가 발생하였습니다")
}

// ErrMaxRetriesExceeded 최대 재시도 횟수 초과 시 반환하는 에러입니다.
var ErrMaxRetriesExceeded = apperrors.New(apperrors.Unavailable, "최대 재시도 횟수를 초과하여 요청 처리에 실패했습니다")

// newErrMaxRetriesExceeded 최대 재시도 횟수 초과 시 반환하는 에러를 생성합니다.
// 원인 에러(cause)가 존재하는 경우 이를 래핑하여 에러 체인을 보존하고, 그렇지 않은 경우 ErrMaxRetriesExceeded 에러를 반환합니다.
func newErrMaxRetriesExceeded(err error) error {
	if err == nil {
		return ErrMaxRetriesExceeded
	}
	return apperrors.Wrap(err, apperrors.Unavailable, ErrMaxRetriesExceeded.Error())
}

// newErrRetryAfterExceeded 서버가 요구한 재시도 대기 시간(Retry-After)이 설정된 최대 재시도 대기 시간을 초과한 경우 반환하는 에러를 생성합니다.
func newErrRetryAfterExceeded(retryAfter, limit string) error {
	return apperrors.New(apperrors.Unavailable, fmt.Sprintf("서버가 요구한 재시도 대기 시간(%s)이 설정된 최대 재시도 대기 시간(%s)을 초과하여 재시도가 중단되었습니다", retryAfter, limit))
}

// MimeTypeFetcher 관련 에러

// ErrMissingResponseContentType Content-Type 헤더가 누락된 경우 반환하는 에러입니다.
var ErrMissingResponseContentType = apperrors.New(apperrors.InvalidInput, "Content-Type 헤더가 누락되어 요청을 처리할 수 없습니다")

// newErrUnsupportedMediaType 지원하지 않는 미디어 타입인 경우 반환하는 에러를 생성합니다.
func newErrUnsupportedMediaType(mediaType string, allowedTypes []string) error {
	return apperrors.New(apperrors.InvalidInput,
		fmt.Sprintf("지원하지 않는 미디어 타입입니다: %s (허용된 타입: %v)", mediaType, allowedTypes))
}

// MaxBytesFetcher 관련 에러

// newErrResponseBodyTooLarge 응답 본문의 크기가 제한을 초과한 경우 반환하는 에러를 생성합니다.
func newErrResponseBodyTooLarge(limit int64) error {
	return apperrors.New(apperrors.InvalidInput,
		fmt.Sprintf("응답 본문의 크기가 설정된 제한을 초과했습니다 (제한값: %d 바이트)", limit))
}

// newErrResponseBodyTooLargeByContentLength Content-Length 헤더에 명시된 응답 본문의 크기가 제한을 초과한 경우 반환하는 에러를 생성합니다.
func newErrResponseBodyTooLargeByContentLength(contentLength, limit int64) error {
	return apperrors.New(apperrors.InvalidInput,
		fmt.Sprintf("Content-Length 헤더에 명시된 응답 본문의 크기가 설정된 제한을 초과했습니다 (값: %d 바이트, 제한값: %d 바이트)", contentLength, limit))
}

// Transport 관련 에러

// ErrUnsupportedTransport 사용자가 제공한 Transport가 표준 *http.Transport 타입이 아닐 때 반환하는 에러입니다.
var ErrUnsupportedTransport = apperrors.New(apperrors.Internal, "지원하지 않는 Transport 형식입니다: 표준 *http.Transport 타입만 설정을 적용할 수 있습니다")

// newErrInvalidProxyURL 제공된 프록시 URL이 유효한 형식이 아니어서 파싱할 수 없을 때 반환하는 에러를 생성합니다.
func newErrInvalidProxyURL(urlStr string) error {
	return apperrors.New(apperrors.InvalidInput, fmt.Sprintf("프록시 URL의 형식이 유효하지 않습니다 (제공된 URL: %s)", urlStr))
}

// newErrIsolatedTransportCreateFailed 격리된 Transport 인스턴스 생성 중 오류가 발생했을 때 반환하는 에러를 생성합니다.
func newErrIsolatedTransportCreateFailed(err error) error {
	errType := apperrors.Internal
	if apperrors.Is(err, apperrors.InvalidInput) {
		errType = apperrors.InvalidInput
	}
	return apperrors.Wrap(err, errType, "격리된 Transport 인스턴스 생성 중 오류가 발생했습니다")
}

// newErrSharedTransportCreateFailed 공유 Transport 리소스 초기화 또는 캐시 조회 중 오류가 발생했을 때 반환하는 에러를 생성합니다.
func newErrSharedTransportCreateFailed(err error) error {
	errType := apperrors.Internal
	if apperrors.Is(err, apperrors.InvalidInput) {
		errType = apperrors.InvalidInput
	}
	return apperrors.Wrap(err, errType, "공유 Transport 리소스 초기화 또는 조회 중 오류가 발생했습니다")
}
