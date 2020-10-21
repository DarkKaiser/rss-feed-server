package crawling

import (
	"context"
	"fmt"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
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

	running   bool
	runningMu sync.Mutex
}

func NewService(config *g.AppConfig) *CrawlingService {
	return &CrawlingService{
		config: config,

		cron: cron.New(cron.WithLogger(cron.VerbosePrintfLogger(log.StandardLogger()))),

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
	for _, c := range s.config.Crawling.NaverCafe {
		var boards []*naverCafeCrawlingBoard
		for _, b := range c.Boards {
			boards = append(boards, &naverCafeCrawlingBoard{
				id:               b.ID,
				name:             b.Name,
				tp:               b.Type,
				contentCanBeRead: b.ContentCanBeRead,
			})
		}

		crawlingJob := &naverCafeCrawling{
			id:          c.ID,
			clubID:      c.ClubID,
			name:        c.Name,
			description: c.Description,
			url:         c.Url,

			boards: boards,
		}

		_, err := s.cron.AddJob(c.Scheduler.TimeSpec, crawlingJob)
		if err != nil {
			m := fmt.Sprintf("네이버 카페(%s) 크롤링 작업의 스케쥴러 등록이 실패하였습니다. (error:%s)", c.ID, err)

			notifyapi.SendNotifyMessage(m, true)

			log.Panic(m)
		}
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
