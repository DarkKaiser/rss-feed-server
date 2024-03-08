package crawling

import (
	"errors"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/model"
	"github.com/darkkaiser/rss-feed-server/utils"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
	"regexp"
	"strings"
	"time"
)

const (
	// 리스트 1(번호, 제목, 작성자, 등록일, 조회)
	ssangbongSchoolCrawlerBoardTypeList1 string = "L_1"

	// 포토 1
	ssangbongSchoolCrawlerBoardTypePhoto1 string = "P_1"
)

var ssangbongSchoolCrawlerBoardTypes map[string]*ssangbongSchoolCrawlerBoardTypeConfig

type ssangbongSchoolCrawlerBoardTypeConfig struct {
	urlPath1        string
	urlPath2        string
	articleSelector string
}

const ssangbongSchoolUrlPathReplaceStringWithBoardID = "#{board_id}"

func init() {
	supportedCrawlers[g.RssFeedProviderSiteSsangbongSchool] = &supportedCrawlerConfig{
		newCrawlerFn: func(rssFeedProviderID string, config *g.ProviderConfig, rssFeedProviderStore *model.RssFeedProviderStore) cron.Job {
			site := "쌍봉초등학교 홈페이지"

			crawler := &ssangbongSchoolCrawler{
				crawler: crawler{
					config: config,

					rssFeedProviderID:    rssFeedProviderID,
					rssFeedProviderStore: rssFeedProviderStore,

					site:            site,
					siteID:          config.ID,
					siteName:        config.Name,
					siteDescription: config.Description,
					siteUrl:         config.Url,

					crawlingMaxPageCount: 3,
				},
			}

			crawler.crawlingArticlesFn = crawler.crawlingArticles

			log.Debug(fmt.Sprintf("%s('%s') Crawler가 생성되었습니다.", crawler.site, crawler.siteID))

			return crawler
		},
	}

	// 게시판 유형별 설정정보를 초기화한다.
	ssangbongSchoolCrawlerBoardTypes = map[string]*ssangbongSchoolCrawlerBoardTypeConfig{
		ssangbongSchoolCrawlerBoardTypePhoto1: {
			urlPath1:        fmt.Sprintf("/ys-ssangbong_es/na/ntt/selectNttList.do?mi=%s&bbsId=%s", ssangbongSchoolUrlPathReplaceStringWithBoardID, ssangbongSchoolUrlPathReplaceStringWithBoardID),
			urlPath2:        fmt.Sprintf("/ys-ssangbong_es/na/ntt/selectNttInfo.do?mi=%s&bbsId=%s", ssangbongSchoolUrlPathReplaceStringWithBoardID, ssangbongSchoolUrlPathReplaceStringWithBoardID),
			articleSelector: "div.subContent > div.photo_list > ul > li",
		},
		ssangbongSchoolCrawlerBoardTypeList1: {
			urlPath1:        fmt.Sprintf("/ys-ssangbong_es/na/ntt/selectNttList.do?mi=%s&bbsId=%s", ssangbongSchoolUrlPathReplaceStringWithBoardID, ssangbongSchoolUrlPathReplaceStringWithBoardID),
			urlPath2:        fmt.Sprintf("/ys-ssangbong_es/na/ntt/selectNttInfo.do?mi=%s&bbsId=%s", ssangbongSchoolUrlPathReplaceStringWithBoardID, ssangbongSchoolUrlPathReplaceStringWithBoardID),
			articleSelector: "div.subContent > div.bbs_ListA > table > tbody > tr",
		},
	}
}

type ssangbongSchoolCrawler struct {
	crawler
}

// noinspection GoErrorStringFormat,GoUnhandledErrorResult
func (c *ssangbongSchoolCrawler) crawlingArticles() ([]*model.RssFeedProviderArticle, map[string]string, string, error) {
	var articles = make([]*model.RssFeedProviderArticle, 0)
	var newLatestCrawledArticleIDsByBoard = make(map[string]string)

	for _, b := range c.config.Boards {
		boardTypeConfig, exists := ssangbongSchoolCrawlerBoardTypes[b.Type]
		if exists == false {
			return nil, nil, fmt.Sprintf("%s('%s')의 게시판 Type별 정보를 구하는 중에 오류가 발생하였습니다.", c.site, c.siteID), fmt.Errorf("구현되지 않은 게시판 Type('%s') 입니다.", b.Type)
		}

		latestCrawledArticleID, latestCrawledCreatedDate, err := c.rssFeedProviderStore.LatestCrawledInfo(c.rssFeedProviderID, b.ID)
		if err != nil {
			return nil, nil, fmt.Sprintf("%s('%s') %s 게시판에 마지막으로 추가된 게시글 정보를 찾는 중에 오류가 발생하였습니다.", c.site, c.siteID, b.Name), err
		}

		var newLatestCrawledArticleID = ""

		//
		// 게시글 크롤링
		//
		for pageNo := 1; pageNo <= c.crawlingMaxPageCount; pageNo++ {
			ssangbongSchoolPageUrl := strings.ReplaceAll(fmt.Sprintf("%s%s&currPage=%d", c.siteUrl, boardTypeConfig.urlPath1, pageNo), ssangbongSchoolUrlPathReplaceStringWithBoardID, b.ID)

			doc, errOccurred, err := c.getWebPageDocument(ssangbongSchoolPageUrl, fmt.Sprintf("%s('%s') %s 게시판", c.site, c.siteID, b.Name), nil)
			if err != nil {
				return nil, nil, errOccurred, err
			}

			ssangbongSchoolSelection := doc.Find(boardTypeConfig.articleSelector)
			if len(ssangbongSchoolSelection.Nodes) == 0 { // 읽어들인 게시글이 0건인지 확인
				if pageNo > 1 {
					// 2024년 03월 08일 기준으로 체험/행사활동안내, 방과후학교 > 방과후갤러리 게시판의 경우 입력된 데이터가 몇 건 없어서
					// 페이지가 1페이지 ~ 2페이지만 존재하므로 그 이상을 읽게 되면 무조건 빈 값이 반환되므로
					// 특별히 예외처리를 한다. 추후에 데이터가 충분히 추가되면 아래 IF 문은 삭제해도 된다.
					if b.ID == "156457" || b.ID == "156475" {
						goto SPECIALEXIT
					}
				}

				// 다음 게시판을 크롤링한다.
				goto NEXTBOARD
			}

			var foundAlreadyCrawledArticle = false
			ssangbongSchoolSelection.EachWithBreak(func(i int, s *goquery.Selection) bool {
				var article *model.RssFeedProviderArticle
				if article, err = c.extractArticle(b.ID, b.Type, boardTypeConfig.urlPath2, s); err != nil {
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

	SPECIALEXIT:
		if newLatestCrawledArticleID != "" {
			newLatestCrawledArticleIDsByBoard[b.ID] = newLatestCrawledArticleID
		}

	NEXTBOARD:
	}

	//
	// 게시글 내용 크롤링 : 내용은 크롤링이 실패해도 에러를 발생하지 않고 무시한다.
	// 동시에 여러개의 게시글을 읽는 경우 에러가 발생하는 경우가 생기므로 최대 1개씩 순차적으로 읽는다.
	// 만약 에러가 발생하여 게시글 내용을 크롤링 하지 못한 경우가 생길 수 있으므로 2번 크롤링한다.
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

// noinspection GoErrorStringFormat
func (c *ssangbongSchoolCrawler) extractArticle(boardID, boardType, urlDetailPathPath string, s *goquery.Selection) (*model.RssFeedProviderArticle, error) {
	var exists bool
	var article = &model.RssFeedProviderArticle{}

	switch boardType {
	case ssangbongSchoolCrawlerBoardTypeList1:
		// 제목
		as := s.Find("td.bbs_tit > a")
		if as.Length() != 1 {
			return nil, errors.New("게시글에서 제목 정보를 찾을 수 없습니다.")
		}
		article.Title = strings.TrimSpace(as.Text())

		// 게시글 ID
		article.ArticleID, exists = as.Attr("data-id")
		if exists == false || article.ArticleID == "" {
			return nil, errors.New("게시글에서 게시글 ID 추출이 실패하였습니다.")
		}

		// 상세페이지 링크
		article.Link = strings.ReplaceAll(fmt.Sprintf("%s%s&nttSn=%s", c.siteUrl, urlDetailPathPath, article.ArticleID), ssangbongSchoolUrlPathReplaceStringWithBoardID, boardID)

		// 등록자
		as = s.Find("td")
		if as.Length() != 5 {
			return nil, errors.New("게시글에서 작성자 및 등록일 정보를 찾을 수 없습니다.")
		}
		author := strings.TrimSpace(as.Eq(as.Length() - 3).Text())
		if strings.HasPrefix(author, "작성자") == false {
			return nil, errors.New("게시글에서 작성자 파싱이 실패하였습니다.")
		}
		article.Author = strings.TrimSpace(strings.Replace(author, "작성자", "", -1))

		// 등록일
		var createdDateString = strings.TrimSpace(as.Eq(as.Length() - 2).Text())
		if strings.HasPrefix(createdDateString, "등록일") == false {
			return nil, errors.New("게시글에서 등록일 파싱이 실패하였습니다.")
		}
		createdDateString = strings.ReplaceAll(createdDateString, "등록일", "")
		createdDateString = strings.TrimSpace(strings.ReplaceAll(createdDateString, ".", "-"))
		if matched, _ := regexp.MatchString("[0-9]{4}-[0-9]{2}-[0-9]{2}", createdDateString); matched == true {
			var err error
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

	case ssangbongSchoolCrawlerBoardTypePhoto1:
		// 제목
		as := s.Find("a.selectNttInfo")
		if as.Length() != 1 {
			return nil, errors.New("게시글에서 제목 정보를 찾을 수 없습니다.")
		}
		article.Title, exists = as.Attr("title")
		if exists == false {
			return nil, errors.New("게시글에서 제목 추출이 실패하였습니다.")
		}
		article.Title = strings.TrimSpace(article.Title)

		// 게시글 ID
		article.ArticleID, exists = as.Attr("data-param")
		if exists == false || article.ArticleID == "" {
			return nil, errors.New("게시글에서 게시글 ID 추출이 실패하였습니다.")
		}

		// 상세페이지 링크
		article.Link = strings.ReplaceAll(fmt.Sprintf("%s%s&nttSn=%s", c.siteUrl, urlDetailPathPath, article.ArticleID), ssangbongSchoolUrlPathReplaceStringWithBoardID, boardID)

		// 등록일
		as = s.Find("a.selectNttInfo > p.txt > span.date")
		if as.Length() != 1 {
			return nil, errors.New("게시글에서 등록일 정보를 찾을 수 없습니다.")
		}
		var createdDateString = strings.TrimSpace(as.Text())
		if createdDateString == "" {
			return nil, errors.New("게시글에서 등록일 파싱이 실패하였습니다.")
		}
		createdDateString = strings.TrimSpace(strings.ReplaceAll(createdDateString, ".", "-"))
		if matched, _ := regexp.MatchString("[0-9]{4}-[0-9]{2}-[0-9]{2}", createdDateString); matched == true {
			var err error
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

// noinspection GoUnhandledErrorResult
func (c *ssangbongSchoolCrawler) crawlingArticleContent(article *model.RssFeedProviderArticle) {
	doc, errOccurred, err := c.getWebPageDocument(article.Link, fmt.Sprintf("%s('%s') %s 게시판의 게시글('%s') 상세페이지", c.site, c.siteID, article.BoardName, article.ArticleID), nil)
	if err != nil {
		log.Warnf("%s (error:%s)", errOccurred, err)
		return
	}

	// 포토 게시판의 경우 목록에서는 등록자가 표시되지 않으므로 상세 페이지에서 추출한다.
	if article.Author == "" {
		acSelection := doc.Find("div.bbs_ViewA > ul.bbsV_data > li")
		if acSelection.Length() != 3 {
			log.Warnf("게시글('%s')에서 작성자 파싱이 실패하였습니다.", article.ArticleID)
		} else {
			author := strings.TrimSpace(acSelection.Eq(0).Text())
			if strings.HasPrefix(author, "작성자") == false {
				log.Warnf("게시글('%s')에서 작성자 파싱이 실패하였습니다.", article.ArticleID)
			} else {
				article.Author = strings.TrimSpace(strings.Replace(author, "작성자", "", -1))
			}
		}
	}

	acSelection := doc.Find("div.bbs_ViewA > div.bbsV_cont")
	if acSelection.Length() == 0 {
		log.Warnf("게시글('%s')에서 내용 정보를 찾을 수 없습니다.", article.ArticleID)
		return
	}

	article.Content = utils.TrimMultiLine(acSelection.Text())

	// 내용에 이미지 태그가 포함되어 있다면 모두 추출한다.
	acSelection.Find("div.bbs_ViewA > div.bbsV_cont img").Each(func(i int, s *goquery.Selection) {
		var src, _ = s.Attr("src")
		if src != "" {
			var alt, _ = s.Attr("alt")
			article.Content += fmt.Sprintf(`%s<img src="%s%s" alt="%s">`, "\r\n", c.siteUrl, src, alt)
		}
	})
}
