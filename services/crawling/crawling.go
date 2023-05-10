package crawling

import (
	"context"
	"fmt"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/model"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
	"github.com/darkkaiser/rss-feed-server/services"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
	"sync"
)

//
// crawlingService
//
type crawlingService struct {
	config *g.AppConfig

	cron *cron.Cron

	rssFeedProviderStore *model.RssFeedProviderStore

	running   bool
	runningMu sync.Mutex
}

func NewService(config *g.AppConfig, rssFeedProviderStore *model.RssFeedProviderStore) services.Service {
	return &crawlingService{
		config: config,

		cron: cron.New(cron.WithLogger(cron.VerbosePrintfLogger(log.StandardLogger()))),

		rssFeedProviderStore: rssFeedProviderStore,

		running:   false,
		runningMu: sync.Mutex{},
	}
}

func (s *crawlingService) Run(serviceStopCtx context.Context, serviceStopWaiter *sync.WaitGroup) {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()

	log.Debug("크롤링 서비스 시작중...")

	if s.running == true {
		defer serviceStopWaiter.Done()

		log.Warn("크롤링 서비스가 이미 시작됨!!!")

		return
	}

	// 크롤링 스케쥴러를 시작한다.
	for _, p := range s.config.RssFeed.Providers {
		crawlerConfig, err := findConfigFromSupportedCrawler(g.RssFeedProviderSite(p.Site))
		if err != nil {
			m := fmt.Sprintf("%s(ID:%s) 크롤링 작업의 스케쥴러 등록이 실패하였습니다. 구현된 Crawler가 존재하지 않습니다.", p.Site, p.ID)

			notifyapi.Send(m, true)

			log.Panic(m)

			return
		}

		if _, err := s.cron.AddJob(p.CrawlingScheduler.TimeSpec, crawlerConfig.newCrawlerFn(p.ID, p.Config, s.rssFeedProviderStore)); err != nil {
			m := fmt.Sprintf("%s(ID:%s) 크롤링 작업의 스케쥴러 등록이 실패하였습니다. (error:%s)", p.Site, p.ID, err)

			notifyapi.Send(m, true)

			log.Panic(m)
		}
	}

	s.cron.Start()

	go s.run0(serviceStopCtx, serviceStopWaiter)

	s.running = true

	log.Debug("크롤링 서비스 시작됨")
}

func (s *crawlingService) run0(serviceStopCtx context.Context, serviceStopWaiter *sync.WaitGroup) {
	defer serviceStopWaiter.Done()

	for {
		select {
		case <-serviceStopCtx.Done():
			log.Debug("크롤링 서비스 중지중...")

			s.runningMu.Lock()
			{
				// 크롤링 스케쥴러를 중지한다.
				ctx := s.cron.Stop()
				<-ctx.Done()

				s.running = false
			}
			s.runningMu.Unlock()

			log.Debug("크롤링 서비스 중지됨")

			return
		}
	}
}
