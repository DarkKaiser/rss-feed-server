package ssangbonges

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"
)

// component 크롤링 서비스의 쌍봉초등학교 Provider 로깅용 컴포넌트 이름
const component = "crawl.provider.ssangbonges"

const (
	// 리스트 1(번호, 제목, 작성자, 등록일, 조회)
	ssangbongSchoolCrawlerBoardTypeList1 string = "L_1"

	// 포토 1
	ssangbongSchoolCrawlerBoardTypePhoto1 string = "P_1"

	// 회원제(비공개) 처리된 학교앨범 게시판 고유 ID
	ssangbongSchoolCrawlerBoardIDSchoolAlbum string = "156453"
)

var ssangbongSchoolCrawlerBoardTypes map[string]*ssangbongSchoolCrawlerBoardTypeConfig

type ssangbongSchoolCrawlerBoardTypeConfig struct {
	urlPath1             string
	urlPath2             string
	articleSelector      string
	articleGroupSelector string
}

const ssangbongSchoolUrlPathReplaceStringWithBoardID = "#{board_id}"

func init() {
	provider.MustRegister(config.ProviderSiteSsangbongElementarySchool, &provider.CrawlerConfig{
		NewCrawler: func(params provider.NewCrawlerParams) (provider.Crawler, error) {
			crawlerInstance := &crawler{
				Base: provider.NewBase(
					params,
					3,
				),
			}

			crawlerInstance.SetCrawlArticles(crawlerInstance.crawlArticles)

			applog.Debug(crawlerInstance.FormatMessage("Crawler가 생성되었습니다."))

			return crawlerInstance, nil
		},
	})

	// 게시판 유형별 설정정보를 초기화한다.
	ssangbongSchoolCrawlerBoardTypes = map[string]*ssangbongSchoolCrawlerBoardTypeConfig{
		ssangbongSchoolCrawlerBoardTypePhoto1: {
			urlPath1:             fmt.Sprintf("/ys-ssangbong_es/na/ntt/selectNttList.do?mi=%s&bbsId=%s", ssangbongSchoolUrlPathReplaceStringWithBoardID, ssangbongSchoolUrlPathReplaceStringWithBoardID),
			urlPath2:             fmt.Sprintf("/ys-ssangbong_es/na/ntt/selectNttInfo.do?mi=%s&bbsId=%s", ssangbongSchoolUrlPathReplaceStringWithBoardID, ssangbongSchoolUrlPathReplaceStringWithBoardID),
			articleSelector:      "div.subContent > div.photo_list > ul > li",
			articleGroupSelector: "div.subContent > div.photo_list",
		},
		ssangbongSchoolCrawlerBoardTypeList1: {
			urlPath1:             fmt.Sprintf("/ys-ssangbong_es/na/ntt/selectNttList.do?mi=%s&bbsId=%s", ssangbongSchoolUrlPathReplaceStringWithBoardID, ssangbongSchoolUrlPathReplaceStringWithBoardID),
			urlPath2:             fmt.Sprintf("/ys-ssangbong_es/na/ntt/selectNttInfo.do?mi=%s&bbsId=%s", ssangbongSchoolUrlPathReplaceStringWithBoardID, ssangbongSchoolUrlPathReplaceStringWithBoardID),
			articleSelector:      "div.subContent > div.bbs_ListA > table > tbody > tr",
			articleGroupSelector: "div.subContent > div.bbs_ListA",
		},
	}
}

type crawler struct {
	*provider.Base
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ provider.Crawler = (*crawler)(nil)

// noinspection GoErrorStringFormat,GoUnhandledErrorResult
func (c *crawler) crawlArticles(ctx context.Context) ([]*feed.Article, map[string]string, string, error) {
	var articles = make([]*feed.Article, 0)
	var newLatestCrawledArticleIDsByBoard = make(map[string]string)

	for _, b := range c.Config().Boards {
		boardArticles, cursor, msg, err := c.crawlSingleBoard(ctx, b)
		if err != nil {
			c.SendErrorNotification(msg, err)
			continue // 개별 게시판 오류 발생 시 전체 로직을 멈추지 않고 시스템 전파(누수)를 차단하여 성공한 다른 게시판 데이터 보존
		}

		articles = append(articles, boardArticles...)
		if cursor != "" {
			newLatestCrawledArticleIDsByBoard[b.ID] = cursor
		}
	}

	//
	// 게시글 내용 크롤링 (Worker Pool 방식을 통한 제한적 동시 수집 및 개별 재시도)
	// 내용은 크롤링이 실패해도 에러를 발생하지 않고 무시한다.
	// (기존의 불안정한 전체 순차 이중 루프 방식 대신 대상 서버 부하를 고려해 최대 동시 작업 수를 제한하고, 실패 건별로 독립적인 재시도를 수행합니다)
	//
	if err := c.CrawlArticleContentsConcurrently(ctx, articles, 2, c.crawlingArticleContent); err != nil {
		// 본문 수집 중 시스템 에러(context 취소 또는 타임아웃)가 발생했더라도,
		// 이미 목록 크롤링으로 확보된 게시글과 커서 정보는 보존합니다.
		// nil을 반환하면 다음 사이클에서 동일 게시글을 처음부터 다시 수집하는 중복 재처리 루프가 발생합니다.
		// 본문이 없는 상태로 저장하고 커서를 갱신하여 중복 수집을 방지합니다.
		errOccurred := c.FormatMessage("본문 수집 중 시스템 종료 시그널 또는 타임아웃이 발생하여 크롤링 작업이 중단되었습니다.")
		c.SendErrorNotification(errOccurred, err)
	}

	return articles, newLatestCrawledArticleIDsByBoard, "", nil
}

func (c *crawler) crawlSingleBoard(ctx context.Context, b *config.BoardConfig) ([]*feed.Article, string, string, error) {
	var articles = make([]*feed.Article, 0)

	boardTypeConfig, exists := ssangbongSchoolCrawlerBoardTypes[b.Type]
	if exists == false {
		return nil, "", c.FormatMessage("게시판 Type별 정보를 구하는 중에 오류가 발생하였습니다."), fmt.Errorf("구현되지 않은 게시판 Type('%s') 입니다.", b.Type)
	}

	latestCrawledArticleID, latestCrawledCreatedDate, err := c.FeedRepo().GetCrawlingCursor(ctx, c.ProviderID(), b.ID)
	if err != nil {
		return nil, "", c.FormatMessage("%s 게시판에 마지막으로 추가된 게시글 정보를 찾는 중에 오류가 발생하였습니다.", b.Name), err
	}

	// 이전 커서값이 아닌 빈 문자열로 초기화하여, 신규 게시글이 실제로 수집된 경우에만
	// 커서를 갱신합니다. latestCrawledArticleID 로 초기화하면 신규 게시글이 없어도
	// 불필요한 DB Upsert가 발생하고, 특수 상황에서 커서 역전이 일어날 수 있습니다.
	var newLatestCrawledArticleID = ""

	//
	// 게시글 크롤링
	//
	for pageNo := 1; pageNo <= c.MaxPageCount(); pageNo++ {
		ssangbongSchoolPageUrl := strings.ReplaceAll(fmt.Sprintf("%s%s&currPage=%d", c.Config().URL, boardTypeConfig.urlPath1, pageNo), ssangbongSchoolUrlPathReplaceStringWithBoardID, b.ID)

		doc, err := c.fetchDocumentWithPOST(ctx, ssangbongSchoolPageUrl, c.FormatMessage("%s 게시판 접근이 실패하였습니다.", b.Name))
		if err != nil {
			// 부분 수집 시 커서를 갱신하지 않으면 스케줄링 주기에 따라 무한 재처리 및 
			// 타겟 서버 DDoS 부하를 유발하는 치명적 버그가 발생할 수 있으므로, 
			// 페이지 접근 에러 시 부분 반환 대신 전체 롤백(error 반환) 처리합니다.
			return nil, "", c.FormatMessage("%s 게시판 접근이 실패하였습니다. (page: %d)", b.Name, pageNo), err
		}

		ssangbongSchoolSelection := doc.Find(boardTypeConfig.articleSelector)
		if len(ssangbongSchoolSelection.Nodes) == 0 { // 읽어들인 게시글이 0건인지 확인
			if pageNo > 1 {
				// 2페이지 이상에서 게시글이 0건이라면 등록된 게시글을 모두 읽음(EndOfData) 처리
				break
			}
			
			// 1페이지에서 게시글이 없는 경우, 빈 게시판인지 아니면 CSS 클래스 변경에 의한 에러인지 확인하기 위해 상위 컨테이너 존재 여부 파악
			if doc.Find(boardTypeConfig.articleGroupSelector).Length() > 0 {
				// 부모 컨테이너는 존재하지만 자식 요소가 없다면 단순히 등록된 게시글이 하나도 없는 빈 게시판이다.
				return articles, "", "", nil
			}

			// 부모 컨테이너가 발견되지 않는다면 레이아웃 변경 등에 의한 명시적인 CSS 파싱 에러로 간주한다.
			return nil, "", c.FormatMessage("%s 게시판의 게시글 추출이 실패하였습니다. CSS셀렉터를 확인하세요.", b.Name), errors.New("게시글 추출이 실패하였습니다.")
		}

		var foundAlreadyCrawledArticle = false
		ssangbongSchoolSelection.EachWithBreak(func(i int, s *goquery.Selection) bool {
			// 상단에 고정된 공지사항(행 번호 대신 '공' 표시가 있는 게시글)은 항상 최상단에 노출되므로
			// cursor 판별 논리에 치명적인 오류(신규 게시판 무시 및 탐색 영구 중단)를 발생시킵니다.
			// <td class="mPre"> 영역이 존재하는 행은 고정 공지글로 간주하고 무시(스킵)합니다.
			if s.Find("td.mPre").Length() > 0 {
				return true
			}

			article, err := c.extractArticle(b.ID, b.Type, boardTypeConfig.urlPath2, s)
			if err != nil {
				applog.Warn(c.FormatMessage("%s 게시판에서 개별 게시글 추출이 실패하여 스킵합니다. (error:%s)", b.Name, err))
				return true
			}
			article.BoardID = b.ID
			article.BoardName = b.Name
			article.BoardType = b.Type

			// 이미 크롤링 작업을 했었던 게시글인지 먼저 확인한다. 이후의 게시글 추출 작업은 취소된다.
			// 중복 판별을 커서 갱신보다 먼저 수행하여, 이미 수집된 게시글 ID가 최신 커서에
			// 잘못 반영되는 것을 방지합니다.
			// 게시글 삭제 시 무한 루프를 방지하기 위해 정수로 변환하여 대소 비교합니다.
			articleIDInt, errArt := strconv.ParseInt(article.ArticleID, 10, 64)
			latestIDInt, errLatest := strconv.ParseInt(latestCrawledArticleID, 10, 64)

			if errArt == nil && errLatest == nil && latestCrawledArticleID != "" {
				if articleIDInt <= latestIDInt {
					foundAlreadyCrawledArticle = true
					return false
				}
			} else {
				// 숫자로 변환이 불가능한 ID일 경우를 대비한 가드 로직 (문자열 길이가 짧거나, 같을 때 사전식 작거나 같은 경우 처리)
				if latestCrawledArticleID != "" {
					id1, id2 := article.ArticleID, latestCrawledArticleID
					if len(id1) < len(id2) || (len(id1) == len(id2) && id1 <= id2) {
						foundAlreadyCrawledArticle = true
						return false
					}
				}
			}
			// ParseCreatedDate는 당일이 아닌 과거 날짜의 시각을 00:00:00 으로 고정합니다.
			// 시각 정보 불일치에 따른 오판을 방지하기 위해 날짜 문자열(yyyy-MM-dd) 포맷으로 변환하여
			// 순수 연월일 단위로만 일자 경과 여부를 비교합니다. (동일 날짜는 ID 비교로만 처리됨)
			if !latestCrawledCreatedDate.IsZero() && article.CreatedAt.Format("2006-01-02") < latestCrawledCreatedDate.Format("2006-01-02") {
				foundAlreadyCrawledArticle = true
				return false
			}

			// 게시글을 articles에 먼저 추가한 후 커서를 갱신합니다.
			// 순서 역전(커서 갱신 → append) 상태에서 패닉 등 런타임 오류 발생 시
			// 커서만 전진하고 게시글이 영구 누락되는 데이터 무결성 오류를 방지합니다.
			articles = append(articles, article)

			// 신규 게시글로 확인된 경우에만 최신 커서를 갱신합니다.
			if newLatestCrawledArticleID == "" {
				newLatestCrawledArticleID = article.ArticleID
			} else {
				artIDInt, err1 := strconv.ParseInt(article.ArticleID, 10, 64)
				newLatestIDInt, err2 := strconv.ParseInt(newLatestCrawledArticleID, 10, 64)
				if err1 == nil && err2 == nil {
					if artIDInt > newLatestIDInt {
						newLatestCrawledArticleID = article.ArticleID
					}
				} else {
					id1, id2 := article.ArticleID, newLatestCrawledArticleID
					if len(id1) > len(id2) || (len(id1) == len(id2) && id1 > id2) {
						newLatestCrawledArticleID = article.ArticleID
					}
				}
			}

			return true
		})
		// 개별 파싱 에러(parseErr)에 의해 즉시 크롤링을 포기하는 방식을 폐기하고, 경고 로그만 남기며 계속 진행됨

		if foundAlreadyCrawledArticle == true {
			break
		}
	}

	// 해당 게시판의 데이터가 DB에 오래된 글부터 추가되도록 역순으로 재배열합니다.
	for i, j := 0, len(articles)-1; i < j; i, j = i+1, j-1 {
		articles[i], articles[j] = articles[j], articles[i]
	}

	return articles, newLatestCrawledArticleID, "", nil
}

func (c *crawler) fetchDocumentWithPOST(ctx context.Context, url, title string) (*goquery.Document, error) {
	querySplitIndex := strings.Index(url, "?")
	if querySplitIndex == -1 {
		return nil, fmt.Errorf("%s URL에서 쿼리스트링을 찾을 수 없습니다.", title)
	}

	reqBody := bytes.NewBufferString(url[querySplitIndex+1:])

	head := make(http.Header)
	head.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	head.Set("Content-Type", "application/x-www-form-urlencoded")
	// Host와 Origin 헤더 등은 fetcher가 자동으로 설정하거나 기본 클라이언트 정책을 따릅니다.

	doc, err := c.Scraper().FetchHTML(ctx, "POST", url[:querySplitIndex], reqBody, head)
	if err != nil {
		return nil, fmt.Errorf("%s (error:%v)", title, err)
	}

	return doc, nil
}
