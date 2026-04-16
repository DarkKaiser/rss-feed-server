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
// updateCursors 함수가 DB 저장 직전 이 값을 감지하여 빈 문자열("")로 치환합니다.
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

func (b *Base) Logger() *applog.Entry {
	return b.logger
}

// Run 스케줄러가 호출하는 크롤링 작업의 메인 진입점으로, 아래 세 단계 파이프라인을 순서대로 조율합니다.
//
//  1. prepareExecution:  사전 조건 검증 및 실행 타임아웃 컨텍스트 생성
//  2. execute:           신규 게시글 수집 (크롤링 비즈니스 로직 위임)
//  3. finalizeExecution: 수집 결과를 DB에 저장하고 커서를 전진
//
// 이 메서드 자체는 각 단계를 직접 구현하지 않고, 파이프라인 흐름의 조율과 런타임 패닉 복구라는 두 가지 책임만을 담당합니다.
func (b *Base) Run(ctx context.Context) {
	// 크롤링 실행 중 예상치 못한 런타임 패닉이 발생하더라도,
	// defer로 등록된 이 복구 핸들러가 패닉을 가로채어 스케줄러(cron 등)의 메인 고루틴이 죽지 않도록 방어합니다.
	// 복구 후에는 에러를 로깅하고 관리자 알림까지 전송하여 패닉의 발생 사실을 알립니다.
	defer func() {
		if r := recover(); r != nil {
			msg := b.Messagef("크롤링 작업 중단: 런타임 패닉 발생 (상세: %v)", r)

			b.logger.Error(msg)

			// ReportError는 내부적으로 타임아웃 컨텍스트와 2차 패닉 방어 코드를 갖추고 있으므로,
			// 알림 전송의 안전성 보장은 ReportError에 완전히 위임합니다.
			b.ReportError(msg, nil)
		}
	}()

	// [1단계] 사전 조건 검증 및 타임아웃 컨텍스트 생성
	// execCtx는 전체 크롤링 사이클(목록 수집 → 본문 수집)에 10분의 실행 시간 상한을 부여합니다.
	// 이는 특정 사이트의 응답 지연이나 무한 루프로 인해 고루틴이 무기한 잠기는 현상을 방지하기 위함입니다.
	execCtx, cancel := b.prepareExecution(ctx)
	defer cancel()

	// [2단계] 실제 크롤링 실행
	// 신규 게시글(articles)과 다음 크롤링 시작 기준점(cursors)을 반환합니다.
	// 실행 중 에러가 발생하면 execute 내부에서 로깅 및 알림을 처리하고 (nil, nil)을 반환합니다.
	articles, cursors := b.execute(execCtx)

	// [3단계] 후처리
	// 수집한 게시글을 DB에 저장하고, 다음 사이클을 위한 커서를 전진시킵니다.
	// articles가 nil인 경우(2단계 실패)를 스스로 감지하여 안전하게 조기 종료합니다.
	b.finalizeExecution(articles, cursors)
}

// prepareExecution 크롤링 작업 시작 전 사전 조건을 검증하고,
// 실행 시간 상한이 부여된 실행용 컨텍스트(Context)를 생성하여 반환합니다.
//
// 이 메서드는 Run() 메서드의 첫 단계로 호출되며, 크롤링 파이프라인에
// 진입하기 전 아래 두 가지 책임을 순서대로 수행합니다.
//
//  1. 사전 조건 검증 (Precondition Check):
//     비즈니스 로직 함수(crawlArticles)가 올바르게 주입되어 있는지 확인합니다.
//     crawlArticles가 nil인 경우(SetCrawlArticles가 호출되지 않은 경우) 즉시 패닉을 발생시켜
//     초기화 누락을 런타임 조기에 드러냅니다.
//     이 검증은 반드시 context.WithTimeout() 호출보다 먼저 이루어져야 합니다.
//     만약 타임아웃 컨텍스트 생성 이후에 패닉이 발생하면, Run() 내부의 defer cancel()이
//     등록되기 전에 패닉이 전파되어 컨텍스트 리소스가 영구적으로 누수될 수 있습니다.
//
//  2. 타임아웃 컨텍스트 생성 (Timeout Context):
//     상위 스케줄러에서 전달된 컨텍스트(ctx)를 부모로 삼아,
//     별도로 10분의 최대 실행 시간 상한을 부여한 자식 컨텍스트를 파생시킵니다.
//     게시글 목록 페이지 순회(Pagination), 본문 병렬 수집, 외부 API 호출 등을 포함하는
//     전체 크롤링 사이클이 10분을 초과하지 않도록 안전망 역할을 합니다.
//     이를 통해 특정 사이트의 응답 지연이나 무한 루프로 인해
//     고루틴이 무기한 잠기는 현상(Goroutine Leak)을 방지합니다.
//
// 반환값:
//   - context.Context: 10분 타임아웃이 적용된 자식 컨텍스트. 이후 모든 I/O 작업에 전달됩니다.
//   - context.CancelFunc: 호출자(Run)가 반드시 defer cancel()로 등록하여 컨텍스트 리소스를 해제해야 합니다.
func (b *Base) prepareExecution(ctx context.Context) (context.Context, context.CancelFunc) {
	b.logger.Debug(b.Messagef("크롤링 시작: 실행 요청 수신"))

	// [주의] 반드시 context.WithTimeout() 호출 이전에 사전 조건을 검증해야 합니다.
	//
	// 이유: context.WithTimeout()은 내부적으로 타이머 고루틴을 생성합니다.
	// 만약 그 이후에 패닉이 발생하면, 호출자(Run)의 defer cancel()이 아직 등록되지 않은 상태이므로
	// 타이머 고루틴과 컨텍스트 리소스가 10분 타임아웃이 만료될 때까지 회수되지 않습니다.
	// 검증을 먼저 수행함으로써 리소스 누수 가능성을 원천 차단합니다.
	if b.crawlArticles == nil {
		b.logger.Panic("초기화 실패: CrawlArticles 미주입 (SetCrawlArticles 호출 필요)")
	}

	// 전체 크롤링 사이클(목록 수집 → 본문 수집 → 후처리)에 대한 실행 시간 상한(10분)을 설정합니다.
	// 정상적인 크롤링이 10분을 초과하는 경우는 드물므로, 이 값은 사실상 비정상 상황에 대한 안전망입니다.
	return context.WithTimeout(ctx, 10*time.Minute)
}

// execute 주입된 CrawlArticlesFunc를 호출하여 실제 크롤링을 위임하고,
// 그 결과인 신규 게시글 목록(articles)과 게시판별 커서(cursors)를 반환합니다.
//
// 이 메서드는 크롤링 결과를 받아 에러 여부를 판단하고, 에러가 있을 경우 로깅 및 알림을
// 위임(ReportError)하는 얇은 어댑터(Thin Adapter) 계층입니다.
//
// 에러 처리:
//   - 에러 발생 시 (nil, nil)을 반환하여 후속 단계(finalizeExecution)가 데이터 없음을
//     스스로 감지하고 안전하게 종료하도록 설계되었습니다.
//   - 에러 로깅 및 관리자 알림은 이 계층에서 처리하며, 상위 호출자는 별도 처리가 불필요합니다.
//
// CrawlArticlesFunc 계약 (구현체가 반드시 준수해야 할 사항):
//   - 성공 시: non-nil articles(신규 게시글 없을 경우 빈 슬라이스)와 non-nil cursors를 반환해야 합니다.
//   - 실패 시: non-nil error와 함께 errMsg(알림용 메시지 문자열)를 함께 반환해야 합니다.
//   - err != nil인 경우에만 articles가 nil이어야 합니다.
func (b *Base) execute(ctx context.Context) ([]*feed.Article, map[string]string) {
	articles, cursors, errMsg, err := b.crawlArticles(ctx)
	if err != nil {
		b.ReportError(errMsg, err)
		return nil, nil
	}

	return articles, cursors
}

// finalizeExecution 크롤링 결과를 DB에 저장하고, 커서를 전진시키는 후처리 메서드입니다.
//
// 이 메서드는 Run() 파이프라인의 마지막 단계로, 아래 세 가지 책임을 순서대로 수행합니다.
//
//  1. articles가 nil인 경우 execute 단계에서 에러가 발생한 것으로 판단하고 즉시 종료합니다.
//  2. 신규 게시글이 있으면 DB에 저장하고, 성공 시에만 커서를 전진시킵니다.
//  3. 신규 게시글이 없어도 커서를 전진시켜 다음 사이클이 올바른 기준점에서 시작하도록 보장합니다.
//
// 커서 전진 원칙:
//   - DB 저장에 실패하면 커서 전진을 취소합니다. 저장에 실패한 게시글이 있는 상태에서 커서를 전진시키면
//     해당 게시글이 영구적으로 유실되기 때문입니다. 다음 사이클에서 재수집 시 DB 유니크 제약조건이
//     이미 저장된 게시글의 중복 삽입을 안전하게 방어합니다.
func (b *Base) finalizeExecution(articles []*feed.Article, cursors map[string]string) {
	// articles가 nil이면 execute 단계에서 에러가 발생한 것입니다.
	// 에러 로깅과 알림은 이미 execute 내부에서 완료되었으므로 여기서는 아무 처리 없이 종료합니다.
	if articles == nil {
		return
	}

	// DB 저장/커서 갱신 전용으로 독립적인 1분 타임아웃 컨텍스트를 새로 생성합니다.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	if len(articles) > 0 {
		b.logger.Debug(b.Messagef("DB 저장 시작: 신규 게시글 %d건 수집", len(articles)))

		savedCount, err := b.feedRepo.SaveArticles(ctx, b.providerID, articles)
		if err != nil {
			b.ReportError(b.Messagef("크롤링 작업 실패: 신규 게시글 DB 저장 중 오류 발생"), err)

			// DB 저장 실패 시 커서 전진을 취소합니다.
			// 저장에 실패한 게시글이 있는 상태에서 커서를 전진시키면 그 게시글은 다음 사이클에서도
			// 탐색 범위 밖으로 영구히 벗어나 유실됩니다.
			// 커서를 현재 위치에 그대로 두면, 다음 사이클에서 해당 범위를 다시 수집하게 되고,
			// 이미 저장된 게시글은 DB 유니크 제약조건이 중복 삽입을 조용히 방어해 줍니다.
			b.logger.Warn(b.Messagef("커서 전진 취소: 신규 게시글 DB 저장 부분 실패 (데이터 유실 방지)"))

			return
		}

		// DB 저장이 성공한 경우에만 커서를 전진시킵니다.
		b.updateCursors(ctx, cursors)

		// 저장된 게시글 수가 수집한 게시글 수와 다른 경우는 DB 유니크 제약조건으로 인해
		// 이미 존재하는 게시글 일부가 삽입이 무시된 것입니다. (비정상 상황이 아닌 정상 동작)
		if len(articles) != savedCount {
			b.logger.Warn(b.Messagef("크롤링 작업 종료: 부분 성공 (전체 %d건 중 %d건 DB 추가)", len(articles), savedCount))
		} else {
			b.logger.Debug(b.Messagef("크롤링 작업 종료: 신규 게시글 %d건 DB 추가 완료", len(articles)))
		}
	} else {
		// 신규 게시글이 없어도 커서는 반드시 전진시켜야 합니다.
		// 커서를 갱신하지 않으면 다음 사이클이 동일한 기준점부터 재탐색하여 불필요한 중복 수집이 발생합니다.
		b.updateCursors(ctx, cursors)

		b.logger.Debug(b.Messagef("크롤링 작업 종료: 신규 게시글 없음"))
	}
}

// updateCursors 게시판별 크롤링 커서(다음 크롤링 시 탐색을 시작할 기준점)를
// 이번 사이클에서 수집한 최신 게시글 ID로 데이터베이스에 갱신(Upsert)합니다.
//
// 크롤링 커서를 올바르게 전진시켜야만 다음 크롤링 사이클에서 이미 처리한 게시글을 재수집하는
// 중복 수집(Duplicate Crawling) 문제를 방지할 수 있습니다.
//
// EmptyBoardID 치환:
//   - 네이버 카페처럼 게시판 구분 없이 사이트 전체를 단일 커서로 관리하는 크롤러는
//     내부적으로 EmptyBoardID("#empty#")를 맵 키로 사용합니다.
//   - DB에는 의미 없는 센티넬 값 대신 빈 문자열("")로 치환하여 저장합니다.
//   - 자세한 설계 배경은 EmptyBoardID 상수 주석을 참고하세요.
//
// 실패 처리 정책:
//   - 특정 게시판의 커서 갱신이 실패하더라도 나머지 게시판의 커서 갱신을 계속 진행합니다.
//   - 실패 시 로깅 및 관리자 알림을 발송하여 운영자가 인지할 수 있도록 합니다.
//   - 전체 워크플로우를 중단하지 않으므로, 실패한 커서는 다음 사이클에서 재시도됩니다.
func (b *Base) updateCursors(ctx context.Context, cursors map[string]string) {
	for boardID, articleID := range cursors {
		if boardID == EmptyBoardID {
			boardID = ""
		}

		if err := b.feedRepo.UpsertLatestCrawledArticleID(ctx, b.providerID, boardID, articleID); err != nil {
			b.ReportError(b.Messagef("대상 게시판('%s')의 최근 수집 이력(Cursor)을 데이터베이스에 갱신하는 과정에서 예외가 발생하였습니다.", boardID), err)
		}
	}
}

// ReportError 에러 상황을 로깅하고, 관리자에게 알림을 발송하는 중앙 에러 보고 메서드입니다.
//
// 이 메서드는 아래 두 단계를 순서대로 수행합니다.
//
//  1. 에러 로깅: err 동반 여부에 따라 적절한 포맷으로 에러 로그를 즉시 기록합니다.
//  2. 비동기 알림: 메인 크롤링 파이프라인을 차단하지 않도록 별도의 백그라운드 고루틴에서
//     타임아웃 컨텍스트와 함께 알림을 발송합니다.
//
// 매개변수:
//   - message: 상황을 설명하는 핵심 메시지. 로그와 알림 본문에 모두 사용됩니다.
//   - err: 원인 에러 객체. nil이면 message만 전송되고, non-nil이면 메시지와 함께 조합하여 전송됩니다.
func (b *Base) ReportError(message string, err error) {
	// [1단계] 에러 로깅
	if err != nil {
		b.logger.Errorf("%s: %v", message, err)
	} else {
		b.logger.Error(message)
	}

	// notifyClient가 설정되지 않은 환경(예: 개발 환경)에서는 알림을 생략하고 바로 반환합니다.
	if b.notifyClient == nil {
		return
	}

	// [2단계] 비동기 알림 발송
	// 알림 전송은 네트워크 I/O를 수반하므로, 메인 크롤링 파이프라인을 블록하지 않도록
	// 별도의 고루틴에서 실행합니다. 클로저 캡처 대신 값을 직접 인자로 넘겨 데이터 경쟁을 방지합니다.
	go func(msg string, e error) {
		// 알림 클라이언트 내부(예: 네트워크 연결)에서 발생할 수 있는 2차 패닉을 가로채어,
		// 알림 발송 실패가 스케줄러 전체로 전파되지 않도록 방어합니다.
		defer func() {
			if r := recover(); r != nil {
				b.logger.Errorf("알림 발송 중단: 런타임 패닉 발생 (상세: %v)", r)
			}
		}()

		// 알림 발송에 60초 타임아웃을 부여합니다.
		// 크롤링 execCtx는 이미 만료되었을 수 있으므로, 독립적인 Background 컨텍스트에서 파생합니다.
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// 알림 본문 조립: err가 있으면 메시지와 에러 상세를 두 줄 개행으로 구분하여 포함합니다.
		text := msg
		if e != nil {
			text = fmt.Sprintf("%s\r\n\r\n%s", msg, e)
		}

		b.notifyClient.NotifyError(ctx, text)
	}(message, err)
}

// Messagef 사이트 식별 정보(이름 및 ID)를 메시지 앞에 자동으로 붙여,
// 로그 및 알림 메시지에 항상 일관된 출처 컨텍스트가 포함되도록 보장하는 포맷터입니다.
//
// 여러 크롤러가 동시에 실행되는 환경에서 로그 출처를 즉시 식별할 수 있도록,
// 모든 메시지에 "어느 사이트에서 발생한 이벤트인가"를 자동으로 명시합니다.
//
// 출력 형식:
//
//	{site_name}('{site_id}')의 {message}
//	예) 여수 쌍봉초등학교('ssangbonges')의 크롤링 작업을 시작합니다.
func (b *Base) Messagef(format string, args ...any) string {
	msg := fmt.Sprintf(format, args...)
	return fmt.Sprintf("%s('%s')의 %s", b.config.Name, b.config.ID, msg)
}

// CrawlArticleContentsConcurrently 주어진 게시글 목록의 본문을 병렬로 수집하는 메서드입니다.
//
// 동시성 제어:
//   - limit > 0이면 동시에 실행되는 고루틴 수를 해당 값으로 제한합니다.
//   - limit <= 0이면 제한 없이 모든 게시글을 동시에 수집합니다.
//
// 재시도 정책:
//   - 각 게시글 본문 수집은 최대 3회까지 시도하며, 재시도 사이에 선형 백오프(500ms, 1000ms)를 적용합니다.
//   - ErrContentUnavailable(권한 부족, 삭제된 게시글 등 영구적 오류)이 발생하면 즉시 재시도를 중단합니다.
//
// 에러 전파 계약:
//   - 개별 게시글의 수집 실패(일시적 오류 포함)는 해당 고루틴 내부에서 처리되며 외부로 전파되지 않습니다.
//     수집에 실패한 게시글은 빈 본문 상태로 남아 '부분 실패'로 처리됩니다.
//   - 시스템 레벨 에러(context.Canceled, context.DeadlineExceeded)는 즉시 모든 작업을 중단하고 외부로 전파합니다.
func (b *Base) CrawlArticleContentsConcurrently(ctx context.Context, articles []*feed.Article, limit int, fetchContent func(ctx context.Context, article *feed.Article) error) error {
	// 수집 대상이 없으면 고루틴을 생성할 필요 없이 즉시 반환합니다.
	if len(articles) == 0 {
		return nil
	}

	// errgroup은 고루틴 오류 수집과 컨텍스트 취소를 하나로 묶어주는 동시성 조율자입니다.
	// gCtx는 errgroup에 종속된 컨텍스트로, 어느 한 고루틴이 에러를 반환하면 자동으로 취소됩니다.
	g, gCtx := errgroup.WithContext(ctx)
	if limit > 0 {
		// 동시 실행 고루틴 수를 제한하여 대상 서버에 과도한 요청이 쏠리는 것을 방지합니다.
		g.SetLimit(limit)
	}

	for _, article := range articles {
		g.Go(func() (err error) {
			// fetchContent 콜백 또는 본문 파싱 로직 내부에서 예상치 못한 패닉이 발생하더라도,
			// 이 복구 핸들러가 패닉을 가로채어 errgroup 전체가 중단되는 것을 방지합니다.
			// 패닉이 발생한 게시글은 빈 본문 상태로 남겨 부분 실패로 처리합니다.
			// (에러를 반환하면 gCtx가 취소되어 나머지 고루틴까지 중단되므로 nil을 반환합니다)
			defer func() {
				if r := recover(); r != nil {
					b.logger.Errorf("게시글 본문 크롤링 중단 (ArticleID: %s): 런타임 패닉 발생 (상세: %v)", article.ArticleID, r)
					err = nil
				}
			}()

			maxRetries := 3
			for attempt := 1; attempt <= maxRetries; attempt++ {
				// 이전 시도에서 이미 본문이 채워진 경우에도 루프를 탈출합니다.
				// fetchContent 구현체가 내부적으로 본문을 직접 채울 수 있기 때문입니다.
				if article.Content != "" {
					break
				}

				err = fetchContent(gCtx, article)

				// fetchContent 내부의 개별 HTTP 요청 타임아웃과 시스템 레벨 취소를 구별합니다.
				// fetchContent가 반환한 err이 HTTP 내부 타임아웃이라면 gCtx는 여전히 유효하므로 재시도를 진행합니다.
				// 반면 gCtx 자체가 만료/취소되었다면(상위 컨텍스트 취소 또는 다른 고루틴의 에러 전파),
				// 모든 고루틴을 즉시 멈추기 위해 gCtx의 에러를 직접 반환합니다.
				if gCtx.Err() != nil {
					return gCtx.Err()
				}

				if err == nil {
					break // 수집 성공: 본문이 비어있어도 fetchContent가 정상 완료한 것으로 간주합니다.
				}

				if errors.Is(err, ErrContentUnavailable) {
					break // 영구적 오류(권한 부족, 삭제된 게시글 등): 재시도해도 성공할 수 없으므로 즉시 포기합니다.
				}

				// 마지막 시도가 아닐 경우, 다음 시도 전에 선형 백오프(attempt × 500ms)를 적용합니다.
				// 백오프 대기 중에도 시스템 취소 신호가 오면 즉시 종료합니다.
				if attempt < maxRetries {
					select {
					case <-gCtx.Done():
						return gCtx.Err()

					case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
					}
				}
			}

			// 수집에 실패하더라도 에러를 전파하지 않습니다.
			// 개별 게시글의 실패가 errgroup 전체를 취소시키지 않도록 하여 나머지 게시글 수집을 계속 진행합니다.
			return nil
		})
	}

	// g.Wait()이 반환하는 에러는 context.Canceled 또는 context.DeadlineExceeded뿐입니다.
	// 개별 게시글 수집 실패는 각 고루틴이 nil을 반환하도록 설계되어 있으므로 여기까지 전파되지 않습니다.
	return g.Wait()
}
