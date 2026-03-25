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

// @@@@@
type crawler struct {
	config *config.ProviderDetailConfig

	rssFeedProviderID string
	feedRepo          feed.Repository
	notifyClient      *notify.Client

	site            string
	siteID          string
	siteName        string
	siteDescription string
	siteUrl         string

	// 크롤링 할 최대 페이지 수
	crawlingMaxPageCount int

	crawlArticles crawlArticlesFunc
}

// @@@@@
func (c *crawler) Run() {
	// Task 실행 중 발생할 수 있는 런타임 패닉을 복구하여 스케줄러 메인 프로세스가 중단되지 않도록 방어합니다.
	defer func() {
		if r := recover(); r != nil {
			m := fmt.Sprintf("%s('%s') 크롤링 작업 중 런타임 패닉(Panic)이 발생하였습니다.\n\n[오류 상세 내용]\n%v", c.site, c.siteID, r)
			applog.Errorf(m)

			// 알림 전송 로직에서 발생할 수 있는 2차 패닉 차단
			func() {
				defer func() {
					if r2 := recover(); r2 != nil {
						applog.Errorf("알림 처리 중단: 패닉 복구 중 2차 패닉 발생 (panic:%v)", r2)
					}
				}()

				if c.notifyClient != nil {
					// 패닉 발생 시 알림 전송을 동기적으로 수행하되, 최대 60초의 대기 시간을 제한하는 별도 컨텍스트 부여
					notifyCtx, notifyCancel := context.WithTimeout(context.Background(), 60*time.Second)
					defer notifyCancel()

					c.notifyClient.NotifyError(notifyCtx, m)
				}
			}()
		}
	}()

	applog.Debugf("%s('%s')의 크롤링 작업을 시작합니다.", c.site, c.siteID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	articles, latestCrawledArticleIDsByBoard, errOccurred, err := c.crawlArticles(ctx)
	if err != nil {
		c.sendErrorNotification(errOccurred, err)
		return
	}

	if articles != nil {
		if len(articles) > 0 {
			applog.Debugf("%s('%s')의 크롤링 작업 결과로 %d건의 신규 게시글이 추출되었습니다. 신규 게시글을 DB에 추가합니다.", c.site, c.siteID, len(articles))

			insertedCnt, err := c.feedRepo.InsertArticles(ctx, c.rssFeedProviderID, articles)
			if err != nil {
				m := fmt.Sprintf("%s('%s')의 신규 게시글을 DB에 추가하는 중에 오류가 발생하여 크롤링 작업이 실패하였습니다.", c.site, c.siteID)
				c.sendErrorNotification(m, err)
				return
			}

			c.updateLatestCrawledIDs(ctx, latestCrawledArticleIDsByBoard)

			if len(articles) != insertedCnt {
				applog.Warnf("%s('%s')의 크롤링 작업을 종료합니다. 전체 %d건 중에서 %d건의 신규 게시글이 DB에 추가되었습니다.", c.site, c.siteID, len(articles), insertedCnt)
			} else {
				applog.Debugf("%s('%s')의 크롤링 작업을 종료합니다. %d건의 신규 게시글이 DB에 추가되었습니다.", c.site, c.siteID, len(articles))
			}
		} else {
			c.updateLatestCrawledIDs(ctx, latestCrawledArticleIDsByBoard)

			applog.Debugf("%s('%s')의 크롤링 작업을 종료합니다. 신규 게시글이 존재하지 않습니다.", c.site, c.siteID)
		}
	} else {
		applog.Warnf("%s('%s')의 크롤링 작업을 종료합니다. 서버의 일시적인 오류로 인하여 신규 게시글 추출이 실패하였습니다.", c.site, c.siteID)
	}
}

// @@@@@
// sendErrorNotification 작업 실행 중 발생한 에러를 로깅하고 사용자/관리자에게 알림으로 전송합니다.
func (c *crawler) sendErrorNotification(message string, err error) {
	if err != nil {
		applog.Errorf("%s (error:%s)", message, err)
	} else {
		applog.Errorf("%s", message)
	}

	if c.notifyClient == nil {
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

		c.notifyClient.NotifyError(ctx, text)
	}(message, err)
}

// @@@@@
// updateLatestCrawledIDs 크롤링 완료 후, 게시판별 최종 최신 게시글 ID를 DB에 갱신합니다.
func (c *crawler) updateLatestCrawledIDs(ctx context.Context, latestCrawledArticleIDsByBoard map[string]string) {
	for boardID, articleID := range latestCrawledArticleIDsByBoard {
		if boardID == DefaultBoardKey {
			boardID = ""
		}

		if err := c.feedRepo.UpdateLatestCrawledArticleID(ctx, c.rssFeedProviderID, boardID, articleID); err != nil {
			m := fmt.Sprintf("%s('%s')의 크롤링 된 최근 게시글 ID의 DB 갱신이 실패하였습니다.", c.site, c.siteID)
			c.sendErrorNotification(m, err)
		}
	}
}

// @@@@@
// noinspection GoUnhandledErrorResult
func (c *crawler) getWebPageDocument(url, title string, decoder *encoding.Decoder) (*goquery.Document, string, error) {
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
