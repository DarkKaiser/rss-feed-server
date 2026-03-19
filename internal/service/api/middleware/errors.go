package middleware

import (
	"fmt"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/service/api/httputil"
)

var (
	// ErrRateLimitExceeded 허용된 요청 빈도를 초과한 클라이언트에게 반환할 표준 HTTP 429(Too Many Requests) 에러입니다.
	ErrRateLimitExceeded = httputil.NewTooManyRequestsError("요청이 너무 많습니다. 잠시 후 다시 시도해주세요")
)

// NewErrPanicRecovered 캡처된 패닉 값을 내부 시스템 오류로 래핑하여 새로운 에러를 생성합니다.
func NewErrPanicRecovered(r any) error {
	return apperrors.New(apperrors.Internal, fmt.Sprintf("%v", r))
}
