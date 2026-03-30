package provider

import (
	"context"
	"fmt"
	"time"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/scraper"
)

// DefaultBoardKey는 게시판 구분이 없는 단일 게시판 Provider에서
// 게시판 ID를 대신하여 사용하는 sentinel(기본값 표식) 상수입니다.
// DB 갱신 시 이 값을 감지하면 빈 문자열("")로 변환되어 저장됩니다.
// 예: UpdateLatestCrawledIDs 함수에서 boardID == DefaultBoardKey 조건으로 처리됩니다.
const DefaultBoardKey = "#empty#"

// Base 개별 크롤링 Provider의 공통 속성과 동작을 캡슐화한 핵심 기본(Base) 구조체입니다.
//
// 이 구조체는 크롤링 작업에 필요한 데이터베이스 저장소, 알림 전송 인터페이스,
// 그리고 대상 웹사이트의 기본 정보 등을 담고 있습니다. 각각의 구체적인 크롤러(예: navercafe)는
// 이 Base 구조체를 내장(Embed)하거나 포함시켜 공통 로직을 재사용합니다.
type Base struct {
	providerID   string
	config       *config.ProviderDetailConfig
	feedRepo     feed.Repository
	notifyClient *notify.Client

	site            string
	siteID          string
	siteName        string
	siteDescription string
	siteUrl         string

	// crawlingMaxPageCount 크롤링 할 최대 페이지 수
	crawlingMaxPageCount int

	crawlArticles CrawlArticlesFunc

	// scraper 웹스크래핑을 수행하는 컴포넌트입니다.
	scraper scraper.Scraper

	// logger 고정 필드가 바인딩된 로거 인스턴스입니다.
	logger *applog.Entry
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ Crawler = (*Base)(nil)

// baseParams Base 구조체 초기화에 필요한 매개변수들을 그룹화한 내부 구조체입니다.
//
// 설계 목적:
//   - Base 구조체 초기화에 필요한 매개변수들을 하나의 구조체로 묶어 함수 시그니처를 간결하게 유지합니다.
//   - 향후 Base 구조체 필드 추가 시 기존 호출 코드를 수정하지 않아도 되는 확장성을 제공합니다.
//   - 필드 캡슐화를 통해 구조체 내부 상태를 보호하고, 객체 생성 이후 의도치 않은 수정을 방지합니다.
type baseParams struct {
	ProviderID string
	Config     *config.ProviderDetailConfig

	FeedRepo     feed.Repository
	NotifyClient *notify.Client

	Site            string
	SiteID          string
	SiteName        string
	SiteDescription string
	SiteUrl         string

	CrawlingMaxPageCount int

	Scraper scraper.Scraper
}

// newBase baseParams를 받아 Base 인스턴스를 생성하는 내부 팩토리 함수입니다.
//
// 이 함수는 패키지 내부에서만 사용되며, 외부에서는 NewBase() 함수를 통해 간접적으로 호출됩니다.
// Base 구조체의 모든 필드를 초기화하며, 특히 logger는 생성 시점에 고정 필드를 바인딩하여
// 이후 로깅 시 매번 필드를 복사하는 오버헤드를 방지합니다.
func newBase(p baseParams) Base {
	scr := p.Scraper
	if scr == nil {
		// 기본 Fetcher 및 Scraper 생성 (향후 각 Provider 생성 시 주입받도록 개선 예정)
		f := fetcher.New(3, 2*time.Second, 10*1024*1024, fetcher.WithTimeout(15*time.Second))
		scr = scraper.New(f)
	}

	return Base{
		providerID:           p.ProviderID,
		config:               p.Config,
		feedRepo:             p.FeedRepo,
		notifyClient:         p.NotifyClient,
		site:                 p.Site,
		siteID:               p.SiteID,
		siteName:             p.SiteName,
		siteDescription:      p.SiteDescription,
		siteUrl:              p.SiteUrl,
		crawlingMaxPageCount: p.CrawlingMaxPageCount,

		scraper: scr,

		logger: applog.WithFields(applog.Fields{
			"site":    p.Site,
			"site_id": p.SiteID,
		}),
	}
}

// NewBase NewCrawlerParams와 사이트 메타데이터를 기반으로 Base 인스턴스를 생성하는 공개 팩토리 함수입니다.
//
// 이 함수는 개별 크롤러(예: navercafe) 설정부에서 호출되며, 반복적으로 나타나는
// Base 초기화 코드를 간소화하고 패키지 내부(baseParams) 의존성을 숨김으로써 구조를 통일합니다.
func NewBase(
	p NewCrawlerParams,
	site string,
	siteID string,
	siteName string,
	siteDescription string,
	siteUrl string,
	crawlingMaxPageCount int,
	scraper scraper.Scraper,
) Base {
	return newBase(baseParams{
		ProviderID:           p.ProviderID,
		Config:               p.Config,
		FeedRepo:             p.FeedRepo,
		NotifyClient:         p.NotifyClient,
		Site:                 site,
		SiteID:               siteID,
		SiteName:             siteName,
		SiteDescription:      siteDescription,
		SiteUrl:              siteUrl,
		CrawlingMaxPageCount: crawlingMaxPageCount,
		Scraper:              scraper,
	})
}

// Getter 메서드들 (캡슐화 및 읽기 전용 속성 제공)
func (c *Base) ProviderID() string                   { return c.providerID }
func (c *Base) Config() *config.ProviderDetailConfig { return c.config }
func (c *Base) FeedRepo() feed.Repository            { return c.feedRepo }
func (c *Base) Site() string                         { return c.site }
func (c *Base) SiteID() string                       { return c.siteID }
func (c *Base) SiteName() string                     { return c.siteName }
func (c *Base) SiteDescription() string              { return c.siteDescription }
func (c *Base) SiteUrl() string                      { return c.siteUrl }
func (c *Base) CrawlingMaxPageCount() int            { return c.crawlingMaxPageCount }
func (c *Base) Scraper() scraper.Scraper             { return c.scraper }

// SetCrawlArticles 개별 크롤러 구현체가 실제 크롤링 로직을 바인딩할 수 있도록 제공하는 Setter 입니다.
func (c *Base) SetCrawlArticles(fn CrawlArticlesFunc) {
	c.crawlArticles = fn
}

// Run 크롤링 작업의 전체 생명주기를 전담하는 메인 진입점입니다.
//
// 핵심 역할:
//   - 런타임 패닉(Panic)을 복구하여 스케줄러(cron 등)가 중단되는 것을 방지합니다.
//   - 상위 호출 계층(스케줄러 등)의 컨텍스트를 받아 크롤러 생명주기와 동기화합니다.
//   - 상위 컨텍스트에 추가 타임아웃 컨텍스트(context)를 설정하여 무한 대기 현상을 차단합니다.
//   - 크롤링 로직을 호출하고, DB 갱신 및 오류 알림 등의 후처리 작업을 조율합니다.
func (c *Base) Run(ctx context.Context) {
	// Task 실행 중 발생할 수 있는 런타임 패닉을 복구하여 스케줄러 메인 프로세스가 중단되지 않도록 방어합니다.
	defer func() {
		if r := recover(); r != nil {
			m := c.formatMessage("크롤링 작업 중 런타임 패닉(Panic)이 발생하였습니다.😱\n\n[오류 상세 내용]\n%v", r)
			c.logger.Error(m)

			// SendErrorNotification 안에서 타임아웃 및 2차 패닉 차단을 알아서 처리하게 위임
			c.SendErrorNotification(m, nil)
		}
	}()

	execCtx, cancel := c.prepareExecution(ctx)
	defer cancel()

	articles, latestIDs := c.execute(execCtx)
	c.finalizeExecution(execCtx, articles, latestIDs)
}

// prepareExecution 크롤링 작업에 필요한 초기 설정, 의존성 검증 및 컨텍스트 타임아웃 설정을 수행합니다.
// 외부에서 주입된 parentCtx를 바탕으로 10분의 제한 시간을 부여한 새로운 컨텍스트를 생성합니다.
func (c *Base) prepareExecution(parentCtx context.Context) (context.Context, context.CancelFunc) {
	c.logger.Debug(c.formatMessage("크롤링 작업을 시작합니다."))

	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Minute)

	if c.crawlArticles == nil {
		c.logger.Panic("CrawlArticles 함수가 주입되지 않았습니다. SetCrawlArticles를 확인해 주세요.")
	}

	return ctx, cancel
}

// execute 실제 게시글 스크래핑 비즈니스 로직을 호출하여 신규 게시글 목록과 최신 커서(ID)를 반환합니다.
func (c *Base) execute(ctx context.Context) ([]*feed.Article, map[string]string) {
	articles, latestCrawledArticleIDsByBoard, errOccurred, err := c.crawlArticles(ctx)
	if err != nil {
		c.SendErrorNotification(errOccurred, err)
		return nil, nil
	}

	if articles == nil {
		c.logger.Warn(c.formatMessage("크롤링 작업을 종료합니다. 서버의 일시적인 오류로 인하여 신규 게시글 추출이 실패하였습니다."))
		return nil, nil
	}

	return articles, latestCrawledArticleIDsByBoard
}

// finalizeExecution 크롤링된 결과를 데이터베이스에 저장하고, 알림을 발송하며, 상태 마커를 업데이트하고 리소스를 정리합니다.
func (c *Base) finalizeExecution(ctx context.Context, articles []*feed.Article, latestCrawledArticleIDsByBoard map[string]string) {
	if articles == nil {
		// 오류 발생으로 인해 articles가 없는 경우는 이미 execute 단계에서 로깅/알림 완료됨
		return
	}

	if len(articles) > 0 {
		c.logger.Debug(c.formatMessage("크롤링 작업 결과로 %d건의 신규 게시글이 추출되었습니다. 신규 게시글을 DB에 추가합니다.", len(articles)))

		insertedCnt, err := c.feedRepo.SaveArticles(ctx, c.providerID, articles)
		if err != nil {
			m := c.formatMessage("신규 게시글을 DB에 추가하는 중에 오류가 발생하여 크롤링 작업이 실패하였습니다.😱")
			c.SendErrorNotification(m, err)
			return
		}

		c.UpdateLatestCrawledIDs(ctx, latestCrawledArticleIDsByBoard)

		if len(articles) != insertedCnt {
			c.logger.Warn(c.formatMessage("크롤링 작업을 종료합니다. 전체 %d건 중에서 %d건의 신규 게시글이 DB에 추가되었습니다.", len(articles), insertedCnt))
		} else {
			c.logger.Debug(c.formatMessage("크롤링 작업을 종료합니다. %d건의 신규 게시글이 DB에 추가되었습니다.", len(articles)))
		}
	} else {
		c.UpdateLatestCrawledIDs(ctx, latestCrawledArticleIDsByBoard)

		c.logger.Debug(c.formatMessage("크롤링 작업을 종료합니다. 신규 게시글이 존재하지 않습니다."))
	}
}

// SendErrorNotification 작업 실행 중 발생한 에러 메타데이터를 로깅하고 사용자/관리자에게 알림으로 전송합니다.
//
// 알림 전송 과정은 주요 크롤링 파이프라인(Main Flow)을 블록킹하지 않도록
// 타임아웃 컨텍스트가 부여된 별도의 백그라운드 고루틴(Goroutine)에서 비동기적으로 실행됩니다.
//
// 매개변수:
//   - message: 로깅 및 사용자에게 전달할 핵심 상황 메시지
//   - err: 발생한 에러 객체 (nil 인 경우 단순 메시지만 전송됨)
func (c *Base) SendErrorNotification(message string, err error) {
	if err != nil {
		c.logger.Errorf("%s (error:%s)", message, err)
	} else {
		c.logger.Error(message)
	}

	if c.notifyClient == nil {
		return
	}

	// 알림 발송은 메인 흐름을 차단하지 않도록 별도의 고루틴에서 타임아웃과 함께 비동기로 실행합니다.
	go func(msg string, e error) {
		// 예약 외의 치명적 2차 패닉(예: notifyClient 연결 과정 문제 등)을 방지
		defer func() {
			if r := recover(); r != nil {
				c.logger.Errorf("알림 발송 중단: 패닉 복구 (panic:%v)", r)
			}
		}()

		// 타임아웃을 설정한 컨텍스트 (기존 5초에서 시스템 유연성을 위해 60초 사용)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		var text string
		if e != nil {
			text = fmt.Sprintf("%s\r\n\r\n%s", msg, e)
		} else {
			text = msg
		}

		c.notifyClient.NotifyError(ctx, text)
	}(message, err)
}

// UpdateLatestCrawledIDs 크롤링 완료 후, 다음 크롤링 시점의 중복 수집을 방지하기 위해
// 게시판별 가장 마지막에 확인된 (가장 최신의) 게시글 ID 커서를 데이터베이스에 갱신(Upsert)합니다.
//
// 게시판 구분이 없는 단일 게시판(DefaultBoardKey) 환경의 경우 빈 문자열("")로 치환하여 저장됩니다.
// 단일 건 저장에 실패하더라도 전체 워크플로우를 중단하지 않고 로깅 및 에러 알림 후 다음 건을 계속 처리합니다.
func (c *Base) UpdateLatestCrawledIDs(ctx context.Context, latestCrawledArticleIDsByBoard map[string]string) {
	for boardID, articleID := range latestCrawledArticleIDsByBoard {
		if boardID == DefaultBoardKey {
			boardID = ""
		}

		if err := c.feedRepo.UpsertLatestCrawledArticleID(ctx, c.providerID, boardID, articleID); err != nil {
			m := c.formatMessage("크롤링 된 최근 게시글 ID의 DB 갱신이 실패하였습니다.😱")
			c.SendErrorNotification(m, err)
		}
	}
}

// formatMessage 알림이나 로깅에 사용할 일반적인 메시지 형식을 생성합니다.
// site와 siteID를 일관되게 포함하여 가독성을 높입니다.
func (c *Base) formatMessage(format string, args ...any) string {
	msg := fmt.Sprintf(format, args...)
	return fmt.Sprintf("%s('%s')의 %s", c.site, c.siteID, msg)
}
