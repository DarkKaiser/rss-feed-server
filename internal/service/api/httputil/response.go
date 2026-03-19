package httputil

import (
	"net/http"

	"github.com/darkkaiser/rss-feed-server/internal/service/api/model/response"
	"github.com/labstack/echo/v4"
)

// NewBadRequestError 400 Bad Request 에러를 생성합니다
func NewBadRequestError(message string) error {
	return echo.NewHTTPError(http.StatusBadRequest, response.ErrorResponse{
		ResultCode: http.StatusBadRequest,
		Message:    message,
	})
}

// NewUnauthorizedError 401 Unauthorized 에러를 생성합니다
func NewUnauthorizedError(message string) error {
	return echo.NewHTTPError(http.StatusUnauthorized, response.ErrorResponse{
		ResultCode: http.StatusUnauthorized,
		Message:    message,
	})
}

// NewNotFoundError 404 Not Found 에러를 생성합니다
func NewNotFoundError(message string) error {
	return echo.NewHTTPError(http.StatusNotFound, response.ErrorResponse{
		ResultCode: http.StatusNotFound,
		Message:    message,
	})
}

// NewTooManyRequestsError 429 Too Many Requests 에러를 생성합니다
func NewTooManyRequestsError(message string) error {
	return echo.NewHTTPError(http.StatusTooManyRequests, response.ErrorResponse{
		ResultCode: http.StatusTooManyRequests,
		Message:    message,
	})
}

// NewInternalServerError 500 Internal Server Error 에러를 생성합니다
func NewInternalServerError(message string) error {
	return echo.NewHTTPError(http.StatusInternalServerError, response.ErrorResponse{
		ResultCode: http.StatusInternalServerError,
		Message:    message,
	})
}

// NewServiceUnavailableError 503 Service Unavailable 에러를 생성합니다
func NewServiceUnavailableError(message string) error {
	return echo.NewHTTPError(http.StatusServiceUnavailable, response.ErrorResponse{
		ResultCode: http.StatusServiceUnavailable,
		Message:    message,
	})
}

// Success 표준 성공 응답(200 OK)을 JSON 형식으로 반환합니다.
func Success(c echo.Context) error {
	return c.JSON(http.StatusOK, response.SuccessResponse{
		ResultCode: 0,
		Message:    "성공",
	})
}
