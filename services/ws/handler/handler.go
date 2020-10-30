package handler

import (
	"database/sql"
	"encoding/xml"
	"fmt"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
	"github.com/darkkaiser/rss-feed-server/services/ws/model"
	"github.com/darkkaiser/rss-feed-server/utils"
	"github.com/gorilla/feeds"
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

		rssFeedMaxItemCount: config.RSSFeed.MaxItemCount,
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

func (h *WebServiceHandlers) GetNaverCafeRSSFeedHandler(c echo.Context) error {
	// 입력된 네이버 카페의 ID를 구한다.
	cafeID := c.Param("cafeid")
	if strings.HasSuffix(strings.ToLower(cafeID), ".xml") == true {
		cafeID = cafeID[:len(cafeID)-len(".xml")]
	}

	rssFeed := &feeds.RssFeed{}
	for _, c := range h.config.RSSFeed.NaverCafes {
		if c.ID == cafeID {
			var boardIDs []string
			for _, b := range c.Boards {
				boardIDs = append(boardIDs, b.ID)
			}

			articles, err := h.naverCafe.GetArticles(cafeID, boardIDs, h.rssFeedMaxItemCount)
			if err != nil {
				m := fmt.Sprintf("네이버 카페('%s')의 게시글을 DB에서 읽어오는 중에 오류가 발생하였습니다.", cafeID)

				log.Errorf("%s (error:%s)", m, err)

				notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)

				return echo.NewHTTPError(http.StatusInternalServerError, err)
			}

			rssFeed = h.generateRSSFeed(c, articles)

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

// @@@@@
func (h *WebServiceHandlers) generateRSSFeed(c *g.NaverCafeCrawlingConfig, articles []*model.NaverCafeArticle) *feeds.RssFeed {
	var now = time.Now()
	pub := utils.AnyTimeFormat(time.RFC1123Z, now)
	build := ""
	//author := ""

	channel := &feeds.RssFeed{
		Title:          c.Name,
		Link:           fmt.Sprintf("%s/%s", model.NaverCafeHomeUrl, c.ID),
		Description:    c.Description,
		ManagingEditor: "",
		PubDate:        pub,
		LastBuildDate:  build,
		Copyright:      "",
	}
	for _, article := range articles {
		channel.Items = append(channel.Items, newRssItem2(article))
	}
	//for _, i := range r.Items {
	//	channel.Items = append(channel.Items, newRssItem(i))
	//}
	return channel
	//////////////////////////////
}

// @@@@@
// create a new RssItem with a generic Item struct's data
func newRssItem2(i *model.NaverCafeArticle) *feeds.RssItem {
	item := &feeds.RssItem{
		Title:       i.Title,
		Link:        i.Link,
		Description: i.Content,
		Guid:        i.Link,
		PubDate:     utils.AnyTimeFormat(time.RFC1123Z, i.CreatedAt),
	}
	if len(i.Content) > 0 {
		item.Content = &feeds.RssContent{Content: i.Content}
	}
	//if i.Source != nil {
	//	item.Source = i.Source.Href
	//}

	// Define a closure
	//if i.Enclosure != nil && i.Enclosure.Type != "" && i.Enclosure.Length != "" {
	//	item.Enclosure = &feeds.RssEnclosure{Url: i.Enclosure.Url, Type: i.Enclosure.Type, Length: i.Enclosure.Length}
	//}

	if i.Author != "" {
		item.Author = i.Author
	}

	item.Category = "category"

	return item
}
