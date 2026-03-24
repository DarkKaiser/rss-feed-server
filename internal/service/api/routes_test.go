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
	appConf := newTestAppConfig()
	return rss.New(&appConf.RSSFeed, &mockFeedRepository{}, nil)
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
// HTTP 엔드포인트 라우팅 매핑 검증
// =============================================================================

func TestRoutes_RoutingBehavior(t *testing.T) {
	e := echo.New()
	RegisterRoutes(e, newTestRSSHandler())

	tests := []struct {
		name           string
		method         string
		requestPath    string
		expectedPath   string
		expectedParams map[string]string
	}{
		{
			name:         "GET / 요청은 최상위 라우트로 매핑된다",
			method:       http.MethodGet,
			requestPath:  "/",
			expectedPath: "/",
		},
		{
			name:         "GET /some-feed-id 요청은 /:id 라우트로 매핑되며 파라미터를 추출한다",
			method:       http.MethodGet,
			requestPath:  "/some-feed-id",
			expectedPath: "/:id",
			expectedParams: map[string]string{
				"id": "some-feed-id",
			},
		},
		{
			name:         "GET /some-feed.xml 요청도 /:id 라우트로 매핑되며 파라미터를 추출한다",
			method:       http.MethodGet,
			requestPath:  "/some-feed.xml",
			expectedPath: "/:id",
			expectedParams: map[string]string{
				"id": "some-feed.xml",
			},
		},
		{
			name:         "GET /swagger/index.html 요청은 Swagger 라우트로 매핑된다",
			method:       http.MethodGet,
			requestPath:  "/swagger/index.html",
			expectedPath: "/swagger/*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.requestPath, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			e.Router().Find(tt.method, tt.requestPath, c)

			assert.Equal(t, tt.expectedPath, c.Path(), "라우팅된 경로가 예상과 다릅니다")

			for key, expectedVal := range tt.expectedParams {
				assert.Equal(t, expectedVal, c.Param(key), "파라미터 추출 결과가 예상과 다릅니다")
			}
		})
	}

	t.Run("등록되지 않은 HTTP 메서드는 라우팅되지 않는다", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/some-feed-id", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		e.Router().Find(http.MethodPost, "/some-feed-id", c)
		
		// echo.Router().Find 가 메서드를 찾지 못하면 MethodNotAllowedHandler 를 Context 에 바인딩합니다.
		err := c.Handler()(c)
		assert.Equal(t, echo.ErrMethodNotAllowed, err, "등록되지 않은 메서드는 405 에러 핸들러로 매핑되어야 합니다")
	})
}
