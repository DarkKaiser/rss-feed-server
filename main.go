package main

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/darkkaiser/rss-feed-server/db"
	"github.com/darkkaiser/rss-feed-server/g"
	_log_ "github.com/darkkaiser/rss-feed-server/log"
	"github.com/darkkaiser/rss-feed-server/model"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
	"github.com/darkkaiser/rss-feed-server/services"
	"github.com/darkkaiser/rss-feed-server/services/crawling"
	"github.com/darkkaiser/rss-feed-server/services/ws"
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
		AppKey:        config.NotifyAPI.AppKey,
		ApplicationID: config.NotifyAPI.ApplicationID,
	})

	// 아스키아트 출력(https://ko.rakko.tools/tools/68/, 폰트:standard)
	fmt.Printf(banner, g.AppVersion)

	// 데이터베이스를 초기화한다.
	db := db.New()
	defer func(db *sql.DB) {
		err := db.Close()
		if err != nil {
			m := "DB를 닫는 중에 오류가 발생하였습니다."

			log.Errorf("%s (error:%s)", m, err)

			notifyapi.Send(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)
		}
	}(db)

	// RSS Feed Store를 초기화한다.
	rssFeedProviderStore := model.NewRssFeedProviderStore(config, db)

	// 서비스를 생성하고 초기화한다.
	webService := ws.NewService(config, rssFeedProviderStore)
	crawlingService := crawling.NewService(config, rssFeedProviderStore)

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
