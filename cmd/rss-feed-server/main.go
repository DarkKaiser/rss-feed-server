package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/db"
	"github.com/darkkaiser/rss-feed-server/internal/model"
	"github.com/darkkaiser/rss-feed-server/internal/notifyapi"
	"github.com/darkkaiser/rss-feed-server/internal/pkg/version"
	"github.com/darkkaiser/rss-feed-server/internal/services"
	"github.com/darkkaiser/rss-feed-server/internal/services/crawling"
	"github.com/darkkaiser/rss-feed-server/internal/services/ws"
)

const (
	banner = `
  ____   ____   ____    _____                 _   ____
 |  _ \ / ___| / ___|  |  ___| ___   ___   __| | / ___|  _ __ __   __
 | |_) |\___ \ \___ \  | |_   / _ \ / _ \ / _| | \___ \ | '__|\ \ / /
 |  _ <  ___) | ___) | |  _| |  __/|  __/| (_| |  ___) || |    \ V /
 |_| \_\|____/ |____/  |_|    \___| \___| \__,_| |____/ |_|     \_/ %s
                                                   developed by DarkKaiser
---------------------------------------------------------------------------
`
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run() error {
	runtime.GOMAXPROCS(runtime.NumCPU()) // 모든 CPU 사용

	// 환경설정 정보를 읽어들인다.
	appConfig := config.InitAppConfig()

	// 2. 로그 시스템 초기화
	// 환경설정(Debug 모드 여부)에 따라 개발용 또는 운영용 로거를 구성합니다.
	// 초기화된 클로저(appLogCloser)는 함수 종료 시 반드시 호출하여 버퍼에 남은 로그를 플러시해야 합니다.
	var logOpts applog.Options
	if appConfig.Debug {
		logOpts = applog.NewDevelopmentOptions(config.AppName)
	} else {
		logOpts = applog.NewProductionOptions(config.AppName)
	}
	// 로그 파일 경로 단축을 위해 프로젝트 모듈 경로 주입
	logOpts.CallerPathPrefix = "github.com/darkkaiser/rss-feed-server"

	appLogCloser, err := applog.Setup(logOpts)
	if err != nil {
		return fmt.Errorf("로그 시스템을 초기화하는 중 치명적인 오류가 발생했습니다: %w", err)
	}
	defer appLogCloser.Close()

	// NotifyAPI를 초기화한다.
	notifyapi.Init(&notifyapi.Config{
		Url:           appConfig.NotifyAPI.Url,
		AppKey:        appConfig.NotifyAPI.AppKey,
		ApplicationID: appConfig.NotifyAPI.ApplicationID,
	})

	// 아스키아트 출력(https://ko.rakko.tools/tools/68/, 폰트:standard)
	fmt.Printf(banner, version.Version())

	// 데이터베이스를 초기화한다.
	sqlDb := db.New()
	defer func(db *sql.DB) {
		err := db.Close()
		if err != nil {
			m := "DB를 닫는 중에 오류가 발생하였습니다."

			applog.Errorf("%s (error:%s)", m, err)

			notifyapi.Send(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)
		}
	}(sqlDb)

	// RSS Feed Store를 초기화한다.
	rssFeedProviderStore := model.NewRssFeedProviderStore(appConfig, sqlDb)

	// 서비스를 생성하고 초기화한다.
	webService := ws.NewService(appConfig, rssFeedProviderStore)
	crawlingService := crawling.NewService(appConfig, rssFeedProviderStore)

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
	applog.Info("Shutdown signal received")
	cancel()                 // Signal cancellation to context.Context
	serviceStopWaiter.Wait() // Block here until are workers are done

	return nil
}
