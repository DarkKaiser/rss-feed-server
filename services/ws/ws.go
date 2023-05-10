package ws

import (
	"context"
	"embed"
	"fmt"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/model"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
	"github.com/darkkaiser/rss-feed-server/services"
	"github.com/darkkaiser/rss-feed-server/services/ws/handler"
	"github.com/darkkaiser/rss-feed-server/services/ws/router"
	"github.com/labstack/echo"
	log "github.com/sirupsen/logrus"
	"net/http"
	"sync"
	"time"
)

var (
	//go:embed views
	views embed.FS
)

//
// webService
//
type webService struct {
	config *g.AppConfig

	handler *handler.Handler

	running   bool
	runningMu sync.Mutex
}

func NewService(config *g.AppConfig, rssFeedProviderStore *model.RssFeedProviderStore) services.Service {
	return &webService{
		config: config,

		handler: handler.NewHandler(config, rssFeedProviderStore),

		running:   false,
		runningMu: sync.Mutex{},
	}
}

func (s *webService) Run(serviceStopCtx context.Context, serviceStopWaiter *sync.WaitGroup) {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()

	log.Debug("웹 서비스 시작중...")

	if s.running == true {
		defer serviceStopWaiter.Done()

		log.Warn("웹 서비스가 이미 시작됨!!!")

		return
	}

	var e *echo.Echo
	e = router.New(views)
	e.GET("/", s.handler.GetRssFeedSummaryViewHandler)
	e.GET("/:id", s.handler.GetRssFeedHandler)

	go func(listenPort int) {
		log.Debugf("웹 서비스 > http 서버(:%d) 시작됨", listenPort)

		var err error
		if s.config.WS.TLSServer == true {
			err = e.StartTLS(fmt.Sprintf(":%d", listenPort), s.config.WS.TLSCertFile, s.config.WS.TLSKeyFile)
		} else {
			err = e.Start(fmt.Sprintf(":%d", listenPort))
		}

		// Start(), StartTLS() 함수는 항상 nil이 아닌 error를 반환한다.
		if err == http.ErrServerClosed {
			log.Debug("웹 서비스 > http 서버 중지됨")
		} else {
			m := "웹 서비스 > http 서버를 구성하는 중에 치명적인 오류가 발생하였습니다."

			log.Errorf("%s (error:%s)", m, err)

			notifyapi.Send(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)
		}
	}(s.config.WS.ListenPort)

	go func() {
		defer serviceStopWaiter.Done()

		select {
		case <-serviceStopCtx.Done():
			log.Debug("웹 서비스 중지중...")

			s.runningMu.Lock()
			{
				// 웹 서비스를 중지한다.
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				if err := e.Shutdown(ctx); err != nil {
					m := "웹 서비스를 중지하는 중에 오류가 발생하였습니다."

					log.Errorf("%s (error:%s)", m, err)

					notifyapi.Send(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)
				}

				s.running = false
			}
			s.runningMu.Unlock()

			log.Debug("웹 서비스 중지됨")
		}
	}()

	s.running = true

	log.Debug("웹 서비스 시작됨")
}
