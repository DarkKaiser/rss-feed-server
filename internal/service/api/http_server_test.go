package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/darkkaiser/rss-feed-server/internal/service/api/httputil"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// 테스트 헬퍼
// =============================================================================

// requestToEcho Echo 인스턴스에 HTTP 요청을 전송하고 응답을 반환하는 헬퍼입니다.
func requestToEcho(t *testing.T, e *echo.Echo, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// newEchoWithHandler 테스트용 핸들러를 등록한 Echo 인스턴스를 반환하는 헬퍼입니다.
func newEchoWithHandler(t *testing.T, method, path string, handler echo.HandlerFunc) *echo.Echo {
	t.Helper()
	e := NewEchoServer(ServerConfig{AllowOrigins: []string{"*"}}, views)
	e.Add(method, path, handler)
	return e
}

// =============================================================================
// 상수 검증
// =============================================================================

func TestServerConstants(t *testing.T) {
	t.Run("HTTP 연결 타임아웃 상수값이 의도한 값이다", func(t *testing.T) {
		assert.Equal(t, 30*time.Second, defaultReadTimeout)
		assert.Equal(t, 10*time.Second, defaultReadHeaderTimeout)
		assert.Equal(t, 65*time.Second, defaultWriteTimeout)
		assert.Equal(t, 120*time.Second, defaultIdleTimeout)
	})

	t.Run("요청 본문 크기 제한 상수값이 의도한 값이다", func(t *testing.T) {
		assert.Equal(t, "128K", defaultMaxBodySize)
	})

	t.Run("Rate Limit 상수값이 의도한 값이다", func(t *testing.T) {
		assert.Equal(t, 20, defaultRateLimitPerSecond, "초당 허용 요청 수는 20이어야 한다")
		assert.Equal(t, 40, defaultRateLimitBurst, "버스트 용량은 40이어야 한다")
		// 버스트는 폭발적 트래픽 흡수를 위해 RPS의 2배로 설정한다.
		assert.Equal(t, defaultRateLimitPerSecond*2, defaultRateLimitBurst, "버스트는 RPS의 2배여야 한다")
	})
}

// =============================================================================
// NewEchoServer 기본 설정 검증
// =============================================================================

func TestNewEchoServer_Settings(t *testing.T) {
	t.Run("Debug=true 시 Echo.Debug가 true가 된다", func(t *testing.T) {
		e := NewEchoServer(ServerConfig{Debug: true, AllowOrigins: []string{"*"}}, views)
		assert.True(t, e.Debug)
	})

	t.Run("Debug=false 시 Echo.Debug가 false가 된다", func(t *testing.T) {
		e := NewEchoServer(ServerConfig{Debug: false, AllowOrigins: []string{"*"}}, views)
		assert.False(t, e.Debug)
	})

	t.Run("HideBanner가 true로 설정된다", func(t *testing.T) {
		e := NewEchoServer(ServerConfig{AllowOrigins: []string{"*"}}, views)
		assert.True(t, e.HideBanner)
	})

	t.Run("HTTP 서버 타임아웃이 올바르게 설정된다", func(t *testing.T) {
		e := NewEchoServer(ServerConfig{AllowOrigins: []string{"*"}}, views)

		assert.Equal(t, defaultReadTimeout, e.Server.ReadTimeout)
		assert.Equal(t, defaultReadHeaderTimeout, e.Server.ReadHeaderTimeout)
		assert.Equal(t, defaultWriteTimeout, e.Server.WriteTimeout)
		assert.Equal(t, defaultIdleTimeout, e.Server.IdleTimeout)
	})

	t.Run("커스텀 HTTPErrorHandler가 등록된다", func(t *testing.T) {
		e := NewEchoServer(ServerConfig{AllowOrigins: []string{"*"}}, views)
		assert.NotNil(t, e.HTTPErrorHandler)
	})

	t.Run("Renderer가 설정된다", func(t *testing.T) {
		e := NewEchoServer(ServerConfig{AllowOrigins: []string{"*"}}, views)
		assert.NotNil(t, e.Renderer, "templateRenderer가 주입되어야 한다")
	})
}

// =============================================================================
// 미들웨어: 보안 헤더 (Secure)
// =============================================================================

func TestNewEchoServer_ServerHeaderRemoved(t *testing.T) {
	t.Run("응답에서 Server 헤더가 비어 있어야 한다 (기술 스택 노출 방지)", func(t *testing.T) {
		e := newEchoWithHandler(t, http.MethodGet, "/ping", func(c echo.Context) error {
			return c.String(http.StatusOK, "pong")
		})
		rec := requestToEcho(t, e, http.MethodGet, "/ping")
		assert.Empty(t, rec.Header().Get(echo.HeaderServer), "Server 헤더는 빈 문자열이어야 한다")
	})
}

func TestNewEchoServer_SecurityHeaders(t *testing.T) {
	e := newEchoWithHandler(t, http.MethodGet, "/ping", func(c echo.Context) error {
		return c.String(http.StatusOK, "pong")
	})
	rec := requestToEcho(t, e, http.MethodGet, "/ping")

	t.Run("X-Content-Type-Options 헤더가 nosniff으로 설정된다", func(t *testing.T) {
		assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	})

	t.Run("X-XSS-Protection 헤더가 설정된다", func(t *testing.T) {
		assert.NotEmpty(t, rec.Header().Get("X-XSS-Protection"))
	})
}

// =============================================================================
// 미들웨어: HSTS
// =============================================================================

func TestNewEchoServer_HSTS(t *testing.T) {
	const hstsHeader = "Strict-Transport-Security"

	// HSTS는 TLS(HTTPS) 연결에서만 동작하므로 req.TLS 필드를 설정하여 시뮬레이션한다.
	setupAndRequest := func(enableHSTS bool) *httptest.ResponseRecorder {
		e := NewEchoServer(ServerConfig{
			EnableHSTS:   enableHSTS,
			AllowOrigins: []string{"*"},
		}, views)
		e.GET("/ping", func(c echo.Context) error {
			return c.String(http.StatusOK, "pong")
		})

		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		req.TLS = &tls.ConnectionState{}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	t.Run("EnableHSTS=true 시 Strict-Transport-Security 헤더가 포함된다", func(t *testing.T) {
		hsts := setupAndRequest(true).Header().Get(hstsHeader)
		assert.NotEmpty(t, hsts, "HSTS 헤더가 설정되어야 한다")
		assert.Contains(t, hsts, "max-age=31536000", "HSTS max-age가 1년(31536000)이어야 한다")
		assert.Contains(t, hsts, "includeSubdomains", "HSTS가 서브도메인을 포함해야 한다")
	})

	t.Run("EnableHSTS=false 시 Strict-Transport-Security 헤더가 포함되지 않는다", func(t *testing.T) {
		hsts := setupAndRequest(false).Header().Get(hstsHeader)
		assert.Empty(t, hsts, "HSTS 비활성화 시 헤더가 없어야 한다")
	})
}

// =============================================================================
// 미들웨어: CORS
// =============================================================================

func TestNewEchoServer_CORS(t *testing.T) {
	t.Run("허용된 Origin의 Preflight 요청에 CORS 헤더가 반환된다", func(t *testing.T) {
		allowedOrigin := "https://example.com"
		e := NewEchoServer(ServerConfig{AllowOrigins: []string{allowedOrigin}}, views)
		e.GET("/api", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })

		req := httptest.NewRequest(http.MethodOptions, "/api", nil)
		req.Header.Set("Origin", allowedOrigin)
		req.Header.Set("Access-Control-Request-Method", "GET")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, allowedOrigin, rec.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("와일드카드(*) Origin 설정 시 모든 출처가 허용된다", func(t *testing.T) {
		e := NewEchoServer(ServerConfig{AllowOrigins: []string{"*"}}, views)
		e.GET("/api", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })

		req := httptest.NewRequest(http.MethodOptions, "/api", nil)
		req.Header.Set("Origin", "https://any-domain.co.kr")
		req.Header.Set("Access-Control-Request-Method", "GET")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	})
}

// =============================================================================
// 미들웨어: RequestID
// =============================================================================

func TestNewEchoServer_RequestID(t *testing.T) {
	t.Run("모든 응답에 X-Request-ID 헤더가 포함된다", func(t *testing.T) {
		e := newEchoWithHandler(t, http.MethodGet, "/ping", func(c echo.Context) error {
			return c.String(http.StatusOK, "pong")
		})
		rec := requestToEcho(t, e, http.MethodGet, "/ping")
		assert.NotEmpty(t, rec.Header().Get(echo.HeaderXRequestID), "X-Request-ID 헤더가 모든 응답에 포함되어야 한다")
	})

	t.Run("요청마다 고유한 X-Request-ID가 생성된다", func(t *testing.T) {
		e := newEchoWithHandler(t, http.MethodGet, "/ping", func(c echo.Context) error {
			return c.String(http.StatusOK, "pong")
		})
		rec1 := requestToEcho(t, e, http.MethodGet, "/ping")
		rec2 := requestToEcho(t, e, http.MethodGet, "/ping")

		id1 := rec1.Header().Get(echo.HeaderXRequestID)
		id2 := rec2.Header().Get(echo.HeaderXRequestID)
		assert.NotEmpty(t, id1)
		assert.NotEmpty(t, id2)
		assert.NotEqual(t, id1, id2, "각 요청의 Request-ID는 달라야 한다")
	})
}

// =============================================================================
// 미들웨어: BodyLimit
// =============================================================================

func TestNewEchoServer_BodyLimit(t *testing.T) {
	t.Run("128K를 초과하는 요청 본문에 413 상태 코드를 반환한다", func(t *testing.T) {
		e := NewEchoServer(ServerConfig{AllowOrigins: []string{"*"}}, views)
		e.POST("/upload", func(c echo.Context) error {
			return c.String(http.StatusOK, "ok")
		})

		// 129KB 크기의 요청 본문 생성 (128K 초과)
		body := bytes.Repeat([]byte("x"), 129*1024)
		req := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewReader(body))
		req.Header.Set(echo.MIMETextPlain, "text/plain")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code,
			"128K를 초과하는 본문은 413 Request Entity Too Large를 반환해야 한다")
	})

	t.Run("128K 이하의 요청 본문은 정상 처리된다", func(t *testing.T) {
		e := NewEchoServer(ServerConfig{AllowOrigins: []string{"*"}}, views)
		e.POST("/upload", func(c echo.Context) error {
			return c.String(http.StatusOK, "ok")
		})

		// 1KB 크기의 요청 본문 (제한 이하)
		body := bytes.Repeat([]byte("x"), 1024)
		req := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

// =============================================================================
// 미들웨어: PanicRecovery
// =============================================================================

func TestNewEchoServer_PanicRecovery(t *testing.T) {
	t.Run("핸들러에서 panic이 발생해도 500으로 복구되어 서버가 다운되지 않는다", func(t *testing.T) {
		e := NewEchoServer(ServerConfig{AllowOrigins: []string{"*"}}, views)
		e.GET("/panic", func(c echo.Context) error {
			panic("테스트 패닉")
		})
		e.GET("/ok", func(c echo.Context) error {
			return c.String(http.StatusOK, "ok")
		})

		// panic 핸들러 호출 → 500 응답으로 복구
		rec := requestToEcho(t, e, http.MethodGet, "/panic")
		assert.Equal(t, http.StatusInternalServerError, rec.Code,
			"panic 발생 시 500 Internal Server Error로 복구되어야 한다")

		// panic 이후 다음 요청도 정상 처리되는지 확인
		rec2 := requestToEcho(t, e, http.MethodGet, "/ok")
		assert.Equal(t, http.StatusOK, rec2.Code,
			"panic 복구 후에도 서버는 정상적으로 요청을 처리해야 한다")
	})
}

// =============================================================================
// 커스텀 ErrorHandler 검증
// =============================================================================

// errorResponseBody JSON 응답에서 result_code와 message를 파싱하는 헬퍼입니다.
func errorResponseBody(t *testing.T, rec *httptest.ResponseRecorder) (int, string) {
	t.Helper()
	var body struct {
		ResultCode int    `json:"result_code"`
		Message    string `json:"message"`
	}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err, "JSON 응답 디코딩에 실패했습니다")
	return body.ResultCode, body.Message
}

func TestNewEchoServer_ErrorHandler(t *testing.T) {
	t.Run("존재하지 않는 경로에 대한 404 응답이 한국어 메시지를 포함한다", func(t *testing.T) {
		e := NewEchoServer(ServerConfig{AllowOrigins: []string{"*"}}, views)
		rec := requestToEcho(t, e, http.MethodGet, "/non-existent-path")

		assert.Equal(t, http.StatusNotFound, rec.Code)
		_, msg := errorResponseBody(t, rec)
		assert.Contains(t, msg, "요청한 리소스를 찾을 수 없습니다",
			"404 응답은 한국어 메시지를 포함해야 한다")
	})

	t.Run("400 에러는 올바른 JSON(result_code, message)으로 응답한다", func(t *testing.T) {
		const errMsg = "잘못된 요청입니다"
		e := newEchoWithHandler(t, http.MethodGet, "/bad", func(c echo.Context) error {
			return httputil.NewBadRequestError(errMsg)
		})
		rec := requestToEcho(t, e, http.MethodGet, "/bad")

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		code, msg := errorResponseBody(t, rec)
		assert.Equal(t, http.StatusBadRequest, code)
		assert.Equal(t, errMsg, msg)
	})

	t.Run("500 에러는 올바른 JSON(result_code, message)으로 응답한다", func(t *testing.T) {
		const errMsg = "서버 내부 오류"
		e := newEchoWithHandler(t, http.MethodGet, "/500", func(c echo.Context) error {
			return httputil.NewInternalServerError(errMsg)
		})
		rec := requestToEcho(t, e, http.MethodGet, "/500")

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		code, msg := errorResponseBody(t, rec)
		assert.Equal(t, http.StatusInternalServerError, code)
		assert.Equal(t, errMsg, msg)
	})

	t.Run("HEAD 요청 시 에러가 발생하면 본문 없이 헤더만 반환한다", func(t *testing.T) {
		e := newEchoWithHandler(t, http.MethodHead, "/head-err", func(c echo.Context) error {
			return httputil.NewBadRequestError("bad")
		})
		req := httptest.NewRequest(http.MethodHead, "/head-err", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Empty(t, rec.Body.String(), "HEAD 요청 에러 응답은 본문이 없어야 한다")
	})

	t.Run("이미 커밋된 응답에 대해서는 추가 응답을 시도하지 않는다", func(t *testing.T) {
		// 핸들러 내에서 직접 응답을 쓴 후 에러를 반환하는 케이스
		e := NewEchoServer(ServerConfig{AllowOrigins: []string{"*"}}, views)
		e.GET("/committed", func(c echo.Context) error {
			// 직접 응답을 기록하여 Committed 상태로 만든다
			c.Response().WriteHeader(http.StatusOK)
			_, _ = c.Response().Write([]byte("already written"))
			// 이후 에러를 반환해도 ErrorHandler가 추가 응답하지 않아야 한다
			return echo.ErrInternalServerError
		})
		rec := requestToEcho(t, e, http.MethodGet, "/committed")

		// 첫 번째 WriteHeader가 이미 200을 기록했으므로 상태는 200이어야 한다
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "already written")
	})
}

// =============================================================================
// templateRenderer 테스트
// =============================================================================

func TestTemplateRenderer_Render(t *testing.T) {
	e := NewEchoServer(ServerConfig{AllowOrigins: []string{"*"}}, views)
	renderer, ok := e.Renderer.(*templateRenderer)
	require.True(t, ok, "Renderer는 *templateRenderer여야 한다")

	t.Run("유효한 템플릿 이름으로 렌더링이 성공한다", func(t *testing.T) {
		var buf bytes.Buffer
		data := map[string]interface{}{
			"serviceUrl": "http://localhost:8080",
			"rssFeed": map[string]interface{}{
				"MaxItemCount": 100,
				"Providers":    []interface{}{},
			},
		}
		err := renderer.Render(&buf, "rss_summary.tmpl", data, nil)
		require.NoError(t, err)
		assert.True(t, strings.Contains(buf.String(), "RSS 피드 목록"),
			"렌더링 결과에 페이지 제목이 포함되어야 한다")
	})

	t.Run("존재하지 않는 템플릿 이름으로 렌더링 시 에러가 반환된다", func(t *testing.T) {
		var buf bytes.Buffer
		err := renderer.Render(&buf, "non_existent_template.tmpl", nil, nil)
		assert.Error(t, err, "존재하지 않는 템플릿은 에러를 반환해야 한다")
	})
}
