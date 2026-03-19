package crawl

import (
	"context"
	"fmt"
	"sync"

	"github.com/darkkaiser/notify-server/pkg/cronx"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"
	"github.com/robfig/cron/v3"
)

// Service
type Service struct {
	appConfig *config.AppConfig

	cron *cron.Cron

	feedRepo     feed.Repository
	notifyClient *notify.Client

	running   bool
	runningMu sync.Mutex
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ service.Service = (*Service)(nil)

func NewService(appConfig *config.AppConfig, feedRepo feed.Repository, notifyClient *notify.Client) *Service {
	return &Service{
		appConfig: appConfig,

		cron: cron.New(
			cron.WithParser(cronx.StandardParser()),
			cron.WithLogger(cron.VerbosePrintfLogger(applog.StandardLogger())),
			cron.WithChain(
				cron.Recover(cron.VerbosePrintfLogger(applog.StandardLogger())),
				cron.SkipIfStillRunning(cron.VerbosePrintfLogger(applog.StandardLogger())),
			),
		),

		feedRepo:     feedRepo,
		notifyClient: notifyClient,

		running:   false,
		runningMu: sync.Mutex{},
	}
}

func (s *Service) Start(serviceStopCtx context.Context, serviceStopWG *sync.WaitGroup) error {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()

	applog.Debug("크롤링 서비스 시작중...")

	if s.running == true {
		defer serviceStopWG.Done()

		applog.Warn("크롤링 서비스가 이미 시작됨!!!")

		// @@@@@
		return nil
	}

	// 크롤링 스케쥴러를 시작한다.
	for _, p := range s.appConfig.RssFeed.Providers {
		crawlerConfig, err := provider.FindConfigFromSupportedCrawler(config.ProviderSite(p.Site))
		if err != nil {
			defer serviceStopWG.Done()

			m := fmt.Sprintf("%s(ID:%s) 크롤링 작업의 스케쥴러 등록이 실패하였습니다. 구현된 Crawler가 존재하지 않습니다.", p.Site, p.ID)

			if s.notifyClient != nil {
				s.notifyClient.NotifyError(context.Background(), m)
			}

			applog.Panic(m)

			// @@@@@
			return nil
		}

		if _, err := s.cron.AddJob(p.Scheduler.TimeSpec, crawlerConfig.NewCrawlerFn(p.ID, p.Config, s.feedRepo, s.notifyClient)); err != nil {
			m := fmt.Sprintf("%s(ID:%s) 크롤링 작업의 스케쥴러 등록이 실패하였습니다. (error:%s)", p.Site, p.ID, err)

			if s.notifyClient != nil {
				s.notifyClient.NotifyError(context.Background(), m)
			}

			applog.Panic(m)
		}
	}

	s.cron.Start()

	go s.run0(serviceStopCtx, serviceStopWG)

	s.running = true

	applog.Debug("크롤링 서비스 시작됨")

	return nil
}

func (s *Service) run0(serviceStopCtx context.Context, serviceStopWaiter *sync.WaitGroup) {
	defer serviceStopWaiter.Done()

	<-serviceStopCtx.Done()

	applog.Debug("크롤링 서비스 중지중...")

	s.runningMu.Lock()
	{
		// 크롤링 스케쥴러를 중지한다.
		ctx := s.cron.Stop()
		<-ctx.Done()

		s.running = false
	}
	s.runningMu.Unlock()

	applog.Debug("크롤링 서비스 중지됨")
}
