package crawl

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/darkkaiser/notify-server/pkg/cronx"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"
	_ "github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider/navercafe"
	_ "github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider/ssangbonges"
	_ "github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider/yeosucityhall"
	"github.com/robfig/cron/v3"
)

// component 크롤링 서비스의 로깅용 컴포넌트 이름
const component = "crawl.service"

// Service RSSFeedConfig에 정의된 RSS 피드 제공자(Provider)들을 Cron 스케줄에 맞춰 자동으로 크롤링하는 서비스입니다.
type Service struct {
	cfg *config.RSSFeedConfig

	cron *cron.Cron

	feedRepo feed.Repository

	notifyClient *notify.Client

	running   bool
	runningMu sync.Mutex
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ service.Service = (*Service)(nil)

// NewService 새로운 Crawl 서비스 인스턴스를 생성합니다.
func NewService(cfg *config.RSSFeedConfig, feedRepo feed.Repository, notifyClient *notify.Client) *Service {
	if cfg == nil {
		panic("config.RSSFeedConfig는 필수입니다")
	}
	if feedRepo == nil {
		panic("feed.Repository는 필수입니다")
	}

	return &Service{
		cfg: cfg,

		feedRepo: feedRepo,

		notifyClient: notifyClient,
	}
}

// Start 크롤링 서비스를 시작하고 설정에 정의된 Provider들을 Cron 스케줄러에 등록합니다.
//
// 매개변수:
//   - serviceStopCtx: 서비스 종료 신호를 받기 위한 Context
//   - serviceStopWG: 서비스 종료 완료를 알리기 위한 WaitGroup
//
// 반환값:
//   - error: cfg 또는 feedRepo가 nil인 경우
func (s *Service) Start(serviceStopCtx context.Context, serviceStopWG *sync.WaitGroup) error {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()

	applog.WithComponent(component).Info("서비스 시작 진입: 크롤링 서비스 초기화 프로세스를 시작합니다")

	if s.cfg == nil {
		serviceStopWG.Done()
		return apperrors.New(apperrors.Internal, "config.RSSFeedConfig 객체가 초기화되지 않았습니다")
	}
	if s.feedRepo == nil {
		serviceStopWG.Done()
		return apperrors.New(apperrors.Internal, "feed.Repository 객체가 초기화되지 않았습니다")
	}

	if s.running {
		serviceStopWG.Done()
		applog.WithComponent(component).Warn("크롤링 서비스가 이미 실행 중입니다 (중복 호출)")
		return nil
	}

	// 1. Cron 엔진 초기화
	// - StandardParser: 초 단위 스케줄링 지원 (6개 필드: 초 분 시 일 월 요일)
	// - Recover: Panic 발생 시 복구하여 다른 작업에 영향을 주지 않음
	// - SkipIfStillRunning: 이전 실행이 끝나지 않았으면 다음 실행을 건너뜀
	s.cron = cron.New(
		cron.WithParser(cronx.StandardParser()),
		cron.WithLogger(cron.VerbosePrintfLogger(applog.StandardLogger())),
		cron.WithChain(
			cron.Recover(cron.VerbosePrintfLogger(applog.StandardLogger())),
			cron.SkipIfStillRunning(cron.VerbosePrintfLogger(applog.StandardLogger())),
		),
	)

	// 2. 작업 등록
	if err := s.registerJobs(serviceStopCtx); err != nil {
		s.cron = nil
		serviceStopWG.Done()
		return err
	}

	// 3. 스케줄러 시작
	s.cron.Start()
	s.running = true

	applog.WithComponentAndFields(component, applog.Fields{
		"configured_providers": len(s.cfg.Providers),
		"registered_schedules": len(s.cron.Entries()),
		"notify_enabled":       s.notifyClient != nil,
	}).Info("서비스 시작 완료: Scheduler 서비스가 정상적으로 초기화되었습니다")

	// 4. 종료 신호 대기 (고루틴)
	// 서비스 생명주기 컨텍스트(serviceStopCtx)의 취소 이벤트를 비동기로 모니터링합니다.
	// 종료 시그널 수신 시 Stop() 메서드를 호출하여 리소스를 안전하게 해제하고 그 결과를 보장합니다.
	go func() {
		defer serviceStopWG.Done()

		<-serviceStopCtx.Done()

		s.stop()
	}()

	return nil
}

// stop 실행 중인 스케줄러를 안전하게 중지합니다.
func (s *Service) stop() {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()

	if !s.running {
		return
	}

	applog.WithComponent(component).Info("종료 절차 진입: 크롤링 서비스 중지 시그널을 수신했습니다")

	// Cron 엔진 중지 및 실행 중인 작업 완료 대기
	if s.cron != nil {
		ctx := s.cron.Stop()
		<-ctx.Done()
	}

	s.cron = nil
	s.running = false

	applog.WithComponent(component).Info("크롤링 서비스 종료 완료: 모든 리소스가 정리되었습니다")
}

// registerJobs 설정 파일에 정의된 모든 Provider를 순회하며 Cron 스케줄러에 등록합니다.
func (s *Service) registerJobs(_ context.Context) error {
	for _, p := range s.cfg.Providers {
		cfg, err := provider.Lookup(config.ProviderSite(p.Site))
		if err != nil {
			s.logAndNotifyError(fmt.Sprintf("Site(%s, ID: %s)에 매핑된 크롤러 구현체가 없어 스케줄 등록에 실패했습니다.", p.Site, p.ID), err)
			return apperrors.Wrapf(err, apperrors.Internal, "크롤러 스케줄 등록 실패: Site(%s)에 매핑된 크롤러 구현체가 없습니다", p.Site)
		}

		if _, err := s.cron.AddJob(p.Scheduler.TimeSpec, cfg.NewCrawler(p.ID, p.Config, s.feedRepo, s.notifyClient)); err != nil {
			s.logAndNotifyError(fmt.Sprintf("Site(%s, ID: %s)의 Cron 표현식 구문에 오류가 있어 스케줄 등록에 실패했습니다.", p.Site, p.ID), err)
			return apperrors.Wrapf(err, apperrors.Internal, "크롤러 스케줄 등록 실패: Cron 표현식 구문이 잘못되었습니다 (Site: %s, ID: %s, TimeSpec: '%s')", config.ProviderSite(p.Site), p.ID, p.Scheduler.TimeSpec)
		}
	}

	return nil
}

// logAndNotifyError 크롤러 실행 중 발생한 오류를 로깅하고 관리자에게 알림을 전송합니다.
func (s *Service) logAndNotifyError(message string, err error) {
	fields := applog.Fields{}

	if err != nil {
		fields["error"] = err

		// 에러 객체가 있으면 메시지에 상세 내용 추가
		message = fmt.Sprintf("%s: %v", message, err)
	}

	applog.WithComponentAndFields(component, fields).Error(message)

	// ========================================
	// 에러 알림 전송
	// ========================================
	if s.notifyClient != nil {
		go func(msg string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			s.notifyClient.NotifyError(ctx, msg)
		}(message)
	}
}
