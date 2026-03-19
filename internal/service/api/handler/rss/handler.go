package rss

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"time"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service/api/httputil"
	_ "github.com/darkkaiser/rss-feed-server/internal/service/api/model/response"
	"github.com/darkkaiser/rss-feed-server/internal/service/api/rss"
	"github.com/labstack/echo/v4"
)

// component RSS 핸들러의 로깅용 컴포넌트 이름
const component = "api.handler.rss"

// Handler RSS 피드 관련 HTTP 요청을 처리하는 핸들러입니다.
type Handler struct {
	appConfig *config.AppConfig

	// @@@@@
	// providerMap 은 설정에 등록된 RSS 피드 프로바이더를 ID 기준으로 인덱싱한 맵입니다.
	// GetFeed 요청마다 슬라이스를 순회하는 O(n) 탐색 대신 O(1) 조회를 위해 사용됩니다.
	providerMap map[string]*config.ProviderConfig

	feedRepo feed.Repository

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
	// 요청마다 반복되는 선형 탐색을 피하기 위해 Provider ID를 키로 하는 맵을 미리 구성한다.
	providerMap := make(map[string]*config.ProviderConfig, len(appConfig.RssFeed.Providers))
	for _, p := range appConfig.RssFeed.Providers {
		providerMap[p.ID] = p
	}

	return &Handler{
		appConfig: appConfig,

		providerMap: providerMap,

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
	applog.WithComponentAndFields(component, applog.Fields{
		"request_id": c.Response().Header().Get(echo.HeaderXRequestID),
		"endpoint":   "/",
		"method":     c.Request().Method,
		"remote_ip":  c.RealIP(),
		"user_agent": c.Request().UserAgent(),
	}).Debug("RSS 피드 목록 요약 페이지 조회")

	// @@@@@
	return c.Render(http.StatusOK, "rss_summary.tmpl", map[string]any{
		"serviceURL": fmt.Sprintf("%s://%s", c.Scheme(), c.Request().Host),
		"rssFeed":    h.appConfig.RssFeed,
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
	if strings.HasSuffix(strings.ToLower(id), ".xml") {
		id = id[:len(id)-len(".xml")]
	}

	applog.WithComponentAndFields(component, applog.Fields{
		"request_id": c.Response().Header().Get(echo.HeaderXRequestID),
		"endpoint":   "/{id}",
		"feed_id":    id,
		"method":     c.Request().Method,
		"remote_ip":  c.RealIP(),
		"user_agent": c.Request().UserAgent(),
	}).Debug("개별 RSS 피드 조회")

	// @@@@@
	// providerMap에서 O(1) 조회로 해당 프로바이더를 찾는다.
	// 등록되지 않은 ID라면 즉시 400 에러를 반환한다.
	p, ok := h.providerMap[id]
	if !ok {
		return httputil.NewBadRequestError(fmt.Sprintf("요청하신 식별자(ID:%s)의 RSS 피드를 찾을 수 없습니다.", id))
	}

	// 해당 프로바이더에 속한 모든 게시판 ID를 수집한다.
	// DB 조회 시 여러 게시판의 게시글을 한 번에 가져오기 위해 슬라이스로 구성한다.
	var boardIDs []string
	for _, b := range p.Config.Boards {
		boardIDs = append(boardIDs, b.ID)
	}

	// DB에서 최신 게시글을 MaxItemCount 개 한도로 조회한다.
	articles, err := h.feedRepo.GetArticles(p.ID, boardIDs, h.appConfig.RssFeed.MaxItemCount)
	if err != nil {
		m := fmt.Sprintf("DB에서 게시글을 읽어오는 중에 오류가 발생하였습니다. (p_id:%s)", p.ID)

		applog.Errorf("%s (error:%s)", m, err)

		// 텔레그램 등 외부 알림 채널을 통해 담당자에게 즉시 오류를 전파한다.
		if h.notifyClient != nil {
			h.notifyClient.NotifyError(context.Background(), fmt.Sprintf("%s\r\n\r\n%s", m, err))
		}

		return httputil.NewInternalServerError(m)
	}

	// 조회된 게시글 목록을 RSS 2.0 규격의 Feed 객체로 변환한다.

	// RSS <lastBuildDate> 필드는 피드에서 가장 최근에 변경된 시각을 나타낸다.
	// DB 결과가 CreatedAt 내림차순이라고 가정하여 첫 번째 게시글의 작성시간을 사용한다.
	var lastBuildDate time.Time
	if len(articles) > 0 {
		lastBuildDate = articles[0].CreatedAt
	}

	rssFeed := rss.NewRssFeed(p.Config.Name, p.Config.URL, p.Config.Description, "ko", config.AppName, time.Now(), lastBuildDate)
	for _, article := range articles {
		// 게시글 본문의 줄바꿈(CRLF)을 HTML <br> 태그로 치환하여
		// RSS 리더에서 줄바꿈이 올바르게 렌더링되도록 한다.
		rssFeed.Items = append(rssFeed.Items,
			rss.NewRssFeedItem(article.Title, article.Link, strings.ReplaceAll(article.Content, "\r\n", "<br>"), article.Author, article.BoardName, article.CreatedAt),
		)
	}

	// Feed 객체를 RSS 클라이언트가 파싱하기 쉽도록 들여쓰기된 XML 바이트로 직렬화한다.
	xmlBytes, err := xml.MarshalIndent(rssFeed.FeedXml(), "", "  ")
	if err != nil {
		m := fmt.Sprintf("RSS Feed 객체를 XML로 변환하는 중에 오류가 발생하였습니다. (ID:%s)", id)

		applog.Errorf("%s (error:%s)", m, err)

		// 텔레그램 등 외부 알림 채널을 통해 담당자에게 즉시 오류를 전파한다.
		if h.notifyClient != nil {
			h.notifyClient.NotifyError(context.Background(), fmt.Sprintf("%s\r\n\r\n%s", m, err))
		}

		return httputil.NewInternalServerError(m)
	}

	return c.XMLBlob(http.StatusOK, xmlBytes)
}
