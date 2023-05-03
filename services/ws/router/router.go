package router

import (
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/services/ws/handler"
	_middleware_ "github.com/darkkaiser/rss-feed-server/services/ws/middleware"
	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	log "github.com/sirupsen/logrus"
	"html/template"
	"io"
	"net/http"
)

type TemplateRegistry struct {
	templates *template.Template
}

func (t *TemplateRegistry) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

func New(config *g.AppConfig) (*echo.Echo, *handler.WebServiceHandlers) {
	e := echo.New()

	e.Debug = true
	e.HideBanner = true

	e.Renderer = &TemplateRegistry{
		templates: template.Must(template.ParseFiles("services/ws/templates/rss_feed_summary_view.html")),
	}

	// echo에서 출력되는 로그를 Logrus Logger로 출력되도록 한다.
	// echo Logger의 인터페이스를 래핑한 객체를 이용하여 Logrus Logger로 보낸다.
	e.Logger = _middleware_.Logger{Logger: log.StandardLogger()}
	e.Use(_middleware_.LogrusLogger())
	// echo 기본 로그출력 구문, 필요치 않음!!!
	//e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
	//	Format: `time="${time_rfc3339}" level=${level} remote_ip="${remote_ip}" host="${host}" method="${method}" uri="${uri}" user_agent="${user_agent}" ` +
	//		`status=${status} error="${error}" latency=${latency} latency_human="${latency_human}" bytes_in=${bytes_in} bytes_out=${bytes_out}` + "\n",
	//}))

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{ // CORS Middleware
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPut, http.MethodPost, http.MethodDelete},
	}))
	e.Use(middleware.Recover()) // Recover from panics anywhere in the chain
	e.Use(middleware.Secure())

	h := handler.NewWebServiceHandlers(config)
	{
		e.GET("/", h.GetRssFeedSummaryViewHandler)
		e.GET("/:id", h.GetRssFeedHandler)
	}

	return e, h
}
