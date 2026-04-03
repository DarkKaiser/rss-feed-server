package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/scraper"
)

// EmptyBoardID 크롤링 커서를 게시판별로 관리하지 않고 사이트 전체 단위로 단일 관리하는 크롤러에서
// boardID 맵의 더미 키로 사용하는 센티넬(Sentinel) 상수입니다.
//
// 배경:
// 크롤링 커서(어디까지 읽었는가)는 게시판별로 map[boardID]articleID 형태의 맵으로 관리됩니다.
// 그런데 네이버 카페처럼 전체글 목록 URL을 한 번에 순회하며 커서를 사이트 전체 단위로 관리하는
// 크롤러는 맵의 키로 쓸 boardID가 없습니다. (게시판이 설정에 존재하더라도 커서는 하나뿐입니다)
//
// 해결책:
// EmptyBoardID를 더미 키로 사용하면, 커서 관리 방식에 상관없이 모두 동일한 map 구조로 통일됩니다.
//
//	// 게시판별 커서 관리 (예: 여수시청, 쌍봉초)
//	{"free": "1234", "notice": "5678"}
//	// 사이트 전체 단위 커서 관리 (예: 네이버 카페)
//	{EmptyBoardID: "9999"}
//
// 주의:
// "#empty#"는 런타임 내부에서만 사용하는 식별자로, DB에 그대로 저장되어서는 안 됩니다.
// UpdateLatestCrawledIDs 함수가 DB 저장 직전 이 값을 감지하여 빈 문자열("")로 치환합니다.
const EmptyBoardID = "#empty#"

// Base 개별 크롤링 Provider의 공통 속성과 동작을 캡슐화한 핵심 구조체입니다.
//
// 이 구조체는 크롤링 대상(어느 사이트를, 어떻게)과 실행에 필요한 의존성(저장소, 알림 클라이언트 등)을 모두 캡슐화하며,
// 각각의 구체적인 크롤러(예: navercafe)는 이를 내장(Embed)하여 공통 로직을 재사용합니다.
type Base struct {
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 식별자
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

	// providerID 이 크롤러가 담당하는 RSS 피드 공급자의 고유 식별자입니다.
	providerID string

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 설정 및 제어
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

	// config 이 크롤러가 크롤링할 사이트의 상세 설정 정보(이름, URL, 게시판 목록 등)입니다.
	config *config.ProviderDetailConfig

	// maxPageCount 크롤링 시 탐색할 최대 페이지 수입니다.
	// 무한 루프를 방지하고 크롤링 범위를 제한하는 상한값으로 작동합니다.
	maxPageCount int

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 비즈니스 로직 및 의존성
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

	// crawlArticles 신규 게시글을 실제로 스크래핑하는 비즈니스 로직 함수입니다.
	// SetCrawlArticles() 메서드를 통해 개별 크롤러 구현체에서 주입됩니다.
	crawlArticles CrawlArticlesFunc

	// scraper 웹 요청(HTTP) 및 HTML/JSON 파싱을 수행하는 컴포넌트입니다.
	scraper scraper.Scraper

	// feedRepo 크롤링된 게시글과 커서 정보를 영구 저장하고 조회하는 저장소 인터페이스입니다.
	feedRepo feed.Repository

	// notifyClient 크롤링 오류 발생 시 관리자에게 알림을 전송하는 클라이언트입니다.
	notifyClient *notify.Client

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 유틸리티
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

	// logger 고정 필드(site, site_id 등)가 바인딩된 로거 인스턴스입니다.
	// 생성 시점에 초기화하여 로깅 시 매번 필드를 복사하는 오버헤드를 방지합니다.
	logger *applog.Entry
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ Crawler = (*Base)(nil)

// baseParams Base 구조체 초기화에 필요한 매개변수들을 그룹화한 구조체입니다.
//
// 설계 목적:
//   - Base 구조체 초기화에 필요한 매개변수들을 하나의 구조체로 묶어 함수 시그니처를 간결하게 유지합니다.
//   - 향후 Base 구조체 필드 추가 시 기존 호출 코드를 수정하지 않아도 되는 확장성을 제공합니다.
//   - 필드 캡슐화를 통해 구조체 내부 상태를 보호하고, 객체 생성 이후 의도치 않은 수정을 방지합니다.
type baseParams struct {
	ProviderID string

	Config       *config.ProviderDetailConfig
	maxPageCount int

	Scraper      scraper.Scraper
	FeedRepo     feed.Repository
	NotifyClient *notify.Client
}

// newBase baseParams를 받아 Base 인스턴스를 생성하는 내부 팩토리 함수입니다.
//
// 이 함수는 패키지 내부에서만 사용되며, 외부에서는 NewBase() 함수를 통해 간접적으로 호출됩니다.
// Base 구조체의 모든 필드를 초기화하며, 특히 logger는 생성 시점에 고정 필드를 바인딩하여
// 이후 로깅 시 매번 필드를 복사하는 오버헤드를 방지합니다.
//
// 매개변수:
//   - p: Base 초기화에 필요한 모든 매개변수를 담은 구조체
//
// 반환값: 완전히 초기화된 Base 인스턴스 포인터
func newBase(p baseParams) *Base {
	return &Base{
		providerID: p.ProviderID,

		config:       p.Config,
		maxPageCount: p.maxPageCount,

		scraper:      p.Scraper,
		feedRepo:     p.FeedRepo,
		notifyClient: p.NotifyClient,

		logger: applog.WithFields(applog.Fields{
			"provider_id":    p.ProviderID,
			"site_id":        p.Config.ID,
			"site":           p.Config.Name,
			"max_page_count": p.maxPageCount,
		}),
	}
}

// NewBase NewCrawlerParams를 기반으로 Base 인스턴스를 생성하는 공개 팩토리 함수입니다.
//
// 이 함수는 개별 크롤러(navercafe, ssangbonges, yeosucityhall 등)의 NewCrawler 팩토리에서 호출되며,
// 반복적으로 나타나는 Base 초기화 코드를 간소화하여 코드 중복을 방지합니다.
// NewCrawlerParams를 내부 baseParams로 변환하고, Fetcher를 기반으로 Scraper를 초기화한 후
// newBase() 내부 팩토리 함수를 호출하여 완전히 초기화된 Base 인스턴스를 반환합니다.
//
// 매개변수:
//   - p: 크롤러 생성에 필요한 모든 매개변수를 담은 구조체 (p.Fetcher는 필수이며, nil일 경우 패닉 발생)
//   - maxPageCount: 한 번의 크롤링 사이클에서 탐색할 최대 페이지 수
//
// 반환값: 완전히 초기화된 Base 인스턴스 포인터
func NewBase(p NewCrawlerParams, maxPageCount int) *Base {
	if p.Fetcher == nil {
		panic(fmt.Sprintf("NewBase: 스크래핑 작업에는 Fetcher 주입이 필수입니다 (ProviderID=%s)", p.ProviderID))
	}

	return newBase(baseParams{
		ProviderID: p.ProviderID,

		Config:       p.Config,
		maxPageCount: maxPageCount,

		Scraper:      scraper.New(p.Fetcher),
		FeedRepo:     p.FeedRepo,
		NotifyClient: p.NotifyClient,
	})
}

func (b *Base) ProviderID() string {
	return b.providerID
}

func (b *Base) Config() *config.ProviderDetailConfig {
	return b.config
}

func (b *Base) MaxPageCount() int {
	return b.maxPageCount
}

func (b *Base) SetCrawlArticles(crawlArticles CrawlArticlesFunc) {
	b.crawlArticles = crawlArticles
}

func (b *Base) Scraper() scraper.Scraper {
	return b.scraper
}

func (b *Base) FeedRepo() feed.Repository {
	return b.feedRepo
}

// @@@@@
// Run 크롤링 작업의 전체 생명주기를 전담하는 메인 진입점입니다.
//
// 핵심 역할:
//   - 런타임 패닉(Panic)을 복구하여 스케줄러(cron 등)가 중단되는 것을 방지합니다.
//   - 상위 호출 계층(스케줄러 등)의 컨텍스트를 받아 크롤러 생명주기와 동기화합니다.
//   - 상위 컨텍스트에 추가 타임아웃 컨텍스트(context)를 설정하여 무한 대기 현상을 차단합니다.
//   - 크롤링 로직을 호출하고, DB 갱신 및 오류 알림 등의 후처리 작업을 조율합니다.
func (b *Base) Run(ctx context.Context) {
	// Task 실행 중 발생할 수 있는 런타임 패닉을 복구하여 스케줄러 메인 프로세스가 중단되지 않도록 방어합니다.
	defer func() {
		if r := recover(); r != nil {
			m := b.FormatMessage("크롤링 작업 중 런타임 패닉(Panic)이 발생하였습니다.😱\n\n[오류 상세 내용]\n%v", r)
			b.logger.Error(m)

			// SendErrorNotification 안에서 타임아웃 및 2차 패닉 차단을 알아서 처리하게 위임
			b.SendErrorNotification(m, nil)
		}
	}()

	execCtx, cancel := b.prepareExecution(ctx)
	defer cancel()

	articles, latestIDs := b.execute(execCtx)
	b.finalizeExecution(execCtx, articles, latestIDs)
}

// @@@@@
// prepareExecution 크롤링 작업에 필요한 초기 설정, 의존성 검증 및 컨텍스트 타임아웃 설정을 수행합니다.
// 외부에서 주입된 parentCtx를 바탕으로 10분의 제한 시간을 부여한 새로운 컨텍스트를 생성합니다.
func (b *Base) prepareExecution(parentCtx context.Context) (context.Context, context.CancelFunc) {
	b.logger.Debug(b.FormatMessage("크롤링 작업을 시작합니다."))

	// context.WithTimeout 호출 이전에 사전 조건을 검증합니다.
	// panic 이 WithTimeout 이후 발생하면 cancel() 이 defer 등록되지 않아 context 리소스가 누수됩니다.
	if b.crawlArticles == nil {
		b.logger.Panic("CrawlArticles 함수가 주입되지 않았습니다. SetCrawlArticles를 확인해 주세요.")
	}

	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Minute)

	return ctx, cancel
}

// @@@@@
// execute 실제 게시글 스크래핑 비즈니스 로직을 호출하여 신규 게시글 목록과 최신 커서(ID)를 반환합니다.
//
// 계약:
//   - CrawlArticlesFunc 구현체는 에러 없이 성공한 경우 반드시 non-nil articles(빈 슬라이스 포함)를 반환해야 합니다.
//   - 서버 일시 오류 등 파싱 자체가 불가한 경우 반드시 non-nil error를 함께 반환해야 합니다.
//   - err != nil 인 경우에만 articles 가 nil 이어야 합니다.
func (b *Base) execute(ctx context.Context) ([]*feed.Article, map[string]string) {
	articles, latestCrawledArticleIDsByBoard, errOccurred, err := b.crawlArticles(ctx)
	if err != nil {
		b.SendErrorNotification(errOccurred, err)
		return nil, nil
	}

	return articles, latestCrawledArticleIDsByBoard
}

// @@@@@
// finalizeExecution 크롤링된 결과를 데이터베이스에 저장하고, 알림을 발송하며, 상태 마커를 업데이트하고 리소스를 정리합니다.
func (b *Base) finalizeExecution(ctx context.Context, articles []*feed.Article, latestCrawledArticleIDsByBoard map[string]string) {
	if articles == nil {
		// 오류 발생으로 인해 articles가 없는 경우는 이미 execute 단계에서 로깅/알림 완료됨
		return
	}

	// 기존 (크롤링 작업 시 사용된) 만료 가능성 있는 Context 대신 1분짜리 신규 DB 트랜잭션용 컨텍스트를 생성합니다.
	storeCtx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	if len(articles) > 0 {
		b.logger.Debug(b.FormatMessage("크롤링 작업 결과로 %d건의 신규 게시글이 추출되었습니다. 신규 게시글을 DB에 추가합니다.", len(articles)))

		insertedCnt, err := b.feedRepo.SaveArticles(storeCtx, b.providerID, articles)
		if err != nil {
			m := b.FormatMessage("신규 게시글을 DB에 추가하는 중에 오류가 발생하여 크롤링 작업이 실패하였습니다.😱")
			b.SendErrorNotification(m, err)
			
			// 부분 실패 시, 삽입에 실패하여 누락된 게시글이 있음에도 불구하고 
			// 수집 결과를 기반으로 미리 계산된 최고(Max) 커서로 갱신해 버리면 
			// 누락된 게시글이 영구적으로 유실되는 버그가 존재합니다.
			// 따라서 부분 저장(insertedCnt > 0)에 성공했더라도, 데이터 무결성 보장을 위해 
			// 이번 사이클의 커서 전진을 취소합니다. (중복 재처리는 DB 제약조건에서 안전하게 방어됨)
			b.logger.Warn(b.FormatMessage("게시물 DB 추가 중 발생한 부분 실패로 인해 데이터 유실 방지 차원에서 커서 전진을 취소합니다."))
			return
		}

		b.updateLatestCrawledIDs(storeCtx, latestCrawledArticleIDsByBoard)

		if len(articles) != insertedCnt {
			b.logger.Warn(b.FormatMessage("크롤링 작업을 종료합니다. 전체 %d건 중에서 %d건의 신규 게시글이 DB에 추가되었습니다.", len(articles), insertedCnt))
		} else {
			b.logger.Debug(b.FormatMessage("크롤링 작업을 종료합니다. %d건의 신규 게시글이 DB에 추가되었습니다.", len(articles)))
		}
	} else {
		b.updateLatestCrawledIDs(storeCtx, latestCrawledArticleIDsByBoard)

		b.logger.Debug(b.FormatMessage("크롤링 작업을 종료합니다. 신규 게시글이 존재하지 않습니다."))
	}
}

// @@@@@
// SendErrorNotification 작업 실행 중 발생한 에러 메타데이터를 로깅하고 사용자/관리자에게 알림으로 전송합니다.
//
// 알림 전송 과정은 주요 크롤링 파이프라인(Main Flow)을 블록킹하지 않도록
// 타임아웃 컨텍스트가 부여된 별도의 백그라운드 고루틴(Goroutine)에서 비동기적으로 실행됩니다.
//
// 매개변수:
//   - message: 로깅 및 사용자에게 전달할 핵심 상황 메시지
//   - err: 발생한 에러 객체 (nil 인 경우 단순 메시지만 전송됨)
func (b *Base) SendErrorNotification(message string, err error) {
	if err != nil {
		b.logger.Errorf("%s (error:%s)", message, err)
	} else {
		b.logger.Error(message)
	}

	if b.notifyClient == nil {
		return
	}

	// 알림 발송은 메인 흐름을 차단하지 않도록 별도의 고루틴에서 타임아웃과 함께 비동기로 실행합니다.
	go func(msg string, e error) {
		// 예약 외의 치명적 2차 패닉(예: notifyClient 연결 과정 문제 등)을 방지
		defer func() {
			if r := recover(); r != nil {
				b.logger.Errorf("알림 발송 중단: 패닉 복구 (panic:%v)", r)
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

		b.notifyClient.NotifyError(ctx, text)
	}(message, err)
}

// @@@@@
// updateLatestCrawledIDs 크롤링 완료 후, 다음 크롤링 시점의 중복 수집을 방지하기 위해
// 게시판별 가장 마지막에 확인된 (가장 최신의) 게시글 ID 커서를 데이터베이스에 갱신(Upsert)합니다.
//
// 게시판 구분이 없는 단일 게시판(DefaultBoardKey) 환경의 경우 빈 문자열("")로 치환하여 저장됩니다.
// 단일 건 저장에 실패하더라도 전체 워크플로우를 중단하지 않고 로깅 및 에러 알림 후 다음 건을 계속 처리합니다.
func (b *Base) updateLatestCrawledIDs(ctx context.Context, latestCrawledArticleIDsByBoard map[string]string) {
	for boardID, articleID := range latestCrawledArticleIDsByBoard {
		if boardID == EmptyBoardID {
			boardID = ""
		}

		if err := b.feedRepo.UpsertLatestCrawledArticleID(ctx, b.providerID, boardID, articleID); err != nil {
			m := b.FormatMessage("크롤링 된 최근 게시글 ID의 DB 갱신이 실패하였습니다.😱")
			b.SendErrorNotification(m, err)
		}
	}
}

// @@@@@
// FormatMessage 알림이나 로깅에 사용할 일반적인 메시지 형식을 생성합니다.
// site (Config.Name) 와 siteID를 일관되게 포함하여 가독성을 높입니다.
func (b *Base) FormatMessage(format string, args ...any) string {
	msg := fmt.Sprintf(format, args...)
	return fmt.Sprintf("%s('%s')의 %s", b.config.Name, b.config.ID, msg)
}

// @@@@@
// CrawlArticleContentsConcurrently 여러 게시글의 본문을 지정된 동시성 제한 내에서 병렬로 수집합니다.
// 본문 수집 실패 시 최대 3회까지 백오프를 주며 재시도합니다.
// 시스템 에러(context.Canceled, context.DeadlineExceeded)가 발생하면 즉시 외부로 에러를 전파합니다.
func (b *Base) CrawlArticleContentsConcurrently(
	ctx context.Context,
	articles []*feed.Article,
	limit int,
	fetchContent func(ctx context.Context, article *feed.Article) error,
) error {
	if len(articles) == 0 {
		return nil
	}

	g, gCtx := errgroup.WithContext(ctx)
	if limit > 0 {
		g.SetLimit(limit)
	}

	for _, article := range articles {
		a := article
		g.Go(func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					b.logger.Errorf("병렬 게시글 본문 크롤링 중 고루틴 패닉 발생 (ArticleID: %s): %v", a.ArticleID, r)
					err = nil // 패닉이 발생하더라도 롤백되지 않고, 부분 실패(빈 본문)로 처리되도록 에러 전파를 무시합니다.
				}
			}()

			// 최대 3회 재시도 (재시도 간 짧은 백오프 대기)
			maxRetries := 3
			for attempt := 1; attempt <= maxRetries; attempt++ {
				if a.Content != "" {
					break // 이미 성공했거나 내용이 채워져 있으면 루프 탈출
				}

				err := fetchContent(gCtx, a)

				// 외부 시스템에 의해 전체 작업 컨텍스트가 취소되거나 타임아웃된 경우 전체 작업을 즉시 중지합니다.
				// (개별 게시글 수집 시 발생한 HTTP 내부 타임아웃 오류가 errgroup 전체를 취소시키지 않도록 방어)
				if gCtx.Err() != nil {
					return gCtx.Err()
				}

				if err == nil {
					break // 에러 없이 완전히 처리가 끝났다면(정상 빈 값일지라도) 재시도 중단
				}
				if errors.Is(err, ErrSkipContentRetry) {
					break // 권한 부족, 삭제글 등 영구적인 오류이므로 재시도 중단
				}

				if attempt < maxRetries {
					select {
					case <-gCtx.Done():
						return gCtx.Err() // 컨텍스트 취소 시 즉시 종료
					case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
					}
				}
			}
			return nil
		})
	}

	// fetchContent 콜백 내부의 에러는 해당 고루틴의 재시도 로직에서만 처리/로깅되며 외부로 전파되지 않도록 설계되었습니다.
	// 따라서 g.Wait()이 반환하는 에러는 컨텍스트 취소(Canceled) 또는 타임아웃(DeadlineExceeded)뿐입니다.
	// 시스템 인터럽트에 의한 의도치 않은 에러 묵살을 방지하기 위해 g.Wait() 결과를 그대로 반환합니다.
	return g.Wait()
}
