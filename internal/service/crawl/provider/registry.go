package provider

import (
	"sync"

	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/robfig/cron/v3"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
)

// NewCrawlerFunc 새로운 크롤러 인스턴스를 생성하는 팩토리 함수 타입입니다.
type NewCrawlerFunc func(string, *config.ProviderDetailConfig, feed.Repository, *notify.Client) cron.Job

// CrawlerConfig 크롤러 생성 및 실행 구성을 위한 메타데이터를 정의하는 구조체입니다.
type CrawlerConfig struct {
	// NewCrawler 새로운 크롤러 인스턴스를 생성하는 팩토리 함수입니다.
	NewCrawler NewCrawlerFunc
}

// Validate 크롤러 설정의 유효성을 검증합니다.
func (c *CrawlerConfig) Validate() error {
	if c.NewCrawler == nil {
		return apperrors.New(apperrors.InvalidInput, "NewCrawler 팩토리 함수는 필수값입니다")
	}

	return nil
}

// Registry 등록된 모든 크롤러 설정을 관리하는 중앙 저장소입니다.
type Registry struct {
	// configs ProviderSite를 키로 하는 설정 맵입니다.
	configs map[config.ProviderSite]*CrawlerConfig

	// mu 동시성 제어를 위한 읽기/쓰기 락입니다.
	// 등록 시에는 쓰기 락, 조회 시에는 읽기 락을 사용합니다.
	mu sync.RWMutex
}

// defaultRegistry 전역에서 사용하는 기본 Registry 인스턴스입니다.
var defaultRegistry = newRegistry()

// newRegistry 새로운 Registry 인스턴스를 생성합니다.
func newRegistry() *Registry {
	return &Registry{
		configs: make(map[config.ProviderSite]*CrawlerConfig),
	}
}

// MustRegister 크롤러 설정을 전역 Registry에 등록하며, 실패 시 패닉을 발생시킵니다.
func MustRegister(site config.ProviderSite, cfg *CrawlerConfig) {
	if err := defaultRegistry.Register(site, cfg); err != nil {
		panic(err.Error())
	}
}

// Register 크롤러 설정을 Registry에 등록합니다.
func (r *Registry) Register(site config.ProviderSite, cfg *CrawlerConfig) error {
	if cfg == nil {
		return apperrors.New(apperrors.InvalidInput, "크롤러 설정 객체는 필수값입니다")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := cfg.Validate(); err != nil {
		return err
	}

	// 중복 등록 방지
	if _, exists := r.configs[site]; exists {
		return apperrors.Newf(apperrors.Conflict, "해당 Site에 대한 크롤러 설정이 이미 등록되어 있습니다: %s", site)
	}

	r.configs[site] = cfg

	applog.WithComponentAndFields(component, applog.Fields{
		"site": site,
	}).Info("크롤러 설정 등록 성공: 유효성 검증 통과 및 Registry 저장 완료")

	return nil
}

// Lookup 전역 Registry에서 주어진 Site에 해당하는 크롤러 설정을 조회합니다.
func Lookup(site config.ProviderSite) (*CrawlerConfig, error) {
	return defaultRegistry.Lookup(site)
}

// Lookup Registry에서 주어진 Site에 해당하는 크롤러 설정을 조회합니다.
func (r *Registry) Lookup(site config.ProviderSite) (*CrawlerConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cfg, exists := r.configs[site]
	if exists {
		return cfg, nil
	}

	return nil, apperrors.Newf(apperrors.NotFound, "해당 Site(%s)에 대한 크롤러 설정을 찾을 수 없습니다", site)
}
