package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"golang.org/x/net/html"
	"golang.org/x/text/encoding"
)

// component 크롤링 서비스의 Provider 로깅용 컴포넌트 이름
const component = "crawl.provider"

// DefaultBoardKey는 게시판 구분이 없는 단일 게시판 Provider에서
// 게시판 ID를 대신하여 사용하는 sentinel(기본값 표식) 상수입니다.
// DB 갱신 시 이 값을 감지하면 빈 문자열("")로 변환되어 저장됩니다.
// 예: UpdateLatestCrawledIDs 함수에서 boardID == DefaultBoardKey 조건으로 처리됩니다.
const DefaultBoardKey = "#empty#"


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

// BaseParams Base 구조체 초기화에 필요한 매개변수들을 그룹화한 구조체입니다.
//
// 설계 목적:
//   - Base 구조체 초기화에 필요한 매개변수들을 하나의 구조체로 묶어 함수 시그니처를 간결하게 유지합니다.
//   - 필드 캡슐화를 통해 구조체 내부 상태를 보호하고, 객체 생성 이후 의도치 않은 수정을 방지합니다.
type BaseParams struct {
	Config *config.ProviderDetailConfig

	RssFeedProviderID string
	FeedRepo          feed.Repository
	NotifyClient      *notify.Client

	Site            string
	SiteID          string
	SiteName        string
	SiteDescription string
	SiteUrl         string

	// CrawlingMaxPageCount 크롤링 할 최대 페이지 수
	CrawlingMaxPageCount int
}

// NewBase BaseParams를 받아 완전히 초기화된 Base 인스턴스를 생성하는 팩토리 함수입니다.
//
// 이 함수는 개별 크롤러(예: navercafe) 초기화 시 호출되며, 구조체 필드의 무분별한 접근을
// 막기 위한 캡슐화 패턴(Encapsulation Pattern)을 강제합니다.
func NewBase(p BaseParams) Base {
	return Base{
		config:               p.Config,
		rssFeedProviderID:    p.RssFeedProviderID,
		feedRepo:             p.FeedRepo,
		notifyClient:         p.NotifyClient,
		site:                 p.Site,
		siteID:               p.SiteID,
		siteName:             p.SiteName,
		siteDescription:      p.SiteDescription,
		siteUrl:              p.SiteUrl,
		crawlingMaxPageCount: p.CrawlingMaxPageCount,

		logger: applog.WithFields(applog.Fields{
			"site":    p.Site,
			"site_id": p.SiteID,
		}),
	}
}

// Base 개별 크롤링 Provider의 공통 속성과 동작을 캡슐화한 핵심 기본(Base) 구조체입니다.
//
// 이 구조체는 크롤링 작업에 필요한 데이터베이스 저장소, 알림 전송 인터페이스,
// 그리고 대상 웹사이트의 기본 정보 등을 담고 있습니다. 각각의 구체적인 크롤러(예: navercafe)는
// 이 Base 구조체를 내장(Embed)하거나 포함시켜 공통 로직을 재사용합니다.
type Base struct {
	config *config.ProviderDetailConfig

	rssFeedProviderID string
	feedRepo          feed.Repository
	notifyClient      *notify.Client

	site            string
	siteID          string
	siteName        string
	siteDescription string
	siteUrl         string

	// crawlingMaxPageCount 크롤링 할 최대 페이지 수
	crawlingMaxPageCount int

	crawlArticles CrawlArticlesFunc

	// logger 고정 필드가 바인딩된 로거 인스턴스입니다.
	logger *applog.Entry
}

// Getter 메서드들 (캡슐화 및 읽기 전용 속성 제공)
func (c *Base) Config() *config.ProviderDetailConfig { return c.config }
func (c *Base) RssFeedProviderID() string            { return c.rssFeedProviderID }
func (c *Base) FeedRepo() feed.Repository            { return c.feedRepo }
func (c *Base) Site() string                         { return c.site }
func (c *Base) SiteID() string                       { return c.siteID }
func (c *Base) SiteName() string                     { return c.siteName }
func (c *Base) SiteDescription() string              { return c.siteDescription }
func (c *Base) SiteUrl() string                      { return c.siteUrl }
func (c *Base) CrawlingMaxPageCount() int            { return c.crawlingMaxPageCount }

// SetCrawlArticles 개별 크롤러 구현체가 실제 크롤링 로직을 바인딩할 수 있도록 제공하는 Setter 입니다.
func (c *Base) SetCrawlArticles(fn CrawlArticlesFunc) {
	c.crawlArticles = fn
}

// Run 크롤링 작업의 전체 생명주기를 전담하는 메인 진입점입니다.
//
// 핵심 역할:
//   - 런타임 패닉(Panic)을 복구하여 스케줄러(cron 등)가 중단되는 것을 방지합니다.
//   - 타임아웃 컨텍스트(context)를 설정하여 무한 대기 현상을 차단합니다.
//   - 크롤링 로직을 호출하고, DB 갱신 및 오류 알림 등의 후처리 작업을 조율합니다.
func (c *Base) Run() {
	// Task 실행 중 발생할 수 있는 런타임 패닉을 복구하여 스케줄러 메인 프로세스가 중단되지 않도록 방어합니다.
	defer func() {
		if r := recover(); r != nil {
			m := fmt.Sprintf("%s('%s') 크롤링 작업 중 런타임 패닉(Panic)이 발생하였습니다.😱\n\n[오류 상세 내용]\n%v", c.site, c.siteID, r)
			c.logger.Error(m)

			// 알림 전송 로직에서 발생할 수 있는 2차 패닉 차단
			func() {
				defer func() {
					if r2 := recover(); r2 != nil {
						c.logger.Errorf("알림 처리 중단: 패닉 복구 중 2차 패닉 발생 (panic:%v)", r2)
					}
				}()

				if c.notifyClient != nil {
					// 패닉 발생 시 알림 전송을 동기적으로 수행하되, 최대 60초의 대기 시간을 제한하는 별도 컨텍스트 부여
					notifyCtx, notifyCancel := context.WithTimeout(context.Background(), 60*time.Second)
					defer notifyCancel()

					c.notifyClient.NotifyError(notifyCtx, m)
				}
			}()
		}
	}()

	c.logger.Debugf("%s('%s')의 크롤링 작업을 시작합니다.", c.site, c.siteID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if c.crawlArticles == nil {
		c.logger.Panic("CrawlArticles 함수가 주입되지 않았습니다. SetCrawlArticles를 확인해 주세요.")
	}

	c.execute(ctx)
}

// execute 실제 게시글 스크래핑과 DB 저장, 그리고 포인터 갱신을 순차적으로 수행하는 비즈니스 진입점입니다.
func (c *Base) execute(ctx context.Context) {
	articles, latestCrawledArticleIDsByBoard, errOccurred, err := c.crawlArticles(ctx)
	if err != nil {
		c.SendErrorNotification(errOccurred, err)
		return
	}

	if articles != nil {
		if len(articles) > 0 {
			c.logger.Debugf("%s('%s')의 크롤링 작업 결과로 %d건의 신규 게시글이 추출되었습니다. 신규 게시글을 DB에 추가합니다.", c.site, c.siteID, len(articles))

			insertedCnt, err := c.feedRepo.SaveArticles(ctx, c.rssFeedProviderID, articles)
			if err != nil {
				m := fmt.Sprintf("%s('%s')의 신규 게시글을 DB에 추가하는 중에 오류가 발생하여 크롤링 작업이 실패하였습니다.😱", c.site, c.siteID)
				c.SendErrorNotification(m, err)
				return
			}

			c.UpdateLatestCrawledIDs(ctx, latestCrawledArticleIDsByBoard)

			if len(articles) != insertedCnt {
				c.logger.Warnf("%s('%s')의 크롤링 작업을 종료합니다. 전체 %d건 중에서 %d건의 신규 게시글이 DB에 추가되었습니다.", c.site, c.siteID, len(articles), insertedCnt)
			} else {
				c.logger.Debugf("%s('%s')의 크롤링 작업을 종료합니다. %d건의 신규 게시글이 DB에 추가되었습니다.", c.site, c.siteID, len(articles))
			}
		} else {
			c.UpdateLatestCrawledIDs(ctx, latestCrawledArticleIDsByBoard)

			c.logger.Debugf("%s('%s')의 크롤링 작업을 종료합니다. 신규 게시글이 존재하지 않습니다.", c.site, c.siteID)
		}
	} else {
		c.logger.Warnf("%s('%s')의 크롤링 작업을 종료합니다. 서버의 일시적인 오류로 인하여 신규 게시글 추출이 실패하였습니다.", c.site, c.siteID)
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
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

		if err := c.feedRepo.UpsertLatestCrawledArticleID(ctx, c.rssFeedProviderID, boardID, articleID); err != nil {
			m := fmt.Sprintf("%s('%s')의 크롤링 된 최근 게시글 ID의 DB 갱신이 실패하였습니다.😱", c.site, c.siteID)
			c.SendErrorNotification(m, err)
		}
	}
}

// GetWebPageDocument 지정된 URL로 HTTP GET 요청을 보내고, 응답 본문을 파싱하여 
// goquery.Document 객체로 반환하는 HTTP/HTML 스크래핑 유틸리티입니다.
//
// 대상 서버의 일시적인 순단(Connection Reset by Peer 등) 방어를 위해 일정 시간 슬립 후
// 통신을 1회 재시도(Retry)하는 로직이 내장되어 있습니다. 또한 웹페이지의 문자 인코딩(예: EUC-KR)에 
// 대응하기 위해 decoder를 선택적으로 주입할 수 있습니다.
//
// 반환값:
//   - *goquery.Document: HTML DOM 트리를 파싱 완료한 객체 (성공 시)
//   - string: 오류 발생 시 사용자/관리자에게 전달할 한글 오류 메시지
//   - error: 컨텍스트나 네트워크 통신, HTML 파싱 과정의 구체적 에러 구조체
// noinspection GoUnhandledErrorResult
func (c *Base) GetWebPageDocument(url, title string, decoder *encoding.Decoder) (*goquery.Document, string, error) {
	res, err := http.Get(url)
	if err != nil {
		// 2022년 10월 중순경부터 네이버카페의 글을 일정 시간이 지난후에 http.Get()을 호출하게 되면 'connection reset by peer' 에러가 발생함!
		// 그래서 http.Get()에서 에러가 발생하면 최대 2번 호출하도록 변경함!!
		for i := 1; i <= 2; i++ {
			time.Sleep(100 * time.Millisecond)

			res, err = http.Get(url)
			if err == nil {
				goto SUCCEED
			}
		}

		return nil, fmt.Sprintf("%s 접근이 실패하였습니다.", title), err
	}
SUCCEED:
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Sprintf("%s 접근이 실패하였습니다.", title), fmt.Errorf("HTTP Response StatusCode %d", res.StatusCode)
	}
	defer res.Body.Close()

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		if strings.Contains(err.Error(), "unexpected EOF") && len(bodyBytes) != 0 {
			goto pars
		}
		return nil, fmt.Sprintf("%s의 내용을 읽을 수 없습니다.", title), err
	}

pars:
	if decoder != nil {
		bodyString, err := decoder.String(string(bodyBytes))
		if err != nil {
			return nil, fmt.Sprintf("%s의 문자열 디코딩이 실패하였습니다.", title), err
		}

		root, err := html.Parse(strings.NewReader(bodyString))
		if err != nil {
			return nil, fmt.Sprintf("%s의 HTML 파싱이 실패하였습니다.", title), err
		}

		return goquery.NewDocumentFromNode(root), "", nil
	} else {
		root, err := html.Parse(strings.NewReader(string(bodyBytes)))
		if err != nil {
			return nil, fmt.Sprintf("%s의 HTML 파싱이 실패하였습니다.", title), err
		}

		return goquery.NewDocumentFromNode(root), "", nil
	}
}
