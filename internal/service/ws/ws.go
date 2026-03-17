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
	"github.com/darkkaiser/rss-feed-server/internal/service/ws/router"
	"github.com/darkkaiser/rss-feed-server/internal/store"
	"github.com/labstack/echo/v4"
)

var (
	//go:embed views
	views embed.FS
)

// webService
type webService struct {
	config *config.AppConfig

	handler *handler.Handler

	notifyClient *notify.Client

	running   bool
	runningMu sync.Mutex
}

func NewService(config *config.AppConfig, rssFeedProviderStore *store.RssFeedProviderStore, notifyClient *notify.Client) service.Service {
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

	applog.Debug("웹 서비스 시작중...")

	if s.running == true {
		defer serviceStopWG.Done()

		applog.Warn("웹 서비스가 이미 시작됨!!!")

		// @@@@@
		return nil
	}

	var e *echo.Echo
	e = router.New(views)
	e.GET("/", s.handler.GetRssFeedSummaryViewHandler)
	e.GET("/:id", s.handler.GetRssFeedHandler)

	echo.NotFoundHandler = func(c echo.Context) error {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("페이지를 찾을 수 없습니다."))
	}

	go func(listenPort int) {
		applog.Debugf("웹 서비스 > http 서버(:%d) 시작됨", listenPort)

		var err error
		if s.config.WS.TLSServer == true {
			err = e.StartTLS(fmt.Sprintf(":%d", listenPort), s.config.WS.TLSCertFile, s.config.WS.TLSKeyFile)
		} else {
			err = e.Start(fmt.Sprintf(":%d", listenPort))
		}

		// Start(), StartTLS() 함수는 항상 nil이 아닌 error를 반환한다.
		if errors.Is(err, http.ErrServerClosed) == true {
			applog.Debug("웹 서비스 > http 서버 중지됨")
		} else {
			m := "웹 서비스 > http 서버를 구성하는 중에 치명적인 오류가 발생하였습니다."

			applog.Errorf("%s (error:%s)", m, err)

			if s.notifyClient != nil {
				s.notifyClient.NotifyError(context.Background(), fmt.Sprintf("%s\r\n\r\n%s", m, err))
			}
		}
	}(s.config.WS.ListenPort)

	go func() {
		defer serviceStopWG.Done()

		select {
		case <-serviceStopCtx.Done():
			applog.Debug("웹 서비스 중지중...")

			s.runningMu.Lock()
			{
				// 웹 서비스를 중지한다.
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				if err := e.Shutdown(ctx); err != nil {
					m := "웹 서비스를 중지하는 중에 오류가 발생하였습니다."

					applog.Errorf("%s (error:%s)", m, err)

					if s.notifyClient != nil {
						s.notifyClient.NotifyError(context.Background(), fmt.Sprintf("%s\r\n\r\n%s", m, err))
					}
				}

				s.running = false
			}
			s.runningMu.Unlock()

			applog.Debug("웹 서비스 중지됨")
		}
	}()

	s.running = true

	applog.Debug("웹 서비스 시작됨")

	return nil
}
