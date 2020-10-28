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
	"golang.org/x/text/encoding/korean"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	naverCafeCrawlingBoardTypeList string = "L"

	// 크롤링 할 최대 페이지 수
	crawlingMaxPageCount = 10
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

	articles, errDescription, err := c.runArticleCrawling()
	if errDescription != "" {
		log.Errorf("%s (error:%s)", errDescription, err)

		notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", errDescription, err), true)

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

		if len(articles) != insertedCnt {
			log.Debugf("네이버 카페('%s') 크롤링 작업을 종료합니다. 전체 %d건 중에서 %d건의 새로운 게시글이 DB에 추가되었습니다.", c.config.ID, len(articles), insertedCnt)
		} else {
			log.Debugf("네이버 카페('%s') 크롤링 작업을 종료합니다. %d건의 새로운 게시글이 DB에 추가되었습니다.", c.config.ID, len(articles))
		}
	} else {
		log.Debugf("네이버 카페('%s') 크롤링 작업을 종료합니다. 새로운 게시글이 존재하지 않습니다.", c.config.ID)
	}
}

//noinspection GoErrorStringFormat,GoUnhandledErrorResult
func (c *naverCafeCrawling) runArticleCrawling() ([]*model.NaverCafeArticle, string, error) {
	latestArticleID, err := c.model.GetLatestArticleID(c.config.ID)
	if err != nil {
		return nil, fmt.Sprintf("네이버 카페('%s')에 마지막으로 추가된 게시글 ID를 찾는 중에 오류가 발생하였습니다.", c.config.ID), err
	}

	articles := make([]*model.NaverCafeArticle, 0)

	//
	// 게시글 크롤링
	//
	euckrDecoder := korean.EUCKR.NewDecoder()
	for pageNo := 1; pageNo <= crawlingMaxPageCount; pageNo++ {
		ncPageUrl := fmt.Sprintf("%s/%s/ArticleList.nhn?search.clubid=%s&userDisplay=50&search.boardtype=L&search.totalCount=501&search.page=%d", model.NaverCafeHomeUrl, c.config.ID, c.config.ClubID, pageNo)

		res, err := http.Get(ncPageUrl)
		if err != nil {
			return nil, fmt.Sprintf("네이버 카페('%s') 페이지 접근이 실패하였습니다.", c.config.ID), err
		}
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Sprintf("네이버 카페('%s') 페이지 접근이 실패하였습니다.", c.config.ID), fmt.Errorf("HTTP Response StatusCode:%d", res.StatusCode)
		}

		bodyBytes, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return nil, fmt.Sprintf("네이버 카페('%s') 페이지의 내용을 읽을 수 없습니다.", c.config.ID), err
		}
		res.Body.Close()

		bodyString, err := euckrDecoder.String(string(bodyBytes))
		if err != nil {
			return nil, fmt.Sprintf("네이버 카페('%s') 페이지의 문자열 변환(EUC-KR to UTF-8)이 실패하였습니다.", c.config.ID), err
		}

		root, err := html.Parse(strings.NewReader(bodyString))
		if err != nil {
			return nil, fmt.Sprintf("네이버 카페('%s') 페이지의 HTML 파싱이 실패하였습니다.", c.config.ID), err
		}

		doc := goquery.NewDocumentFromNode(root)
		ncSelection := doc.Find("div.article-board > table > tbody > tr:not(.board-notice)")
		if len(ncSelection.Nodes) == 0 { // 전체글보기의 게시글이 0건이라면 CSS 파싱이 실패한것으로 본다.
			return nil, fmt.Sprintf("네이버 카페('%s')의 게시글 추출이 실패하였습니다. CSS셀렉터를 확인하세요.", c.config.ID), err
		}

		var foundArticleAlreadyAddedToDB = false
		ncSelection.EachWithBreak(func(i int, s *goquery.Selection) bool {
			// 게시판
			as := s.Find("td.td_article > div.board-name a.link_name")
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
			if c.config.ContainsBoard(boardID) == false {
				return true
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

			// 이미 DB에 추가되어 있는 게시글인지 확인한다. 이후의 게시글 추출 작업은 취소된다.
			if articleID <= latestArticleID {
				foundArticleAlreadyAddedToDB = true
				return false
			}

			// 작성자
			as = s.Find("td.td_name > div.pers_nick_area td.p-nick")
			if as.Length() != 1 {
				err = errors.New("게시글에서 작성자 정보를 찾을 수 없습니다.")
				return false
			}
			author := strings.TrimSpace(as.Text())

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

			articles = append(articles, &model.NaverCafeArticle{
				BoardID:   boardID,
				BoardName: boardName,
				ArticleID: articleID,
				Title:     title,
				Content:   "",
				Link:      fmt.Sprintf("%s/%s/ArticleRead.nhn?articleid=%d&clubid=%s", model.NaverCafeHomeUrl, c.config.ID, articleID, c.config.ClubID),
				Author:    author,
				CreatedAt: createdAt,
			})

			return true
		})
		if err != nil {
			return nil, fmt.Sprintf("네이버 카페('%s')의 게시글 추출이 실패하였습니다. CSS셀렉터를 확인하세요.", c.config.ID), err
		}

		if foundArticleAlreadyAddedToDB == true {
			break
		}
	}

	//
	// 게시글 내용 크롤링 : 내용은 크롤링이 실패해도 에러를 발생하지 않고 무시한다.
	// 로그아웃 상태에서 게시판의 내용이 접근 가능한 게시판이더라도 특정 게시글은 로그인 페이지가 나타나는 현상이 있음!!!
	//
	for _, article := range articles {
		if c.config.ContentCanBeReadBoard(article.BoardID) == true {
			res, err := http.Get(article.Link)
			if err != nil {
				log.Warnf("네이버 카페('%s > %s') 게시글(%d)의 상세페이지 접근이 실패하였습니다. (error:%s)", c.config.ID, article.BoardName, article.ArticleID, err)
				continue
			}
			if res.StatusCode != http.StatusOK {
				log.Warnf("네이버 카페('%s > %s') 게시글(%d)의 상세페이지 접근이 실패하였습니다. (HTTP Response StatusCode:%d)", c.config.ID, article.BoardName, article.ArticleID, res.StatusCode)
				continue
			}

			bodyBytes, err := ioutil.ReadAll(res.Body)
			if err != nil {
				log.Warnf("네이버 카페('%s > %s') 게시글(%d)의 상세페이지 내용을 읽을 수 없습니다. (error:%s)", c.config.ID, article.BoardName, article.ArticleID, err)
				continue
			}
			res.Body.Close()

			bodyString, err := euckrDecoder.String(string(bodyBytes))
			if err != nil {
				log.Warnf("네이버 카페('%s > %s') 게시글(%d) 상세페이지의 문자열 변환(EUC-KR to UTF-8)이 실패하였습니다. (error:%s)", c.config.ID, article.BoardName, article.ArticleID, err)
				continue
			}

			root, err := html.Parse(strings.NewReader(bodyString))
			if err != nil {
				log.Warnf("네이버 카페('%s > %s') 게시글(%d) 상세페이지의 HTML 파싱이 실패하였습니다. (error:%s)", c.config.ID, article.BoardName, article.ArticleID, err)
				continue
			}

			doc := goquery.NewDocumentFromNode(root)
			ncSelection := doc.Find("#tbody div.se-viewer > div.se-main-container")
			if len(ncSelection.Nodes) == 0 {
				log.Warnf("네이버 카페('%s > %s') 게시글(%d)의 상세페이지 내용 추출이 실패하였습니다.", c.config.ID, article.BoardName, article.ArticleID)
				continue
			}

			article.Content = utils.CleanString(ncSelection.Text())
		}
	}

	return articles, "", nil
}
