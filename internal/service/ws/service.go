package ws

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/service"
	"github.com/darkkaiser/rss-feed-server/internal/service/ws/handler"
	"github.com/darkkaiser/rss-feed-server/internal/store/sqlite"
	"github.com/labstack/echo/v4"
)

var (
	//go:embed views
	views embed.FS
)

// component WS 서비스의 로깅용 컴포넌트 이름
const component = "ws.service"

var (
	// shutdownTimeout Graceful Shutdown 시 최대 대기 시간 (5초)
	shutdownTimeout = 5 * time.Second
)

// webService
type webService struct {
	config *config.AppConfig

	handler *handler.Handler

	notifyClient *notify.Client

	running   bool
	runningMu sync.Mutex
}

func NewService(config *config.AppConfig, rssFeedProviderStore *sqlite.Store, notifyClient *notify.Client) service.Service {
	if config == nil {
		panic("AppConfig는 필수입니다")
	}

	return &webService{
		config: config,

		handler: handler.NewHandler(config, rssFeedProviderStore, notifyClient),

		notifyClient: notifyClient,

		running:   false,
		runningMu: sync.Mutex{},
	}
}

func (s *webService) Start(serviceStopCtx context.Context, serviceStopWG *sync.WaitGroup) error {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()

	applog.WithComponent(component).Info("서비스 시작 진입: WS 서비스 초기화 프로세스를 시작합니다")

	if s.running {
		defer serviceStopWG.Done()
		applog.WithComponent(component).Warn("WS 서비스가 이미 시작됨 (중복 호출)")
		return nil
	}

	s.running = true

	go s.runEventLoop(serviceStopCtx, serviceStopWG)

	applog.WithComponent(component).Info("서비스 시작 완료: WS 서비스가 정상적으로 초기화되었습니다")

	return nil
}

func (s *webService) runEventLoop(serviceStopCtx context.Context, serviceStopWG *sync.WaitGroup) {
	defer serviceStopWG.Done()

	// 1. 서버 설정 및 라우팅 (추후 http_server.go, routes.go 로 분리될 대상)
	e := s.setupServer()

	// 2. HTTP 서버 시작
	httpServerDone := make(chan struct{})
	go s.startHTTPServer(serviceStopCtx, e, httpServerDone)

	// 3. 종료 대기
	s.waitForShutdown(serviceStopCtx, e, httpServerDone)
}

func (s *webService) setupServer() *echo.Echo {
	e := NewEchoServer(ServerConfig{
		Debug:        true,                  // 기존 router 설정 로직(e.Debug = true)을 그대로 인계
		EnableHSTS:   s.config.WS.TLSServer, // TLS 사용 시 HSTS 적용
		AllowOrigins: []string{"*"},         // 기본으로 모든 도메인 허용
	}, views)

	RegisterRoutes(e, s.handler)

	return e
}

func (s *webService) startHTTPServer(serviceStopCtx context.Context, e *echo.Echo, httpServerDone chan struct{}) {
	defer close(httpServerDone)

	listenPort := s.config.WS.ListenPort
	applog.WithComponentAndFields(component, applog.Fields{
		"port": listenPort,
	}).Debug("HTTP 서버 가동: 리스너가 포트에 바인딩되었습니다")

	var err error
	if s.config.WS.TLSServer {
		err = e.StartTLS(fmt.Sprintf(":%d", listenPort), s.config.WS.TLSCertFile, s.config.WS.TLSKeyFile)
	} else {
		err = e.Start(fmt.Sprintf(":%d", listenPort))
	}

	s.handleServerError(err)
}

// handleServerError HTTP 서버 시작 중 발생한 에러를 처리합니다.
func (s *webService) handleServerError(err error) {
	if err == nil {
		return
	}

	if errors.Is(err, http.ErrServerClosed) {
		applog.WithComponent(component).Info("HTTP 서버 정지: 서버가 닫혔습니다 (Server Closed)")
		return
	}

	m := "HTTP 서버 기동 실패: 치명적인 오류가 발생하였습니다"
	applog.WithComponentAndFields(component, applog.Fields{
		"port":  s.config.WS.ListenPort,
		"error": err,
	}).Error(m)

	if s.notifyClient != nil {
		s.notifyClient.NotifyError(context.Background(), fmt.Sprintf("%s\r\n\r\n%s", m, err))
	}
}

func (s *webService) waitForShutdown(serviceStopCtx context.Context, e *echo.Echo, httpServerDone chan struct{}) {
	select {
	case <-serviceStopCtx.Done():
		applog.WithComponent(component).Info("종료 절차 진입: WS 서비스 중지 시그널을 수신했습니다")

	case <-httpServerDone:
		// 서버가 예기치 않게 종료됨
		applog.WithComponent(component).Error("비정상 종료: WS 서비스가 예기치 않게 중단되었습니다")
		s.cleanup()
		return
	}

	// 웹 서비스를 중지한다.
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		m := "종료 처리 실패: HTTP 서버 Shutdown 중 오류가 발생했습니다"
		applog.WithComponentAndFields(component, applog.Fields{
			"error": err,
		}).Error(m)

		if s.notifyClient != nil {
			s.notifyClient.NotifyError(context.Background(), fmt.Sprintf("%s\r\n\r\n%s", m, err))
		}
	}

	<-httpServerDone

	s.cleanup()
}

func (s *webService) cleanup() {
	s.runningMu.Lock()
	s.running = false
	s.runningMu.Unlock()

	applog.WithComponent(component).Info("WS 서비스 종료 완료: 모든 리소스가 정리되었습니다")
}
