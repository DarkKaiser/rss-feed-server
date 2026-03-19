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

type Handler struct {
	config *config.AppConfig

	feedRepo feed.Repository

	notifyClient *notify.Client
}

func New(config *config.AppConfig, feedRepo feed.Repository, notifyClient *notify.Client) *Handler {
	return &Handler{
		config: config,

		feedRepo: feedRepo,

		notifyClient: notifyClient,
	}
}

// ViewSummary 핸들러
//
// @Summary 전체 RSS 피드 정보 요약 페이지
// @Description 현재 서버가 제공하고 있는 전체 RSS 목록, 서비스 소개 및 기본 정보를 HTML 웹 페이지 형태로 제공합니다. 브라우저로 접속 시 이 페이지를 볼 수 있습니다.
// @Tags RSS
// @Produce text/html
// @Success 200 {string} string "HTML 요약 페이지"
// @Router / [get]
func (h *Handler) ViewSummary(c echo.Context) error {
	return c.Render(http.StatusOK, "rss_summary.tmpl", map[string]interface{}{
		"serviceUrl": fmt.Sprintf("%s://%s", c.Scheme(), c.Request().Host),
		"rssFeed":    h.config.RssFeed,
	})
}

// GetFeed 핸들러
//
// @Summary 개별 RSS 피드 조회 (XML)
// @Description 지정된 식별자(id)에 해당하는 특정 게시판의 최신 게시글 목록을 RSS 2.0 호환 XML 형식으로 제공합니다. RSS 리더 등에서 구독 링크로 사용할 수 있습니다.
// @Tags RSS
// @Produce application/xml
// @Param id path string true "RSS 피드 고유 식별자 (예: gangnam, gangnam.xml 형식 모두 가능)"
// @Success 200 {string} string "RSS 2.0 규격의 XML 포맷 데이터"
// @Failure 400 {object} response.ErrorResponse "잘못된 요청 또는 유효하지 않은 피드 식별자"
// @Failure 500 {object} response.ErrorResponse "서버 내부 처리 중 오류 발생 (DB 조회 실패, XML 파싱 오류 등)"
// @Router /{id} [get]
func (h *Handler) GetFeed(c echo.Context) error {
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
