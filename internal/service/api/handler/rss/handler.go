package rss

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service/api/httputil"
	_ "github.com/darkkaiser/rss-feed-server/internal/service/api/model/response"
	"github.com/darkkaiser/rss-feed-server/pkg/rss"
	"github.com/labstack/echo/v4"
)

// component RSS 핸들러의 로깅용 컴포넌트 이름
const component = "api.handler.rss"

var (
	// nl2brReplacer 게시글 본문의 줄바꿈 문자(\r\n, \n)를 HTML <br/> 태그로 치환합니다.
	nl2brReplacer = strings.NewReplacer("\r\n", "<br/>", "\n", "<br/>")

	// htmlTagRegex 게시글 본문에 이미 구조적인 HTML 태그가 포함되어 있는지 판별하는 정규표현식입니다.
	htmlTagRegex = regexp.MustCompile(`(?i)<(?:br|p|div|img|table|ul|ol|li|h[1-6])[^>]*>`)
)

// providerCache 단일 RSS 프로바이더의 원본 설정과 런타임 조회를 위한 파생 데이터를 묶은 구조체입니다.
type providerCache struct {
	// cfg 이 프로바이더에 대한 원본 설정 데이터입니다.
	// 사이트 종류, 게시판 목록, 수집 스케줄 등의 정보를 담고 있습니다.
	cfg *config.ProviderConfig

	// boardIDs 이 프로바이더가 수집하는 전체 게시판 ID 목록입니다.
	boardIDs []string

	// boardNameByID 게시판 ID를 표시용 이름으로 빠르게 치환하기 위한 맵입니다.
	boardNameByID map[string]string
}

// Handler RSS 피드 관련 HTTP 요청을 처리하는 핸들러입니다.
type Handler struct {
	// cfg 서버 구동 시 파싱된 전체 RSS 피드 설정입니다.
	// 프로바이더 목록 및 피드 내 최대 게시글 수 등의 정책을 포함합니다.
	cfg *config.RSSFeedConfig

	// providers 각 프로바이더 상세 설정 및 파생 캐시를 피드 ID로 인덱싱한 맵입니다.
	providers map[string]providerCache

	// feedRepo 게시글의 영속성을 담당하는 저장소 인터페이스입니다.
	feedRepo feed.Repository

	// notifyClient 텔레그램 등 외부 알림 채널과 통신하는 클라이언트입니다.
	notifyClient *notify.Client

	// startedAt HTTP 핸들러가 생성(초기화)된 시각입니다.
	// 게시글이 없을 경우, RSS 피드가 갱신된 것처럼 보이지 않도록 LastBuildDate 고정값으로 사용됩니다.
	startedAt time.Time
}

// New Handler 인스턴스를 생성하고 반환합니다.
func New(cfg *config.RSSFeedConfig, feedRepo feed.Repository, notifyClient *notify.Client) *Handler {
	if cfg == nil {
		panic("config.RSSFeedConfig는 필수입니다")
	}
	if feedRepo == nil {
		panic("feed.Repository는 필수입니다")
	}

	// 서버 기동 시점에 프로바이더 조회용 맵을 미리 구성합니다.
	providers := make(map[string]providerCache, len(cfg.Providers))
	for _, p := range cfg.Providers {
		var boardIDs []string
		var boardNameByID = make(map[string]string, len(p.Config.Boards))
		for _, b := range p.Config.Boards {
			boardIDs = append(boardIDs, b.ID)
			boardNameByID[b.ID] = b.Name
		}

		// 피드 ID 비교 시 대소문자를 구분하지 않도록 소문자로 정규화하여 저장합니다.
		providers[strings.ToLower(p.ID)] = providerCache{
			cfg:           p,
			boardIDs:      boardIDs,
			boardNameByID: boardNameByID,
		}
	}

	return &Handler{
		cfg:          cfg,
		providers:    providers,
		feedRepo:     feedRepo,
		notifyClient: notifyClient,
		startedAt:    time.Now(),
	}
}

// ViewSummary godoc
// @Summary RSS 피드 목록 요약 페이지
// @Description 현재 서버가 서비스 중인 전체 RSS 피드 목록과 각 피드의 상세 정보를 HTML 페이지로 제공합니다.
// @Description 각 피드의 구독 주소(URL), 사이트 이름, 게시판 목록, 크롤링 주기 등을 한눈에 확인할 수 있습니다.
// @Tags RSS
// @Produce text/html
// @Success 200 {string} string "RSS 피드 목록 HTML 페이지"
// @Failure 500 {object} response.ErrorResponse "서버 내부 오류 (템플릿 렌더링 실패 등)"
// @Router / [get]
func (h *Handler) ViewSummary(c echo.Context) error {
	applog.WithComponentAndFields(component, applog.Fields{
		"request_id": c.Response().Header().Get(echo.HeaderXRequestID),
		"endpoint":   "/",
		"method":     c.Request().Method,
		"remote_ip":  c.RealIP(),
		"user_agent": c.Request().UserAgent(),
	}).Debug("RSS 피드 목록 요약 페이지 조회")

	return c.Render(http.StatusOK, "rss_summary.tmpl", map[string]any{
		"baseURL":    fmt.Sprintf("%s://%s", c.Scheme(), c.Request().Host),
		"feedConfig": h.cfg,
	})
}

// GetFeed godoc
// @Summary 개별 RSS 피드 조회
// @Description 지정된 식별자(id)에 해당하는 게시판의 최신 게시글을 RSS 2.0 규격의 XML 형식으로 반환합니다.
// @Description RSS 리더 앱(Feedly, Inoreader 등)의 구독 주소로 직접 사용할 수 있습니다.
// @Description
// @Description **식별자 형식**: `/{id}` 와 `/{id}.xml` 형식 모두 동일하게 처리됩니다.
// @Tags RSS
// @Produce application/xml
// @Param id path string true "RSS 피드 고유 식별자" example(naver-cafe)
// @Success 200 {string} string "RSS 2.0 규격 XML 문서 (<rss version=\"2.0\"><channel>...</channel></rss>)"
// @Failure 400 {object} response.ErrorResponse "유효하지 않은 피드 식별자 (등록되지 않은 ID)"
// @Failure 500 {object} response.ErrorResponse "서버 내부 오류 (DB 조회 실패 또는 XML 직렬화 오류)"
// @Router /{id} [get]
func (h *Handler) GetFeed(c echo.Context) error {
	// =========================================================================
	// 1단계: 피드 식별자(ID) 추출
	// =========================================================================
	// URL 경로에서 ID를 추출하고 정규화합니다.
	// - 소문자 변환: 대소문자 구분 없이 설정 파일의 ID와 매핑하기 위함
	// - .xml 제거: 일부 RSS 리더가 자동으로 붙이는 확장자(.xml) 예외 처리
	id := c.Param("id")
	id = strings.ToLower(id)
	id = strings.TrimSuffix(id, ".xml")

	// 단일 요청 추적을 위해 주요 컨텍스트를 로그에 바인딩합니다.
	logger := applog.WithComponentAndFields(component, applog.Fields{
		"request_id": c.Response().Header().Get(echo.HeaderXRequestID),
		"endpoint":   "/{id}",
		"feed_id":    id,
		"method":     c.Request().Method,
		"remote_ip":  c.RealIP(),
		"user_agent": c.Request().UserAgent(),
	})
	logger.Debug("개별 RSS 피드 조회")

	// =========================================================================
	// 2단계: 프로바이더 유효성 검증
	// =========================================================================
	// 서버 구동 시 생성한 캐시 맵에서 O(1)로 설정 데이터를 가져옵니다.
	provider, ok := h.providers[id]
	if !ok {
		return httputil.NewNotFoundError(fmt.Sprintf("요청하신 식별자(%s)에 해당하는 RSS 피드 제공자 정보를 찾을 수 없습니다. 올바른 주소인지 확인해 주시기 바랍니다.", id))
	}

	var err error
	var articles []*feed.Article

	// =========================================================================
	// 3단계: DB 조회 (게시글 수집)
	// =========================================================================
	// 게시판이 설정된 경우에만 캐싱 로직 없이 매 요청마다 최신 데이터를 조회하여 정합성을 보장합니다.
	if len(provider.boardIDs) > 0 {
		articles, err = h.feedRepo.GetArticles(c.Request().Context(), provider.cfg.ID, provider.boardIDs, h.cfg.MaxItemCount)
		if err != nil {
			// 클라이언트 측 요청 취소/타임아웃은 서버 장애가 아니므로 경고 로그만 남깁니다.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				logger.Warnf("DB 조회 중단: 클라이언트 요청 취소 또는 타임아웃 (p_id:%s, error:%s)", provider.cfg.ID, err)
				return err
			}

			// 그 외 DB 접근 에러는 예기치 못한 서버측 문제이므로 관리자 알림을 발송합니다.
			return h.notifyError(logger, fmt.Sprintf("게시글 데이터를 조회하는 과정에서 시스템 내부 오류가 발생했습니다. (제공자 식별자: %s)", provider.cfg.ID), err)
		}
	}

	// =========================================================================
	// 4단계: RSS 갱신 기준일(LastBuildDate) 계산
	// =========================================================================
	// RSS 리더가 갱신 여부를 판단할 수 있도록 수집된 게시글 중 가장 최신 날짜를 찾습니다.
	var lastBuildDate time.Time
	for _, article := range articles {
		if article == nil {
			continue
		}

		if article.CreatedAt.After(lastBuildDate) {
			lastBuildDate = article.CreatedAt
		}
	}

	// 게시글이 없을 때 현재 시각(time.Now)을 넘기면, RSS 리더기가 매번 피드가 갱신된 것으로 착각할 수 있습니다.
	// 이를 방지하고자 갱신 기준일(LastBuildDate)을 변하지 않는 고정 시각(서버 구동 시점)으로 설정합니다.
	if lastBuildDate.IsZero() {
		lastBuildDate = h.startedAt
	}

	// =========================================================================
	// 5단계: RSS Feed 객체 조립
	// =========================================================================
	// DB에서 조회한 게시글들을 RSS 2.0 규격에 맞는 데이터 구조(Feed, Item)로 조립합니다.
	// 발행일(PubDate)과 최종갱신일(LastBuildDate)을 가장 최신 게시글 쓰인 시각으로 일치시켜서, 새 글이 없을 때 RSS 리더기가 중복 알림을 울리지 않도록 예방합니다.
	feed := rss.NewFeed(provider.cfg.Config.Name, provider.cfg.Config.URL, provider.cfg.Config.Description, "ko", config.AppName, lastBuildDate, lastBuildDate)
	feed.Items = make([]*rss.RssItem, 0, len(articles))
	for _, article := range articles {
		if article == nil {
			continue
		}

		// 본문 줄바꿈 브라우저 렌더링 호환성 처리
		// - 텍스트 단락: RSS 리더가 개행을 무시하지 않도록 <br/> 태그 치환
		// - 완전한 HTML: 크롤러가 HTML 구조(p, div 등)를 유지한 경우 이중 치환으로 본문이 깨지지 않도록 방어
		content := article.Content
		if !htmlTagRegex.MatchString(content) {
			content = nl2brReplacer.Replace(content)
		}

		// 카테고리를 위한 게시판 ID(영문/숫자 등)를 사람이 읽기 좋은 표시용 이름으로 변환합니다.
		boardName, exists := provider.boardNameByID[article.BoardID]
		if !exists {
			boardName = article.BoardID
		}

		feed.Items = append(feed.Items,
			rss.NewFeedItem(article.Title, article.Link, content, content, article.Author, boardName, article.CreatedAt),
		)
	}

	// =========================================================================
	// 6단계: XML 직렬화
	// =========================================================================
	// 조립된 객체를 XML 바이트로 변환합니다.
	xmlBytes, err := xml.Marshal(feed.Document())
	if err != nil {
		return h.notifyError(logger, fmt.Sprintf("RSS 피드 데이터를 XML 문서로 변환하는 과정에서 시스템 내부 오류가 발생했습니다. (제공자 식별자: %s)", id), err)
	}

	// =========================================================================
	// 7단계: HTTP 응답 반환
	// =========================================================================
	// RSS 리더의 과도한 반복 풀링을 막기 위해 60초 캐싱 헤더를 주입합니다.
	c.Response().Header().Set("Cache-Control", "public, max-age=60")

	// 일부 RSS 리더의 파싱 거부 대응을 위해 반드시 XML 선언(<?xml ...>) 헤더를 포함하여 반환합니다.
	return c.Blob(http.StatusOK, "application/rss+xml; charset=UTF-8", append([]byte(xml.Header), xmlBytes...))
}

// notifyError 핸들러 내부에서 복구 불가능한 오류가 발생했을 때 호출되는 공통 에러 처리 헬퍼입니다.
func (h *Handler) notifyError(logger *applog.Entry, message string, err error) error {
	// 1. 서버 로그 기록
	logger.Errorf("%s: %v", message, err)

	// 2. 관리자 알림 발송 (텔레그램 등)
	if h.notifyClient != nil {
		go func(msg string, e error) {
			// 알림 발송 중 예기치 못한 패닉이 발생해도, 전체 API 서버가 중단되는 것을 방어합니다.
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("알림 전송 실패: 패닉 발생 (%v)", r)
				}
			}()

			// 외부 API 장애로 인해 자원(고루틴)이 영구적으로 적체(Leak)되는 것을 막기 위해 5초 제한을 둡니다.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			h.notifyClient.NotifyError(ctx, fmt.Sprintf("%s\r\n\r\n%s", msg, e))
		}(message, err)
	}

	// 3. HTTP 500 에러 응답 반환
	return httputil.NewInternalServerError(message)
}
