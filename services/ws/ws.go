package ws

import (
	"context"
	"fmt"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
	"github.com/darkkaiser/rss-feed-server/services"
	"github.com/darkkaiser/rss-feed-server/services/ws/handler"
	"github.com/darkkaiser/rss-feed-server/services/ws/model"
	"github.com/darkkaiser/rss-feed-server/services/ws/router"
	"github.com/labstack/echo"
	log "github.com/sirupsen/logrus"
	"net/http"
	"sync"
	"time"
)

//
// webService
//
type webService struct {
	config *g.AppConfig

	handlers *handler.WebServiceHandlers

	running   bool
	runningMu sync.Mutex
}

func NewService(config *g.AppConfig) services.Service {
	return &webService{
		config: config,

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
	e, s.handlers = router.New(s.config)

	go func(listenPort int) {
		log.Debugf("웹 서비스 > http 서버(:%d) 시작됨", listenPort)

		var err error
		if s.config.WS.TLSServer == true {
			err = e.StartTLS(fmt.Sprintf(":%d", listenPort), s.config.WS.CertFilePath, s.config.WS.KeyFilePath)
		} else {
			err = e.Start(fmt.Sprintf(":%d", listenPort))
		}

		// StartTLS(), Start() 함수는 항상 nil이 아닌 error를 반환한다.
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

				// 웹 서비스의 핸들러를 닫는다.
				s.handlers.Close()

				s.handlers = nil
				s.running = false
			}
			s.runningMu.Unlock()

			log.Debug("웹 서비스 중지됨")
		}
	}()

	s.running = true

	log.Debug("웹 서비스 시작됨")
}

func (s *webService) GetModel() model.RssFeedProvidersAccessor {
	return s.handlers.GetModel()
}
