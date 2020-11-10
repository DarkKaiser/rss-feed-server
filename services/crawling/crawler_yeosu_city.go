package crawling

import (
	"errors"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
	"github.com/darkkaiser/rss-feed-server/services/ws/model"
	"github.com/darkkaiser/rss-feed-server/utils"
	"github.com/robfig/cron"
	log "github.com/sirupsen/logrus"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// 포토뉴스
	yeosuCityBoardTypePhotoNews string = "P"

	// 리스트 1(번호, 제목, 등록자, 등록일, 조회)
	yeosuCityBoardTypeList1 string = "L_1"

	// 리스트 2(번호, 분류, 제목, 담당부서, 등록일, 조회)
	yeosuCityBoardTypeList2 string = "L_2"
)

var yeosuCityBoardTypes = make(map[string]*yeosuCityBoardTypeConfig)

type yeosuCityBoardTypeConfig struct {
	urlPath string

	articleSelector string
}

const yeosuCityUrlPathReplaceStringWithBoardID = "#{board_id}"

func init() {
	supportedCrawlers[g.RssFeedSupportedSiteYeosuCity] = &supportedCrawlerConfig{
		newCrawlerFn: func(rssFeedProviderID string, config *g.ProviderConfig, modelGetter model.ModelGetter) cron.Job {
			site := "여수시 홈페이지"

			rssFeedProvidersAccessor, ok := modelGetter.GetModel().(model.RssFeedProvidersAccessor)
			if ok == false {
				m := fmt.Sprintf("%s Crawler에서 사용할 RSS Feed Providers를 찾을 수 없습니다.", site)

				notifyapi.SendNotifyMessage(m, true)

				log.Panic(m)
			}

			return &yeosuCityCrawler{
				crawler: crawler{
					config: config,

					rssFeedProviderID:        rssFeedProviderID,
					rssFeedProvidersAccessor: rssFeedProvidersAccessor,

					site:            site,
					siteID:          config.ID,
					siteName:        config.Name,
					siteDescription: config.Description,
					siteUrl:         config.Url,

					crawlingMaxPageCount: 10,
				},
			}
		},
	}

	// 게시판 유형별 설정정보를 초기화한다.
	yeosuCityBoardTypes[yeosuCityBoardTypePhotoNews] = &yeosuCityBoardTypeConfig{
		urlPath: fmt.Sprintf("/www/information/mn01/%s/yeosu.go", yeosuCityUrlPathReplaceStringWithBoardID),

		articleSelector: "#content > dl.board_photonews",
	}
	yeosuCityBoardTypes[yeosuCityBoardTypeList1] = &yeosuCityBoardTypeConfig{
		urlPath: fmt.Sprintf("/www/information/mn01/%s/yeosu.go", yeosuCityUrlPathReplaceStringWithBoardID),

		articleSelector: "#board_list_table > tbody > tr",
	}
	yeosuCityBoardTypes[yeosuCityBoardTypeList2] = &yeosuCityBoardTypeConfig{
		urlPath: fmt.Sprintf("/www/information/mn01/%s/yeosu.go", yeosuCityUrlPathReplaceStringWithBoardID),

		articleSelector: "#board_list_table > tbody > tr",
	}
}

type yeosuCityCrawler struct {
	crawler
}

// @@@@@
func (c *yeosuCityCrawler) Run() {
	log.Debugf("%s('%s')의 크롤링 작업을 시작합니다.", c.site, c.siteID)

	articles, newLatestCrawledArticleIDMap, errOccurred, err := c.crawlingArticles()
	if err != nil {
		log.Errorf("%s (error:%s)", errOccurred, err)

		notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", errOccurred, err), true)

		return
	}

	if len(articles) > 0 {
		log.Debugf("%s('%s')의 크롤링 작업 결과로 %d건의 새로운 게시글이 추출되었습니다. 새로운 게시글을 DB에 추가합니다.", c.site, c.siteID, len(articles))

		insertedCnt, err := c.rssFeedProvidersAccessor.InsertArticles(c.rssFeedProviderID, articles)
		if err != nil {
			m := fmt.Sprintf("새로운 게시글을 DB에 추가하는 중에 오류가 발생하여 %s('%s')의 크롤링 작업이 실패하였습니다.", c.site, c.siteID)

			log.Errorf("%s (error:%s)", m, err)

			notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)

			return
		}

		for boardID, articleID := range newLatestCrawledArticleIDMap {
			if err = c.rssFeedProvidersAccessor.UpdateLatestCrawledArticleID(c.rssFeedProviderID, boardID, articleID); err != nil {
				m := fmt.Sprintf("%s('%s')의 크롤링 된 최근 게시글 ID의 DB 반영이 실패하였습니다.", c.site, c.siteID)

				log.Errorf("%s (error:%s)", m, err)

				notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)
			}
		}

		if len(articles) != insertedCnt {
			log.Debugf("%s('%s')의 크롤링 작업을 종료합니다. 전체 %d건 중에서 %d건의 새로운 게시글이 DB에 추가되었습니다.", c.site, c.siteID, len(articles), insertedCnt)
		} else {
			log.Debugf("%s('%s')의 크롤링 작업을 종료합니다. %d건의 새로운 게시글이 DB에 추가되었습니다.", c.site, c.siteID, len(articles))
		}
	} else {
		for boardID, articleID := range newLatestCrawledArticleIDMap {
			if err = c.rssFeedProvidersAccessor.UpdateLatestCrawledArticleID(c.rssFeedProviderID, boardID, articleID); err != nil {
				m := fmt.Sprintf("%s('%s')의 크롤링 된 최근 게시글 ID의 DB 반영이 실패하였습니다.", c.site, c.siteID)

				log.Errorf("%s (error:%s)", m, err)

				notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)
			}
		}

		log.Debugf("%s('%s')의 크롤링 작업을 종료합니다. 새로운 게시글이 존재하지 않습니다.", c.site, c.siteID)
	}
}

//noinspection GoErrorStringFormat,GoUnhandledErrorResult
func (c *yeosuCityCrawler) crawlingArticles() ([]*model.RssFeedProviderArticle, map[string]string, string, error) {
	var articles = make([]*model.RssFeedProviderArticle, 0)
	var newLatestCrawledArticleIDsByBoard = make(map[string]string, 0)

	for _, b := range c.config.Boards {
		boardTypeConfig, exists := yeosuCityBoardTypes[b.Type]
		if exists == false {
			return nil, nil, fmt.Sprintf("%s('%s')의 게시판 Type별 정보를 구하는 중에 오류가 발생하였습니다.", c.site, c.siteID), fmt.Errorf("구현되지 않은 게시판 Type('%s') 입니다.", b.Type)
		}

		latestCrawledArticleID, latestCrawledCreatedDate, err := c.rssFeedProvidersAccessor.LatestCrawledArticleData(c.rssFeedProviderID, b.ID)
		if err != nil {
			return nil, nil, fmt.Sprintf("%s('%s') %s 게시판에 마지막으로 추가된 게시글 자료를 찾는 중에 오류가 발생하였습니다.", c.site, c.siteID, b.Name), err
		}

		var newLatestCrawledArticleID = ""

		//
		// 게시글 크롤링
		//
		for pageNo := 1; pageNo <= c.crawlingMaxPageCount; pageNo++ {
			ysPageUrl := strings.Replace(fmt.Sprintf("%s%s?page=%d", c.siteUrl, boardTypeConfig.urlPath, pageNo), yeosuCityUrlPathReplaceStringWithBoardID, b.ID, -1)

			doc, errOccurred, err := httpWebPageDocument(ysPageUrl, fmt.Sprintf("%s('%s') %s 게시판", c.site, c.siteID, b.Name), nil)
			if err != nil {
				return nil, nil, errOccurred, err
			}

			ysSelection := doc.Find(boardTypeConfig.articleSelector)
			if len(ysSelection.Nodes) == 0 { // 게시글이 0건이라면 CSS 파싱이 실패한것으로 본다.
				return nil, nil, fmt.Sprintf("%s('%s') %s 게시판의 게시글 추출이 실패하였습니다. CSS셀렉터를 확인하세요.", c.site, c.siteID, b.Name), err
			}

			var foundArticleAlreadyCrawled = false
			ysSelection.EachWithBreak(func(i int, s *goquery.Selection) bool {
				var article *model.RssFeedProviderArticle
				if article, err = c.extractArticle(b.Type, s); err != nil {
					return false
				}
				article.BoardID = b.ID
				article.BoardName = b.Name

				// 크롤링 된 게시글 목록 중에서 가장 최근의 게시글 ID를 구한다.
				if newLatestCrawledArticleID == "" {
					newLatestCrawledArticleID = article.ArticleID
				}

				// 이미 크롤링 작업을 했었던 게시글인지 확인한다. 이후의 게시글 추출 작업은 취소된다.
				if article.ArticleID == latestCrawledArticleID {
					foundArticleAlreadyCrawled = true
					return false
				}
				if latestCrawledCreatedDate.IsZero() == false && article.CreatedDate.Before(latestCrawledCreatedDate) == true {
					foundArticleAlreadyCrawled = true
					return false
				}

				articles = append(articles, article)

				return true
			})
			if err != nil {
				return nil, nil, fmt.Sprintf("%s('%s') %s 게시판의 게시글 추출이 실패하였습니다. CSS셀렉터를 확인하세요.", c.site, c.siteID, b.Name), err
			}

			if foundArticleAlreadyCrawled == true {
				break
			}
		}

		if newLatestCrawledArticleID != "" {
			newLatestCrawledArticleIDsByBoard[b.ID] = newLatestCrawledArticleID
		}
	}

	//
	// 게시글 내용 크롤링 : 내용은 크롤링이 실패해도 에러를 발생하지 않고 무시한다.
	//
	crawlingWaiter := &sync.WaitGroup{}
	crawlingRequestC := make(chan *model.RssFeedProviderArticle, len(articles))

	for i := 1; i <= 5; i++ {
		go func(crawlingRequestC <-chan *model.RssFeedProviderArticle, crawlingWaiter *sync.WaitGroup) {
			for article := range crawlingRequestC {
				c.crawlingArticleContent(article, crawlingWaiter)
			}
		}(crawlingRequestC, crawlingWaiter)
	}

	for _, article := range articles {
		if article.Content != "" {
			crawlingWaiter.Add(1)

			crawlingRequestC <- article
		}
	}

	// 채널을 닫는다. 더이상 채널에 데이터를 추가하지는 못하지만 이미 추가한 데이터는 처리가 완료된다.
	close(crawlingRequestC)

	// 크롤링 작업이 모두 완료될 때 까지 대기한다.
	crawlingWaiter.Wait()

	// DB에 오래된 게시글부터 추가되도록 하기 위해 역순으로 재배열한다.
	for i, j := 0, len(articles)-1; i < j; i, j = i+1, j-1 {
		articles[i], articles[j] = articles[j], articles[i]
	}

	return articles, newLatestCrawledArticleIDsByBoard, "", nil
}

//noinspection GoErrorStringFormat
func (c *yeosuCityCrawler) extractArticle(boardType string, s *goquery.Selection) (*model.RssFeedProviderArticle, error) {
	var exists bool
	var article = &model.RssFeedProviderArticle{}

	switch boardType {
	case yeosuCityBoardTypePhotoNews:
		// 제목, 링크
		as := s.Find("dd > a")
		if as.Length() != 1 {
			return nil, errors.New("게시글에서 제목 정보를 찾을 수 없습니다.")
		}
		article.Title = strings.TrimSpace(as.Text())
		article.Link, exists = as.Attr("href")
		if exists == false {
			return nil, errors.New("게시글에서 상세페이지 URL 추출이 실패하였습니다.")
		}
		article.Link = fmt.Sprintf("%s%s", c.siteUrl, article.Link)

		// 분류
		as = s.Find("dd > span.cate")
		if as.Length() != 1 {
			return nil, errors.New("게시글에서 분류 정보를 찾을 수 없습니다.")
		}
		classification := strings.TrimSpace(as.Text())
		if classification != "" {
			article.Title = fmt.Sprintf("[ %s ] %s", classification, article.Title)
		}

		// 게시글 ID
		u, err := url.Parse(article.Link)
		if err != nil {
			return nil, fmt.Errorf("게시글에서 상세페이지 URL 파싱이 실패하였습니다. (error:%s)", err)
		}
		pathTokens := strings.Split(u.Path, "/")
		for i, token := range pathTokens {
			if token == "show" {
				if len(pathTokens) > i+1 {
					article.ArticleID = pathTokens[i+1]
				}
				break
			}
		}
		if article.ArticleID == "" {
			return nil, errors.New("게시글에서 게시글 ID 추출이 실패하였습니다.")
		}

		// 등록자, 등록일
		as = s.Find("dd > span.date")
		if as.Length() != 1 {
			return nil, errors.New("게시글에서 등록자 및 등록일 정보를 찾을 수 없습니다.")
		}
		var complexString = strings.TrimSpace(as.Text())
		if matched, _ := regexp.MatchString("\\(.+ / [0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}\\)", complexString); matched == true {
			// 앞/뒤 괄호를 제거한다.
			complexString = complexString[1 : len(complexString)-1]

			slashPos := strings.LastIndex(complexString, "/")
			spacePos := strings.LastIndex(complexString, " ")

			article.Author = strings.TrimSpace(complexString[:slashPos])

			dateSplitSlice := strings.Split(strings.TrimSpace(complexString[slashPos+1:spacePos]), "-")
			year, _ := strconv.Atoi(dateSplitSlice[0])
			month, _ := strconv.Atoi(dateSplitSlice[1])
			day, _ := strconv.Atoi(dateSplitSlice[2])

			timeSplitSlice := strings.Split(strings.TrimSpace(complexString[spacePos:]), ":")
			hour, _ := strconv.Atoi(timeSplitSlice[0])
			minute, _ := strconv.Atoi(timeSplitSlice[1])

			article.CreatedDate = time.Date(year, time.Month(month), day, hour, minute, 0, 0, time.Local)
		} else {
			return nil, fmt.Errorf("게시글에서 등록자 및 등록일('%s') 파싱이 실패하였습니다.", complexString)
		}

		// 내용
		as = s.Find("dd > span.memo")
		if as.Length() != 1 {
			return nil, errors.New("게시글에서 내용 정보를 찾을 수 없습니다.")
		}
		article.Content = utils.CleanStringByLine(as.Text())

		return article, nil

	case yeosuCityBoardTypeList1, yeosuCityBoardTypeList2:
		// 제목, 링크
		as := s.Find("td.list_title > a")
		if as.Length() != 1 {
			return nil, errors.New("게시글에서 제목 정보를 찾을 수 없습니다.")
		}
		article.Title = strings.TrimSpace(as.Text())
		article.Link, exists = as.Attr("href")
		if exists == false {
			return nil, errors.New("게시글에서 상세페이지 URL 추출이 실패하였습니다.")
		}
		article.Link = fmt.Sprintf("%s%s", c.siteUrl, article.Link)

		if boardType == yeosuCityBoardTypeList2 {
			// 분류
			as = s.Find("td.list_cate")
			if as.Length() != 1 {
				return nil, errors.New("게시글에서 분류 정보를 찾을 수 없습니다.")
			}
			classification := strings.TrimSpace(as.Text())
			if classification != "" {
				article.Title = fmt.Sprintf("[ %s ] %s", classification, article.Title)
			}
		}

		// 게시글 ID
		u, err := url.Parse(article.Link)
		if err != nil {
			return nil, fmt.Errorf("게시글에서 상세페이지 URL 파싱이 실패하였습니다. (error:%s)", err)
		}
		pathTokens := strings.Split(u.Path, "/")
		for i, token := range pathTokens {
			if token == "show" {
				if len(pathTokens) > i+1 {
					article.ArticleID = pathTokens[i+1]
				}
				break
			}
		}
		if article.ArticleID == "" {
			return nil, errors.New("게시글에서 게시글 ID 추출이 실패하였습니다.")
		}

		if boardType == yeosuCityBoardTypeList1 {
			// 등록자
			as = s.Find("td.list_department")
			if as.Length() != 1 {
				return nil, errors.New("게시글에서 등록자 정보를 찾을 수 없습니다.")
			}
			article.Author = strings.TrimSpace(as.Text())
		} else {
			// 담당부서
			as = s.Find("td.list_member_name")
			if as.Length() != 1 {
				return nil, errors.New("게시글에서 담당부서 정보를 찾을 수 없습니다.")
			}
			article.Author = strings.TrimSpace(as.Text())
		}

		// 등록일
		as = s.Find("td.list_reg_date")
		if as.Length() != 1 {
			return nil, errors.New("게시글에서 등록일 정보를 찾을 수 없습니다.")
		}
		var createdDateString = strings.TrimSpace(as.Text())
		if matched, _ := regexp.MatchString("[0-9]{4}-[0-9]{2}-[0-9]{2}", createdDateString); matched == true {
			dateSplitSlice := strings.Split(createdDateString, "-")
			year, _ := strconv.Atoi(dateSplitSlice[0])
			month, _ := strconv.Atoi(dateSplitSlice[1])
			day, _ := strconv.Atoi(dateSplitSlice[2])

			article.CreatedDate = time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
		} else {
			return nil, fmt.Errorf("게시글에서 등록일('%s') 파싱이 실패하였습니다.", createdDateString)
		}

		return article, nil

	default:
		return nil, fmt.Errorf("구현되지 않은 게시판 Type('%s') 입니다.", boardType)
	}
}

//noinspection GoUnhandledErrorResult
func (c *yeosuCityCrawler) crawlingArticleContent(article *model.RssFeedProviderArticle, crawlingWaiter *sync.WaitGroup) {
	defer crawlingWaiter.Done()

	doc, errOccurred, err := httpWebPageDocument(article.Link, fmt.Sprintf("%s('%s') %s 게시판의 게시글('%s') 상세페이지", c.site, c.siteID, article.BoardName, article.ArticleID), nil)
	if err != nil {
		log.Warnf("%s (error:%s)", errOccurred, err)
		return
	}

	ncSelection := doc.Find("div.con_detail")
	if ncSelection.Length() == 0 {
		// 로그인을 하지 않아 접근 권한이 없는 페이지인 경우 오류가 발생하므로 로그 처리를 하지 않는다.
		return
	}

	article.Content = utils.CleanStringByLine(ncSelection.Text())
}
