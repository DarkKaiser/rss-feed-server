package provider

import (
	"context"
	"errors"

	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher"
)

// component 크롤링 서비스의 Provider 로깅용 컴포넌트 이름
const component = "crawl.provider"

// @@@@@
// ErrSkipContentRetry 게시글 본문 크롤링 시 권한 부족, 삭제된 게시글, 레이아웃 변경 등
// 일시적인 네트워크 오류가 아닌 영구적인 오류가 생겨 재시도가 무의미할 때 반환하는 센티넬 에러입니다.
var ErrSkipContentRetry = errors.New("provider: skip article content crawl retry")

// Crawler 개별 크롤러 인스턴스의 생명주기를 제어하고 상태를 조회하기 위한 인터페이스입니다.
//
// 이 인터페이스는 Service 레이어와 구체적인 크롤러 구현체(Base 기반) 사이의 계약을 정의합니다.
// Service는 구현 세부사항을 알 필요 없이 이 인터페이스만으로 크롤러를 실행, 취소, 모니터링할 수 있습니다.
type Crawler interface {
	// ProviderID 이 크롤러가 담당하는 RSS 피드 공급자의 고유 식별자를 반환합니다.
	ProviderID() string

	// Config 이 크롤러가 크롤링할 사이트의 상세 설정 정보(이름, URL, 게시판 목록 등)를 반환합니다.
	Config() *config.ProviderDetailConfig

	// MaxPageCount 크롤링 시 탐색할 최대 페이지 수를 반환합니다.
	// 무한 루프를 방지하고 크롤링 범위를 제한하는 상한값으로 사용됩니다.
	MaxPageCount() int

	// Run 크롤링 작업의 핵심 비즈니스 로직을 실행합니다.
	// 이 메서드는 동기적으로 실행되며, 파라미터로 전달된 ctx가 취소되거나 작업이 완료될 때 정상 반환됩니다.
	Run(ctx context.Context)
}

// CrawlArticlesFunc 실제 웹 페이지 크롤링을 수행하는 함수 타입입니다.
//
// Base 구조체는 이 타입의 함수를 필드로 보유하며, 각 크롤러 구현체는
// SetCrawlArticles()를 통해 자신의 크롤링 로직을 주입합니다. (전략 패턴)
//
// 매개변수:
//   - ctx: 요청의 생명주기를 제어하는 컨텍스트 (취소, 타임아웃 등)
//
// 반환값:
//   - []*feed.Article: 새로 수집된 신규 게시글 목록. 서버 일시 오류로 파싱 자체가 불가한 경우 nil을 반환합니다.
//   - map[string]string: 게시판별 가장 최신 게시글 ID 맵 (key: boardID, value: articleID). 다음 크롤링 시 중복 수집 방지에 사용됩니다.
//   - string: 오류 발생 시 사용자 또는 관리자에게 전달할 알림 메시지. 정상 처리 시 빈 문자열("")을 반환합니다.
//   - error: 오류 객체. 정상 처리 시 nil을 반환합니다.
type CrawlArticlesFunc func(ctx context.Context) ([]*feed.Article, map[string]string, string, error)

// NewCrawlerParams 새로운 크롤러 인스턴스 생성에 필요한 매개변수들을 정의하는 구조체입니다.
type NewCrawlerParams struct {
	// ProviderID 이 크롤러가 담당하는 RSS 피드 공급자의 고유 식별자입니다.
	ProviderID string

	// Config 이 크롤러가 크롤링할 사이트의 상세 설정 정보(이름, URL, 게시판 목록 등)입니다.
	Config *config.ProviderDetailConfig

	// Fetcher HTTP 요청을 수행하는 인터페이스입니다.
	Fetcher fetcher.Fetcher

	// FeedRepo 크롤링된 게시글과 커서 정보를 영구 저장하고 조회하는 저장소 인터페이스입니다.
	FeedRepo feed.Repository

	// NotifyClient 크롤링 오류 발생 시 관리자에게 알림을 전송하는 클라이언트입니다.
	NotifyClient *notify.Client
}

// NewCrawlerFunc 새로운 크롤러 인스턴스를 생성하는 팩토리 함수 타입입니다.
type NewCrawlerFunc func(NewCrawlerParams) (Crawler, error)
