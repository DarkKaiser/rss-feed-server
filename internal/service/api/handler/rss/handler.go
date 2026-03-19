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

// @@@@@
type Handler struct {
	config *config.AppConfig

	feedRepo feed.Repository

	notifyClient *notify.Client
}

// @@@@@
func New(config *config.AppConfig, feedRepo feed.Repository, notifyClient *notify.Client) *Handler {
	return &Handler{
		config: config,

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
	// @@@@@
	return c.Render(http.StatusOK, "rss_summary.tmpl", map[string]interface{}{
		"serviceUrl": fmt.Sprintf("%s://%s", c.Scheme(), c.Request().Host),
		"rssFeed":    h.config.RssFeed,
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
	// 입력된 ID를 구한다.
	id := c.Param("id")
	if strings.HasSuffix(strings.ToLower(id), ".xml") == true {
		id = id[:len(id)-len(".xml")]
	}

	for _, p := range h.config.RssFeed.Providers {
		if p.ID == id {
			//
			// 게시글을 검색한다.
			//
			var boardIDs []string
			for _, b := range p.Config.Boards {
				boardIDs = append(boardIDs, b.ID)
			}

			articles, err := h.feedRepo.GetArticles(p.ID, boardIDs, h.config.RssFeed.MaxItemCount)
			if err != nil {
				m := fmt.Sprintf("DB에서 게시글을 읽어오는 중에 오류가 발생하였습니다. (p_id:%s)", p.ID)

				applog.Errorf("%s (error:%s)", m, err)

				if h.notifyClient != nil {
					h.notifyClient.NotifyError(context.Background(), fmt.Sprintf("%s\r\n\r\n%s", m, err))
				}

				return httputil.NewInternalServerError(m)
			}

			//
			// 검색된 게시글을 RSS Feed로 변환한다.
			//

			// 가장 최근에 작성된 게시글의 작성시간을 구한다.
			var lastBuildDate time.Time
			if len(articles) > 0 {
				lastBuildDate = articles[0].CreatedAt
			}

			rssFeed := rss.NewRssFeed(p.Config.Name, p.Config.URL, p.Config.Description, "ko", config.AppName, time.Now(), lastBuildDate)
			for _, article := range articles {
				rssFeed.Items = append(rssFeed.Items,
					rss.NewRssFeedItem(article.Title, article.Link, strings.ReplaceAll(article.Content, "\r\n", "<br>"), article.Author, article.BoardName, article.CreatedAt),
				)
			}

			xmlBytes, err := xml.MarshalIndent(rssFeed.FeedXml(), "", "  ")
			if err != nil {
				m := fmt.Sprintf("RSS Feed 객체를 XML로 변환하는 중에 오류가 발생하였습니다. (ID:%s)", id)

				applog.Errorf("%s (error:%s)", m, err)

				if h.notifyClient != nil {
					h.notifyClient.NotifyError(context.Background(), fmt.Sprintf("%s\r\n\r\n%s", m, err))
				}

				return httputil.NewInternalServerError(m)
			}

			return c.XMLBlob(http.StatusOK, xmlBytes)
		}
	}

	return httputil.NewBadRequestError(fmt.Sprintf("요청하신 식별자(ID:%s)의 RSS 피드를 찾을 수 없습니다.", id))
}
