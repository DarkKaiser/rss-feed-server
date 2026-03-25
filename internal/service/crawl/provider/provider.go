package provider

import (
	"context"

	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/robfig/cron/v3"
)

// component 크롤링 서비스의 Provider 로깅용 컴포넌트 이름
const component = "crawl.provider"

// @@@@@
// DefaultBoardKey는 게시판 구분이 없는 단일 게시판 Provider에서
// 게시판 ID를 대신하여 사용하는 sentinel(기본값 표식) 상수입니다.
// DB 갱신 시 이 값을 감지하면 빈 문자열("")로 변환되어 저장됩니다.
// 예: updateLatestCrawledIDs 함수에서 boardID == DefaultBoardKey 조건으로 처리됩니다.
const DefaultBoardKey = "#empty#"

// @@@@@
// NewCrawlerFunc는 새로운 크롤러(cron.Job) 인스턴스를 생성하는 팩토리 함수(Factory Function)의 타입입니다.
// 각 크롤링 Provider는 init() 함수 안에서 이 시그니처를 구현하는 함수를 CrawlerConfig에 등록합니다.
//
// 매개변수:
//   - rssFeedProviderID: RSS 피드 제공자를 식별하는 고유 ID (DB 조회/저장에 사용)
//   - providerConfig:    YAML 설정 파일에서 읽어온 해당 Provider의 상세 설정 정보
//   - feedRepo:          게시글 조회 및 저장에 사용하는 Repository 인터페이스
//   - notifyClient:      오류 발생 시 관리자에게 알림을 전송하기 위한 클라이언트 (nil 허용)
type NewCrawlerFunc func(string, *config.ProviderDetailConfig, feed.Repository, *notify.Client) cron.Job

// @@@@@
// crawlArticlesFunc는 실제 웹 페이지 크롤링을 수행하는 함수의 타입입니다.
// 전략 패턴(Strategy Pattern)을 통해 base crawler가 구체적인 크롤링 구현에 의존하지
// 않도록 분리하며, 각 크롤러 구조체(예: yeosuCityHallCrawler)는 자신의 crawlArticles
// 메서드를 이 타입으로 crawler.crawlArticles 필드에 주입합니다.
//
// 반환값:
//   - []*feed.Article:      새로 발견된 신규 게시글 목록 (서버 오류 시 nil 반환)
//   - map[string]string:    게시판별 최신 크롤링 게시글 ID 맵 (key: boardID, value: articleID)
//   - string:               오류 발생 시 사용자/관리자에게 전달할 오류 메시지 문자열
//   - error:                오류 객체 (정상 처리 시 nil)
type crawlArticlesFunc func(ctx context.Context) ([]*feed.Article, map[string]string, string, error)
