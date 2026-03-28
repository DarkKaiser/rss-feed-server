package provider

import (
	"github.com/darkkaiser/rss-feed-server/internal/config"
)

// Crawler 개별 크롤러 인스턴스의 생명주기를 제어하고 상태를 조회하기 위한 인터페이스입니다.
//
// 이 인터페이스는 Service 레이어와 구체적인 크롤러 구현체(Base 기반) 사이의 계약을 정의합니다.
// Service는 구현 세부사항을 알 필요 없이 이 인터페이스만으로 크롤러를 실행, 취소, 모니터링할 수 있습니다.
type Crawler interface {
	Config() *config.ProviderDetailConfig
	RssFeedProviderID() string
	Site() string
	SiteID() string
	SiteName() string
	SiteDescription() string
	SiteUrl() string
	CrawlingMaxPageCount() int

	// Run 크롤링 작업의 핵심 비즈니스 로직을 실행합니다.
	// 이 메서드는 동기적으로 실행되며, 작업이 완료되거나 취소될 때까지 블로킹됩니다.
	Run()
}
