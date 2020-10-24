package ws

import (
	"context"
	"fmt"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
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
// WebService
//
type WebService struct {
	config *g.AppConfig

	handlers *handler.WebServiceHandlers

	running   bool
	runningMu sync.Mutex
}

func NewService(config *g.AppConfig) *WebService {
	return &WebService{
		config: config,

		running:   false,
		runningMu: sync.Mutex{},
	}
}

func (s *WebService) Run(serviceStopCtx context.Context, serviceStopWaiter *sync.WaitGroup) {
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
		log.Debug("웹 서비스 > http 서버 시작")
		if err := e.Start(fmt.Sprintf(":%d", listenPort)); err != nil {
			if err == http.ErrServerClosed {
				log.Debug("웹 서비스 > http 서버 중지됨")
			} else {
				m := fmt.Sprintf("웹 서비스를 구성하는 중에 치명적인 오류가 발생하였습니다.\r\n\r\n%s", err)

				log.Error(m)

				notifyapi.SendNotifyMessage(m, true)
			}
		}
	}(s.config.WS.ListenPort)

	go func() {
		defer serviceStopWaiter.Done()

		select {
		case <-serviceStopCtx.Done():
			log.Debug("웹 서비스 중지중...")

			// 웹서버를 종료한다.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := e.Shutdown(ctx); err != nil {
				m := fmt.Sprintf("웹 서비스를 중지하는 중에 오류가 발생하였습니다.\r\n\r\n%s", err)

				log.Error(m)

				notifyapi.SendNotifyMessage(m, true)
			}

			// @@@@@ dbclose??
			s.handlers.Close()

			s.runningMu.Lock()
			s.handlers = nil
			s.running = false
			s.runningMu.Unlock()

			log.Debug("웹 서비스 중지됨")
		}
	}()

	s.running = true

	log.Debug("웹 서비스 시작됨")
}

func (s *WebService) Find(modelType model.ModelType) interface{} {
	return s.handlers.Find(modelType)
}
