package api

import (
	"embed"
	"html/template"
	"io"
	"net/http"
	"time"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/rss-feed-server/internal/service/api/httputil"
	appmiddleware "github.com/darkkaiser/rss-feed-server/internal/service/api/middleware"
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

// 트래픽 제한 정책 (Rate Limiting)
const (
	// defaultRateLimitPerSecond IP별 초당 허용 요청 수 (기본값: 20)
	defaultRateLimitPerSecond = 20

	// defaultRateLimitBurst IP별 버스트 허용량 (기본값: 40)
	defaultRateLimitBurst = 40
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

// NewEchoServer 설정된 미들웨어를 포함한 Echo 인스턴스를 생성합니다.
//
// 미들웨어는 다음 순서로 적용됩니다 (순서가 중요합니다):
//
//  1. HTTPLogger - HTTP 요청/응답 로깅
//     - 모든 HTTP 요청과 응답 정보를 구조화된 로그로 기록
//     - 민감 정보(app_key, password 등)는 자동으로 마스킹
//     - 요청 처리 시간, 상태 코드, IP 주소 등 기록
//
//  2. PanicRecovery - 패닉 복구 및 로깅
//     - 핸들러에서 발생한 panic을 복구하여 서버 다운 방지
//     - 스택 트레이스와 함께 에러를 로깅
//     - 가장 먼저 적용되어야 다른 미들웨어의 panic도 복구 가능
//
//  3. RequestID - 요청 ID 생성
//     - 각 요청에 고유한 ID를 부여 (X-Request-ID 헤더)
//     - 로깅 및 디버깅 시 요청 추적에 사용
//     - 로깅 미들웨어보다 먼저 적용되어야 로그에 request_id 포함 가능
//
//  4. Secure - 보안 헤더 설정
//     - X-XSS-Protection, X-Content-Type-Options 등 보안 헤더 자동 추가
//     - XSS, 클릭재킹 등의 공격 방어
//     - 가장 마지막에 적용되어 모든 응답에 보안 헤더 추가
//
//  5. CORS - Cross-Origin Resource Sharing
//     - 허용된 Origin에서의 크로스 도메인 요청 처리
//     - Preflight 요청(OPTIONS) 자동 응답
//     - 프로덕션 환경에서는 특정 도메인만 허용 권장
//
//  6. ServerHeader - Server 헤더 제거
//     - 응답 헤더에서 Server 필드를 삭제하여 기술 스택 노출 방지
//     - 공격자가 서버 버전을 파악하여 취약점을 악용하는 것을 어렵게 함
//     - 보안 감화를 위한 조치 (Security through Obscurity)
//
//  7. RateLimit - IP 기반 요청 제한
//     - IP 주소별로 초당 요청 수 제한 (기본: 20 req/s, 버스트: 40)
//     - Brute Force 공격 방어 및 서버 리소스 보호
//     - 제한 초과 시 429 Too Many Requests 응답
//     - 로깅 전에 적용하여 과도한 로그 생성 방지
//
//  8. BodyLimit - 요청 본문 크기 제한 (초과 시 413 응답)
//     - 대용량 요청으로 인한 메모리 고갈 및 DoS 공격 방지
//
// 라우트 설정은 포함되지 않으며, 반환된 Echo 인스턴스에 별도로 설정해야 합니다.
func NewEchoServer(cfg ServerConfig, views embed.FS) *echo.Echo {
	e := echo.New()

	e.Debug = cfg.Debug
	e.HideBanner = true

	// 보안 및 리소스 관리를 위한 HTTP 서버 타임아웃 설정
	e.Server.ReadTimeout = defaultReadTimeout             // 요청 본문 읽기 제한
	e.Server.ReadHeaderTimeout = defaultReadHeaderTimeout // 요청 헤더 읽기 제한
	e.Server.WriteTimeout = defaultWriteTimeout           // 응답 쓰기 제한
	e.Server.IdleTimeout = defaultIdleTimeout             // Keep-Alive 연결 유휴 제한

	// Echo 프레임워크의 내부 로그를 애플리케이션 로거로 통합합니다.
	// 이를 통해 모든 로그가 동일한 형식과 출력 대상을 사용하게 됩니다.
	e.Logger = appmiddleware.Logger{Logger: applog.StandardLogger()}

	// 전역 HTTP 에러 핸들러 설정
	e.HTTPErrorHandler = httputil.ErrorHandler

	// 미들웨어 적용 (권장 순서)

	// 1. HTTP 로깅 (가장 바깥쪽에서 모든 요청/응답 기록, Panic 포함)
	e.Use(appmiddleware.HTTPLogger())
	// 2. Panic 복구
	e.Use(appmiddleware.PanicRecovery())
	// 3. Request ID
	e.Use(middleware.RequestID())
	// 4. 보안 헤더 (XSS Protection 등)
	// 가장 먼저 적용하여 에러 응답(429, 503 등)을 포함한 모든 응답에 보안 헤더가 추가되도록 합니다.
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
	// 보안 헤더와 마찬가지로 모든 응답에 적용되어야 하므로 상위에 위치합니다.
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: cfg.AllowOrigins,
		AllowMethods: []string{http.MethodGet, http.MethodPut, http.MethodPost, http.MethodDelete},
	}))
	// 6. Server 헤더 제거 (보안 강화)
	// 공격자에게 서버 스택 정보(Go/Echo 버전 등)를 노출하지 않도록 합니다.
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Response().Header().Set(echo.HeaderServer, "")
			return next(c)
		}
	})
	// 7. Rate Limiting
	e.Use(appmiddleware.RateLimit(defaultRateLimitPerSecond, defaultRateLimitBurst))
	// 8. Body Limit
	e.Use(middleware.BodyLimit(defaultMaxBodySize))

	// HTML 템플릿 렌더러를 주입합니다. 없으면 c.Render() 호출 시 런타임 오류가 발생합니다.
	e.Renderer = &templateRenderer{
		templates: template.Must(template.ParseFS(views, "views/templates/rss_summary.tmpl")),
	}

	return e
}

// templateRenderer echo.Renderer 인터페이스를 구현하는 내부 헬퍼 구조체입니다.
type templateRenderer struct {
	templates *template.Template
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ echo.Renderer = (*templateRenderer)(nil)

// Render 지정된 템플릿에 데이터를 바인딩하여 응답 스트림(w)에 씁니다. echo.Renderer 인터페이스를 구현합니다.
func (t *templateRenderer) Render(w io.Writer, name string, data any, _ echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}
