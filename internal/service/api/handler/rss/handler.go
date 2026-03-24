package rss

import (
	"context"
	"encoding/xml"
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

// @@@@@
var (
	contentReplacer = strings.NewReplacer("\r\n", "<br>", "\n", "<br>")

	// hasHTMLTags는 RSS 본문에 구조적인 HTML 태그가 이미 포함되어 있는지 판별합니다.
	hasHTMLTags = regexp.MustCompile(`(?i)<(?:br|p|div|img|table|ul|ol|li|h[1-6])[^>]*>`)
)

// @@@@@
// providerInfo 개별 공급자 설정과 최소화된 할당을 위한 캐시(게시판 ID 목록) 등을 담는 내부 구조체입니다.
type providerInfo struct {
	config   *config.ProviderConfig
	boardIDs []string
}

// Handler RSS 피드 관련 HTTP 요청을 처리하는 핸들러입니다.
type Handler struct {
	appConfig *config.AppConfig

	// @@@@@
	// providers 설정에 등록된 RSS 피드 프로바이더 정보 및 캐시를 ID 기준으로 인덱싱한 맵입니다.
	providers map[string]providerInfo

	// feedRepo 게시글 영속성을 담당하는 저장소 인터페이스입니다.
	feedRepo feed.Repository

	// notifyClient 텔레그램 등 외부 알림 채널과 통신하는 클라이언트입니다.
	// DB 조회 또는 XML 직렬화 실패 시 담당자에게 즉시 오류를 전파하는 데 사용됩니다.
	// nil 이 허용되며, nil 인 경우 외부 알림은 전송되지 않습니다.
	notifyClient *notify.Client
}

// New Handler 인스턴스를 생성합니다.
func New(appConfig *config.AppConfig, feedRepo feed.Repository, notifyClient *notify.Client) *Handler {
	if appConfig == nil {
		panic("AppConfig는 필수입니다")
	}
	if feedRepo == nil {
		panic("feed.Repository는 필수입니다")
	}

	// @@@@@
	// 클라이언트가 특정 피드 ID를 요청했을 때 매번 전체 목록을 뒤지지 않고 즉시 꺼내 쓸 수 있도록, ID를 Key로 하는 사전을 미리 만들어 둡니다.
	providers := make(map[string]providerInfo, len(appConfig.RSSFeed.Providers))
	for _, p := range appConfig.RSSFeed.Providers {
		var boardIDs []string
		for _, b := range p.Config.Boards {
			boardIDs = append(boardIDs, b.ID)
		}
		providers[strings.ToLower(p.ID)] = providerInfo{
			config:   p,
			boardIDs: boardIDs,
		}
	}

	return &Handler{
		appConfig: appConfig,

		providers: providers,

		feedRepo: feedRepo,

		notifyClient: notifyClient,
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
	logger := applog.WithComponentAndFields(component, applog.Fields{
		"request_id": c.Response().Header().Get(echo.HeaderXRequestID),
		"endpoint":   "/",
		"method":     c.Request().Method,
		"remote_ip":  c.RealIP(),
		"user_agent": c.Request().UserAgent(),
	})
	logger.Debug("RSS 피드 목록 요약 페이지 조회")

	return c.Render(http.StatusOK, "rss_summary.tmpl", map[string]any{
		"baseURL":    fmt.Sprintf("%s://%s", c.Scheme(), c.Request().Host),
		"feedConfig": h.appConfig.RSSFeed,
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
	// @@@@@
	// URL 경로에서 피드 식별자(ID)를 추출한다.
	// RSS 리더 앱 호환성을 위해 ".xml" 확장자가 붙은 경우(예: /naver-cafe.xml)에도
	// 정상적으로 처리될 수 있도록 확장자를 제거한다.
	id := c.Param("id")
	id = strings.ToLower(id)
	id = strings.TrimSuffix(id, ".xml")

	logger := applog.WithComponentAndFields(component, applog.Fields{
		"request_id": c.Response().Header().Get(echo.HeaderXRequestID),
		"endpoint":   "/{id}",
		"feed_id":    id,
		"method":     c.Request().Method,
		"remote_ip":  c.RealIP(),
		"user_agent": c.Request().UserAgent(),
	})
	logger.Debug("개별 RSS 피드 조회")

	// @@@@@
	// providers에서 O(1) 조회로 해당 프로바이더를 찾는다.
	// 등록되지 않은 ID라면 즉시 404 Not Found 에러를 반환한다.
	pInfo, ok := h.providers[id]
	if !ok {
		return httputil.NewNotFoundError(fmt.Sprintf("요청하신 식별자(ID:%s)의 RSS 피드를 찾을 수 없습니다.", id))
	}
	p := pInfo.config

	// DB에서 최신 게시글을 MaxItemCount 개 한도로 모두 조회한다.
	// SQLite의 조회 성능이 충분히 빠르므로, 복잡한 If-Modified-Since(304) 로직 대신 매번 결과를 반환하여 정합성을 보장한다.
	articles, err := h.feedRepo.GetArticles(c.Request().Context(), p.ID, pInfo.boardIDs, h.appConfig.RSSFeed.MaxItemCount)
	if err != nil {
		return h.notifyError(logger, fmt.Sprintf("DB에서 게시글을 읽어오는 중에 오류가 발생하였습니다. (p_id:%s)", p.ID), err)
	}

	now := time.Now()
	cacheBuildDate := now
	if len(articles) > 0 {
		cacheBuildDate = articles[0].CreatedAt
	}

	c.Response().Header().Set("Cache-Control", "public, max-age=60")

	// 조회된 게시글 목록을 RSS 2.0 규격의 Feed 객체로 변환한다.
	rssFeed := rss.NewRSSFeed(p.Config.Name, p.Config.URL, p.Config.Description, "ko", config.AppName, now, cacheBuildDate)
	rssFeed.Items = make([]*rss.RssItem, 0, len(articles))

	for _, article := range articles {
		// 게시글 본문의 줄바꿈(\r\n, \n)을 HTML <br> 태그로 치환하여
		// RSS 리더에서 줄바꿈이 올바르게 렌더링되도록 한다.
		// 단, 본문에 이미 구조적 HTML 태그가 포함되어 있다면 치환하지 않는다.
		content := article.Content
		if !hasHTMLTags.MatchString(content) {
			content = contentReplacer.Replace(content)
		}

		rssFeed.Items = append(rssFeed.Items,
			rss.NewRSSFeedItem(article.Title, article.Link, content, article.Author, article.BoardName, article.CreatedAt),
		)
	}

	// Feed 객체를 직렬화한다.
	xmlBytes, err := xml.Marshal(rssFeed.FeedXml())
	if err != nil {
		return h.notifyError(logger, fmt.Sprintf("RSS Feed 객체를 XML로 변환하는 중에 오류가 발생하였습니다. (ID:%s)", id), err)
	}

	// 직렬화된 XML 바이트 앞에 표준 XML 선언 헤더를 추가하여 반환한다.
	return c.Blob(http.StatusOK, "application/rss+xml; charset=UTF-8", append([]byte(xml.Header), xmlBytes...))
}

// @@@@@
// notifyError 에러를 로깅하고 알림 클라이언트를 통해 담당자에게 전파한 후, InternalServerError 응답 객체를 반환합니다.
func (h *Handler) notifyError(logger *applog.Entry, message string, err error) error {
	logger.Errorf("%s (error:%s)", message, err)

	if h.notifyClient != nil {
		go func(msg string, e error) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			h.notifyClient.NotifyError(ctx, fmt.Sprintf("%s\r\n\r\n%s", msg, e))
		}(message, err)
	}

	return httputil.NewInternalServerError(message)
}
