package crawling

import (
	"errors"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
	"github.com/darkkaiser/rss-feed-server/services/ws/model"
	"github.com/darkkaiser/rss-feed-server/utils"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/html"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/korean"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// 크롤링 할 최대 페이지 수
	crawlingMaxPageCount = 10

	// 크롤링 지연 시간(분)
	// 네이버 검색을 이용하여 카페 게시글을 검색한 후 게시글 내용을 크롤링하는 방법을 이용하는 경우
	// 게시글이 등록되고 나서 일정 시간(그때그때 검색 시스템의 상황에 따라 차이가 존재함)이 경과한 후에
	// 검색이 가능하므로 크롤링 지연 시간을 둔다.
	crawlingDelayTimeMinutes = 40
)

type naverCafeCrawling struct {
	config *g.NaverCafeCrawlingConfig

	model *model.NaverCafe
}

func newNaverCafeCrawling(config *g.NaverCafeCrawlingConfig, model *model.NaverCafe) *naverCafeCrawling {
	return &naverCafeCrawling{
		config: config,

		model: model,
	}
}

func (c *naverCafeCrawling) Run() {
	log.Debugf("네이버 카페('%s') 크롤링 작업을 시작합니다.", c.config.ID)

	articles, newCrawledLatestArticleID, errOccurred, err := c.runArticleCrawling()
	if errOccurred != "" {
		log.Errorf("%s (error:%s)", errOccurred, err)

		notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", errOccurred, err), true)

		return
	}

	if len(articles) > 0 {
		log.Debugf("네이버 카페('%s') 크롤링 작업 결과로 %d건의 새로운 게시글이 추출되었습니다. 새로운 게시글을 DB에 추가합니다.", c.config.ID, len(articles))

		insertedCnt, err := c.model.InsertArticles(c.config.ID, articles)
		if err != nil {
			m := fmt.Sprintf("새로운 게시글을 DB에 추가하는 중에 오류가 발생하여 네이버 카페('%s') 크롤링 작업이 실패하였습니다.", c.config.ID)

			log.Errorf("%s (error:%s)", m, err)

			notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)

			return
		}

		if err = c.model.UpdateCrawledLatestArticleID(c.config.ID, newCrawledLatestArticleID); err != nil {
			m := fmt.Sprintf("네이버 카페('%s') 크롤링 된 최근 게시글 ID의 DB 반영이 실패하였습니다.", c.config.ID)

			log.Errorf("%s (error:%s)", m, err)

			notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)
		}

		if len(articles) != insertedCnt {
			log.Debugf("네이버 카페('%s') 크롤링 작업을 종료합니다. 전체 %d건 중에서 %d건의 새로운 게시글이 DB에 추가되었습니다.", c.config.ID, len(articles), insertedCnt)
		} else {
			log.Debugf("네이버 카페('%s') 크롤링 작업을 종료합니다. %d건의 새로운 게시글이 DB에 추가되었습니다.", c.config.ID, len(articles))
		}
	} else {
		if err = c.model.UpdateCrawledLatestArticleID(c.config.ID, newCrawledLatestArticleID); err != nil {
			m := fmt.Sprintf("네이버 카페('%s') 크롤링 된 최근 게시글 ID의 DB 반영이 실패하였습니다.", c.config.ID)

			log.Errorf("%s (error:%s)", m, err)

			notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)
		}

		log.Debugf("네이버 카페('%s') 크롤링 작업을 종료합니다. 새로운 게시글이 존재하지 않습니다.", c.config.ID)
	}
}

//noinspection GoErrorStringFormat,GoUnhandledErrorResult
func (c *naverCafeCrawling) runArticleCrawling() ([]*model.NaverCafeArticle, int64, string, error) {
	crawledLatestArticleID, err := c.model.CrawledLatestArticleID(c.config.ID)
	if err != nil {
		return nil, 0, fmt.Sprintf("네이버 카페('%s')에 마지막으로 추가된 게시글 ID를 찾는 중에 오류가 발생하였습니다.", c.config.ID), err
	}

	articles := make([]*model.NaverCafeArticle, 0)
	newCrawledLatestArticleID := crawledLatestArticleID

	crawlingDelayStartTime := time.Now().Add(time.Duration(-1*crawlingDelayTimeMinutes) * time.Minute)

	//
	// 게시글 크롤링
	//
	euckrDecoder := korean.EUCKR.NewDecoder()
	for pageNo := 1; pageNo <= crawlingMaxPageCount; pageNo++ {
		ncPageUrl := fmt.Sprintf("%s/ArticleList.nhn?search.clubid=%s&userDisplay=50&search.boardtype=L&search.totalCount=501&search.page=%d", model.NaverCafeUrl(c.config.ID), c.config.ClubID, pageNo)

		res, err := http.Get(ncPageUrl)
		if err != nil {
			return nil, 0, fmt.Sprintf("네이버 카페('%s') 페이지 접근이 실패하였습니다.", c.config.ID), err
		}
		if res.StatusCode != http.StatusOK {
			return nil, 0, fmt.Sprintf("네이버 카페('%s') 페이지 접근이 실패하였습니다.", c.config.ID), fmt.Errorf("HTTP Response StatusCode:%d", res.StatusCode)
		}

		bodyBytes, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return nil, 0, fmt.Sprintf("네이버 카페('%s') 페이지의 내용을 읽을 수 없습니다.", c.config.ID), err
		}
		res.Body.Close()

		bodyString, err := euckrDecoder.String(string(bodyBytes))
		if err != nil {
			return nil, 0, fmt.Sprintf("네이버 카페('%s') 페이지의 문자열 변환(EUC-KR to UTF-8)이 실패하였습니다.", c.config.ID), err
		}

		root, err := html.Parse(strings.NewReader(bodyString))
		if err != nil {
			return nil, 0, fmt.Sprintf("네이버 카페('%s') 페이지의 HTML 파싱이 실패하였습니다.", c.config.ID), err
		}

		doc := goquery.NewDocumentFromNode(root)
		ncSelection := doc.Find("div.article-board > table > tbody > tr:not(.board-notice)")
		if len(ncSelection.Nodes) == 0 { // 전체글보기의 게시글이 0건이라면 CSS 파싱이 실패한것으로 본다.
			return nil, 0, fmt.Sprintf("네이버 카페('%s')의 게시글 추출이 실패하였습니다. CSS셀렉터를 확인하세요.", c.config.ID), err
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
			var createdAt time.Time
			var createdAtString = strings.TrimSpace(as.Text())
			if matched, _ := regexp.MatchString("[0-9]{2}:[0-9]{2}", createdAtString); matched == true {
				s := strings.Split(createdAtString, ":")
				hour, _ := strconv.Atoi(s[0])
				minute, _ := strconv.Atoi(s[1])

				var now = time.Now()
				createdAt = time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.Local)
			} else if matched, _ := regexp.MatchString("[0-9]{4}.[0-9]{2}.[0-9]{2}.", createdAtString); matched == true {
				s := strings.Split(createdAtString, ".")
				year, _ := strconv.Atoi(s[0])
				month, _ := strconv.Atoi(s[1])
				day, _ := strconv.Atoi(s[2])

				createdAt = time.Date(year, time.Month(month), day, 23, 59, 59, 0, time.Local)
			} else {
				err = fmt.Errorf("게시글에서 작성일('%s') 파싱이 실패하였습니다.", createdAtString)
				return false
			}
			// 크롤링 대기 시간을 경과한 게시글인지 확인한다.
			// 아직 경과하지 않은 게시글이라면 크롤링 하지 않는다.
			if createdAt.After(crawlingDelayStartTime) == true {
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

			// 작성자
			as = s.Find("td.td_name > div.pers_nick_area td.p-nick")
			if as.Length() != 1 {
				err = errors.New("게시글에서 작성자 정보를 찾을 수 없습니다.")
				return false
			}
			author := strings.TrimSpace(as.Text())

			articles = append(articles, &model.NaverCafeArticle{
				BoardID:   boardID,
				BoardName: boardName,
				ArticleID: articleID,
				Title:     title,
				Content:   "",
				Link:      fmt.Sprintf("%s/ArticleRead.nhn?articleid=%d&clubid=%s", model.NaverCafeUrl(c.config.ID), articleID, c.config.ClubID),
				Author:    author,
				CreatedAt: createdAt,
			})

			return true
		})
		if err != nil {
			return nil, 0, fmt.Sprintf("네이버 카페('%s')의 게시글 추출이 실패하였습니다. CSS셀렉터를 확인하세요.", c.config.ID), err
		}

		if foundArticleAlreadyCrawled == true {
			break
		}
	}

	//
	// 게시글 내용 크롤링 : 내용은 크롤링이 실패해도 에러를 발생하지 않고 무시한다.
	//
	crawlingWaiter := &sync.WaitGroup{}
	crawlingRequestC := make(chan *model.NaverCafeArticle, len(articles))

	for i := 1; i <= 5; i++ {
		go func(crawlingRequestC <-chan *model.NaverCafeArticle, crawlingWaiter *sync.WaitGroup) {
			euckrDecoder := korean.EUCKR.NewDecoder()

			for article := range crawlingRequestC {
				c.runArticleContentCrawling(article, euckrDecoder, crawlingWaiter)
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

	return articles, newCrawledLatestArticleID, "", nil
}

//noinspection GoUnhandledErrorResult
func (c *naverCafeCrawling) runArticleContentCrawling(article *model.NaverCafeArticle, euckrDecoder *encoding.Decoder, crawlingWaiter *sync.WaitGroup) {
	defer crawlingWaiter.Done()

	c.runArticleContentCrawlingUsingNaverSearch(article)
	if article.Content == "" {
		c.runArticleContentCrawlingUsingLink(article, euckrDecoder)
	}
}

//noinspection GoUnhandledErrorResult
func (c *naverCafeCrawling) runArticleContentCrawlingUsingLink(article *model.NaverCafeArticle, euckrDecoder *encoding.Decoder) {
	res, err := http.Get(article.Link)
	if err != nil {
		log.Warnf("네이버 카페('%s > %s') 게시글(%d)의 상세페이지 접근이 실패하였습니다. (error:%s)", c.config.ID, article.BoardName, article.ArticleID, err)
		return
	}
	if res.StatusCode != http.StatusOK {
		log.Warnf("네이버 카페('%s > %s') 게시글(%d)의 상세페이지 접근이 실패하였습니다. (HTTP Response StatusCode:%d)", c.config.ID, article.BoardName, article.ArticleID, res.StatusCode)
		return
	}

	bodyBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.Warnf("네이버 카페('%s > %s') 게시글(%d)의 상세페이지 내용을 읽을 수 없습니다. (error:%s)", c.config.ID, article.BoardName, article.ArticleID, err)
		return
	}
	res.Body.Close()

	bodyString, err := euckrDecoder.String(string(bodyBytes))
	if err != nil {
		log.Warnf("네이버 카페('%s > %s') 게시글(%d)의 상세페이지 문자열 변환(EUC-KR to UTF-8)이 실패하였습니다. (error:%s)", c.config.ID, article.BoardName, article.ArticleID, err)
		return
	}

	root, err := html.Parse(strings.NewReader(bodyString))
	if err != nil {
		log.Warnf("네이버 카페('%s > %s') 게시글(%d)의 상세페이지 HTML 파싱이 실패하였습니다. (error:%s)", c.config.ID, article.BoardName, article.ArticleID, err)
		return
	}

	doc := goquery.NewDocumentFromNode(root)
	ncSelection := doc.Find("#tbody")
	if ncSelection.Length() == 0 {
		// 로그인을 하지 않아 접근 권한이 없는 페이지인 경우 오류가 발생하므로 로그 처리를 하지 않는다.
		return
	}

	article.Content = utils.CleanString(ncSelection.Text())
}

//noinspection GoUnhandledErrorResult
func (c *naverCafeCrawling) runArticleContentCrawlingUsingNaverSearch(article *model.NaverCafeArticle) {
	searchUrl := fmt.Sprintf("https://search.naver.com/search.naver?where=article&query=%s&ie=utf8&st=date&date_option=0&date_from=&date_to=&board=&srchby=title&dup_remove=0&cafe_url=%s&without_cafe_url=&sm=tab_opt&nso=so:dd,p:all,a:t&t=0&mson=0&prdtype=0", url.QueryEscape(article.Title), c.config.ID)

	res, err := http.Get(searchUrl)
	if err != nil {
		log.Warnf("네이버 카페('%s > %s') 게시글(%d)의 네이버 검색페이지 접근이 실패하였습니다. (error:%s)", c.config.ID, article.BoardName, article.ArticleID, err)
		return
	}
	if res.StatusCode != http.StatusOK {
		log.Warnf("네이버 카페('%s > %s') 게시글(%d)의 네이버 검색페이지 접근이 실패하였습니다. (HTTP Response StatusCode:%d)", c.config.ID, article.BoardName, article.ArticleID, res.StatusCode)
		return
	}

	bodyBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.Warnf("네이버 카페('%s > %s') 게시글(%d)의 네이버 검색페이지 내용을 읽을 수 없습니다. (error:%s)", c.config.ID, article.BoardName, article.ArticleID, err)
		return
	}
	res.Body.Close()

	root, err := html.Parse(strings.NewReader(string(bodyBytes)))
	if err != nil {
		log.Warnf("네이버 카페('%s > %s') 게시글(%d)의 네이버 검색페이지 HTML 파싱이 실패하였습니다. (error:%s)", c.config.ID, article.BoardName, article.ArticleID, err)
		return
	}

	doc := goquery.NewDocumentFromNode(root)
	ncSelection := doc.Find(fmt.Sprintf("a.total_dsc[href='%s/%d']", model.NaverCafeUrl(c.config.ID), article.ArticleID))
	if ncSelection.Length() == 1 {
		article.Content = utils.CleanString(ncSelection.Text())
	}
}
