package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"golang.org/x/net/html"
	"golang.org/x/text/encoding"
)

// component 크롤링 서비스의 Provider 로깅용 컴포넌트 이름
const component = "crawl.provider"

// @@@@@
// DefaultBoardKey는 게시판 구분이 없는 단일 게시판 Provider에서
// 게시판 ID를 대신하여 사용하는 sentinel(기본값 표식) 상수입니다.
// DB 갱신 시 이 값을 감지하면 빈 문자열("")로 변환되어 저장됩니다.
// 예: UpdateLatestCrawledIDs 함수에서 boardID == DefaultBoardKey 조건으로 처리됩니다.
const DefaultBoardKey = "#empty#"

// @@@@@
// CrawlArticlesFunc는 실제 웹 페이지 크롤링을 수행하는 함수의 타입입니다.
// 전략 패턴(Strategy Pattern)을 통해 base crawler가 구체적인 크롤링 구현에 의존하지
// 않도록 분리하며, 각 크롤러 구조체(예: crawler)는 자신의 crawlArticles
// 메서드를 이 타입으로 Base.CrawlArticles 필드에 주입합니다.
//
// 반환값:
//   - []*feed.Article:      새로 발견된 신규 게시글 목록 (서버 오류 시 nil 반환)
//   - map[string]string:    게시판별 최신 크롤링 게시글 ID 맵 (key: boardID, value: articleID)
//   - string:               오류 발생 시 사용자/관리자에게 전달할 오류 메시지 문자열
//   - error:                오류 객체 (정상 처리 시 nil)
type CrawlArticlesFunc func(ctx context.Context) ([]*feed.Article, map[string]string, string, error)

// @@@@@
type Base struct {
	Config *config.ProviderDetailConfig

	RssFeedProviderID string
	FeedRepo          feed.Repository
	NotifyClient      *notify.Client

	Site            string
	SiteID          string
	SiteName        string
	SiteDescription string
	SiteUrl         string

	// 크롤링 할 최대 페이지 수
	CrawlingMaxPageCount int

	CrawlArticles CrawlArticlesFunc
}

// @@@@@
func (c *Base) Run() {
	// Task 실행 중 발생할 수 있는 런타임 패닉을 복구하여 스케줄러 메인 프로세스가 중단되지 않도록 방어합니다.
	defer func() {
		if r := recover(); r != nil {
			m := fmt.Sprintf("%s('%s') 크롤링 작업 중 런타임 패닉(Panic)이 발생하였습니다.\n\n[오류 상세 내용]\n%v", c.Site, c.SiteID, r)
			applog.Errorf(m)

			// 알림 전송 로직에서 발생할 수 있는 2차 패닉 차단
			func() {
				defer func() {
					if r2 := recover(); r2 != nil {
						applog.Errorf("알림 처리 중단: 패닉 복구 중 2차 패닉 발생 (panic:%v)", r2)
					}
				}()

				if c.NotifyClient != nil {
					// 패닉 발생 시 알림 전송을 동기적으로 수행하되, 최대 60초의 대기 시간을 제한하는 별도 컨텍스트 부여
					notifyCtx, notifyCancel := context.WithTimeout(context.Background(), 60*time.Second)
					defer notifyCancel()

					c.NotifyClient.NotifyError(notifyCtx, m)
				}
			}()
		}
	}()

	applog.Debugf("%s('%s')의 크롤링 작업을 시작합니다.", c.Site, c.SiteID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	articles, latestCrawledArticleIDsByBoard, errOccurred, err := c.CrawlArticles(ctx)
	if err != nil {
		c.SendErrorNotification(errOccurred, err)
		return
	}

	if articles != nil {
		if len(articles) > 0 {
			applog.Debugf("%s('%s')의 크롤링 작업 결과로 %d건의 신규 게시글이 추출되었습니다. 신규 게시글을 DB에 추가합니다.", c.Site, c.SiteID, len(articles))

			insertedCnt, err := c.FeedRepo.InsertArticles(ctx, c.RssFeedProviderID, articles)
			if err != nil {
				m := fmt.Sprintf("%s('%s')의 신규 게시글을 DB에 추가하는 중에 오류가 발생하여 크롤링 작업이 실패하였습니다.", c.Site, c.SiteID)
				c.SendErrorNotification(m, err)
				return
			}

			c.UpdateLatestCrawledIDs(ctx, latestCrawledArticleIDsByBoard)

			if len(articles) != insertedCnt {
				applog.Warnf("%s('%s')의 크롤링 작업을 종료합니다. 전체 %d건 중에서 %d건의 신규 게시글이 DB에 추가되었습니다.", c.Site, c.SiteID, len(articles), insertedCnt)
			} else {
				applog.Debugf("%s('%s')의 크롤링 작업을 종료합니다. %d건의 신규 게시글이 DB에 추가되었습니다.", c.Site, c.SiteID, len(articles))
			}
		} else {
			c.UpdateLatestCrawledIDs(ctx, latestCrawledArticleIDsByBoard)

			applog.Debugf("%s('%s')의 크롤링 작업을 종료합니다. 신규 게시글이 존재하지 않습니다.", c.Site, c.SiteID)
		}
	} else {
		applog.Warnf("%s('%s')의 크롤링 작업을 종료합니다. 서버의 일시적인 오류로 인하여 신규 게시글 추출이 실패하였습니다.", c.Site, c.SiteID)
	}
}

// @@@@@
// SendErrorNotification 작업 실행 중 발생한 에러를 로깅하고 사용자/관리자에게 알림으로 전송합니다.
func (c *Base) SendErrorNotification(message string, err error) {
	if err != nil {
		applog.Errorf("%s (error:%s)", message, err)
	} else {
		applog.Errorf("%s", message)
	}

	if c.NotifyClient == nil {
		return
	}

	// 알림 발송은 메인 흐름을 차단하지 않도록 별도의 고루틴에서 타임아웃과 함께 비동기로 실행합니다.
	go func(msg string, e error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var text string
		if e != nil {
			text = fmt.Sprintf("%s\r\n\r\n%s", msg, e)
		} else {
			text = msg
		}

		c.NotifyClient.NotifyError(ctx, text)
	}(message, err)
}

// @@@@@
// UpdateLatestCrawledIDs 크롤링 완료 후, 게시판별 최종 최신 게시글 ID를 DB에 갱신합니다.
func (c *Base) UpdateLatestCrawledIDs(ctx context.Context, latestCrawledArticleIDsByBoard map[string]string) {
	for boardID, articleID := range latestCrawledArticleIDsByBoard {
		if boardID == DefaultBoardKey {
			boardID = ""
		}

		if err := c.FeedRepo.UpdateLatestCrawledArticleID(ctx, c.RssFeedProviderID, boardID, articleID); err != nil {
			m := fmt.Sprintf("%s('%s')의 크롤링 된 최근 게시글 ID의 DB 갱신이 실패하였습니다.", c.Site, c.SiteID)
			c.SendErrorNotification(m, err)
		}
	}
}

// @@@@@
// noinspection GoUnhandledErrorResult
func (c *Base) GetWebPageDocument(url, title string, decoder *encoding.Decoder) (*goquery.Document, string, error) {
	res, err := http.Get(url)
	if err != nil {
		// 2022년 10월 중순경부터 네이버카페의 글을 일정 시간이 지난후에 http.Get()을 호출하게 되면 'connection reset by peer' 에러가 발생함!
		// 그래서 http.Get()에서 에러가 발생하면 최대 2번 호출하도록 변경함!!
		for i := 1; i <= 2; i++ {
			time.Sleep(100 * time.Millisecond)

			res, err = http.Get(url)
			if err == nil {
				goto SUCCEED
			}
		}

		return nil, fmt.Sprintf("%s 접근이 실패하였습니다.", title), err
	}
SUCCEED:
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Sprintf("%s 접근이 실패하였습니다.", title), fmt.Errorf("HTTP Response StatusCode %d", res.StatusCode)
	}
	defer res.Body.Close()

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		if strings.Contains(err.Error(), "unexpected EOF") && len(bodyBytes) != 0 {
			goto pars
		}
		return nil, fmt.Sprintf("%s의 내용을 읽을 수 없습니다.", title), err
	}

pars:
	if decoder != nil {
		bodyString, err := decoder.String(string(bodyBytes))
		if err != nil {
			return nil, fmt.Sprintf("%s의 문자열 디코딩이 실패하였습니다.", title), err
		}

		root, err := html.Parse(strings.NewReader(bodyString))
		if err != nil {
			return nil, fmt.Sprintf("%s의 HTML 파싱이 실패하였습니다.", title), err
		}

		return goquery.NewDocumentFromNode(root), "", nil
	} else {
		root, err := html.Parse(strings.NewReader(string(bodyBytes)))
		if err != nil {
			return nil, fmt.Sprintf("%s의 HTML 파싱이 실패하였습니다.", title), err
		}

		return goquery.NewDocumentFromNode(root), "", nil
	}
}
