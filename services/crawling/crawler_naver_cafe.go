package crawling

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
	"github.com/darkkaiser/rss-feed-server/services/ws/model"
	"github.com/darkkaiser/rss-feed-server/utils"
	"github.com/robfig/cron"
	log "github.com/sirupsen/logrus"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/korean"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

func init() {
	supportedCrawlers[g.RssFeedSupportedSiteNaverCafe] = &supportedCrawlerConfig{
		newCrawlerFn: func(rssFeedProviderID string, config *g.ProviderConfig, modelGetter model.ModelGetter) cron.Job {
			site := "네이버 카페"

			rssFeedProvidersAccessor, ok := modelGetter.GetModel().(model.RssFeedProvidersAccessor)
			if ok == false {
				m := fmt.Sprintf("%s Crawler에서 사용할 RSS Feed Providers를 찾을 수 없습니다.", site)

				notifyapi.SendNotifyMessage(m, true)

				log.Panic(m)
			}

			data := naverCafeCrawlerConfigData{}
			if err := data.fillFromMap(config.Data); err != nil {
				m := fmt.Sprintf("작업 데이터가 유효하지 않아 %s('%s') 크롤링 객체 생성이 실패하였습니다. (error:%s)", site, config.ID, err)

				notifyapi.SendNotifyMessage(m, true)

				log.Panic(m)
			}

			return &naverCafeCrawler{
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

				siteClubID: data.ClubID,

				crawlingDelayTimeMinutes: 40,
			}
		},
	}
}

type naverCafeCrawlerConfigData struct {
	ClubID string `json:"club_id"`
}

func (d *naverCafeCrawlerConfigData) fillFromMap(m map[string]interface{}) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, d); err != nil {
		return err
	}
	return nil
}

type naverCafeCrawler struct {
	crawler

	siteClubID string

	// 크롤링 지연 시간(분)
	// 네이버 검색을 이용하여 카페 게시글을 검색한 후 게시글 내용을 크롤링하는 방법을 이용하는 경우
	// 게시글이 등록되고 나서 일정 시간(그때그때 검색 시스템의 상황에 따라 차이가 존재함)이 경과한 후에
	// 검색이 가능하므로 크롤링 지연 시간을 둔다.
	crawlingDelayTimeMinutes int
}

func (c *naverCafeCrawler) Run() {
	log.Debugf("%s('%s')의 크롤링 작업을 시작합니다.", c.site, c.siteID)

	articles, newCrawledLatestArticleID, errOccurred, err := c.crawlingArticles()
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

		if err = c.rssFeedProvidersAccessor.UpdateCrawledLatestArticleID(c.rssFeedProviderID, "", newCrawledLatestArticleID); err != nil {
			m := fmt.Sprintf("%s('%s')의 크롤링 된 최근 게시글 ID의 DB 반영이 실패하였습니다.", c.site, c.siteID)

			log.Errorf("%s (error:%s)", m, err)

			notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)
		}

		if len(articles) != insertedCnt {
			log.Debugf("%s('%s')의 크롤링 작업을 종료합니다. 전체 %d건 중에서 %d건의 새로운 게시글이 DB에 추가되었습니다.", c.site, c.siteID, len(articles), insertedCnt)
		} else {
			log.Debugf("%s('%s')의 크롤링 작업을 종료합니다. %d건의 새로운 게시글이 DB에 추가되었습니다.", c.site, c.siteID, len(articles))
		}
	} else {
		if err = c.rssFeedProvidersAccessor.UpdateCrawledLatestArticleID(c.rssFeedProviderID, "", newCrawledLatestArticleID); err != nil {
			m := fmt.Sprintf("%s('%s')의 크롤링 된 최근 게시글 ID의 DB 반영이 실패하였습니다.", c.site, c.siteID)

			log.Errorf("%s (error:%s)", m, err)

			notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)
		}

		log.Debugf("%s('%s')의 크롤링 작업을 종료합니다. 새로운 게시글이 존재하지 않습니다.", c.site, c.siteID)
	}
}

//noinspection GoErrorStringFormat,GoUnhandledErrorResult
func (c *naverCafeCrawler) crawlingArticles() ([]*model.RssFeedProviderArticle, string, string, error) {
	idString, crawledLatestCreatedDate, err := c.rssFeedProvidersAccessor.CrawledLatestArticleData(c.rssFeedProviderID, "")
	if err != nil {
		return nil, "", fmt.Sprintf("%s('%s')에 마지막으로 추가된 게시글 자료를 찾는 중에 오류가 발생하였습니다.", c.site, c.siteID), err
	}
	var crawledLatestArticleID int64 = 0
	if idString != "" {
		crawledLatestArticleID, err = strconv.ParseInt(idString, 10, 64)
		if err != nil {
			return nil, "", fmt.Sprintf("%s('%s')에 마지막으로 추가된 게시글 ID를 숫자로 변환하는 중에 오류가 발생하였습니다.", c.site, c.siteID), err
		}
	}

	articles := make([]*model.RssFeedProviderArticle, 0)
	newCrawledLatestArticleID := crawledLatestArticleID
	crawlingDelayStartTime := time.Now().Add(time.Duration(-1*c.crawlingDelayTimeMinutes) * time.Minute)

	//
	// 게시글 크롤링
	//
	euckrDecoder := korean.EUCKR.NewDecoder()
	for pageNo := 1; pageNo <= c.crawlingMaxPageCount; pageNo++ {
		ncPageUrl := fmt.Sprintf("%s/ArticleList.nhn?search.clubid=%s&userDisplay=50&search.boardtype=L&search.totalCount=501&search.page=%d", c.siteUrl, c.siteClubID, pageNo)

		doc, errOccurred, err := httpWebPageDocument(ncPageUrl, fmt.Sprintf("%s('%s') 페이지", c.site, c.siteID), euckrDecoder)
		if err != nil {
			return nil, "", errOccurred, err
		}

		ncSelection := doc.Find("div.article-board > table > tbody > tr:not(.board-notice)")
		if len(ncSelection.Nodes) == 0 { // 전체글보기의 게시글이 0건이라면 CSS 파싱이 실패한것으로 본다.
			return nil, "", fmt.Sprintf("%s('%s')의 게시글 추출이 실패하였습니다. CSS셀렉터를 확인하세요.", c.site, c.siteID), err
		}

		var foundArticleAlreadyCrawled = false
		ncSelection.EachWithBreak(func(i int, s *goquery.Selection) bool {
			// 게시글의 답글을 표시하는 행인지 확인한다.
			// 게시글 제목 오른쪽에 답글이라는 링크가 있으며 이 링크를 클릭하면 아래쪽에 등록된 답글이 나타난다.
			// 이 때 사용할 목적으로 답글이 있는 게시물 아래에 보이지 않는 <TR> 태그가 하나 있다.
			as := s.Find("td")
			if as.Length() == 1 {
				for _, attr := range as.Nodes[0].Attr {
					if attr.Key == "id" && strings.HasPrefix(attr.Val, "reply_") == true {
						return true
					}
				}
			}

			// 작성일
			as = s.Find("td.td_date")
			if as.Length() != 1 {
				err = errors.New("게시글에서 작성일 정보를 찾을 수 없습니다.")
				return false
			}
			var createdDate time.Time
			var createdDateString = strings.TrimSpace(as.Text())
			if matched, _ := regexp.MatchString("[0-9]{2}:[0-9]{2}", createdDateString); matched == true {
				s := strings.Split(createdDateString, ":")
				hour, _ := strconv.Atoi(s[0])
				minute, _ := strconv.Atoi(s[1])

				var now = time.Now()
				createdDate = time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.Local)
			} else if matched, _ := regexp.MatchString("[0-9]{4}.[0-9]{2}.[0-9]{2}.", createdDateString); matched == true {
				s := strings.Split(createdDateString, ".")
				year, _ := strconv.Atoi(s[0])
				month, _ := strconv.Atoi(s[1])
				day, _ := strconv.Atoi(s[2])

				createdDate = time.Date(year, time.Month(month), day, 23, 59, 59, 0, time.Local)
			} else {
				err = fmt.Errorf("게시글에서 작성일('%s') 파싱이 실패하였습니다.", createdDateString)
				return false
			}
			// 크롤링 대기 시간을 경과한 게시글인지 확인한다.
			// 아직 경과하지 않은 게시글이라면 크롤링 하지 않는다.
			if createdDate.After(crawlingDelayStartTime) == true {
				return true
			}

			// 게시판 ID, 이름
			as = s.Find("td.td_article > div.board-name a.link_name")
			if as.Length() != 1 {
				err = errors.New("게시글에서 게시판 정보를 찾을 수 없습니다.")
				return false
			}
			boardUrl, exists := as.Attr("href")
			if exists == false {
				err = errors.New("게시글에서 게시판 URL 추출이 실패하였습니다.")
				return false
			}
			u, err := url.Parse(boardUrl)
			if err != nil {
				err = fmt.Errorf("게시글에서 게시판 URL 파싱이 실패하였습니다. (error:%s)", err)
				return false
			}
			q, err := url.ParseQuery(u.RawQuery)
			if err != nil {
				err = fmt.Errorf("게시글에서 게시판 URL 파싱이 실패하였습니다. (error:%s)", err)
				return false
			}
			boardID := strings.TrimSpace(q.Get("search.menuid"))
			if boardID == "" {
				err = errors.New("게시글에서 게시판 ID 추출이 실패하였습니다.")
				return false
			}
			boardName := strings.TrimSpace(as.Text())

			// 제목, 링크
			as = s.Find("td.td_article > div.board-list a.article")
			if as.Length() != 1 {
				err = errors.New("게시글에서 제목 정보를 찾을 수 없습니다.")
				return false
			}
			title := strings.TrimSpace(as.Text())
			link, exists := as.Attr("href")
			if exists == false {
				err = errors.New("게시글에서 상세페이지 URL 추출이 실패하였습니다.")
				return false
			}

			// 게시글 ID
			u, err = url.Parse(link)
			if err != nil {
				err = fmt.Errorf("게시글에서 상세페이지 URL 파싱이 실패하였습니다. (error:%s)", err)
				return false
			}
			q, err = url.ParseQuery(u.RawQuery)
			if err != nil {
				err = fmt.Errorf("게시글에서 상세페이지 URL 파싱이 실패하였습니다. (error:%s)", err)
				return false
			}
			articleID, err := strconv.ParseInt(q.Get("articleid"), 10, 64)
			if err != nil {
				err = fmt.Errorf("게시글에서 게시글 ID 추출이 실패하였습니다. (error:%s)", err)
				return false
			}

			// 크롤링 된 게시글 목록 중에서 가장 최근의 게시글 ID를 구한다.
			if newCrawledLatestArticleID < articleID {
				newCrawledLatestArticleID = articleID
			}

			// 추출해야 할 게시판인지 확인한다.
			if c.config.ContainsBoard(boardID) == false {
				return true
			}

			// 이미 크롤링 작업을 했었던 게시글인지 확인한다. 이후의 게시글 추출 작업은 취소된다.
			if articleID <= crawledLatestArticleID {
				foundArticleAlreadyCrawled = true
				return false
			}
			if crawledLatestCreatedDate.IsZero() == false && createdDate.Before(crawledLatestCreatedDate) == true {
				foundArticleAlreadyCrawled = true
				return false
			}

			// 작성자
			as = s.Find("td.td_name > div.pers_nick_area td.p-nick")
			if as.Length() != 1 {
				err = errors.New("게시글에서 작성자 정보를 찾을 수 없습니다.")
				return false
			}
			author := strings.TrimSpace(as.Text())

			articles = append(articles, &model.RssFeedProviderArticle{
				BoardID:     boardID,
				BoardName:   boardName,
				ArticleID:   strconv.FormatInt(articleID, 10),
				Title:       title,
				Content:     "",
				Link:        fmt.Sprintf("%s/ArticleRead.nhn?articleid=%d&clubid=%s", c.siteUrl, articleID, c.siteClubID),
				Author:      author,
				CreatedDate: createdDate,
			})

			return true
		})
		if err != nil {
			return nil, "", fmt.Sprintf("%s('%s')의 게시글 추출이 실패하였습니다. CSS셀렉터를 확인하세요.", c.site, c.siteID), err
		}

		if foundArticleAlreadyCrawled == true {
			break
		}
	}

	//
	// 게시글 내용 크롤링 : 내용은 크롤링이 실패해도 에러를 발생하지 않고 무시한다.
	//
	crawlingWaiter := &sync.WaitGroup{}
	crawlingRequestC := make(chan *model.RssFeedProviderArticle, len(articles))

	for i := 1; i <= 5; i++ {
		go func(crawlingRequestC <-chan *model.RssFeedProviderArticle, crawlingWaiter *sync.WaitGroup) {
			euckrDecoder := korean.EUCKR.NewDecoder()

			for article := range crawlingRequestC {
				c.crawlingArticleContent(article, euckrDecoder, crawlingWaiter)
			}
		}(crawlingRequestC, crawlingWaiter)
	}

	crawlingWaiter.Add(len(articles))
	for _, article := range articles {
		crawlingRequestC <- article
	}

	// 채널을 닫는다. 더이상 채널에 데이터를 추가하지는 못하지만 이미 추가한 데이터는 처리가 완료된다.
	close(crawlingRequestC)

	// 크롤링 작업이 모두 완료될 때 까지 대기한다.
	crawlingWaiter.Wait()

	// DB에 오래된 게시글부터 추가되도록 하기 위해 역순으로 재배열한다.
	for i, j := 0, len(articles)-1; i < j; i, j = i+1, j-1 {
		articles[i], articles[j] = articles[j], articles[i]
	}

	return articles, strconv.FormatInt(newCrawledLatestArticleID, 10), "", nil
}

//noinspection GoUnhandledErrorResult
func (c *naverCafeCrawler) crawlingArticleContent(article *model.RssFeedProviderArticle, euckrDecoder *encoding.Decoder, crawlingWaiter *sync.WaitGroup) {
	defer crawlingWaiter.Done()

	c.crawlingArticleContentUsingNaverSearch(article)
	if article.Content == "" {
		c.crawlingArticleContentUsingLink(article, euckrDecoder)
	}
}

//noinspection GoUnhandledErrorResult
func (c *naverCafeCrawler) crawlingArticleContentUsingLink(article *model.RssFeedProviderArticle, euckrDecoder *encoding.Decoder) {
	doc, errOccurred, err := httpWebPageDocument(article.Link, fmt.Sprintf("%s('%s > %s') 게시글('%s')의 상세페이지", c.site, c.siteID, article.BoardName, article.ArticleID), euckrDecoder)
	if err != nil {
		log.Warnf("%s (error:%s)", errOccurred, err)
		return
	}

	ncSelection := doc.Find("#tbody")
	if ncSelection.Length() == 0 {
		// 로그인을 하지 않아 접근 권한이 없는 페이지인 경우 오류가 발생하므로 로그 처리를 하지 않는다.
		return
	}

	article.Content = utils.CleanStringByLine(ncSelection.Text())
}

//noinspection GoUnhandledErrorResult
func (c *naverCafeCrawler) crawlingArticleContentUsingNaverSearch(article *model.RssFeedProviderArticle) {
	searchUrl := fmt.Sprintf("https://search.naver.com/search.naver?where=article&query=%s&ie=utf8&st=date&date_option=0&date_from=&date_to=&board=&srchby=title&dup_remove=0&cafe_url=%s&without_cafe_url=&sm=tab_opt&nso=so:dd,p:all,a:t&t=0&mson=0&prdtype=0", url.QueryEscape(article.Title), c.siteID)

	doc, errOccurred, err := httpWebPageDocument(searchUrl, fmt.Sprintf("%s('%s > %s') 게시글('%s')의 네이버 검색페이지", c.site, c.siteID, article.BoardName, article.ArticleID), nil)
	if err != nil {
		log.Warnf("%s (error:%s)", errOccurred, err)
		return
	}

	ncSelection := doc.Find(fmt.Sprintf("a.total_dsc[href='%s/%s']", c.siteUrl, article.ArticleID))
	if ncSelection.Length() == 1 {
		article.Content = utils.CleanStringByLine(ncSelection.Text())
	}
}
