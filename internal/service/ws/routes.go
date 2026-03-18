package ws

import (
	"github.com/darkkaiser/rss-feed-server/internal/service/ws/handler"
	"github.com/labstack/echo/v4"
)

// RegisterRoutes 웹 서비스의 전역 라우트를 등록합니다.
func RegisterRoutes(e *echo.Echo, h *handler.Handler) {
	e.GET("/", h.GetRssFeedSummaryViewHandler)
	e.GET("/:id", h.GetRssFeedHandler)
}
