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

// mockFeedRepo는 테스트를 추상화하기 위한 feed.Repository 더미 구현체입니다.
type mockFeedRepo struct {
	feed.Repository
}

// testCrawlerDone은 크롤러 실행 진입 여부를 비동기로 감지하기 위한 전역 채널 구조체입니다.
var testCrawlerDone chan struct{}

// mockCrawler는 테스트 중 실제 크롤러 동작을 대체합니다.
// done 채널을 통해 Run() 메서드의 호출과 완료 시점을 동기화(Sync)합니다.
type mockCrawler struct {
	config *config.ProviderDetailConfig
	id     string
}

func (m *mockCrawler) ProviderID() string                   { return m.id }
func (m *mockCrawler) Config() *config.ProviderDetailConfig { return m.config }
func (m *mockCrawler) MaxPageCount() int                    { return 1 }
func (m *mockCrawler) Run(ctx context.Context) {
	if testCrawlerDone != nil {
		// 채널이 열려있다면 1회만 데이터 전송 (다중 실행 환경 방어)
		select {
		case testCrawlerDone <- struct{}{}:
		default:
		}
	}
}

// mockFetcher는 Fetcher 리소스 반환 실패(Close Error) 시나리오 검증용 구조체입니다.
type mockFetcher struct {
	CloseError error
}

func (m *mockFetcher) Do(req *http.Request) (*http.Response, error) { return nil, nil }
func (m *mockFetcher) Close() error                                 { return m.CloseError }

func init() {
	// 정상적인 Provider 식별자 예약
	provider.MustRegister("test_site_success", &provider.CrawlerConfig{
		NewCrawler: func(params provider.NewCrawlerParams) (provider.Crawler, error) {
			return &mockCrawler{
				config: params.Config,
				id:     params.ProviderID,
			}, nil
		},
	})

	// 잘못된 Cron 스케줄 테스트를 위한 Mock Provider 등록
	provider.MustRegister("bad_cron_site", &provider.CrawlerConfig{
		NewCrawler: func(params provider.NewCrawlerParams) (provider.Crawler, error) {
			return &mockCrawler{}, nil
		},
	})

	// 팩토리 초기화 에러 반환을 위한 Mock Provider 등록
	provider.MustRegister("new_crawler_fail_site", &provider.CrawlerConfig{
		NewCrawler: func(params provider.NewCrawlerParams) (provider.Crawler, error) {
			return nil, errors.New("초기화 팩토리 검증 오류")
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
			assert.NotNil(t, s.fetcher) // Fetcher 인스턴스 초기화 보장
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
	testCrawlerDone = make(chan struct{}, 1) // 통신용 버퍼 채널 초기화

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

	t.Run("성공: Start 호출 및 중복 방어, 채널 기반 동기화 및 Graceful Shutdown", func(t *testing.T) {
		s := NewService(cfg, repo, nil)

		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup

		wg.Add(1)
		err := s.Start(ctx, &wg)
		require.NoError(t, err)

		assert.True(t, s.running, "서비스 상태 플래그가 활성화되어야 합니다")
		assert.NotNil(t, s.cron, "Cron 인스턴스가 생성되어야 합니다")
		assert.Equal(t, 1, len(s.cron.Entries()), "1개의 Provider 일정이 등록되어야 합니다")

		// 중복 Start 호출 시 Idempotence 확보
		wg.Add(1)
		err = s.Start(ctx, &wg)
		assert.NoError(t, err)

		// Flaky한 time.Sleep 대신 고루틴 통신 채널을 통한 엄격한 동기 처리
		select {
		case <-testCrawlerDone:
			// Run() 호출이 정상 트리거 됨
		case <-time.After(3 * time.Second):
			t.Fatal("Start 내부의 Cron 스케줄러가 3초 내에 트리거되지 않았습니다.")
		}

		// 구조적인 종료 플로우 및 리소스 반환 검증
		cancel()

		waitCh := make(chan struct{})
		go func() {
			wg.Wait()
			close(waitCh)
		}()

		select {
		case <-waitCh:
		case <-time.After(2 * time.Second):
			t.Fatal("Start 내부 고루틴의 종료 지연 발생 (고루틴 유출 위험)")
		}

		assert.False(t, s.running, "컨텍스트 취소 시 완전히 중지되어야 합니다")
		assert.Nil(t, s.cron, "Cron 리소스는 nil로 GC 반환 처리되어야 합니다")
	})

	t.Run("실패: registerJobs 내부 에러 전파 시 즉시 취소", func(t *testing.T) {
		cfgFail := &config.RSSFeedConfig{
			Providers: []*config.ProviderConfig{{Site: "unknown_illegal_site"}},
		}
		s := NewService(cfgFail, repo, nil)
		var wg sync.WaitGroup
		wg.Add(1)
		err := s.Start(context.Background(), &wg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "크롤러 구현체가 없습니다")
		assert.Nil(t, s.cron, "실패 시 cron 인스턴스는 즉각 파괴되어야 합니다")
	})

	t.Run("성공: 명시적인 stop() 메서드 호출 동작 검증 및 중복 정지 방어", func(t *testing.T) {
		s := NewService(cfg, repo, nil)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var wg sync.WaitGroup

		wg.Add(1)
		err := s.Start(ctx, &wg)
		require.NoError(t, err)

		// 강제 정지 명령
		s.stop()

		assert.False(t, s.running)
		assert.Nil(t, s.cron)

		// 중복 stop() 호출 - Panic/Error 없이 패스되어야 함 (!s.running 통과 확인용)
		assert.NotPanics(t, func() {
			s.stop()
		})
	})

	t.Run("실패: Configuration 포인터 소멸(cfg == nil) 검증", func(t *testing.T) {
		s := &Service{cfg: nil, feedRepo: repo}
		var wg sync.WaitGroup
		wg.Add(1)
		err := s.Start(context.Background(), &wg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "config.RSSFeedConfig 객체가 초기화되지 않았습니다")
	})

	t.Run("실패: Repository 포인터 소멸(feedRepo == nil) 검증", func(t *testing.T) {
		s := &Service{cfg: cfg, feedRepo: nil}
		var wg sync.WaitGroup
		wg.Add(1)
		err := s.Start(context.Background(), &wg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "feed.Repository 객체가 초기화되지 않았습니다")
	})

	t.Run("성공: 모든 리소스(cron, fetcher)가 nil일 때 stop() 호출 시 패닉 없음", func(t *testing.T) {
		s := &Service{running: true, cron: nil, fetcher: nil}
		assert.NotPanics(t, func() {
			s.stop()
		})
		assert.False(t, s.running)
	})
}

func TestService_stop_CloseError(t *testing.T) {
	// fetcher.Close() 호출 시 에러가 발생하는 예외 상황을 처리하는 방어 로직 검증 (100% 커버리지 확보)
	t.Run("성공: Fetcher.Close 에러 로깅 시 패닉 없이 안전한 서비스 종료", func(t *testing.T) {
		s := NewService(&config.RSSFeedConfig{}, &mockFeedRepo{}, nil)
		s.running = true // !s.running 조기 반환(Early Return) 우회
		s.fetcher = &mockFetcher{CloseError: errors.New("mock network resource close error")}

		assert.NotPanics(t, func() {
			s.stop()
		})
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
		s.cron = cron.New()

		err := s.registerJobs(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Site(unknown_illegal_site)에 매핑된 크롤러 구현체가 없습니다")
	})

	t.Run("실패: 잘못된 Cron 문자열(표현식) 지정 시 에러", func(t *testing.T) {
		cfg := &config.RSSFeedConfig{
			Providers: []*config.ProviderConfig{
				{Site: "bad_cron_site", Scheduler: config.SchedulerConfig{TimeSpec: "invalid_%_string"}},
			},
		}
		s := NewService(cfg, repo, nil)
		s.cron = cron.New(cron.WithParser(cronx.StandardParser()))

		err := s.registerJobs(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Cron 표현식 구문이 잘못되었습니다")
	})

	t.Run("실패: NewCrawler 초기화 중 오류 반환 시 에러 전파", func(t *testing.T) {
		cfg := &config.RSSFeedConfig{
			Providers: []*config.ProviderConfig{
				{Site: "new_crawler_fail_site"},
			},
		}
		s := NewService(cfg, repo, nil)
		s.cron = cron.New(cron.WithParser(cronx.StandardParser()))

		err := s.registerJobs(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "초기화 팩토리 검증 오류")
	})
}

func TestService_logAndNotifyError(t *testing.T) {
	t.Run("성공: 알림 클라이언트가 nil일 때 패닉 없이 로그만 처리", func(t *testing.T) {
		s := NewService(&config.RSSFeedConfig{}, &mockFeedRepo{}, nil)

		assert.NotPanics(t, func() {
			s.logAndNotifyError("알림 채널 없는 에러 통제 테스트", errors.New("mock background error"))
		})
	})

	t.Run("성공: 알림 클라이언트가 존재할 때 정상적인 비동기 API 발송 트리거", func(t *testing.T) {
		requestReceived := make(chan struct{}, 1)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestReceived <- struct{}{}
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		notifyClient, err := notify.NewClient(&notify.Config{
			URL:           ts.URL,
			AppKey:        "test",
			ApplicationID: "test",
		})
		require.NoError(t, err)

		s := NewService(&config.RSSFeedConfig{}, &mockFeedRepo{}, notifyClient)

		// 발송 개시
		s.logAndNotifyError("통합 발송 테스트", errors.New("트리거 작동"))

		select {
		case <-requestReceived:
			// 성공적인 서버 응답 확인
		case <-time.After(2 * time.Second):
			t.Fatal("가짜 서버(NotifyTestServer)로 2초 내에 요청이 들어오지 않았습니다.")
		}
	})
}
