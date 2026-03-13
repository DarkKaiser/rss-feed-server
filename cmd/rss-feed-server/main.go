package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/db"
	"github.com/darkkaiser/rss-feed-server/internal/model"
	"github.com/darkkaiser/rss-feed-server/internal/notifyapi"
	"github.com/darkkaiser/rss-feed-server/internal/pkg/version"
	"github.com/darkkaiser/rss-feed-server/internal/service"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawling"
	"github.com/darkkaiser/rss-feed-server/internal/service/ws"
)

// @@@@@ swagger 주석

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

// component main 로깅용 컴포넌트 이름
const component = "main"

const (
	// shutdownTimeout 종료 시그널 수신 후 최대 대기 시간
	shutdownTimeout = 30 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// 1. 환경설정 로드
	// 애플리케이션 구동에 필요한 모든 설정(로깅, 타임아웃, 포트 등)을 파일로부터 읽어 메모리에 적재합니다.
	// 이 단계가 실패하면 서버는 정상 동작할 수 없으므로 즉시 종료됩니다.
	// 반환된 경고 메시지(warnings)는 로그 초기화 후 출력합니다.
	appConfig, warnings, err := config.Load()
	if err != nil {
		return fmt.Errorf("환경설정 파일을 로드하는 중 치명적인 오류가 발생했습니다: %w", err)
	}

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

	// 3. 운영 적합성 진단
	// 서비스 안정성과 보안을 높이기 위한 권장 설정 준수 여부를 검사한 결과입니다.
	// 미준수 항목은 경고(Warn) 레벨로 로깅되며, 실행 흐름에는 영향을 주지 않습니다.
	for _, warning := range warnings {
		applog.WithComponent(component).Warn(warning)
	}

	// 4. 서버 아이덴티티 출력
	// 서버 시작 시 시각적으로 식별 가능한 배너(Ascii Art)와 버전 정보를 출력하여,
	// 운영자가 현재 구동되는 서버의 종류와 버전을 직관적으로 확인할 수 있게 합니다.
	fmt.Printf(banner, version.Version())

	// 5. 빌드 메타데이터 조회
	buildInfo := version.Get()

	// 6. 초기화 시작 로그 기록
	applog.WithComponentAndFields(component, applog.Fields{
		"env":          map[bool]string{true: "development", false: "production"}[appConfig.Debug],
		"version":      buildInfo.Version,
		"commit":       buildInfo.Commit,
		"build_date":   buildInfo.BuildDate,
		"build_number": buildInfo.BuildNumber,
		"go_version":   buildInfo.GoVersion,
		"os":           buildInfo.OS,
		"arch":         buildInfo.Arch,
	}).Info("RSS Feed Server 초기화 프로세스를 시작합니다")

	// @@@@@
	// NotifyAPI를 초기화한다.
	notifyapi.Init(&notifyapi.Config{
		URL:           appConfig.NotifyAPI.URL,
		AppKey:        appConfig.NotifyAPI.AppKey,
		ApplicationID: appConfig.NotifyAPI.ApplicationID,
	})

	// @@@@@
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

	// @@@@@
	// RSS Feed Store를 초기화한다.
	rssFeedProviderStore := model.NewRssFeedProviderStore(appConfig, sqlDb)

	// @@@@@
	// 7. 서비스 객체 생성 및 연결
	webService := ws.NewService(appConfig, rssFeedProviderStore)
	crawlingService := crawling.NewService(appConfig, rssFeedProviderStore)

	// 8. 서비스 생명주기 관리 컨텍스트 설정
	// 전체 서비스의 종료 신호를 전파하는 Context(serviceStopCtx)와
	// 모든 서비스가 안전하게 종료될 때까지 대기하는 WaitGroup(serviceStopWG)을 초기화합니다.
	serviceStopCtx, serviceStopCancel := context.WithCancel(context.Background())
	serviceStopWG := &sync.WaitGroup{}

	// 9. 서비스 병렬 기동
	// 준비된 모든 서비스를 별도의 고루틴 또는 비동기 컨텍스트에서 시작합니다.
	// 하나라도 초기화에 실패하면 즉시 전체 서버 구동을 중단하고 롤백(종료) 절차를 밟습니다.
	services := []service.Service{webService, crawlingService}
	for _, s := range services {
		serviceStopWG.Add(1)
		if err := s.Start(serviceStopCtx, serviceStopWG); err != nil {
			serviceStopCancel()  // 다른 서비스들도 종료
			serviceStopWG.Wait() // 이미 시작된 서비스들의 종료를 대기
			return fmt.Errorf("핵심 서비스(%T)를 시작하는 중 치명적인 오류가 발생했습니다: %w", s, err)
		}
	}

	// 10. OS 시그널 처리기 등록
	// 운영체제로부터의 종료 신호(SIGTERM: 정상 종료, SIGINT: Ctrl+C)를 수신할 채널을 생성합니다.
	// 이는 서버가 즉시 종료되지 않고, 진행 중인 작업을 마무리할 시간을 확보(Graceful Shutdown)하기 위함입니다.
	termC := make(chan os.Signal, 1)
	signal.Notify(termC, syscall.SIGINT, syscall.SIGTERM)

	applog.WithComponent(component).Info("RSS Feed Server 초기화가 성공적으로 완료되었습니다 (Ready to Serve)")

	// 11. 메인 루프 대기
	// 종료 신호가 들어올 때까지 메인 고루틴을 블로킹 상태로 유지합니다.
	sig := <-termC
	applog.WithComponentAndFields(component, applog.Fields{
		"signal": sig,
	}).Info("종료 신호(Signal)를 수신했습니다. Graceful Shutdown 프로세스를 시작합니다")

	// 12. 서비스 종료 전파
	// 취소 함수(serviceStopCancel)를 호출하여 `serviceStopCtx`를 대기하고 있는 모든 하위 서비스에 종료를 알립니다.
	// 각 서비스는 이를 감지하고 리소스 정리, 연결 해제 등의 정리 작업을 수행해야 합니다.
	serviceStopCancel()

	// 13. 종료 타임아웃 프로세스
	// 서비스들이 무한정 종료되지 않는 상황(Deadlock 등)을 방지하기 위해 강제 종료 타임아웃(30초)을 설정합니다.
	// `serviceStopCtx`는 이미 취소되었으므로, 타임아웃 카운트는 별도의 독립적인 Context(Background)에서 시작해야 합니다.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	// 메인 고루틴이 Select 절에서 타임아웃을 감지할 수 있도록, Wait() 호출을 별도 고루틴으로 위임합니다.
	done := make(chan struct{})
	go func() {
		serviceStopWG.Wait()
		close(done)
	}()

	// 14. 종료 완료 대기 또는 강제 종료
	select {
	case <-done:
		applog.WithComponent(component).Info("모든 서비스가 리소스를 정리하고 정상적으로 종료되었습니다")

	case <-shutdownCtx.Done():
		applog.WithComponent(component).Error("종료 타임아웃 발생: 일부 서비스가 응답하지 않아 강제 종료합니다")
		return fmt.Errorf("종료 제한 시간(%v)을 초과하여 프로세스를 강제 종료합니다", shutdownTimeout)
	}

	return nil
}
