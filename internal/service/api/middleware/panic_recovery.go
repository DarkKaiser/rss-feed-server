package middleware

import (
	"runtime"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/labstack/echo/v4"
)

// componentPanicRecovery 패닉 복구 미들웨어의 로깅용 컴포넌트 이름
const componentPanicRecovery = "api.middleware.panic_recovery"

const (
	// stackBufferSize 스택 트레이스 버퍼 크기 (4KB)
	stackBufferSize = 4 << 10
)

// PanicRecovery 패닉을 복구하고 로깅하는 미들웨어를 반환합니다.
//
// 핸들러에서 발생한 패닉을 복구하여 서버 다운을 방지하고,
// 스택 트레이스와 함께 에러를 로깅한 후 HTTP 500 에러를 반환합니다.
//
// 사용 예시:
//
//	e := echo.New()
//	e.Use(middleware.PanicRecovery())
func PanicRecovery() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			defer func() {
				if r := recover(); r != nil {
					// 1. 패닉 값을 에러로 변환
					err, ok := r.(error)
					if !ok {
						err = NewErrPanicRecovered(r)
					}

					// 2. 스택 트레이스 수집
					stack := make([]byte, stackBufferSize)
					length := runtime.Stack(stack, false)

					// 3. 로깅 필드 구성
					fields := applog.Fields{
						"error": err,
						"stack": string(stack[:length]),
					}

					// Request ID 추가 (있는 경우)
					if requestID := c.Response().Header().Get(echo.HeaderXRequestID); requestID != "" {
						fields["request_id"] = requestID
					}

					// 4. 패닉 로그 기록
					applog.WithComponentAndFields(componentPanicRecovery, fields).Error("패닉 복구: 예기치 못한 오류가 발생하여 안전하게 복구했습니다")

					// 5. Echo 에러 핸들러로 전달 (HTTP 500 응답)
					c.Error(err)
				}
			}()

			return next(c)
		}
	}
}
