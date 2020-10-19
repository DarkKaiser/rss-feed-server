package main

import (
	"fmt"
	"github.com/darkkaiser/rss-server/g"
	_log_ "github.com/darkkaiser/rss-server/log"
	"github.com/darkkaiser/rss-server/notifyapi"
	"runtime"
)

const (
	banner = `
  ____   ____   ____    ____
 |  _ \ / ___| / ___|  / ___|   ___  _ __ __   __  ___  _ __
 | |_) |\___ \ \___ \  \___ \  / _ \| '__|\ \ / / / _ \| '__|
 |  _ <  ___) | ___) |  ___) ||  __/| |    \ V / |  __/| |
 |_| \_\|____/ |____/  |____/  \___||_|     \_/   \___||_|               v%s
                                                        developed by DarkKaiser
--------------------------------------------------------------------------------
`
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU()) // 모든 CPU 사용

	// 환경설정 정보를 읽어들인다.
	config := g.InitAppConfig()

	// 로그를 초기화하고, 일정 시간이 지난 로그 파일을 모두 삭제한다.
	_log_.InitLog(config.Debug, g.AppName, 30.)

	// NotifyAPI를 초기화한다.
	notifyapi.Init(config)

	// 아스키아트(https://ko.rakko.tools/tools/68/, 폰트:standard)
	fmt.Printf(banner, g.AppVersion)

	// @@@@@
	//// 서비스를 생성하고 초기화한다.
	//taskService := task.NewService(config)
	//notificationService := notification.NewService(config, taskService)
	//notifyAPIService := api.NewNotifyAPIService(config, notificationService)
	//
	//taskService.SetTaskNotificationSender(notificationService)
	//
	//// Set up cancellation context and waitgroup
	//serviceStopCtx, cancel := context.WithCancel(context.Background())
	//serviceStopWaiter := &sync.WaitGroup{}
	//
	//// 서비스를 시작한다.
	//for _, s := range []service.Service{taskService, notificationService, notifyAPIService} {
	//	serviceStopWaiter.Add(1)
	//	s.Run(serviceStopCtx, serviceStopWaiter)
	//}
	//
	//// Handle sigterm and await termC signal
	//termC := make(chan os.Signal)
	//signal.Notify(termC, syscall.SIGINT, syscall.SIGTERM)
	//
	//<-termC // Blocks here until interrupted
	//
	//// Handle shutdown
	//log.Info("Shutdown signal received")
	//cancel()                 // Signal cancellation to context.Context
	//serviceStopWaiter.Wait() // Block here until are workers are done
}
