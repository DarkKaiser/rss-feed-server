package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/darkkaiser/rss-feed-server/internal/service/api/handler/rss"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test Helpers
// =============================================================================

// newTestRSSHandler 테스트용 최소 rss.Handler를 생성합니다.
func newTestRSSHandler() *rss.Handler {
	return rss.New(newTestAppConfig(), &mockFeedRepository{}, nil)
}

// routeExists 주어진 method/path 조합의 라우트가 Echo 인스턴스에 등록되어 있는지 확인합니다.
func routeExists(e *echo.Echo, method, path string) bool {
	for _, r := range e.Routes() {
		if r.Method == method && r.Path == path {
			return true
		}
	}
	return false
}

// =============================================================================
// RegisterRoutes 테스트
// =============================================================================

func TestRegisterRoutes(t *testing.T) {
	e := echo.New()
	RegisterRoutes(e, newTestRSSHandler())

	t.Run("RSS 피드 요약 라우트가 등록된다 (GET /)", func(t *testing.T) {
		assert.True(t, routeExists(e, http.MethodGet, "/"), "GET / 라우트가 존재해야 한다")
	})

	t.Run("개별 RSS 피드 라우트가 등록된다 (GET /:id)", func(t *testing.T) {
		assert.True(t, routeExists(e, http.MethodGet, "/:id"), "GET /:id 라우트가 존재해야 한다")
	})

	t.Run("Swagger UI 라우트가 등록된다 (GET /swagger/*)", func(t *testing.T) {
		assert.True(t, routeExists(e, http.MethodGet, "/swagger/*"), "GET /swagger/* 라우트가 존재해야 한다")
	})

	t.Run("RSS(2) + Swagger(1) 이상의 라우트가 등록된다", func(t *testing.T) {
		// Swagger는 내부적으로 추가 라우트를 등록할 수 있으므로 최소 3개를 보장한다.
		require.GreaterOrEqual(t, len(e.Routes()), 3,
			"RegisterRoutes는 최소 3개의 라우트를 등록해야 한다")
	})
}

// =============================================================================
// registerRSSRoutes 테스트
// =============================================================================

func TestRegisterRSSRoutes(t *testing.T) {
	e := echo.New()
	registerRSSRoutes(e, newTestRSSHandler())

	t.Run("GET / 라우트가 등록된다", func(t *testing.T) {
		assert.True(t, routeExists(e, http.MethodGet, "/"))
	})

	t.Run("GET /:id 라우트가 등록된다", func(t *testing.T) {
		assert.True(t, routeExists(e, http.MethodGet, "/:id"))
	})

	t.Run("Swagger 라우트는 등록되지 않는다", func(t *testing.T) {
		assert.False(t, routeExists(e, http.MethodGet, "/swagger/*"),
			"registerRSSRoutes는 Swagger 라우트를 등록하면 안 된다")
	})

	t.Run("정확히 RSS 라우트 2개만 등록된다", func(t *testing.T) {
		assert.Len(t, e.Routes(), 2, "RSS 라우트는 정확히 2개여야 한다")
	})
}

// =============================================================================
// registerSwaggerRoutes 테스트
// =============================================================================

func TestRegisterSwaggerRoutes(t *testing.T) {
	e := echo.New()
	registerSwaggerRoutes(e)

	t.Run("GET /swagger/* 라우트가 등록된다", func(t *testing.T) {
		assert.True(t, routeExists(e, http.MethodGet, "/swagger/*"))
	})

	t.Run("RSS 라우트는 등록되지 않는다", func(t *testing.T) {
		assert.False(t, routeExists(e, http.MethodGet, "/"),
			"registerSwaggerRoutes는 GET / 를 등록하면 안 된다")
		assert.False(t, routeExists(e, http.MethodGet, "/:id"),
			"registerSwaggerRoutes는 GET /:id 를 등록하면 안 된다")
	})
}

// =============================================================================
// HTTP 엔드포인트 라우팅 검증
// =============================================================================

// serveRequest 테스트용 Echo에 요청을 보내고 응답을 반환하는 헬퍼입니다.
func serveRequest(e *echo.Echo, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestRoutes_RoutingBehavior(t *testing.T) {
	// 라우트 등록만 확인하는 단위 테스트와 달리,
	// 실제 HTTP 요청이 올바른 핸들러로 라우팅되는지 검증합니다.
	// 미들웨어 없이 순수 라우팅 동작에만 집중합니다.
	e := echo.New()
	e.HideBanner = true
	RegisterRoutes(e, newTestRSSHandler())

	t.Run("GET / 는 핸들러가 실행된다 (404가 아닌 응답)", func(t *testing.T) {
		// ViewSummary 핸들러가 실행되면 Renderer가 없어 500이 반환되지만, 라우팅은 성공한 것이다.
		rec := serveRequest(e, http.MethodGet, "/")
		assert.NotEqual(t, http.StatusNotFound, rec.Code,
			"GET / 는 등록된 라우트이므로 404가 아닌 응답을 반환해야 한다")
	})

	t.Run("GET /:id 는 핸들러가 실행된다 (404가 아닌 응답)", func(t *testing.T) {
		// GetFeed 핸들러가 실행되면 config에 피드가 없으므로 400이 반환된다.
		rec := serveRequest(e, http.MethodGet, "/some-feed-id")
		assert.NotEqual(t, http.StatusNotFound, rec.Code,
			"GET /:id 는 등록된 라우트이므로 404가 아닌 응답을 반환해야 한다")
	})

	t.Run("GET /swagger/index.html 는 핸들러가 실행된다 (404가 아닌 응답)", func(t *testing.T) {
		rec := serveRequest(e, http.MethodGet, "/swagger/index.html")
		assert.NotEqual(t, http.StatusNotFound, rec.Code,
			"GET /swagger/* 는 등록된 라우트이므로 404가 아닌 응답을 반환해야 한다")
	})

	t.Run("등록되지 않은 HTTP 메서드는 405를 반환한다", func(t *testing.T) {
		// /:id 패턴은 GET만 허용하므로, POST로 요청하면 405 Method Not Allowed가 반환된다.
		// (echo는 메서드 미등록 시 405를 자동으로 처리한다)
		rec := serveRequest(e, http.MethodPost, "/some-feed-id")
		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code,
			"등록되지 않은 메서드(POST /:id)는 405를 반환해야 한다")
	})

	t.Run("GET /:id 는 .xml 확장자 경로도 라우팅된다", func(t *testing.T) {
		// GetFeed 핸들러는 .xml 접미사를 제거하여 처리한다.
		// /:id 라우트가 /some-feed.xml을 잡아야 한다.
		rec := serveRequest(e, http.MethodGet, "/some-feed.xml")
		assert.NotEqual(t, http.StatusNotFound, rec.Code,
			"GET /:id 는 .xml 확장자 경로도 처리해야 한다")
	})
}
