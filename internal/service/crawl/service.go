package crawl

import (
	"context"
	"fmt"
	"sync"

	"github.com/darkkaiser/notify-server/pkg/cronx"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/service"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/crawler"
	"github.com/darkkaiser/rss-feed-server/internal/store/sqlite"
	"github.com/robfig/cron/v3"
)

// crawlingService
type crawlingService struct {
	config *config.AppConfig

	cron *cron.Cron

	rssFeedProviderStore *sqlite.Store
	notifyClient         *notify.Client

	running   bool
	runningMu sync.Mutex
}

func NewService(config *config.AppConfig, rssFeedProviderStore *sqlite.Store, notifyClient *notify.Client) service.Service {
	return &crawlingService{
		config: config,

		cron: cron.New(
			cron.WithParser(cronx.StandardParser()),
			cron.WithLogger(cron.VerbosePrintfLogger(applog.StandardLogger())),
			cron.WithChain(
				cron.Recover(cron.VerbosePrintfLogger(applog.StandardLogger())),
				cron.SkipIfStillRunning(cron.VerbosePrintfLogger(applog.StandardLogger())),
			),
		),

		rssFeedProviderStore: rssFeedProviderStore,
		notifyClient:         notifyClient,

		running:   false,
		runningMu: sync.Mutex{},
	}
}

func (s *crawlingService) Start(serviceStopCtx context.Context, serviceStopWG *sync.WaitGroup) error {
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
	for _, p := range s.config.RssFeed.Providers {
		crawlerConfig, err := crawler.FindConfigFromSupportedCrawler(config.ProviderSite(p.Site))
		if err != nil {
			m := fmt.Sprintf("%s(ID:%s) 크롤링 작업의 스케쥴러 등록이 실패하였습니다. 구현된 Crawler가 존재하지 않습니다.", p.Site, p.ID)

			if s.notifyClient != nil {
				s.notifyClient.NotifyError(context.Background(), m)
			}

			applog.Panic(m)

			// @@@@@
			return nil
		}

		if _, err := s.cron.AddJob(p.Scheduler.TimeSpec, crawlerConfig.NewCrawlerFn(p.ID, p.Config, s.rssFeedProviderStore, s.notifyClient)); err != nil {
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

func (s *crawlingService) run0(serviceStopCtx context.Context, serviceStopWaiter *sync.WaitGroup) {
	defer serviceStopWaiter.Done()

	for {
		select {
		case <-serviceStopCtx.Done():
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

			return
		}
	}
}
