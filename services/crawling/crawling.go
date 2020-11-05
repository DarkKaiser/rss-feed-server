package crawling

import (
	"context"
	"fmt"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
	"github.com/darkkaiser/rss-feed-server/services/ws/model"
	"github.com/robfig/cron"
	log "github.com/sirupsen/logrus"
	"sync"
)

//
// CrawlingService
//
type CrawlingService struct {
	config *g.AppConfig

	cron *cron.Cron

	modelFinder model.Finder

	running   bool
	runningMu sync.Mutex
}

func NewService(config *g.AppConfig, modelFinder model.Finder) *CrawlingService {
	return &CrawlingService{
		config: config,

		cron: cron.New(cron.WithLogger(cron.VerbosePrintfLogger(log.StandardLogger()))),

		modelFinder: modelFinder,

		running:   false,
		runningMu: sync.Mutex{},
	}
}

func (s *CrawlingService) Run(serviceStopCtx context.Context, serviceStopWaiter *sync.WaitGroup) {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()

	log.Debug("크롤링 서비스 시작중...")

	if s.running == true {
		defer serviceStopWaiter.Done()

		log.Warn("크롤링 서비스가 이미 시작됨!!!")

		return
	}

	// 크롤링 스케쥴러를 시작한다.
	if rssProviderModel, ok := s.modelFinder.Find(model.RssProviderModel).(*model.RssProvider); ok == true {
		for _, p := range s.config.RssFeed.Providers {
			if rssProviderModel.RssFeedSupportedSite(p.Site) == false {
				m := fmt.Sprintf("RSS Feed Provider에서 지원되지 않는 Site('%s')입니다.", p.Site)

				notifyapi.SendNotifyMessage(m, true)

				log.Panic(m)
			}

			if p.Site == g.RssFeedSupportedSiteNaverCafe {
				if _, err := s.cron.AddJob(p.CrawlingScheduler.TimeSpec, newNaverCafeCrawling(p.Config, p.ID, rssProviderModel)); err != nil {
					m := fmt.Sprintf("네이버 카페('%s') 크롤링 작업의 스케쥴러 등록이 실패하였습니다. (error:%s)", p.Config.ID, err)

					notifyapi.SendNotifyMessage(m, true)

					log.Panic(m)
				}
			} else {
				m := fmt.Sprintf("RSS Feed Provider에서 지원 가능한 Site('%s')가 크롤링 작업의 스케쥴러 등록에서 미구현 되었습니다.", p.Site)

				notifyapi.SendNotifyMessage(m, true)

				log.Panic(m)
			}
		}
	} else {
		m := fmt.Sprintf("RSS Feed Provider를 찾을 수 없습니다.")

		notifyapi.SendNotifyMessage(m, true)

		log.Panic(m)
	}

	s.cron.Start()

	go s.run0(serviceStopCtx, serviceStopWaiter)

	s.running = true

	log.Debug("크롤링 서비스 시작됨")
}

func (s *CrawlingService) run0(serviceStopCtx context.Context, serviceStopWaiter *sync.WaitGroup) {
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
