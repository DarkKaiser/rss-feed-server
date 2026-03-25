package crawl

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/darkkaiser/notify-server/pkg/cronx"
	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"
	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockFeedRepo는 테스트를 추상화하기 위한 feed.Repository 빈(Dummy) 구현체입니다.
type mockFeedRepo struct {
	feed.Repository
}

func TestNewService(t *testing.T) {
	cfg := &config.RSSFeedConfig{}
	repo := &mockFeedRepo{}
	var client *notify.Client // nil 허용 여부(선택적 알림) 검증

	t.Run("성공: 올바른 의존성 주입 시 정상 초기화", func(t *testing.T) {
		assert.NotPanics(t, func() {
			s := NewService(cfg, repo, client)
			assert.NotNil(t, s)
			assert.Equal(t, cfg, s.cfg)
			assert.Equal(t, repo, s.feedRepo)
		})
	})

	t.Run("실패: RSSFeedConfig 누락 시 패닉", func(t *testing.T) {
		assert.PanicsWithValue(t, "config.RSSFeedConfig는 필수입니다", func() {
			NewService(nil, repo, client)
		})
	})

	t.Run("실패: Repository 누락 시 패닉", func(t *testing.T) {
		assert.PanicsWithValue(t, "feed.Repository는 필수입니다", func() {
			NewService(cfg, nil, client)
		})
	})
}

func TestService_StartAndStop(t *testing.T) {
	// 정상 흐름 검증을 위한 가짜 크롤러 레지스트리 사전 등록
	provider.MustRegister("test_start_site", &provider.CrawlerConfig{
		NewCrawler: func(id string, c *config.ProviderDetailConfig, r feed.Repository, n *notify.Client) cron.Job {
			return cron.FuncJob(func() {}) // 실행되지 않을 빈 작업
		},
	})

	cfg := &config.RSSFeedConfig{
		Providers: []*config.ProviderConfig{
			&config.ProviderConfig{
				Site:      "test_start_site",
				ID:        "test-uuid-1",
				Scheduler: config.SchedulerConfig{TimeSpec: "* * * * * *"}, // 초 단위 구문 (cronx)
			},
		},
	}
	repo := &mockFeedRepo{}

	t.Run("성공적인 Start 호출 및 중복 실행 방어(Idempotence)", func(t *testing.T) {
		s := NewService(cfg, repo, nil)

		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		wg.Add(1)

		err := s.Start(ctx, &wg)
		require.NoError(t, err)
		assert.True(t, s.running, "서비스 상태 플래그가 활성화되어야 합니다")
		assert.NotNil(t, s.cron, "Cron 인스턴스가 생성되어야 합니다")
		assert.Equal(t, 1, len(s.cron.Entries()), "1개의 Provider 일정이 성공적으로 등록되어야 합니다")

		// 중복 Start 호출 시 내부적으로 무시되며 리소스 충돌(Err)이 없어야 함을 검증
		wg.Add(1)
		err = s.Start(ctx, &wg)
		assert.NoError(t, err)

		// 고루틴 트리거 메커니즘을 통한 정상 종료 플로우 검증
		cancel()
		wg.Wait() // Start 내에 생성된 비동기 종료 감시 고루틴의 작업 종료 보장

		assert.False(t, s.running, "컨텍스트 취소 후 서비스는 완벽히 중지 상태가 되어야 합니다")
		assert.Nil(t, s.cron, "cron 리소스 참조가 메모리 누수 방지를 위해 nil로 안전하게 초기화되어야 합니다")
	})

	t.Run("실패: Configuration 포인터 소멸(cfg == nil)", func(t *testing.T) {
		s := &Service{cfg: nil, feedRepo: repo}
		var wg sync.WaitGroup
		wg.Add(1)
		err := s.Start(context.Background(), &wg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "config.RSSFeedConfig 객체가 초기화되지 않았습니다")
	})

	t.Run("실패: Repository 포인터 소멸(feedRepo == nil)", func(t *testing.T) {
		s := &Service{cfg: cfg, feedRepo: nil}
		var wg sync.WaitGroup
		wg.Add(1)
		err := s.Start(context.Background(), &wg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "feed.Repository 객체가 초기화되지 않았습니다")
	})
}

func TestService_registerJobs_Exceptions(t *testing.T) {
	repo := &mockFeedRepo{}

	t.Run("레지스트리에 구현체가 등록되지 않은 Provider 사이트 요청 시 거부", func(t *testing.T) {
		cfg := &config.RSSFeedConfig{
			Providers: []*config.ProviderConfig{
				&config.ProviderConfig{Site: "unknown_illegal_site", ID: "u-1"},
			},
		}
		s := NewService(cfg, repo, nil)
		s.cron = cron.New(cron.WithParser(cronx.StandardParser()))

		err := s.registerJobs(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "크롤러 스케줄 등록 실패: Site(unknown_illegal_site)에 매핑된 크롤러 구현체가 없습니다")
	})

	t.Run("잘못된 Cron 문자열(표현식) 지정 시 구조적 결함 식별", func(t *testing.T) {
		provider.MustRegister("bad_cron_site", &provider.CrawlerConfig{
			NewCrawler: func(id string, c *config.ProviderDetailConfig, r feed.Repository, n *notify.Client) cron.Job {
				return cron.FuncJob(func() {})
			},
		})

		cfg := &config.RSSFeedConfig{
			Providers: []*config.ProviderConfig{
				&config.ProviderConfig{
					Site:      "bad_cron_site",
					ID:        "b-1",
					Scheduler: config.SchedulerConfig{TimeSpec: "invalid_% $ # string"}, // 의도적인 문법 파괴
				},
			},
		}
		s := NewService(cfg, repo, nil)
		s.cron = cron.New(cron.WithParser(cronx.StandardParser()))

		err := s.registerJobs(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Cron 표현식 구문이 잘못되었습니다")
	})
}

func TestService_logAndNotifyError(t *testing.T) {
	s := NewService(&config.RSSFeedConfig{}, &mockFeedRepo{}, nil)

	// notifyClient가 인스턴스화되지 않았을 때 (nil), 백그라운드 고루틴이 
	// 패닉 없이 로깅 파이프라인만 정상 동작(Skip)하는지 검증합니다.
	assert.NotPanics(t, func() {
		// 변경된 시그니처: message 뒤에 error 객체를 수용
		s.logAndNotifyError("에러 파이프라인 테스트", errors.New("mock background pipeline error"))
	})
}
