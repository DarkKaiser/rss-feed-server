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
	cafeId := c.Param("cafeid")
	if strings.HasSuffix(strings.ToLower(cafeId), ".xml") == true {
		cafeId = cafeId[:len(cafeId)-len(".xml")]
	}

	rssFeed := &feeds.RssFeed{}
	for _, c := range h.config.RSSFeed.NaverCafes {
		if c.ID == cafeId {
			var boardIDs []string
			for _, b := range c.Boards {
				boardIDs = append(boardIDs, b.ID)
			}

			articles, err := h.naverCafe.GetArticles(cafeId, boardIDs, h.rssFeedMaxItemCount)
			if err != nil {
				m := fmt.Sprintf("네이버 카페('%s')의 게시글을 DB에서 읽어오는 중에 오류가 발생하였습니다.", cafeId)

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
		m := fmt.Sprintf("네이버 카페('%s')의 게시글을 RSS Feed로 변환하는 중에 오류가 발생하였습니다.", cafeId)

		log.Errorf("%s (error:%s)", m, err)

		notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)

		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}

	return c.XMLBlob(http.StatusOK, xmlBytes)
}

// @@@@@
func (h *WebServiceHandlers) generateRSSFeed(c *g.NaverCafeCrawlingConfig, articles []*model.NaverCafeArticle) *feeds.RssFeed {
	feed := &feeds.Feed{
		Title:       c.Name,
		Link:        &feeds.Link{Href: ""},
		Description: c.Description,
		Author:      &feeds.Author{Name: "Jason Moiron", Email: "jmoiron@jmoiron.net"},
		Created:     time.Now(),
	}

	for _, article := range articles {
		item := &feeds.Item{
			Title:       article.Title,
			Link:        &feeds.Link{Href: article.Link},
			Author:      &feeds.Author{Name: article.Author, Email: "jmoiron@jmoiron.net"},
			Description: article.Content,
			//Id:          article.ArticleID,
			Content: article.Content,
		}

		feed.Items = append(feed.Items, item)
	}

	//https://github.com/gorilla/feeds/blob/master/doc.go
	rssFeed := &feeds.Rss{Feed: feed}

	return RssFeed(rssFeed)
}

// @@@@@
func RssFeed(r *feeds.Rss) *feeds.RssFeed {
	pub := utils.AnyTimeFormat(time.RFC1123Z, r.Created, r.Updated)
	build := utils.AnyTimeFormat(time.RFC1123Z, r.Updated)
	author := ""
	if r.Author != nil {
		author = r.Author.Email
		if len(r.Author.Name) > 0 {
			author = fmt.Sprintf("%s (%s)", r.Author.Email, r.Author.Name)
		}
	}

	var image *feeds.RssImage
	if r.Image != nil {
		image = &feeds.RssImage{Url: r.Image.Url, Title: r.Image.Title, Link: r.Image.Link, Width: r.Image.Width, Height: r.Image.Height}
	}

	channel := &feeds.RssFeed{
		Title:          r.Title,
		Link:           r.Link.Href,
		Description:    r.Description,
		ManagingEditor: author,
		PubDate:        pub,
		LastBuildDate:  build,
		Copyright:      r.Copyright,
		Image:          image,
	}
	for _, i := range r.Items {
		channel.Items = append(channel.Items, newRssItem(i))
	}
	return channel
}

// @@@@@
// create a new RssItem with a generic Item struct's data
func newRssItem(i *feeds.Item) *feeds.RssItem {
	item := &feeds.RssItem{
		Title:       i.Title,
		Link:        i.Link.Href,
		Description: i.Description,
		Guid:        i.Id,
		PubDate:     utils.AnyTimeFormat(time.RFC1123Z, i.Created, i.Updated),
	}
	if len(i.Content) > 0 {
		item.Content = &feeds.RssContent{Content: i.Content}
	}
	if i.Source != nil {
		item.Source = i.Source.Href
	}

	// Define a closure
	if i.Enclosure != nil && i.Enclosure.Type != "" && i.Enclosure.Length != "" {
		item.Enclosure = &feeds.RssEnclosure{Url: i.Enclosure.Url, Type: i.Enclosure.Type, Length: i.Enclosure.Length}
	}

	if i.Author != nil {
		item.Author = i.Author.Name
	}

	item.Category = "category"

	return item
}
