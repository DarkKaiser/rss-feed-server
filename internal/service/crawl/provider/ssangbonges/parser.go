package ssangbonges

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/strutil"
	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"
)

// extractArticle 게시판 유형(boardType)에 맞는 파서 함수를 선택하여 HTML 행(row) 하나를 feed.Article로 변환합니다.
// 게시판 유형별로 HTML 구조(CSS 셀렉터, 데이터 위치)가 다르기 때문에 타입에 따라 별도의 파서 함수로 위임합니다.
//
// 지원하는 게시판 유형:
//   - boardTypeList1("L_1"): 번호·제목·작성자·등록일이 표 형식으로 나오는 일반 목록형 게시판
//   - boardTypePhoto1("P_1"): 썸네일 사진이 그리드로 나열되는 포토 갤러리형 게시판
//
// 매개변수:
//   - boardID: 게시판의 고유 ID
//   - boardType: 게시판 유형 식별자
//   - detailURLTemplate: 상세페이지 URL의 경로 템플릿 (boardIDPlaceholder 포함)
//   - s: goquery로 읽어들인 게시글 한 줄(row 또는 li)에 해당하는 HTML 선택 객체
//
// 반환값:
//   - *feed.Article: 파싱된 게시글 정보 (제목, 링크, 작성자, 등록일 등)
//   - error: 지원하지 않는 boardType이거나 개별 필드 파싱에 실패한 경우 non-nil
func (c *crawler) extractArticle(boardID, boardType, detailURLTemplate string, s *goquery.Selection) (*feed.Article, error) {
	switch boardType {
	case boardTypeList1:
		return c.extractList1Article(boardID, detailURLTemplate, s)

	case boardTypePhoto1:
		return c.extractPhoto1Article(boardID, detailURLTemplate, s)

	default:
		return nil, apperrors.Newf(apperrors.System, "지원하지 않는 게시판 유형('%s')이 감지되어 파싱 작업을 수행할 수 없습니다", boardType)
	}
}

// @@@@@
func (c *crawler) extractList1Article(boardID, detailURLTemplate string, s *goquery.Selection) (*feed.Article, error) {
	var exists bool
	var article = &feed.Article{}

	// 제목
	as := s.Find("td.bbs_tit > a")
	if as.Length() != 1 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글에서 제목 정보를 찾을 수 없습니다.")
	}
	article.Title = strings.TrimSpace(as.Text())

	// 게시글 ID
	article.ArticleID, exists = as.Attr("data-id")
	if exists == false || article.ArticleID == "" {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글에서 게시글 ID 추출이 실패하였습니다.")
	}

	// 상세페이지 링크
	article.Link = strings.ReplaceAll(fmt.Sprintf("%s%s&nttSn=%s", c.Config().URL, detailURLTemplate, article.ArticleID), boardIDPlaceholder, boardID)

	// 등록자
	as = s.Find("td")
	if as.Length() < 4 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글에서 작성자 및 등록일 정보를 찾을 수 없습니다(항목 부족).")
	}
	author := strings.TrimSpace(as.Eq(as.Length() - 3).Text())
	if strings.HasPrefix(author, "작성자") == false {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글에서 작성자 파싱이 실패하였습니다.")
	}
	article.Author = strings.TrimSpace(strings.TrimPrefix(author, "작성자"))

	// 등록일
	var createdDateString = strings.TrimSpace(as.Eq(as.Length() - 2).Text())
	if strings.HasPrefix(createdDateString, "등록일") == false {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글에서 등록일 파싱이 실패하였습니다.")
	}
	createdDateString = strings.ReplaceAll(createdDateString, "등록일", "")
	// 점(.)을 대시(-)로 변환한 뒤 후행 '-'를 제거합니다.
	// 예: "2024.03.15." → "2024-03-15-" → "2024-03-15"
	// TrimSpace만으로는 후행 '-'가 남아 ParseCreatedAt 패턴 매칭에 실패합니다.
	createdDateString = strings.TrimRight(strings.TrimSpace(strings.ReplaceAll(createdDateString, ".", "-")), "-")
	var err error
	if article.CreatedAt, err = provider.ParseCreatedAt(createdDateString); err != nil {
		return nil, err
	}

	return article, nil
}

// @@@@@
func (c *crawler) extractPhoto1Article(boardID, detailURLTemplate string, s *goquery.Selection) (*feed.Article, error) {
	var exists bool
	var article = &feed.Article{}

	// 제목
	as := s.Find("a.selectNttInfo")
	if as.Length() != 1 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글에서 제목 정보를 찾을 수 없습니다.")
	}
	article.Title, exists = as.Attr("title")
	if exists == false {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글에서 제목 추출이 실패하였습니다.")
	}
	article.Title = strings.TrimSpace(article.Title)

	// 게시글 ID
	article.ArticleID, exists = as.Attr("data-param")
	if exists == false || article.ArticleID == "" {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글에서 게시글 ID 추출이 실패하였습니다.")
	}

	// 상세페이지 링크
	article.Link = strings.ReplaceAll(fmt.Sprintf("%s%s&nttSn=%s", c.Config().URL, detailURLTemplate, article.ArticleID), boardIDPlaceholder, boardID)

	// 특수 처리: 학교앨범(156453)은 비공개 게시판이므로 상세 조회 시 막힘.
	// 목록 화면에 있는 썸네일로 본문을 대체하고 Author를 고정하여 상세페이지 조회를 스킵(Bypass)함.
	if boardID == boardIDSchoolAlbum {
		imgSelection := s.Find("img").First()
		if imgSelection.Length() > 0 {
			src, _ := imgSelection.Attr("src")
			alt, _ := imgSelection.Attr("alt")
			if src != "" {
				baseURL, errBase := url.Parse(article.Link)
				relURL, errRel := url.Parse(src)
				if errBase == nil && errRel == nil {
					resolvedURL := baseURL.ResolveReference(relURL).String()
					article.Content = fmt.Sprintf(`<img src="%s" alt="%s">`, html.EscapeString(resolvedURL), html.EscapeString(alt))
				} else {
					article.Content = fmt.Sprintf(`<img src="%s" alt="%s">`, html.EscapeString(c.Config().URL+src), html.EscapeString(alt))
				}
			}
		}
		article.Author = "쌍봉초등학교"
	}

	// 등록일
	as = s.Find("a.selectNttInfo > p.txt > span.date")
	if as.Length() != 2 {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글에서 등록일 정보를 찾을 수 없습니다.")
	}
	var createdDateString = strings.TrimSpace(as.Eq(0).Text())
	if createdDateString == "" {
		return nil, apperrors.New(apperrors.ParsingFailed, "게시글에서 등록일 파싱이 실패하였습니다.")
	}
	// 점(.)을 대시(-)로 변환한 뒤 후행 '-'를 제거합니다.
	// 예: "2024.03.15." → "2024-03-15-" → "2024-03-15"
	createdDateString = strings.TrimRight(strings.TrimSpace(strings.ReplaceAll(createdDateString, ".", "-")), "-")
	var err error
	if article.CreatedAt, err = provider.ParseCreatedAt(createdDateString); err != nil {
		return nil, err
	}

	return article, nil
}

// @@@@@
func (c *crawler) crawlArticleContent(ctx context.Context, article *feed.Article) error {
	// 이미 Content가 채워진 경우(예: 비공개 학교앨범 게시판의 목록 썸네일)는 상세페이지 조회를 스킵합니다.
	// CrawlArticleContentsConcurrently의 재시도 루프는 2회차부터 Content 체크를 수행하지만,
	// 1회차는 항상 이 함수에 진입하므로 여기서 명시적으로 방어합니다.
	if article.Content != "" {
		return nil
	}

	doc, err := c.fetchHTMLViaPostForm(ctx, article.Link, c.Messagef("%s 게시판의 게시글('%s') 상세페이지 접근이 실패하였습니다.", article.BoardName, article.ArticleID))
	if err != nil {
		if apperrors.Is(err, apperrors.ExecutionFailed) {
			return provider.ErrContentUnavailable
		}
		applog.Warnf("%s", err.Error())
		return err
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
				article.Author = strings.TrimSpace(strings.TrimPrefix(author, "작성자"))
			}
		}
	}

	acSelection := doc.Find("div.bbs_ViewA > div.bbsV_cont")
	if acSelection.Length() == 0 {
		applog.Debugf("게시글('%s')에서 내용 정보를 찾을 수 없습니다. (게시글 비공개/권한 없음 추정)", article.ArticleID)
		return provider.ErrContentUnavailable
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
	acSelection.Find("img").Each(func(i int, s *goquery.Selection) {
		var src, _ = s.Attr("src")
		if src != "" {
			var alt, _ = s.Attr("alt")

			// ※ data:image의 데이터 크기가 너무 큰 항목인 경우 스마트폰 앱이 죽는 현상이 생기므로 기능 비활성화함!!!
			if strings.HasPrefix(src, "data:image/") {
				return // continue to next image
			}

			baseURL, errBase := url.Parse(article.Link)
			relURL, errRel := url.Parse(src)

			if errBase == nil && errRel == nil {
				resolvedURL := baseURL.ResolveReference(relURL).String()
				article.Content += fmt.Sprintf(`%s<img src="%s" alt="%s">`, "\r\n", html.EscapeString(resolvedURL), html.EscapeString(alt))
			}
		}
	})

	return nil
}
