package httputil

import (
	"net/http"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/labstack/echo/v4"
)

// component 로깅용 컴포넌트 이름
const component = "api.error_handler"

// ErrorHandler Echo 프레임워크의 전역 에러 핸들러입니다.
func ErrorHandler(err error, c echo.Context) {
	code := http.StatusInternalServerError
	message := "내부 서버 오류가 발생했습니다"

	if he, ok := err.(*echo.HTTPError); ok {
		code = he.Code
		if msg, ok := he.Message.(string); ok {
			message = msg
		}
	}

	if code == http.StatusNotFound {
		if message == "Not Found" || message == "" {
			message = "요청한 리소스를 찾을 수 없습니다"
		}
	}

	// 에러 로깅
	fields := applog.Fields{
		"path":        c.Request().URL.Path,
		"method":      c.Request().Method,
		"status_code": code,
		"error":       err,
		"remote_ip":   c.RealIP(),
	}

	if code >= http.StatusInternalServerError {
		applog.WithComponentAndFields(component, fields).Error("HTTP 5xx: 서버 내부 오류")
	} else if code >= http.StatusBadRequest {
		applog.WithComponentAndFields(component, fields).Warn("HTTP 4xx: 클라이언트 요청 오류")
	}

	if c.Response().Committed {
		return
	}

	if c.Request().Method == http.MethodHead {
		c.NoContent(code)
		return
	}

	c.String(code, message)
}
