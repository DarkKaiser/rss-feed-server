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
)

//
// WebServiceHandlers
//
type WebServiceHandlers struct {
	config *g.AppConfig

	db *sql.DB

	rssProviderModel *model.RssProvider

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

		rssProviderModel: model.NewRssProvider(config, db),

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
	case model.RssProviderModel:
		return h.rssProviderModel
	}

	return nil
}

func (h *WebServiceHandlers) GetRssFeedSummaryViewHandler(c echo.Context) error {
	var html = fmt.Sprintf(`
		<!DOCTYPE html>
		<html>
		<head>
		<style>
		#providers {
		  font-family: Arial, Helvetica, sans-serif;
		  border-collapse: collapse;
		  width: 100%%;
		}
		
		#providers td, #providers th {
		  border: 1px solid #ddd;
		  padding: 8px;
		}
		
		#providers tr:nth-child(even) { background-color: #f2f2f2; }
		
		#providers tr:hover { background-color: #ddd; }
		
		#providers th {
		  padding-top: 12px;
		  padding-bottom: 12px;
		  text-align: left;
		  background-color: #4CAF50;
		  color: white;
		}
		</style>
		</head>
		<body>
		<h3>RSS 피드 목록</h3>
		* RSS 피드 최대 갯수 : %d개
		<table id="providers">
		  <tr>
		    <th>정보제공처</th>
		    <th>RSS 주소</th>
			<th>ID</th>
		    <th>이름</th>
		    <th>URL</th>
		    <th>게시판목록</th>
		    <th>스케쥴</th>
		    <th>게시글 저장기간</th>
		  </tr>
	`, h.config.RssFeed.MaxItemCount)

	for _, p := range h.config.RssFeed.Providers {
		rssFeedUrl := fmt.Sprintf("%s://%s/%s.xml", c.Scheme(), c.Request().Host, p.ID)

		boardNames := ""
		for _, board := range p.Config.Boards {
			boardNames += fmt.Sprintf("%s<br>", board.Name)
		}

		html += fmt.Sprintf(`
		  <tr>
		    <td>%s</td>
		    <td><a href="%s" target="_blank">%s</a></td>
		    <td>%s</td>
		    <td>%s</td>
		    <td>%s</td>
		    <td>%s</td>
		    <td>%s</td>
		    <td>%d일</td>
		  </tr>
		`, p.Site, rssFeedUrl, rssFeedUrl, p.Config.ID, p.Config.Name, p.Config.Url, boardNames, p.CrawlingScheduler.TimeSpec, p.Config.ArticleArchiveDate)
	}

	html += `
		</table>
		</body>
		</html>
	`

	return c.HTML(200, html)
}

func (h *WebServiceHandlers) GetRssFeedHandler(c echo.Context) error {
	// 입력된 ID를 구한다.
	id := c.Param("id")
	if strings.HasSuffix(strings.ToLower(id), ".xml") == true {
		id = id[:len(id)-len(".xml")]
	}

	rssFeed := &feeds.RssFeed{}

	for _, p := range h.config.RssFeed.Providers {
		if p.ID == id {
			//
			// 게시글을 검색한다.
			//
			var boardIDs []string
			for _, b := range p.Config.Boards {
				boardIDs = append(boardIDs, b.ID)
			}

			// @@@@@
			////////////////////////////
			//		articles, err := h.rssProviderModel.Articles(cafeID, boardIDs, h.rssFeedMaxItemCount)
			//		if err != nil {
			//			m := fmt.Sprintf("네이버 카페('%s')의 게시글을 DB에서 읽어오는 중에 오류가 발생하였습니다.", cafeID)
			//
			//			log.Errorf("%s (error:%s)", m, err)
			//
			//			notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)
			//
			//			return echo.NewHTTPError(http.StatusInternalServerError, err)
			//		}
			//
			//		//
			//		// 검색된 게시글을 RSS Feed로 변환한다.
			//		//
			//
			//		// 가장 최근에 작성된 게시글의 작성시간을 구한다.
			//		var lastBuildDate time.Time
			//		if len(articles) > 0 {
			//			lastBuildDate = articles[0].CreatedAt
			//		}
			//
			//		rssFeed = feeds.NewRssFeed(c.Name, c.Url, c.Description, "ko", g.AppName, time.Now(), lastBuildDate)
			//
			//		for _, article := range articles {
			//			rssFeed.Items = append(rssFeed.Items,
			//				feeds.NewRssFeedItem(article.Title, article.Link, article.Content, article.Author, article.BoardName, article.CreatedAt),
			//			)
			//		}
			////////////////////////////

			break
		}
	}

	xmlBytes, err := xml.MarshalIndent(rssFeed.FeedXml(), "", "  ")
	if err != nil {
		m := fmt.Sprintf("RSS Feed 객체를 XML로 변환하는 중에 오류가 발생하였습니다. (ID:%s)", id)

		log.Errorf("%s (error:%s)", m, err)

		notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)

		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}

	return c.XMLBlob(http.StatusOK, xmlBytes)
}
