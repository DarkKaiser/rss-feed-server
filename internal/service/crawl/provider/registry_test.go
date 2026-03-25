package provider

import (
	"fmt"
	"sync"
	"testing"

	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCrawlerFunc는 테스트를 위해 최소한의 시그니처만 맞춘 팩토리 함수입니다.
func mockCrawlerFunc(_ string, _ *config.ProviderDetailConfig, _ feed.Repository, _ *notify.Client) cron.Job {
	return nil
}

func TestCrawlerConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *CrawlerConfig
		wantErr bool
	}{
		{
			name: "성공: 정상적인 팩토리 함수 제공",
			cfg: &CrawlerConfig{
				NewCrawler: mockCrawlerFunc,
			},
			wantErr: false,
		},
		{
			name: "실패: 팩토리 함수 누락",
			cfg: &CrawlerConfig{
				NewCrawler: nil,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	registry := newRegistry()
	site := config.ProviderSite("test_site")
	cfg := &CrawlerConfig{NewCrawler: mockCrawlerFunc}

	// 1. 초기 조회 실패 확인 (데이터 없음)
	result, err := registry.Lookup(site)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "찾을 수 없습니다")

	// 2. 정상 등록 확인
	err = registry.Register(site, cfg)
	require.NoError(t, err)

	// 3. 등록 후 조회 성공 확인
	result, err = registry.Lookup(site)
	require.NoError(t, err)
	assert.Equal(t, cfg, result)

	// 4. 중복 등록 실패 확인
	err = registry.Register(site, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "이미 등록되어 있습니다")

	// 5. 구조체 누락 (cfg nil) 실패 확인
	err = registry.Register("another_site", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "크롤러 설정 객체는 필수값입니다")

	// 6. 유효성 검증(Validate) 실패에 의한 등록 거부 확인
	err = registry.Register("invalid_site", &CrawlerConfig{NewCrawler: nil})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NewCrawler 팩토리 함수는 필수값입니다")
}

func TestMustRegister(t *testing.T) {
	// MustRegister는 패닉을 발생시킬 수 있고 전역 상태를 오염시키므로
	// 기존 상태를 저장해두고 테스트 후 복구합니다.
	originalRegistry := defaultRegistry
	defer func() { defaultRegistry = originalRegistry }()
	defaultRegistry = newRegistry()

	site := config.ProviderSite("must_test_site")
	cfg := &CrawlerConfig{NewCrawler: mockCrawlerFunc}

	// 1. 정상 등록으로 인한 일반 동작 확인
	assert.NotPanics(t, func() {
		MustRegister(site, cfg)
	})

	// 2. 전역 Lookup() 함수 연동 점검
	res, err := Lookup(site)
	require.NoError(t, err)
	assert.Equal(t, cfg, res)

	// 3. 중복 등록으로 인한 패닉 발생 점검
	assert.Panics(t, func() {
		MustRegister(site, cfg)
	})
}

func TestRegistry_Concurrency(t *testing.T) {
	registry := newRegistry()
	var wg sync.WaitGroup
	workers := 100

	// 동시성 등록 및 조회 테스트 (Race Condition 및 Thread Safety 감지 목적)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			site := config.ProviderSite(fmt.Sprintf("concurrent_site_%d", id))
			cfg := &CrawlerConfig{NewCrawler: mockCrawlerFunc}

			// 등록
			_ = registry.Register(site, cfg)

			// 방금 등록한 site 조회
			_, _ = registry.Lookup(site)

			// 등록되지 않은 랜덤 site 조회
			_, _ = registry.Lookup(config.ProviderSite(fmt.Sprintf("unknown_site_%d", id)))
		}(i)
	}

	wg.Wait()

	// 모든 워커가 종료된 후 총 레지스트리 길이를 검증하여 데이터 손실이 없는지 확인
	assert.Equal(t, workers, len(registry.configs), "모든 동시성 등록 요청이 성공적으로 처리되어야 합니다")
}
