package crawling

import (
	"errors"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
	"github.com/darkkaiser/rss-feed-server/services/ws/model"
	"github.com/darkkaiser/rss-feed-server/utils"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	// 포토뉴스
	yeosuCityCrawlerBoardTypePhotoNews string = "P"

	// 리스트 1(번호, 제목, 등록자, 등록일, 조회)
	yeosuCityCrawlerBoardTypeList1 string = "L_1"

	// 리스트 2(번호, 분류, 제목, 담당부서, 등록일, 조회)
	yeosuCityCrawlerBoardTypeList2 string = "L_2"
)

var yeosuCityCrawlerBoardTypes map[string]*yeosuCityCrawlerBoardTypeConfig

type yeosuCityCrawlerBoardTypeConfig struct {
	urlPath         string
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

			crawler := &yeosuCityCrawler{
				crawler: crawler{
					config: config,

					rssFeedProviderID:        rssFeedProviderID,
					rssFeedProvidersAccessor: rssFeedProvidersAccessor,

					site:            site,
					siteID:          config.ID,
					siteName:        config.Name,
					siteDescription: config.Description,
					siteUrl:         config.Url,

					crawlingMaxPageCount: 3,
				},
			}

			crawler.crawlingArticlesFn = crawler.crawlingArticles

			return crawler
		},
	}

	// 게시판 유형별 설정정보를 초기화한다.
	yeosuCityCrawlerBoardTypes = map[string]*yeosuCityCrawlerBoardTypeConfig{
		yeosuCityCrawlerBoardTypePhotoNews: {
			urlPath:         fmt.Sprintf("/www/information/mn01/%s/yeosu.go", yeosuCityUrlPathReplaceStringWithBoardID),
			articleSelector: "#content > dl.board_photonews",
		},
		yeosuCityCrawlerBoardTypeList1: {
			urlPath:         fmt.Sprintf("/www/information/mn01/%s/yeosu.go", yeosuCityUrlPathReplaceStringWithBoardID),
			articleSelector: "#board_list_table > tbody > tr",
		},
		yeosuCityCrawlerBoardTypeList2: {
			urlPath:         fmt.Sprintf("/www/information/mn01/%s/yeosu.go", yeosuCityUrlPathReplaceStringWithBoardID),
			articleSelector: "#board_list_table > tbody > tr",
		},
	}
}

type yeosuCityCrawler struct {
	crawler
}

//noinspection GoErrorStringFormat,GoUnhandledErrorResult
func (c *yeosuCityCrawler) crawlingArticles() ([]*model.RssFeedProviderArticle, map[string]string, string, error) {
	var articles = make([]*model.RssFeedProviderArticle, 0)
	var newLatestCrawledArticleIDsByBoard = make(map[string]string, 0)

	for _, b := range c.config.Boards {
		boardTypeConfig, exists := yeosuCityCrawlerBoardTypes[b.Type]
		if exists == false {
			return nil, nil, fmt.Sprintf("%s('%s')의 게시판 Type별 정보를 구하는 중에 오류가 발생하였습니다.", c.site, c.siteID), fmt.Errorf("구현되지 않은 게시판 Type('%s') 입니다.", b.Type)
		}

		latestCrawledArticleID, latestCrawledCreatedDate, err := c.rssFeedProvidersAccessor.LatestCrawledArticleData(c.rssFeedProviderID, b.ID)
		if err != nil {
			return nil, nil, fmt.Sprintf("%s('%s') %s 게시판에 마지막으로 추가된 게시글 정보를 찾는 중에 오류가 발생하였습니다.", c.site, c.siteID, b.Name), err
		}

		var newLatestCrawledArticleID = ""

		//
		// 게시글 크롤링
		//
		for pageNo := 1; pageNo <= c.crawlingMaxPageCount; pageNo++ {
			ysPageUrl := strings.Replace(fmt.Sprintf("%s%s?page=%d", c.siteUrl, boardTypeConfig.urlPath, pageNo), yeosuCityUrlPathReplaceStringWithBoardID, b.ID, -1)

			doc, errOccurred, err := c.getWebPageDocument(ysPageUrl, fmt.Sprintf("%s('%s') %s 게시판", c.site, c.siteID, b.Name), nil)
			if err != nil {
				return nil, nil, errOccurred, err
			}

			ysSelection := doc.Find(boardTypeConfig.articleSelector)
			if len(ysSelection.Nodes) == 0 { // 게시글이 0건이라면 CSS 파싱이 실패한것으로 본다.
				return nil, nil, fmt.Sprintf("%s('%s') %s 게시판의 게시글 추출이 실패하였습니다. CSS셀렉터를 확인하세요.", c.site, c.siteID, b.Name), err
			}

			var foundAlreadyCrawledArticle = false
			ysSelection.EachWithBreak(func(i int, s *goquery.Selection) bool {
				var article *model.RssFeedProviderArticle
				if article, err = c.extractArticle(b.Type, s); err != nil {
					return false
				}
				article.BoardID = b.ID
				article.BoardName = b.Name
				article.BoardType = b.Type

				// 크롤링 된 게시글 목록 중에서 가장 최근의 게시글 ID를 구한다.
				if newLatestCrawledArticleID == "" {
					newLatestCrawledArticleID = article.ArticleID
				}

				// 이미 크롤링 작업을 했었던 게시글인지 확인한다. 이후의 게시글 추출 작업은 취소된다.
				if article.ArticleID == latestCrawledArticleID {
					foundAlreadyCrawledArticle = true
					return false
				}
				if latestCrawledCreatedDate.IsZero() == false && article.CreatedDate.Before(latestCrawledCreatedDate) == true {
					foundAlreadyCrawledArticle = true
					return false
				}

				articles = append(articles, article)

				return true
			})
			if err != nil {
				return nil, nil, fmt.Sprintf("%s('%s') %s 게시판의 게시글 추출이 실패하였습니다. CSS셀렉터를 확인하세요.", c.site, c.siteID, b.Name), err
			}

			if foundAlreadyCrawledArticle == true {
				break
			}
		}

		if newLatestCrawledArticleID != "" {
			newLatestCrawledArticleIDsByBoard[b.ID] = newLatestCrawledArticleID
		}
	}

	//
	// 게시글 내용 크롤링 : 내용은 크롤링이 실패해도 에러를 발생하지 않고 무시한다.
	// 동시에 여러개의 게시글을 읽는 경우 에러가 발생하는 경우가 생기므로 최대 1개씩 순차적으로 읽는다.
	// 만약 에러가 발생하여 게시글 내용을 크롤링 하지 못한 경우가 생길 수 있으므로 2번 크롤링한다.
	// ( ※ 여수시 홈페이지의 성능이 좋지 않은것 같음!!! )
	//
	for i := 0; i < 2; i++ {
		for _, article := range articles {
			if article.Content == "" {
				c.crawlingArticleContent(article)
			}
		}
	}

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
	case yeosuCityCrawlerBoardTypePhotoNews:
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

			var slashPos = strings.LastIndex(complexString, "/")
			article.Author = strings.TrimSpace(complexString[:slashPos])
			article.CreatedDate, err = time.ParseInLocation("2006-01-02 15:04", strings.TrimSpace(complexString[slashPos+1:]), time.Local)
			if err != nil {
				return nil, fmt.Errorf("게시글에서 등록자 및 등록일('%s') 파싱이 실패하였습니다. (error:%s)", complexString, err)
			}
		} else {
			return nil, fmt.Errorf("게시글에서 등록자 및 등록일('%s') 파싱이 실패하였습니다.", complexString)
		}

		return article, nil

	case yeosuCityCrawlerBoardTypeList1, yeosuCityCrawlerBoardTypeList2:
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

		if boardType == yeosuCityCrawlerBoardTypeList2 {
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

		// 등록자
		as = s.Find("td.list_department")
		if as.Length() != 1 {
			as = s.Find("td.list_member_name")
			if as.Length() != 1 {
				return nil, errors.New("게시글에서 등록자/담당부서 정보를 찾을 수 없습니다.")
			} else {
				article.Author = strings.TrimSpace(as.Text())
			}
		} else {
			article.Author = strings.TrimSpace(as.Text())
		}

		// 등록일
		as = s.Find("td.list_reg_date")
		if as.Length() != 1 {
			return nil, errors.New("게시글에서 등록일 정보를 찾을 수 없습니다.")
		}
		var createdDateString = strings.TrimSpace(as.Text())
		if matched, _ := regexp.MatchString("[0-9]{4}-[0-9]{2}-[0-9]{2}", createdDateString); matched == true {
			var now = time.Now()
			if fmt.Sprintf("%04d-%02d-%02d", now.Year(), now.Month(), now.Day()) == createdDateString {
				article.CreatedDate, err = time.ParseInLocation("2006-01-02 15:04:05", fmt.Sprintf("%s %02d:%02d:%02d", createdDateString, now.Hour(), now.Minute(), now.Second()), time.Local)
			} else {
				article.CreatedDate, err = time.ParseInLocation("2006-01-02 15:04:05", fmt.Sprintf("%s 23:59:59", createdDateString), time.Local)
			}
			if err != nil {
				return nil, fmt.Errorf("게시글에서 등록일('%s') 파싱이 실패하였습니다. (error:%s)", createdDateString, err)
			}
		} else {
			return nil, fmt.Errorf("게시글에서 등록일('%s') 파싱이 실패하였습니다.", createdDateString)
		}

		return article, nil

	default:
		return nil, fmt.Errorf("구현되지 않은 게시판 Type('%s') 입니다.", boardType)
	}
}

//noinspection GoUnhandledErrorResult
func (c *yeosuCityCrawler) crawlingArticleContent(article *model.RssFeedProviderArticle) {
	doc, errOccurred, err := c.getWebPageDocument(article.Link, fmt.Sprintf("%s('%s') %s 게시판의 게시글('%s') 상세페이지", c.site, c.siteID, article.BoardName, article.ArticleID), nil)
	if err != nil {
		log.Warnf("%s (error:%s)", errOccurred, err)
		return
	}

	ysSelection := doc.Find("div.con_detail")
	if ysSelection.Length() == 0 {
		log.Warnf("게시글('%s')에서 내용 정보를 찾을 수 없습니다.", article.ArticleID)
		return
	}

	article.Content = utils.CleanStringByLine(ysSelection.Text())

	// 내용에 이미지 태그가 포함되어 있다면 모두 추출한다.
	ysSelection.Find("img").Each(func(i int, s *goquery.Selection) {
		var src, _ = s.Attr("src")
		if src != "" {
			var alt, _ = s.Attr("alt")
			var style, _ = s.Attr("style")
			article.Content += fmt.Sprintf(`%s<img src="%s%s" alt="%s" style="%s">`, "\r\n", c.config.Url, src, alt, style)
		}
	})
}
