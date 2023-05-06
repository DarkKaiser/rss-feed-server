package main

import (
	"context"
	"fmt"
	"github.com/darkkaiser/rss-feed-server/g"
	_log_ "github.com/darkkaiser/rss-feed-server/log"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
	"github.com/darkkaiser/rss-feed-server/services"
	"github.com/darkkaiser/rss-feed-server/services/crawling"
	"github.com/darkkaiser/rss-feed-server/services/ws"
	"github.com/darkkaiser/rss-feed-server/services/ws/model"
	log "github.com/sirupsen/logrus"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
)

const (
	banner = `
  ____   ____   ____    _____                 _   ____
 |  _ \ / ___| / ___|  |  ___| ___   ___   __| | / ___|  _ __ __   __
 | |_) |\___ \ \___ \  | |_   / _ \ / _ \ / _| | \___ \ | '__|\ \ / /
 |  _ <  ___) | ___) | |  _| |  __/|  __/| (_| |  ___) || |    \ V /
 |_| \_\|____/ |____/  |_|    \___| \___| \__,_| |____/ |_|     \_/ v%s
                                                   developed by DarkKaiser
---------------------------------------------------------------------------
`
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU()) // 모든 CPU 사용

	// 환경설정 정보를 읽어들인다.
	config := g.InitAppConfig()

	// 로그를 초기화하고, 일정 시간이 지난 로그 파일을 모두 삭제한다.
	_log_.Init(config.Debug, g.AppName, 30.)

	// NotifyAPI를 초기화한다.
	notifyapi.Init(&notifyapi.Config{
		Url:           config.NotifyAPI.Url,
		APIKey:        config.NotifyAPI.APIKey,
		ApplicationID: config.NotifyAPI.ApplicationID,
	})

	// 아스키아트 출력(https://ko.rakko.tools/tools/68/, 폰트:standard)
	fmt.Printf(banner, g.AppVersion)

	// 서비스를 생성하고 초기화한다.
	webService := ws.NewService(config)
	crawlingService := crawling.NewService(config, webService.(model.Accessor))

	// Set up cancellation context and waitgroup
	serviceStopCtx, cancel := context.WithCancel(context.Background())
	serviceStopWaiter := &sync.WaitGroup{}

	// 서비스를 시작한다.
	for _, s := range []services.Service{webService, crawlingService} {
		serviceStopWaiter.Add(1)
		s.Run(serviceStopCtx, serviceStopWaiter)
	}

	// Handle sigterm and await termC signal
	termC := make(chan os.Signal)
	signal.Notify(termC, syscall.SIGINT, syscall.SIGTERM)

	<-termC // Blocks here until interrupted

	// Handle shutdown
	log.Info("Shutdown signal received")
	cancel()                 // Signal cancellation to context.Context
	serviceStopWaiter.Wait() // Block here until are workers are done
}
