package handler

import (
	"encoding/xml"
	"fmt"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
	"github.com/darkkaiser/rss-feed-server/services/ws/feeds"
	"github.com/labstack/echo/v4"
	log "github.com/sirupsen/logrus"
	"net/http"
	"strings"
	"time"
)

func (h *Handler) GetRssFeedSummaryViewHandler(c echo.Context) error {
	return c.Render(http.StatusOK, "rss_feed_summary_view.tmpl", map[string]interface{}{
		"serviceUrl": fmt.Sprintf("%s://%s", c.Scheme(), c.Request().Host),
		"rssFeed":    h.config.RssFeed,
	})
}

func (h *Handler) GetRssFeedHandler(c echo.Context) error {
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

			articles, err := h.rssFeedProviderStore.Articles(p.ID, boardIDs, h.config.RssFeed.MaxItemCount)
			if err != nil {
				m := fmt.Sprintf("DB에서 게시글을 읽어오는 중에 오류가 발생하였습니다. (p_id:%s)", p.ID)

				log.Errorf("%s (error:%s)", m, err)

				notifyapi.Send(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)

				return echo.NewHTTPError(http.StatusInternalServerError, err)
			}

			//
			// 검색된 게시글을 RSS Feed로 변환한다.
			//

			// 가장 최근에 작성된 게시글의 작성시간을 구한다.
			var lastBuildDate time.Time
			if len(articles) > 0 {
				lastBuildDate = articles[0].CreatedDate
			}

			rssFeed := feeds.NewRssFeed(p.Config.Name, p.Config.Url, p.Config.Description, "ko", g.AppName, time.Now(), lastBuildDate)
			for _, article := range articles {
				rssFeed.Items = append(rssFeed.Items,
					feeds.NewRssFeedItem(article.Title, article.Link, strings.ReplaceAll(article.Content, "\r\n", "<br>"), article.Author, article.BoardName, article.CreatedDate),
				)
			}

			xmlBytes, err := xml.MarshalIndent(rssFeed.FeedXml(), "", "  ")
			if err != nil {
				m := fmt.Sprintf("RSS Feed 객체를 XML로 변환하는 중에 오류가 발생하였습니다. (ID:%s)", id)

				log.Errorf("%s (error:%s)", m, err)

				notifyapi.Send(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)

				return echo.NewHTTPError(http.StatusInternalServerError, err)
			}

			return c.XMLBlob(http.StatusOK, xmlBytes)
		}
	}

	return c.NoContent(http.StatusBadRequest)
}
