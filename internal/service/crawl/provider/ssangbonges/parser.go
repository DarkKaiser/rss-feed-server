package ssangbonges

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/strutil"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
)

// noinspection GoErrorStringFormat
func (c *crawler) extractArticle(boardID, boardType, urlDetailPathPath string, s *goquery.Selection) (*feed.Article, error) {
	var exists bool
	var article = &feed.Article{}

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
		article.Link = strings.ReplaceAll(fmt.Sprintf("%s%s&nttSn=%s", c.Config().URL, urlDetailPathPath, article.ArticleID), ssangbongSchoolUrlPathReplaceStringWithBoardID, boardID)

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
				article.CreatedAt, err = time.ParseInLocation("2006-01-02 15:04:05", fmt.Sprintf("%s %02d:%02d:%02d", createdDateString, now.Hour(), now.Minute(), now.Second()), time.Local)
			} else {
				article.CreatedAt, err = time.ParseInLocation("2006-01-02 15:04:05", fmt.Sprintf("%s 23:59:59", createdDateString), time.Local)
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
		article.Link = strings.ReplaceAll(fmt.Sprintf("%s%s&nttSn=%s", c.Config().URL, urlDetailPathPath, article.ArticleID), ssangbongSchoolUrlPathReplaceStringWithBoardID, boardID)

		// 특수 처리: 학교앨범(156453)은 비공개 게시판이므로 상세 조회 시 막힘.
		// 목록 화면에 있는 썸네일로 본문을 대체하고 Author를 고정하여 상세페이지 조회를 스킵(Bypass)함.
		if boardID == ssangbongSchoolCrawlerBoardIDSchoolAlbum {
			imgSelection := s.Find("img").First()
			if imgSelection.Length() > 0 {
				src, _ := imgSelection.Attr("src")
				alt, _ := imgSelection.Attr("alt")
				if src != "" {
					article.Content = fmt.Sprintf(`<img src="%s%s" alt="%s">`, c.Config().URL, src, alt)
				}
			}
			article.Author = "쌍봉초등학교"
		}

		// 등록일
		as = s.Find("a.selectNttInfo > p.txt > span.date")
		if as.Length() != 2 {
			return nil, errors.New("게시글에서 등록일 정보를 찾을 수 없습니다.")
		}
		var createdDateString = strings.TrimSpace(as.Eq(0).Text())
		if createdDateString == "" {
			return nil, errors.New("게시글에서 등록일 파싱이 실패하였습니다.")
		}
		createdDateString = strings.TrimSpace(strings.ReplaceAll(createdDateString, ".", "-"))
		if matched, _ := regexp.MatchString("[0-9]{4}-[0-9]{2}-[0-9]{2}", createdDateString); matched == true {
			var err error
			var now = time.Now()
			if fmt.Sprintf("%04d-%02d-%02d", now.Year(), now.Month(), now.Day()) == createdDateString {
				article.CreatedAt, err = time.ParseInLocation("2006-01-02 15:04:05", fmt.Sprintf("%s %02d:%02d:%02d", createdDateString, now.Hour(), now.Minute(), now.Second()), time.Local)
			} else {
				article.CreatedAt, err = time.ParseInLocation("2006-01-02 15:04:05", fmt.Sprintf("%s 23:59:59", createdDateString), time.Local)
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

func (c *crawler) crawlingArticleContent(ctx context.Context, article *feed.Article) {
	doc, err := c.fetchDocumentWithPOST(ctx, article.Link, c.FormatMessage("%s 게시판의 게시글('%s') 상세페이지 접근이 실패하였습니다.", article.BoardName, article.ArticleID))
	if err != nil {
		applog.Warnf("%s", err.Error())
		return
	}

	// 포토 게시판의 경우 목록에서는 등록자가 표시되지 않으므로 상세 페이지에서 추출한다.
	if article.Author == "" {
		acSelection := doc.Find("div.bbs_ViewA > ul.bbsV_data > li")
		if acSelection.Length() != 3 {
			applog.Debugf("게시글('%s')에서 작성자 파싱이 실패하였습니다. (게시글 비공개/권한 없음 추정)", article.ArticleID)
			article.Author = "쌍봉초등학교"
		} else {
			author := strings.TrimSpace(acSelection.Eq(0).Text())
			if strings.HasPrefix(author, "작성자") == false {
				applog.Debugf("게시글('%s')에서 작성자 파싱이 실패하였습니다. (게시글 비공개/권한 없음 추정)", article.ArticleID)
				article.Author = "쌍봉초등학교"
			} else {
				article.Author = strings.TrimSpace(strings.Replace(author, "작성자", "", -1))
			}
		}
	}

	acSelection := doc.Find("div.bbs_ViewA > div.bbsV_cont")
	if acSelection.Length() == 0 {
		applog.Debugf("게시글('%s')에서 내용 정보를 찾을 수 없습니다. (게시글 비공개/권한 없음 추정)", article.ArticleID)
		return
	}

	acSelection.Children().Each(func(i int, s *goquery.Selection) {
		content := strutil.NormalizeMultiline(s.Text())
		if content != "" {
			if article.Content != "" {
				article.Content += "\r\n"
			}

			article.Content += content
		}
	})

	// 내용에 이미지 태그가 포함되어 있다면 모두 추출한다.
	acSelection.Find("div.bbs_ViewA > div.bbsV_cont img").Each(func(i int, s *goquery.Selection) {
		var src, _ = s.Attr("src")
		if src != "" {
			var alt, _ = s.Attr("alt")
			article.Content += fmt.Sprintf(`%s<img src="%s%s" alt="%s">`, "\r\n", c.Config().URL, src, alt)
		}
	})
}
