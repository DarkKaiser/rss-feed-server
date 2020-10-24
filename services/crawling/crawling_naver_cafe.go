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

// @@@@@
type naverCafeBoardArticle struct {
	boardID   string
	boardName string
	Title     string
	Link      string
	Content   string
	Author    string
	CreateAt  string
}

func (c *naverCafeCrawling) Run() {
	// @@@@@
	//////////////////////////////////////
	var articleID int = 10
	println(articleID)

	pageNo := 1
	ncPageUrl := fmt.Sprintf("%s/ArticleList.nhn?search.clubid=%s&userDisplay=50&search.boardtype=L&search.totalCount=501&search.page=%d", c.config.Url, c.config.ClubID, pageNo)

	res, err := http.Get(ncPageUrl)
	if err != nil {

	}
	if res.StatusCode != http.StatusOK {

	}
	// c.feed.LatestArticleID()

	defer res.Body.Close()

	resBodyBytes, err := ioutil.ReadAll(res.Body)

	doc1 := string(resBodyBytes)

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
		fmt.Print("# " + strings.TrimSpace(s.Find("td.td_article div.board-name a").Text()))
		href1, _ := s.Find("td.td_article div.board-name a").Attr("href")
		fmt.Print(", " + strings.TrimSpace(href1))

		// Title & Link
		fmt.Print(", " + strings.TrimSpace(s.Find("a.article").Text()))
		href, _ := s.Find("a.article").Attr("href")
		fmt.Print(", " + href)

		// Description => Content

		// Author
		fmt.Print(", " + strings.TrimSpace(s.Find("td.td_name > div.pers_nick_area").Text()))

		// Created
		fmt.Print(", " + strings.TrimSpace(s.Find("td.td_date").Text()) + "\n")
	})
}
