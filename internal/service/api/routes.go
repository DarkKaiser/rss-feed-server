package api

import (
	"github.com/darkkaiser/rss-feed-server/internal/service/api/handler/rss"
	"github.com/labstack/echo/v4"
	echoSwagger "github.com/swaggo/echo-swagger"
)

// RegisterRoutes API 서비스의 전역 라우트를 등록합니다.
//
// 이 함수는 다음과 같은 엔드포인트들을 설정합니다:
//   - RSS 피드 서비스: RSS 요약 정보(/) 및 개별 RSS 피드(/:id) 제공
//   - API 문서: Swagger UI (/swagger/*) 제공
func RegisterRoutes(e *echo.Echo, h *rss.Handler) {
	registerRSSRoutes(e, h)
	registerSwaggerRoutes(e)
}

func registerRSSRoutes(e *echo.Echo, h *rss.Handler) {
	e.GET("/", h.ViewSummary)
	e.GET("/:id", h.GetFeed)
}

func registerSwaggerRoutes(e *echo.Echo) {
	// Swagger UI 엔드포인트 설정
	e.GET("/swagger/*", echoSwagger.EchoWrapHandler(
		// Swagger 문서 JSON 파일 위치 지정
		echoSwagger.URL("/swagger/doc.json"),
		// 딥 링크 활성화 (특정 API로 바로 이동 가능한 URL 지원)
		echoSwagger.DeepLinking(true),
		// 문서 로드 시 태그(Tag) 목록만 펼침 상태로 표시 ("list", "full", "none")
		echoSwagger.DocExpansion("list"),
	))
}
