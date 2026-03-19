package api

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
	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service"
	"github.com/darkkaiser/rss-feed-server/internal/service/api/handler/rss"
	"github.com/labstack/echo/v4"
)

// component API 서비스의 로깅용 컴포넌트 이름
const component = "api.service"

var (
	//go:embed views
	views embed.FS

	// shutdownTimeout Graceful Shutdown 시 최대 대기 시간 (5초)
	shutdownTimeout = 5 * time.Second
)

// Service API 서버(Echo 웹 서버)의 생명주기를 관리하는 서비스입니다.
//
// 이 서비스는 다음과 같은 역할을 수행합니다:
//   - Echo 기반 HTTP/HTTPS 서버 시작 및 종료
//   - 미들웨어 체인 설정 (PanicRecovery, RequestID, RateLimit, HTTPLogger, CORS, Secure)
//   - API 엔드포인트 라우팅 설정 (RSS 요약 정보, 개별 RSS 피드 제공)
//   - Swagger UI 제공
//   - 커스텀 HTTP 에러 핸들러 설정
//   - 서비스 상태 관리 (시작/중지)
//   - Graceful Shutdown 지원 (5초 타임아웃)
//   - 서버 에러 처리 및 알림 전송 (예상치 못한 에러 발생 시)
//
// 서비스는 고루틴으로 실행되며, context를 통해 종료 신호를 받습니다.
// Start() 메서드로 시작하고, context 취소로 종료됩니다.
type Service struct {
	appConfig *config.AppConfig

	feedRepo feed.Repository

	notifyClient *notify.Client

	running   bool
	runningMu sync.Mutex
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ service.Service = (*Service)(nil)

// NewService API 서비스를 생성합니다.
func NewService(appConfig *config.AppConfig, feedRepo feed.Repository, notifyClient *notify.Client) *Service {
	if appConfig == nil {
		panic("AppConfig는 필수입니다")
	}
	if feedRepo == nil {
		panic("feed.Repository는 필수입니다")
	}

	return &Service{
		appConfig: appConfig,

		feedRepo: feedRepo,

		notifyClient: notifyClient,

		running:   false,
		runningMu: sync.Mutex{},
	}
}

// Start API 서비스를 시작합니다.
//
// 서비스는 별도의 고루틴에서 실행되며, 다음 작업을 수행합니다:
//  1. 서비스 상태 검증 (feedRepo nil 체크, 중복 실행 방지)
//  2. Echo 서버 설정 (Handler, 미들웨어, 라우트)
//  3. HTTP/HTTPS 서버 시작 (별도 고루틴)
//  4. Shutdown 신호 대기
//  5. Graceful Shutdown 처리 (5초 타임아웃)
//  6. 서버 에러 처리 및 알림 전송 (예상치 못한 에러 발생 시)
//  7. 서비스 상태 정리 (running 플래그 초기화)
//
// 매개변수:
//   - serviceStopCtx: 서비스 종료 신호를 받기 위한 Context
//   - serviceStopWG: 서비스 종료 완료를 알리기 위한 WaitGroup
//
// 반환값:
//   - error: feedRepo가 nil이거나 서비스가 이미 실행 중인 경우
//
// Note: 이 함수는 즉시 반환되며, 실제 서버는 고루틴에서 실행됩니다.
func (s *Service) Start(serviceStopCtx context.Context, serviceStopWG *sync.WaitGroup) error {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()

	applog.WithComponent(component).Info("서비스 시작 진입: API 서비스 초기화 프로세스를 시작합니다")

	if s.feedRepo == nil {
		defer serviceStopWG.Done()
		return apperrors.New(apperrors.Internal, "feed.Repository 객체가 초기화되지 않았습니다")
	}

	if s.running {
		defer serviceStopWG.Done()
		applog.WithComponent(component).Warn("API 서비스가 이미 실행 중입니다 (중복 호출)")
		return nil
	}

	s.running = true

	go s.runEventLoop(serviceStopCtx, serviceStopWG)

	applog.WithComponent(component).Info("서비스 시작 완료: API 서비스가 정상적으로 초기화되었습니다")

	return nil
}

// runEventLoop 서비스의 메인 실행 루프입니다.
// 서버 설정, HTTP 서버 시작, Shutdown 대기를 순차적으로 수행합니다.
func (s *Service) runEventLoop(serviceStopCtx context.Context, serviceStopWG *sync.WaitGroup) {
	defer serviceStopWG.Done()

	// 서버 설정
	e := s.setupServer()

	// HTTP 서버 시작
	httpServerDone := make(chan struct{})
	go s.startHTTPServer(serviceStopCtx, e, httpServerDone)

	// Shutdown 대기
	s.waitForShutdown(serviceStopCtx, e, httpServerDone)
}

// setupServer Echo 서버 인스턴스를 생성하고 모든 설정을 완료합니다.
//
// 다음 순서로 서버를 구성합니다:
//  1. Handler 생성 (RSS 핸들러)
//  2. Echo 서버 생성 (미들웨어 체인, CORS 설정 포함)
//  3. 라우트 등록 (전역 라우트)
func (s *Service) setupServer() *echo.Echo {
	// 1. Handler 생성
	rssHandler := rss.New(s.appConfig, s.feedRepo, s.notifyClient)

	// 2. Echo 서버 생성 (미들웨어 체인 포함)
	e := NewEchoServer(ServerConfig{
		Debug:        s.appConfig.Debug,
		EnableHSTS:   s.appConfig.WS.TLSServer,
		AllowOrigins: []string{"*"},
	}, views)

	// 3. 라우트 등록
	RegisterRoutes(e, rssHandler)

	return e
}

// startHTTPServer HTTP/HTTPS 서버를 시작합니다.
//
// 설정에 따라 TLS 활성화 여부를 결정하며, 서버가 종료되면 httpServerDone 채널을 닫아
// 대기 중인 고루틴에 신호를 보냅니다.
//
// 매개변수:
//   - serviceStopCtx: 서비스 종료 신호를 받기 위한 Context
//   - e: Echo 서버 인스턴스
//   - httpServerDone: HTTP 서버 종료가 완료되었음을 부모 루틴에 알리기 위한 신호 채널
//
// Note: 이 함수는 블로킹되며, 서버가 종료될 때까지 반환되지 않습니다.
func (s *Service) startHTTPServer(serviceStopCtx context.Context, e *echo.Echo, httpServerDone chan struct{}) {
	defer close(httpServerDone)

	port := s.appConfig.WS.ListenPort
	applog.WithComponentAndFields(component, applog.Fields{
		"port": port,
	}).Debug("HTTP 서버 가동: 리스너가 포트에 바인딩되었습니다")

	var err error
	if s.appConfig.WS.TLSServer {
		err = e.StartTLS(
			fmt.Sprintf(":%d", port),
			s.appConfig.WS.TLSCertFile,
			s.appConfig.WS.TLSKeyFile,
		)
	} else {
		err = e.Start(fmt.Sprintf(":%d", port))
	}

	s.handleServerError(serviceStopCtx, err)
}

// handleServerError HTTP 서버 시작 중 발생한 에러를 처리합니다.
//
// 에러 처리 방식:
//   - nil: 처리하지 않음 (정상 종료)
//   - http.ErrServerClosed: Info 레벨 로깅 (Graceful Shutdown)
//   - 그 외: Error 레벨 로깅 + 알림 전송 (예상치 못한 에러)
func (s *Service) handleServerError(_ context.Context, err error) {
	// nil: 정상 종료, 처리 불필요
	if err == nil {
		return
	}

	// http.ErrServerClosed: Graceful Shutdown 완료
	if errors.Is(err, http.ErrServerClosed) {
		applog.WithComponent(component).Info("HTTP 서버 정지: 서버가 닫혔습니다 (Server Closed)")
		return
	}

	// 예상치 못한 에러: 로깅 및 알림 전송
	message := "HTTP 서버 기동 실패: 치명적인 구성 오류가 발생하였습니다"

	applog.WithComponentAndFields(component, applog.Fields{
		"port":  s.appConfig.WS.ListenPort,
		"error": err,
	}).Error(message)

	if s.notifyClient != nil {
		s.notifyClient.NotifyError(context.Background(), fmt.Sprintf("%s\r\n\r\n%s", message, err))
	}
}

// waitForShutdown 종료 신호를 대기하고 Graceful Shutdown을 수행합니다.
//
// 종료 처리 순서:
//  1. 종료 신호 대기 (정상 종료 또는 서버 조기 종료)
//  2. Echo 서버 Shutdown 호출 (5초 타임아웃)
//  3. HTTP 서버 완전 종료 대기
//  4. 서비스 상태 정리 (running 플래그 초기화)
//
// 매개변수:
//   - serviceStopCtx: 서비스 종료 신호를 받기 위한 Context
//   - e: Echo 서버 인스턴스
//   - httpServerDone: HTTP 서버 종료가 완료되었음을 부모 루틴에 알리기 위한 신호 채널
//
// Note: 이 함수는 서비스가 완전히 종료될 때까지 블로킹됩니다.
func (s *Service) waitForShutdown(serviceStopCtx context.Context, e *echo.Echo, httpServerDone chan struct{}) {
	select {
	case <-serviceStopCtx.Done():
		// 정상적인 종료 신호 수신
		applog.WithComponent(component).Info("종료 절차 진입: API 서비스 중지 시그널을 수신했습니다")

	case <-httpServerDone:
		// HTTP 서버가 예기치 않게 종료됨 (포트 바인딩 실패, 패닉 등)
		// 이미 종료되었으므로 Shutdown 호출 없이 상태만 정리
		applog.WithComponent(component).Error("비정상 종료: API 서비스가 예기치 않게 중단되었습니다")

		s.cleanup()

		return
	}

	// Graceful Shutdown 시작 (5초 타임아웃)
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		message := "종료 처리 실패: HTTP 서버 Shutdown 중 오류가 발생했습니다"

		applog.WithComponentAndFields(component, applog.Fields{
			"error": err,
		}).Error(message)

		if s.notifyClient != nil {
			s.notifyClient.NotifyError(context.Background(), fmt.Sprintf("%s\r\n\r\n%s", message, err))
		}
	}

	<-httpServerDone

	s.cleanup()
}

// cleanup 서비스 종료 후 상태를 정리합니다.
func (s *Service) cleanup() {
	s.runningMu.Lock()
	s.running = false
	s.runningMu.Unlock()

	applog.WithComponent(component).Info("API 서비스 종료 완료: 모든 리소스가 정리되었습니다")
}
