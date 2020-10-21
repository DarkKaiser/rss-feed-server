package crawling

import (
	log "github.com/sirupsen/logrus"
)

const (
	naverCafeCrawlingBoardTypeList string = "L"
)

type naverCafeCrawling struct {
	id          string
	clubID      string
	name        string
	description string
	url         string

	boards []*naverCafeCrawlingBoard
}

type naverCafeCrawlingBoard struct {
	id               string
	name             string
	tp               string
	contentCanBeRead bool
}

func (c *naverCafeCrawling) Run() {
	// @@@@@
	log.Print("naverCafeCrawling run~~~~~~~~~~")

	var articleID int = 10
	println(articleID)

	//	clPageUrl := fmt.Sprintf("https://cafe.naver.com/ludypang/ArticleList.nhn?search.clubid=12303558&userDisplay=50&search.boardtype=L&search.totalCount=501&search.page=2")
	//
	//	res, err := http.Post(clPageUrl, "application/x-www-form-urlencoded; charset=UTF-8", nil)
	//	checkErr(err)
	//
	//	defer res.Body.Close()
	//
	//	resBodyBytes, err := ioutil.ReadAll(res.Body)
	//	checkErr(err)
	//
	//	doc1 := string(resBodyBytes)
	//
	//	euckrDecoder := korean.EUCKR.NewDecoder()
	//	name, err0 := euckrDecoder.String(string(doc1))
	//	checkErr(err0)
	//
	//	htmlNode, err := html.Parse(strings.NewReader(name))
	//
	//	doc := goquery.NewDocumentFromNode(htmlNode)
	//
	//	clSelection := doc.Find("td.td_article")
	//	clSelection.Each(func(i int, s *goquery.Selection) {
	//		fmt.Println( string(strings.TrimSpace(s.Find("a.article").Text())))
	//		//println(strings.TrimSpace(s.Find("a.article").Text()))
	//	})
	//
	//	//res, err := http.Get(clPageUrl)
	//	//checkErr(err)
	//	//
	//	//defer res.Body.Close()
	//	//
	//	//doc, err := goquery.NewDocumentFromReader(res.Body)
	//	//println(name)

}
