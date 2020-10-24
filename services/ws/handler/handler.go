package handler

import (
	"database/sql"
	"fmt"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
	"github.com/darkkaiser/rss-feed-server/services/ws/model"
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

	naverCafeRSSFeed *model.NaverCafeRSSFeed
}

func NewWebServiceHandlers(config *g.AppConfig) *WebServiceHandlers {
	db, err := sql.Open("sqlite3", fmt.Sprintf("./%s.db", g.AppName))
	if err != nil {
		m := fmt.Sprintf("DB를 여는 중에 치명적인 오류가 발생하였습니다.\r\n\r\n%s", err)

		notifyapi.SendNotifyMessage(m, true)

		log.Panic(m)
	}

	handlers := &WebServiceHandlers{
		config: config,

		db: db,

		naverCafeRSSFeed: model.NewNaverCafeRSSFeed(config, db),
	}

	return handlers
}

func (h *WebServiceHandlers) Close() {
	err := h.db.Close()
	if err != nil {
		m := fmt.Sprintf("DB를 닫는 중에 오류가 발생하였습니다.\r\n\r\n%s", err)

		log.Error(m)

		notifyapi.SendNotifyMessage(m, true)
	}
}

func (h *WebServiceHandlers) Find(modelType model.ModelType) interface{} {
	switch modelType {
	case model.NaverCafeRSSFeedModel:
		return h.naverCafeRSSFeed
	}

	return nil
}

func (h *WebServiceHandlers) GetNaverCafeRSSFeedHandler(c echo.Context) error {
	// 입력된 네이버 카페의 ID를 구한다.
	cafeId := c.Param("cafeid")
	if strings.HasSuffix(strings.ToLower(cafeId), ".xml") == true {
		cafeId = cafeId[:len(cafeId)-len(".xml")]
	}

	// @@@@@
	//////////////////////////////////////////
	articles := h.naverCafeRSSFeed.GetArticles(cafeId)
	println(articles)

	for _, cafe := range h.config.RSSFeed.NaverCafes {
		if cafe.ID == cafeId {
			now := time.Now()
			feed := &feeds.Feed{
				Title:       cafe.Name,
				Link:        &feeds.Link{Href: cafe.Url},
				Description: cafe.Description,
				Author:      &feeds.Author{Name: "Jason Moiron", Email: "jmoiron@jmoiron.net"},
				Created:     now,
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
			rss, err := feed.ToRss()
			if err != nil {
				log.Fatal(err)
			}

			//https://github.com/gorilla/feeds/blob/master/doc.go
			rssFeed := &feeds.Rss{Feed: feed}
			rssFeed2 := RssFeed(rssFeed)
			// rssFeed.Generator = "gorilla/feeds v1.0 (github.com/gorilla/feeds)"
			rss, _ = feeds.ToXML(rssFeed2)

			// 헤더제거
			rss = rss[len("<?xml version=\"1.0\" encoding=\"UTF-8\"?>"):]

			return c.XMLBlob(http.StatusOK, []byte(rss))
			//return echo.NewHTTPError(http.StatusUnauthorized, fmt.Sprintf("접근이 허용되지 않은 Application입니다"))

			break
		}
	}

	return nil
}

// @@@@@
func RssFeed(r *feeds.Rss) *feeds.RssFeed {
	pub := anyTimeFormat(time.RFC1123Z, r.Created, r.Updated)
	build := anyTimeFormat(time.RFC1123Z, r.Updated)
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
		PubDate:     anyTimeFormat(time.RFC1123Z, i.Created, i.Updated),
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

// @@@@@
func anyTimeFormat(format string, times ...time.Time) string {
	for _, t := range times {
		if !t.IsZero() {
			return t.Format(format)
		}
	}
	return ""
}
