package yeosucityhall

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"

	"github.com/PuerkitoBio/goquery"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
)

const (
	// 포토뉴스
	yeosuCityHallCrawlerBoardTypePhotoNews string = "P"

	// 카드뉴스
	yeosuCityHallCrawlerBoardTypeCardNews string = "C"

	// 리스트 1(번호, 제목, 등록자, 등록일, 조회)
	yeosuCityHallCrawlerBoardTypeList1 string = "L_1"

	// 리스트 2(번호, 분류, 제목, 담당부서, 등록일, 조회)
	yeosuCityHallCrawlerBoardTypeList2 string = "L_2"
)

var yeosuCityHallCrawlerBoardTypes map[string]*yeosuCityHallCrawlerBoardTypeConfig

type yeosuCityHallCrawlerBoardTypeConfig struct {
	urlPath              string
	articleSelector      string
	articleGroupSelector string
}

const yeosuCityHallUrlPathReplaceStringWithBoardID = "#{board_id}"

func init() {
	provider.MustRegister(config.ProviderSiteYeosuCityHall, &provider.CrawlerConfig{
		NewCrawler: func(rssFeedProviderID string, providerConfig *config.ProviderDetailConfig, feedRepo feed.Repository, notifyClient *notify.Client) provider.Crawler {
			site := "여수시청 홈페이지"

			crawlerInstance := &crawler{
				Base: provider.NewBase(provider.BaseParams{
					Config: providerConfig,

					RssFeedProviderID: rssFeedProviderID,
					FeedRepo:          feedRepo,
					NotifyClient:      notifyClient,

					Site:            site,
					SiteID:          providerConfig.ID,
					SiteName:        providerConfig.Name,
					SiteDescription: providerConfig.Description,
					SiteUrl:         providerConfig.URL,

					CrawlingMaxPageCount: 3,
				}),
			}

			crawlerInstance.SetCrawlArticles(crawlerInstance.crawlArticles)

			applog.Debug(fmt.Sprintf("%s('%s') Crawler가 생성되었습니다.", crawlerInstance.Site(), crawlerInstance.SiteID()))

			return crawlerInstance
		},
	})

	// 게시판 유형별 설정정보를 초기화한다.
	yeosuCityHallCrawlerBoardTypes = map[string]*yeosuCityHallCrawlerBoardTypeConfig{
		yeosuCityHallCrawlerBoardTypePhotoNews: {
			urlPath:              fmt.Sprintf("/www/govt/news/%s", yeosuCityHallUrlPathReplaceStringWithBoardID),
			articleSelector:      "#content div.board_list_box div.board_list div.item",
			articleGroupSelector: "#content",
		},
		yeosuCityHallCrawlerBoardTypeList1: {
			urlPath:              fmt.Sprintf("/www/govt/news/%s", yeosuCityHallUrlPathReplaceStringWithBoardID),
			articleSelector:      "#content table.board_basic > tbody > tr:not(.notice)",
			articleGroupSelector: "#content",
		},
		yeosuCityHallCrawlerBoardTypeList2: {
			urlPath:              fmt.Sprintf("/www/govt/news/%s", yeosuCityHallUrlPathReplaceStringWithBoardID),
			articleSelector:      "#content table.board_basic > tbody > tr:not(.notice)",
			articleGroupSelector: "#content",
		},
		yeosuCityHallCrawlerBoardTypeCardNews: {
			urlPath:              fmt.Sprintf("/www/govt/news/%s", yeosuCityHallUrlPathReplaceStringWithBoardID),
			articleSelector:      "#content div.board_list_box div.board_list > div.board_list > div.board_photo > div.item_wrap > div.item",
			articleGroupSelector: "#content",
		},
	}
}

type crawler struct {
	provider.Base
}

// noinspection GoErrorStringFormat,GoUnhandledErrorResult
func (c *crawler) crawlArticles(ctx context.Context) ([]*feed.Article, map[string]string, string, error) {
	var articles = make([]*feed.Article, 0)
	var newLatestCrawledArticleIDsByBoard = make(map[string]string)

	for _, b := range c.Config().Boards {
		boardTypeConfig, exists := yeosuCityHallCrawlerBoardTypes[b.Type]
		if exists == false {
			return nil, nil, fmt.Sprintf("%s('%s')의 게시판 Type별 정보를 구하는 중에 오류가 발생하였습니다.", c.Site(), c.SiteID()), fmt.Errorf("구현되지 않은 게시판 Type('%s') 입니다.", b.Type)
		}

		latestCrawledArticleID, latestCrawledCreatedDate, err := c.FeedRepo().GetCrawlingCursor(ctx, c.RssFeedProviderID(), b.ID)
		if err != nil {
			return nil, nil, fmt.Sprintf("%s('%s') %s 게시판에 마지막으로 추가된 게시글 정보를 찾는 중에 오류가 발생하였습니다.", c.Site(), c.SiteID(), b.Name), err
		}

		var newLatestCrawledArticleID = ""

		//
		// 게시글 크롤링
		//
		for pageNo := 1; pageNo <= c.CrawlingMaxPageCount(); pageNo++ {
			ysPageUrl := strings.Replace(fmt.Sprintf("%s%s?page=%d", c.SiteUrl(), boardTypeConfig.urlPath, pageNo), yeosuCityHallUrlPathReplaceStringWithBoardID, b.ID, -1)

			doc, err := c.Scraper().FetchHTMLDocument(ctx, ysPageUrl, nil)
			if err != nil {
				return nil, nil, fmt.Sprintf("%s('%s') %s 게시판 접근이 실패하였습니다.", c.Site(), c.SiteID(), b.Name), err
			}

			ysSelection := doc.Find(boardTypeConfig.articleSelector)
			if ysSelection.Length() == 0 {
				// 여수시청 서버의 이상으로 가끔씩 게시글을 불러오지 못하는 현상이 발생함!!!
				// 만약 1번째 페이지에 이 현상이 발생하였으면 아무 처리도 하지 않고 다음 게시판을 크롤링한다.
				// 만약 2번째 이후의 페이지에서 이 현상이 발생하였으면 모든 게시판의 크롤링 작업을 취소하고 빈 값을 바로 반환한다.
				switch b.Type {
				case yeosuCityHallCrawlerBoardTypePhotoNews, yeosuCityHallCrawlerBoardTypeCardNews:
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

				case yeosuCityHallCrawlerBoardTypeList1, yeosuCityHallCrawlerBoardTypeList2:
					// 리스트 타입의 경우 서버 이상이 발생한 경우에는 Selection(ysSelection) 노드의 갯수가 1개이므로, 서버 이상 유무를 아래쪽 IF 블럭에서 처리한다.

				default:
					return nil, nil, fmt.Sprintf("%s('%s') %s 게시판의 게시글 추출이 실패하였습니다.", c.Site(), c.SiteID(), b.Name), fmt.Errorf("구현되지 않은 게시판 Type('%s') 입니다.", b.Type)
				}

				// 게시글이 0건이라면 CSS 파싱이 실패한것으로 본다.
				return nil, nil, fmt.Sprintf("%s('%s') %s 게시판의 게시글 추출이 실패하였습니다. CSS셀렉터를 확인하세요.", c.Site(), c.SiteID(), b.Name), errors.New("게시글 추출이 실패하였습니다.")
			} else if ysSelection.Length() == 1 {
				// 여수시청 서버의 이상으로 가끔씩 게시글을 불러오지 못하는 현상이 발생함!!!
				// 만약 1번째 페이지에 이 현상이 발생하였으면 아무 처리도 하지 않고 다음 게시판을 크롤링한다.
				// 만약 2번째 이후의 페이지에서 이 현상이 발생하였으면 모든 게시판의 크롤링 작업을 취소하고 빈 값을 바로 반환한다.
				switch b.Type {
				case yeosuCityHallCrawlerBoardTypePhotoNews, yeosuCityHallCrawlerBoardTypeCardNews:
					// 포토뉴스/카드뉴스 타입의 경우 서버 이상이 발생한 경우에는 Selection(ysSelection) 노드의 갯수가 0개이므로, 서버 이상 유무를 위쪽 IF 블럭에서 처리한다.

				case yeosuCityHallCrawlerBoardTypeList1, yeosuCityHallCrawlerBoardTypeList2:
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
					return nil, nil, fmt.Sprintf("%s('%s') %s 게시판의 게시글 추출이 실패하였습니다.", c.Site(), c.SiteID(), b.Name), fmt.Errorf("구현되지 않은 게시판 Type('%s') 입니다.", b.Type)
				}
			}

			var foundAlreadyCrawledArticle = false
			ysSelection.EachWithBreak(func(i int, s *goquery.Selection) bool {
				var article *feed.Article
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
				if latestCrawledCreatedDate.IsZero() == false && article.CreatedAt.Before(latestCrawledCreatedDate) == true {
					foundAlreadyCrawledArticle = true
					return false
				}

				articles = append(articles, article)

				return true
			})
			if err != nil {
				return nil, nil, fmt.Sprintf("%s('%s') %s 게시판의 게시글 추출이 실패하였습니다. CSS셀렉터를 확인하세요.", c.Site(), c.SiteID(), b.Name), err
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
	// ( ※ 여수시청 홈페이지의 성능이 좋지 않은것 같음!!! )
	//
	for i := 0; i < 2; i++ {
		for _, article := range articles {
			if article.Content == "" {
				c.crawlingArticleContent(ctx, article)
			}
		}
	}

	// DB에 오래된 게시글부터 추가되도록 하기 위해 역순으로 재배열한다.
	for i, j := 0, len(articles)-1; i < j; i, j = i+1, j-1 {
		articles[i], articles[j] = articles[j], articles[i]
	}

	return articles, newLatestCrawledArticleIDsByBoard, "", nil
}
