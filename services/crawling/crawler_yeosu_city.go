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
	urlPath              string
	articleSelector      string
	articleGroupSelector string
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
			urlPath:              fmt.Sprintf("/www/govt/news/%s", yeosuCityUrlPathReplaceStringWithBoardID),
			articleSelector:      "#content div.board_list_box div.board_list div.item",
			articleGroupSelector: "#content",
		},
		yeosuCityCrawlerBoardTypeList1: {
			urlPath:              fmt.Sprintf("/www/govt/news/%s", yeosuCityUrlPathReplaceStringWithBoardID),
			articleSelector:      "#content table.board_basic > tbody > tr:not(.notice)",
			articleGroupSelector: "#content",
		},
		yeosuCityCrawlerBoardTypeList2: {
			urlPath:              fmt.Sprintf("/www/govt/news/%s", yeosuCityUrlPathReplaceStringWithBoardID),
			articleSelector:      "#content table.board_basic > tbody > tr:not(.notice)",
			articleGroupSelector: "#content",
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
			if ysSelection.Length() == 0 {
				// 여수시청 서버의 이상으로 가끔씩 게시글을 불러오지 못하는 현상이 발생함!!!
				// 만약 1번째 페이지에 이 현상이 발생하였으면 아무 처리도 하지 않고 다음 게시판을 크롤링한다.
				// 만약 2번째 이후의 페이지에 이 현상이 발생하였으면 모든 게시판의 크롤링 작업을 취소하고 빈 값을 바로 반환한다.
				switch b.Type {
				case yeosuCityCrawlerBoardTypePhotoNews:
					// 서버의 이상으로 게시글을 불러오지 못한건지 확인한다.
					ysSelection = doc.Find(boardTypeConfig.articleGroupSelector)
					if ysSelection.Length() == 1 {
						// 2번째 이후의 페이지라면 모든 게시판의 크롤링 작업을 취소하고 빈 값을 바로 반환한다.
						if pageNo > 1 {
							return nil, nil, "", nil
						}

						// 다음 게시판을 크롤링한다.
						goto NEXTBOARD
					}

				case yeosuCityCrawlerBoardTypeList1, yeosuCityCrawlerBoardTypeList2:
					// 리스트 타입의 경우 서버 이상이 발생한 경우에는 Selection 노드의 갯수가 1개이므로, 서버 이상 유무를 아래쪽 코드에서 처리한다.

				default:
					return nil, nil, fmt.Sprintf("%s('%s') %s 게시판의 게시글 추출이 실패하였습니다.", c.site, c.siteID, b.Name), fmt.Errorf("구현되지 않은 게시판 Type('%s') 입니다.", b.Type)
				}

				// 게시글이 0건이라면 CSS 파싱이 실패한것으로 본다.
				return nil, nil, fmt.Sprintf("%s('%s') %s 게시판의 게시글 추출이 실패하였습니다. CSS셀렉터를 확인하세요.", c.site, c.siteID, b.Name), errors.New("게시글 추출이 실패하였습니다.")
			} else if ysSelection.Length() == 1 {
				// 여수시청 서버의 이상으로 가끔씩 게시글을 불러오지 못하는 현상이 발생함!!!
				// 만약 1번째 페이지에 이 현상이 발생하였으면 아무 처리도 하지 않고 다음 게시판을 크롤링한다.
				// 만약 2번째 이후의 페이지에 이 현상이 발생하였으면 모든 게시판의 크롤링 작업을 취소하고 빈 값을 바로 반환한다.
				switch b.Type {
				case yeosuCityCrawlerBoardTypePhotoNews:
					// 포토뉴스 타입의 경우 서버 이상이 발생한 경우에는 Selection 노드의 갯수가 0개이므로, 서버 이상 유무를 위쪽 코드에서 처리한다.

				case yeosuCityCrawlerBoardTypeList1, yeosuCityCrawlerBoardTypeList2:
					as := ysSelection.First().Find("td")
					if as.Length() == 1 {
						for _, attr := range as.Nodes[0].Attr {
							// 서버의 이상으로 게시글을 불러오지 못한건지 확인한다.
							if attr.Key == "class" && attr.Val == "data_none" {
								// 2번째 이후의 페이지라면 모든 게시판의 크롤링 작업을 취소하고 빈 값을 바로 반환한다.
								if pageNo > 1 {
									// 2021년 07월 02일 기준으로 시험/채용공고 게시판의 경우 입력된 데이터가 몇 건 없어서
									// 페이지가 1페이지만 존재하므로 2페이지 이상을 읽게 되면 무조건 빈 값이 반환되므로
									// 특별히 예외처리를 한다. 추후에 데이터가 충분히 추가되면 아래 IF 문은 삭제해도 된다.
									if b.ID == "recruit" {
										goto SPECIALEXIT
									}

									return nil, nil, "", nil
								}

								// 다음 게시판을 크롤링한다.
								goto NEXTBOARD
							}
						}
					}

				default:
					return nil, nil, fmt.Sprintf("%s('%s') %s 게시판의 게시글 추출이 실패하였습니다.", c.site, c.siteID, b.Name), fmt.Errorf("구현되지 않은 게시판 Type('%s') 입니다.", b.Type)
				}
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
		// 링크
		as := s.Find("a.item_cont")
		if as.Length() != 1 {
			return nil, errors.New("게시글에서 링크 정보를 찾을 수 없습니다.")
		}
		article.Link, exists = as.Attr("href")
		if exists == false {
			return nil, errors.New("게시글에서 상세페이지 URL 추출이 실패하였습니다.")
		}
		article.Link = fmt.Sprintf("%s%s", c.siteUrl, article.Link)

		// 제목
		as = s.Find("a.item_cont > div.cont_box > div.title_box")
		if as.Length() != 1 {
			return nil, errors.New("게시글에서 제목 정보를 찾을 수 없습니다.")
		}
		article.Title = strings.ReplaceAll(strings.TrimSpace(as.Text()), "새로운글", "")

		// 게시글 ID
		u, err := url.Parse(article.Link)
		if err != nil {
			return nil, fmt.Errorf("게시글에서 상세페이지 URL 파싱이 실패하였습니다. (error:%s)", err)
		}
		m, _ := url.ParseQuery(u.RawQuery)
		if m["idx"] != nil {
			article.ArticleID = m["idx"][0]
		}
		if article.ArticleID == "" {
			return nil, errors.New("게시글에서 게시글 ID 추출이 실패하였습니다.")
		}

		// 등록자
		as = s.Find("a.item_cont > div.cont_box > dl > dd")
		if as.Length() != 3 {
			return nil, errors.New("게시글에서 등록자 및 등록일 정보를 찾을 수 없습니다.")
		}
		authorTokens := strings.Split(strings.TrimSpace(as.Eq(0).Text()), " ")
		article.Author = strings.TrimSpace(authorTokens[len(authorTokens)-1])

		// 등록일
		var createdDateString = strings.TrimSpace(as.Eq(1).Text())
		if matched, _ := regexp.MatchString("[0-9]{2}:[0-9]{2}:[0-9]{2}", createdDateString); matched == true {
			var now = time.Now()
			article.CreatedDate, err = time.ParseInLocation("2006-01-02 15:04:05", fmt.Sprintf("%04d-%02d-%02d %s", now.Year(), now.Month(), now.Day(), createdDateString), time.Local)
			if err != nil {
				return nil, fmt.Errorf("게시글에서 등록일('%s') 파싱이 실패하였습니다. (error:%s)", createdDateString, err)
			}
		} else if matched, _ := regexp.MatchString("[0-9]{4}-[0-9]{2}-[0-9]{2}", createdDateString); matched == true {
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

	case yeosuCityCrawlerBoardTypeList1, yeosuCityCrawlerBoardTypeList2:
		// 제목, 링크
		as := s.Find("a.basic_cont")
		if as.Length() != 1 {
			return nil, errors.New("게시글에서 제목 정보를 찾을 수 없습니다.")
		}
		article.Title = strings.ReplaceAll(strings.TrimSpace(as.Text()), "새로운글", "")
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
		m, _ := url.ParseQuery(u.RawQuery)
		if m["idx"] != nil {
			article.ArticleID = m["idx"][0]
		}
		if article.ArticleID == "" {
			return nil, errors.New("게시글에서 게시글 ID 추출이 실패하였습니다.")
		}

		// 등록자
		as = s.Find("td")
		if (boardType == yeosuCityCrawlerBoardTypeList1 && as.Length() != 5) || (boardType == yeosuCityCrawlerBoardTypeList2 && as.Length() != 6) {
			return nil, errors.New("게시글에서 등록자/담당부서 및 등록일 정보를 찾을 수 없습니다.")
		}
		article.Author = strings.TrimSpace(as.Eq(as.Length() - 3).Text())

		// 등록일
		var createdDateString = strings.TrimSpace(as.Eq(as.Length() - 2).Text())
		if matched, _ := regexp.MatchString("[0-9]{2}:[0-9]{2}:[0-9]{2}", createdDateString); matched == true {
			var now = time.Now()
			article.CreatedDate, err = time.ParseInLocation("2006-01-02 15:04:05", fmt.Sprintf("%04d-%02d-%02d %s", now.Year(), now.Month(), now.Day(), createdDateString), time.Local)
			if err != nil {
				return nil, fmt.Errorf("게시글에서 등록일('%s') 파싱이 실패하였습니다. (error:%s)", createdDateString, err)
			}
		} else if matched, _ := regexp.MatchString("[0-9]{4}-[0-9]{2}-[0-9]{2}", createdDateString); matched == true {
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

	ysSelection := doc.Find("div.contbox > div.viewbox")
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

			if strings.HasPrefix(src, "data:image/") == true {
				article.Content += fmt.Sprintf(`%s<img src="%s" alt="%s" style="%s">`, "\r\n", src, alt, style)
			} else {
				article.Content += fmt.Sprintf(`%s<img src="%s%s" alt="%s" style="%s">`, "\r\n", c.config.Url, src, alt, style)
			}
		}
	})
}
