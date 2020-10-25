package crawling

import (
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/services/ws/model"
	"golang.org/x/net/html"
	"golang.org/x/text/encoding/korean"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
)

const (
	naverCafeCrawlingBoardTypeList string = "L"
)

type naverCafeCrawling struct {
	config *g.NaverCafeCrawlingConfig

	model *model.NaverCafeRSSFeed
}

func newNaverCafeCrawling(config *g.NaverCafeCrawlingConfig, model *model.NaverCafeRSSFeed) *naverCafeCrawling {
	return &naverCafeCrawling{
		config: config,

		model: model,
	}
}

func (c *naverCafeCrawling) Run() {
	// @@@@@
	//////////////////////////////////////
	latestArticleId, err := c.model.GetLatestArticleID(c.config.ID)
	println(latestArticleId)
	println(err)

	var articles []*model.NaverCafeArticle
	for pageNo := 1; pageNo <= 10; pageNo++ {
		ncPageUrl := fmt.Sprintf("%s/ArticleList.nhn?search.clubid=%s&userDisplay=50&search.boardtype=L&search.totalCount=501&search.page=%d", c.config.Url, c.config.ClubID, pageNo)

		res, err := http.Get(ncPageUrl)
		if err != nil {

		}
		if res.StatusCode != http.StatusOK {

		}

		resBodyBytes, err := ioutil.ReadAll(res.Body)
		doc1 := string(resBodyBytes)
		res.Body.Close()

		euckrDecoder := korean.EUCKR.NewDecoder()
		name, err0 := euckrDecoder.String(string(doc1))
		if err0 != nil {

		}

		htmlNode, err := html.Parse(strings.NewReader(name))
		doc := goquery.NewDocumentFromNode(htmlNode)

		//doc, err := goquery.NewDocumentFromReader(res.Body)
		//if err != nil {
		//
		//}

		ncSelection := doc.Find("div.article-board > table > tbody > tr:not(.board-notice)")
		ncSelection.Each(func(i int, s *goquery.Selection) {
			//fmt.Print("# " + strings.TrimSpace(s.Find("td.td_article div.board-name a").Text()))
			href1, _ := s.Find("td.td_article div.board-name a").Attr("href")
			println("### href1:" + href1)
			if href1 == "" {
				return
			}
			p1 := strings.Index(href1, "search.menuid=")
			href1 = href1[p1+14:]
			p2 := strings.Index(href1, "&")
			href1 = href1[:p2]
			println(p1)
			//fmt.Print(", " + strings.TrimSpace(href1))

			// Title & Link
			title := strings.TrimSpace(s.Find("a.article").Text())
			link, _ := s.Find("a.article").Attr("href")
			p3 := strings.Index(link, "articleid=")
			articleId := link[p3+10:]
			p4 := strings.Index(articleId, "&")
			articleId = articleId[:p4]
			aid, _ := strconv.Atoi(articleId)

			// Description => Content

			// Author
			author := strings.TrimSpace(s.Find("td.td_name > div.pers_nick_area").Text())

			// Created
			fmt.Print(", " + strings.TrimSpace(s.Find("td.td_date").Text()) + "\n")

			article := &model.NaverCafeArticle{
				BoardID:   href1,
				ArticleID: aid,
				Title:     title,
				Content:   "",
				Link:      link,
				Author:    author,
				CreatedAt: "",
			}

			articles = append(articles, article)
		})
	}

	c.model.InsertArticles(c.config.ID, articles)
}
