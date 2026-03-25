package provider

import (
	"errors"
	"fmt"
	"sync"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/rss-feed-server/internal/config"
)

// Registry 등록된 모든 크롤러 설정을 관리하는 중앙 저장소입니다.
type Registry struct {
	factories map[config.ProviderSite]*CrawlerFactory
	mu        sync.RWMutex
}

var defaultRegistry = newRegistry()

func newRegistry() *Registry {
	return &Registry{
		factories: make(map[config.ProviderSite]*CrawlerFactory),
	}
}

// MustRegister 크롤러를 전역 Registry에 등록하며, 실패 시 패닉을 발생시킵니다.
func MustRegister(site config.ProviderSite, factory *CrawlerFactory) {
	if err := defaultRegistry.Register(site, factory); err != nil {
		panic(err.Error())
	}
}

// Register 크롤러를 Registry에 등록합니다.
func (r *Registry) Register(site config.ProviderSite, factory *CrawlerFactory) error {
	if factory == nil {
		return errors.New("CrawlerFactory는 nil일 수 없습니다")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// 중복 등록 방지
	if _, exists := r.factories[site]; exists {
		return fmt.Errorf("Site(%s)에 대한 크롤러가 이미 등록되어 있습니다", site)
	}

	r.factories[site] = factory

	// 로그 기록
	applog.WithComponentAndFields("crawl.provider.registry", applog.Fields{
		"site": site,
	}).Info("Crawler 등록 성공")

	return nil
}

// Lookup 전역 Registry를 통해 주어진 Site에 해당하는 크롤러 팩토리를 검색합니다.
func Lookup(site config.ProviderSite) (*CrawlerFactory, error) {
	return defaultRegistry.Lookup(site)
}

// Lookup Registry에서 크롤러 팩토리를 검색합니다.
func (r *Registry) Lookup(site config.ProviderSite) (*CrawlerFactory, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	factory, exists := r.factories[site]
	if exists {
		return factory, nil
	}

	return nil, errors.New("지원하지 않는 Crawler입니다")
}
