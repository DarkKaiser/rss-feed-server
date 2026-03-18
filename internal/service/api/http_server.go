package api

import (
	"embed"
	"html/template"
	"io"
	"net/http"
	"time"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/rss-feed-server/internal/service/api/httputil"
	_middleware_ "github.com/darkkaiser/rss-feed-server/internal/service/api/middleware"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

const (
	// defaultMaxBodySize 요청 본문의 최대 크기 (128KB)
	// DoS 공격 방지 및 메모리 보호를 위해 제한합니다.
	defaultMaxBodySize = "128K"
)

// HTTP 연결 타임아웃 설정 (시간 제한)
const (
	// defaultReadTimeout 요청 본문 읽기 최대 시간 (30초)
	defaultReadTimeout = 30 * time.Second

	// defaultReadHeaderTimeout HTTP 헤더 읽기 최대 대기 시간 (10초)
	// Slowloris DoS 공격을 방어하기 위해 헤더를 매우 느리게 전송하는
	// 악의적인 클라이언트의 연결 고갈 공격을 방지합니다.
	defaultReadHeaderTimeout = 10 * time.Second

	// defaultWriteTimeout 응답 쓰기 최대 시간 (65초)
	defaultWriteTimeout = 65 * time.Second

	// defaultIdleTimeout Keep-Alive 연결 유휴 최대 시간 (120초)
	defaultIdleTimeout = 120 * time.Second
)

// ServerConfig HTTP 서버 생성에 필요한 설정을 정의합니다.
type ServerConfig struct {
	// Debug Echo 프레임워크의 상세 로깅 및 디버그 모드 활성화 여부
	// 활성화 시 상세한 에러 메시지와 스택 트레이스가 응답에 포함될 수 있으므로,
	// 운영(Production) 환경에서는 보안상 반드시 비활성화(false)해야 합니다.
	Debug bool

	// EnableHSTS HSTS(HTTP Strict Transport Security) 보안 헤더 활성화 여부
	// 이 설정이 활성화되면 브라우저에게 "앞으로 일정 기간 동안은 무조건 HTTPS로만 접속하라"고 지시하여,
	// 프로토콜 다운그레이드 공격(SSL Stripping) 및 쿠키 하이재킹과 같은 중간자 공격(MITM)을 원천 차단합니다.
	// TLS(HTTPS) 환경에서는 반드시 활성화(true)하는 것이 강력히 권장됩니다.
	EnableHSTS bool

	// AllowOrigins CORS(Cross-Origin Resource Sharing) 정책에서 허용할 도메인 목록
	// - 개발 환경: ["*"] (모든 출처 허용) 또는 ["http://localhost:3000"]
	// - 운영 환경: ["https://example.com"]과 같이 신뢰할 수 있는 특정 도메인만 명시해야 합니다.
	// 무분별한 허용은 악의적인 웹사이트가 사용자의 브라우저를 통해 API를 호출하는 보안 위협을 초래할 수 있습니다.
	AllowOrigins []string
}

// TemplateRegistry 템플릿 렌더링을 위한 구조체입니다.
type TemplateRegistry struct {
	templates *template.Template
}

// Render echo.Renderer 인터페이스 구현 메서드입니다.
func (t *TemplateRegistry) Render(w io.Writer, name string, data interface{}, _ echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

// NewEchoServer 미들웨어 및 템플릿 렌더러를 포함한 Echo 인스턴스를 생성합니다.
func NewEchoServer(cfg ServerConfig, views embed.FS) *echo.Echo {
	e := echo.New()

	e.Debug = cfg.Debug
	e.HideBanner = true

	// 보안 및 리소스 관리를 위한 HTTP 서버 타임아웃 설정
	e.Server.ReadTimeout = defaultReadTimeout
	e.Server.ReadHeaderTimeout = defaultReadHeaderTimeout
	e.Server.WriteTimeout = defaultWriteTimeout
	e.Server.IdleTimeout = defaultIdleTimeout

	// echo에서 출력되는 로그를 애플리케이션 로거로 출력되도록 설정합니다.
	e.Logger = _middleware_.Logger{Logger: applog.StandardLogger()}

	// 전역 HTTP 에러 핸들러 설정
	e.HTTPErrorHandler = httputil.ErrorHandler

	// 미들웨어 적용 (권장 순서)

	// 1. HTTP 로깅
	e.Use(_middleware_.LogrusLogger())
	// 2. Panic 복구
	e.Use(middleware.Recover())
	// 3. Request ID
	e.Use(middleware.RequestID())
	// 4. 보안 헤더 (XSS Protection 등)
	if cfg.EnableHSTS {
		// HSTS 활성화 (1년, 서브도메인 포함)
		e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
			XSSProtection:         "1; mode=block",
			ContentTypeNosniff:    "nosniff",
			XFrameOptions:         "SAMEORIGIN",
			HSTSMaxAge:            31536000, // 1년
			HSTSExcludeSubdomains: false,
		}))
	} else {
		// 기본 보안 헤더 (HSTS 제외)
		e.Use(middleware.Secure())
	}
	// 5. CORS 설정
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: cfg.AllowOrigins,
		AllowMethods: []string{http.MethodGet, http.MethodPut, http.MethodPost, http.MethodDelete},
	}))
	// 6. Server 헤더 제거 (보안 강화)
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Response().Header().Set(echo.HeaderServer, "")
			return next(c)
		}
	})
	// 7. Body Limit
	e.Use(middleware.BodyLimit(defaultMaxBodySize))

	// HTML 템플릿 렌더러 설정
	e.Renderer = &TemplateRegistry{
		templates: template.Must(template.ParseFS(views, "views/templates/rss_feed_summary_view.tmpl")),
	}

	return e
}
