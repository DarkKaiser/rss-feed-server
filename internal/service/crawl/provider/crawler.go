package provider

import (
	"context"

	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
)

// component 크롤링 서비스의 Provider 로깅용 컴포넌트 이름
const component = "crawl.provider"

// Crawler 개별 크롤러 인스턴스의 생명주기를 제어하고 상태를 조회하기 위한 인터페이스입니다.
//
// 이 인터페이스는 Service 레이어와 구체적인 크롤러 구현체(Base 기반) 사이의 계약을 정의합니다.
// Service는 구현 세부사항을 알 필요 없이 이 인터페이스만으로 크롤러를 실행, 취소, 모니터링할 수 있습니다.
type Crawler interface {
	// @@@@@
	ProviderID() string
	Config() *config.ProviderDetailConfig
	Site() string
	SiteID() string
	SiteName() string
	SiteDescription() string
	SiteUrl() string
	CrawlingMaxPageCount() int

	// Run 크롤링 작업의 핵심 비즈니스 로직을 실행합니다.
	// 이 메서드는 동기적으로 실행되며, 파라미터로 전달된 ctx가 취소되거나 작업이 완료될 때 정상 반환됩니다.
	Run(ctx context.Context)
}

// @@@@@
// CrawlArticlesFunc는 실제 웹 페이지 크롤링을 수행하는 함수의 타입입니다.
// 전략 패턴(Strategy Pattern)을 통해 base crawler가 구체적인 크롤링 구현에 의존하지
// 않도록 분리하며, 각 크롤러 구조체(예: crawler)는 자신의 crawlArticles
// 메서드를 이 타입으로 Base.CrawlArticles 필드에 주입합니다.
//
// 반환값:
//   - []*feed.Article:      새로 발견된 신규 게시글 목록 (서버 오류 시 nil 반환)
//   - map[string]string:    게시판별 최신 크롤링 게시글 ID 맵 (key: boardID, value: articleID)
//   - string:               오류 발생 시 사용자/관리자에게 전달할 오류 메시지 문자열
//   - error:                오류 객체 (정상 처리 시 nil)
type CrawlArticlesFunc func(ctx context.Context) ([]*feed.Article, map[string]string, string, error)

// NewCrawlerParams 새로운 크롤러 인스턴스 생성에 필요한 매개변수들을 정의하는 구조체입니다.
type NewCrawlerParams struct {
	// @@@@@
	// ProviderID 설정 파일에 정의된 Provider의 고유 식별자입니다.
	ProviderID string

	// @@@@@
	// Config 해당 Provider(게시판)의 세부 설정 정보입니다.
	Config *config.ProviderDetailConfig

	// @@@@@
	// FeedRepo 수집된 게시글과 크롤링 상태를 저장하는 데이터베이스 접근 인터페이스입니다.
	FeedRepo feed.Repository

	// @@@@@
	// NotifyClient 크롤링 오류나 상태를 외부(텔레그램 등)로 알리기 위한 클라이언트입니다.
	NotifyClient *notify.Client
}

// NewCrawlerFunc 새로운 크롤러 인스턴스를 생성하는 팩토리 함수 타입입니다.
type NewCrawlerFunc func(NewCrawlerParams) Crawler
