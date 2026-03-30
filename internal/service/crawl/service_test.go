package crawl

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

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
	feed.Repository // interface 지연 로딩용
}

type mockCrawler struct {
	config *config.ProviderDetailConfig
	id     string
}

func (m *mockCrawler) Config() *config.ProviderDetailConfig { return m.config }
func (m *mockCrawler) ProviderID() string                   { return m.id }
func (m *mockCrawler) Site() string                         { return "mock" }
func (m *mockCrawler) SiteID() string                       { return "mock-id" }
func (m *mockCrawler) SiteName() string                     { return "mock-name" }
func (m *mockCrawler) SiteDescription() string              { return "mock-desc" }
func (m *mockCrawler) SiteUrl() string                      { return "http://mock" }
func (m *mockCrawler) CrawlingMaxPageCount() int            { return 1 }
func (m *mockCrawler) Run(ctx context.Context)              {} // 안전한 더미 실행 메서드

func init() {
	// 정상적인 Provider 식별자 예약
	provider.MustRegister("test_site_success", &provider.CrawlerConfig{
		NewCrawler: func(params provider.NewCrawlerParams) provider.Crawler {
			return &mockCrawler{
				config: params.Config,
				id:     params.ProviderID,
			}
		},
	})

	// 잘못된 Cron 스케줄 테스트를 위한 Mock Provider 등록
	provider.MustRegister("bad_cron_site", &provider.CrawlerConfig{
		NewCrawler: func(params provider.NewCrawlerParams) provider.Crawler {
			return &mockCrawler{}
		},
	})
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
	cfg := &config.RSSFeedConfig{
		Providers: []*config.ProviderConfig{
			{
				Site:      "test_site_success",
				ID:        "test-uuid-1",
				Scheduler: config.SchedulerConfig{TimeSpec: "* * * * * *"}, // 초 단위 스케줄러 (매초마다 실행)
			},
		},
	}
	repo := &mockFeedRepo{}

	t.Run("성공: 정상적인 Start 호출 및 중복 실행 방어(Idempotence)", func(t *testing.T) {
		s := NewService(cfg, repo, nil)

		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup

		wg.Add(1)
		err := s.Start(ctx, &wg)
		require.NoError(t, err)

		assert.True(t, s.running, "서비스 상태 플래그가 활성화되어야 합니다")
		assert.NotNil(t, s.cron, "Cron 인스턴스가 생성되어야 합니다")
		assert.Equal(t, 1, len(s.cron.Entries()), "1개의 Provider 일정이 성공적으로 등록되어야 합니다")

		// 중복 Start 호출 시 에러 없이 통과하며, s.running은 계속 활성 상태여야 함 검증
		wg.Add(1)
		err = s.Start(ctx, &wg)
		assert.NoError(t, err)
		assert.True(t, s.running)

		// Cron 작업이 최소 1회 실행되어 crawler.Run() 커버리지를 채우도록 대기
		time.Sleep(1200 * time.Millisecond)

		// 고루틴 트리거 메커니즘을 통한 정상 종료 플로우 검증
		cancel() // context cancel 신호 발생

		// WaitGroup 타임아웃 메커니즘 구축 (고루틴 유출 방지 테스트)
		waitCh := make(chan struct{})
		go func() {
			wg.Wait()
			close(waitCh)
		}()

		select {
		case <-waitCh:
		case <-time.After(2 * time.Second):
			t.Fatal("Start 내부의 종료 고루틴이 2초 내에 응답하지 않아 고루틴 유출이 의심됩니다.")
		}

		assert.False(t, s.running, "컨텍스트 취소 시 서비스는 완벽히 중지 상태여야 합니다")
		assert.Nil(t, s.cron, "Cron 리소스는 nil로 정리되어 메모리 최적화가 이뤄져야 합니다")
	})

	t.Run("실패: Configuration 포인터 소멸(cfg == nil)", func(t *testing.T) {
		// NewService를 우회하여 강제로 잘못된 상태 주입
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

	t.Run("실패: registerJobs 내부 에러 전파 및 스케줄러 중단", func(t *testing.T) {
		cfgFail := &config.RSSFeedConfig{
			Providers: []*config.ProviderConfig{
				{Site: "unknown_illegal_site", ID: "u-1"}, // 실패 유도
			},
		}
		s := NewService(cfgFail, repo, nil)
		var wg sync.WaitGroup
		wg.Add(1)
		err := s.Start(context.Background(), &wg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "크롤러 구현체가 없습니다")
		assert.Nil(t, s.cron, "실패 시 cron 인스턴스는 즉각 정리되어야 합니다")
	})

	t.Run("성공: 명시적인 stop() 메서드 호출에 의한 종료 검증", func(t *testing.T) {
		s := NewService(cfg, repo, nil)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel() // 테스트 종료 시 클린업
		var wg sync.WaitGroup

		wg.Add(1)
		err := s.Start(ctx, &wg)
		require.NoError(t, err)

		assert.True(t, s.running)

		// 강제로 내부 중지 메서드 호출
		s.stop()

		assert.False(t, s.running)
		assert.Nil(t, s.cron)
	})
}

func TestService_registerJobs_Exceptions(t *testing.T) {
	repo := &mockFeedRepo{}

	t.Run("실패: 레지스트리에 구현체가 등록되지 않은 Provider 사이트 요청 시 에러", func(t *testing.T) {
		cfg := &config.RSSFeedConfig{
			Providers: []*config.ProviderConfig{
				{Site: "unknown_illegal_site", ID: "u-1"},
			},
		}
		s := NewService(cfg, repo, nil)
		s.cron = cron.New(cron.WithParser(cronx.StandardParser())) // registerJobs 전 cron 껍데기 세팅

		err := s.registerJobs(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Site(unknown_illegal_site)에 매핑된 크롤러 구현체가 없습니다")
	})

	t.Run("실패: 잘못된 Cron 문자열(표현식) 지정 시 에러", func(t *testing.T) {
		cfg := &config.RSSFeedConfig{
			Providers: []*config.ProviderConfig{
				{
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
	t.Run("성공: 알림 클라이언트(NotifyClient)가 nil일 때 패닉 없이 일반 로깅 처리", func(t *testing.T) {
		s := NewService(&config.RSSFeedConfig{}, &mockFeedRepo{}, nil)

		assert.NotPanics(t, func() {
			s.logAndNotifyError("알림 채널 없는 에러 파이프라인 테스트", errors.New("mock background error"))
		})
	})

	t.Run("성공: 알림 클라이언트가 존재할 때 정상적인 API 발송 트리거", func(t *testing.T) {
		// HTTP 테스트 서버 구축을 통한 notify.Client 거동 가로채기
		requestReceived := make(chan bool, 1) // 호출 여부 트래킹 채널
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Notify API 호출을 가로챕니다.
			requestReceived <- true
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		// notify.Client 초기화
		notifyClient, err := notify.NewClient(&notify.Config{
			URL:           ts.URL,
			AppKey:        "test-app-key",
			ApplicationID: "test-app-id",
		})
		require.NoError(t, err)

		s := NewService(&config.RSSFeedConfig{}, &mockFeedRepo{}, notifyClient)

		// 에러 알림 발송 실행
		s.logAndNotifyError("알림 발송 통합 테스트", errors.New("발송되어야만 하는 에러"))

		// 알림 전송이 비동기 고루틴 내부에서 이루어지므로, 타이머를 두고 대기
		select {
		case <-requestReceived:
			// 성공적인 서버 응답 확인
		case <-time.After(2 * time.Second):
			t.Fatal("제한 시간 내에 가짜 서버(NotifyTestServer)로 알림 요청이 전달되지 않았습니다.")
		}
	})
}
