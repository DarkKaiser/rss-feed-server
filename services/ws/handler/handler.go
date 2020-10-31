package handler

import (
	"database/sql"
	"encoding/xml"
	"fmt"
	"github.com/darkkaiser/rss-feed-server/feeds"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
	"github.com/darkkaiser/rss-feed-server/services/ws/model"
	"github.com/labstack/echo"
	log "github.com/sirupsen/logrus"
	"net/http"
	"strings"
	"time"
)

//
// WebServiceHandlers
//
type WebServiceHandlers struct {
	config *g.AppConfig

	db *sql.DB

	naverCafe *model.NaverCafe

	rssFeedMaxItemCount uint
}

func NewWebServiceHandlers(config *g.AppConfig) *WebServiceHandlers {
	db, err := sql.Open("sqlite3", fmt.Sprintf("./%s.db", g.AppName))
	if err != nil {
		m := "DB를 여는 중에 치명적인 오류가 발생하였습니다."

		notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)

		log.Panicf("%s (error:%s)", m, err)
	}

	handlers := &WebServiceHandlers{
		config: config,

		db: db,

		naverCafe: model.NewNaverCafe(config, db),

		rssFeedMaxItemCount: config.RssFeed.MaxItemCount,
	}

	return handlers
}

func (h *WebServiceHandlers) Close() {
	err := h.db.Close()
	if err != nil {
		m := "DB를 닫는 중에 오류가 발생하였습니다."

		log.Errorf("%s (error:%s)", m, err)

		notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)
	}
}

func (h *WebServiceHandlers) Find(modelType model.ModelType) interface{} {
	switch modelType {
	case model.NaverCafeModel:
		return h.naverCafe
	}

	return nil
}

func (h *WebServiceHandlers) GetNaverCafeRssFeedHandler(c echo.Context) error {
	// 입력된 네이버 카페의 ID를 구한다.
	cafeID := c.Param("cafeid")
	if strings.HasSuffix(strings.ToLower(cafeID), ".xml") == true {
		cafeID = cafeID[:len(cafeID)-len(".xml")]
	}

	rssFeed := &feeds.RssFeed{}

	for _, c := range h.config.RssFeed.NaverCafes {
		if c.ID == cafeID {
			//
			// 게시글을 검색한다.
			//
			var boardIDs []string
			for _, b := range c.Boards {
				boardIDs = append(boardIDs, b.ID)
			}

			articles, err := h.naverCafe.Articles(cafeID, boardIDs, h.rssFeedMaxItemCount)
			if err != nil {
				m := fmt.Sprintf("네이버 카페('%s')의 게시글을 DB에서 읽어오는 중에 오류가 발생하였습니다.", cafeID)

				log.Errorf("%s (error:%s)", m, err)

				notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)

				return echo.NewHTTPError(http.StatusInternalServerError, err)
			}

			//
			// 검색된 게시글을 RSS Feed로 변환한다.
			//

			// 가장 최근에 작성된 게시글의 작성시간을 구한다.
			var lastBuildDate time.Time
			if len(articles) > 0 {
				lastBuildDate = articles[0].CreatedAt
			}

			rssFeed = feeds.NewRssFeed(c.Name, model.NaverCafeUrl(c.ID), c.Description, "ko", g.AppName, time.Now(), lastBuildDate)

			for _, article := range articles {
				rssFeed.Items = append(rssFeed.Items,
					feeds.NewRssFeedItem(article.Title, article.Link, article.Content, article.Author, article.BoardName, article.CreatedAt),
				)
			}

			break
		}
	}

	xmlBytes, err := xml.MarshalIndent(rssFeed.FeedXml(), "", "  ")
	if err != nil {
		m := fmt.Sprintf("네이버 카페('%s')의 게시글을 RSS Feed로 변환하는 중에 오류가 발생하였습니다.", cafeID)

		log.Errorf("%s (error:%s)", m, err)

		notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)

		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}

	return c.XMLBlob(http.StatusOK, xmlBytes)
}
