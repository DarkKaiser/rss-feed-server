package httputil

import (
	"net/http"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/rss-feed-server/internal/service/api/model/response"
	"github.com/labstack/echo/v4"
)

// component 에러 핸들러의 로깅용 컴포넌트 이름
const component = "api.error_handler"

// ErrorHandler Echo 프레임워크의 전역 에러 핸들러입니다.
//
// 모든 HTTP 에러를 가로채서 표준 ErrorResponse JSON 형식으로 변환하여 반환합니다.
// 에러 발생 시 적절한 로그 레벨(Error/Warn)로 상세 정보를 기록합니다.
func ErrorHandler(err error, c echo.Context) {
	code := http.StatusInternalServerError
	message := "내부 서버 오류가 발생했습니다"

	// Echo HTTPError 타입 확인
	if he, ok := err.(*echo.HTTPError); ok {
		code = he.Code
		if msg, ok := he.Message.(string); ok {
			message = msg
		} else if resp, ok := he.Message.(response.ErrorResponse); ok {
			message = resp.Message
		}
	}

	// 404 에러는 사용자 친화적인 한국어 메시지로 통일
	// 단, 비즈니스 로직에서 설정한 구체적인 에러 메시지가 있다면 보존합니다.
	if code == http.StatusNotFound {
		// 기본 404 메시지이거나 빈 메시지인 경우에만 덮어씀
		if message == "Not Found" || message == "" {
			message = "요청한 리소스를 찾을 수 없습니다"
		}
	}

	// 에러 로깅 (보안 및 디버깅 용도)
	fields := applog.Fields{
		"path":        c.Request().URL.Path,
		"method":      c.Request().Method,
		"status_code": code,
		"error":       err,
		"remote_ip":   c.RealIP(),
		"request_id":  c.Response().Header().Get(echo.HeaderXRequestID),
	}

	if code >= http.StatusInternalServerError {
		// 5xx: 서버 내부 오류 - 즉시 조치 필요
		applog.WithComponentAndFields(component, fields).Error("HTTP 5xx: 서버 내부 오류")
	} else if code >= http.StatusBadRequest {
		// 4xx: 클라이언트 요청 오류 - 정상적인 거부 응답
		applog.WithComponentAndFields(component, fields).Warn("HTTP 4xx: 클라이언트 요청 오류")
	}

	// 이중 응답 방지: 이미 응답이 전송된 경우 추가 응답 시도하지 않음
	if c.Response().Committed {
		return
	}

	// HEAD 요청 처리: HTTP 명세에 따라 헤더만 반환하고 본문은 생략
	if c.Request().Method == http.MethodHead {
		c.NoContent(code)
		return
	}

	// 일반 요청: 표준 ErrorResponse JSON 형식으로 응답
	c.JSON(code, response.ErrorResponse{
		ResultCode: code,
		Message:    message,
	})
}
