package feed

import (
	"context"
	"fmt"
	"time"
)

// Article 크롤링하여 수집한 게시글 하나를 나타내는 도메인 모델입니다.
type Article struct {
	// BoardID 게시판의 고유 식별자입니다.
	BoardID string

	// BoardName 게시판의 표시 이름입니다.
	BoardName string

	// BoardType 게시판의 유형을 나타냅니다.
	BoardType string

	// ArticleID 게시글의 고유 식별자입니다.
	ArticleID string

	// Title 게시글의 제목입니다.
	Title string

	// Content 게시글의 본문 내용입니다.
	Content string

	// Link 게시글 원문 페이지로 연결되는 URL입니다.
	Link string

	// Author 게시글 작성자의 이름입니다.
	Author string

	// CreatedAt 게시글이 최초 작성된 일시입니다.
	CreatedAt time.Time
}

func (a Article) String() string {
	return fmt.Sprintf("[%s, %s, %s, %s, %s, %s, %s, %s, %s]", a.BoardID, a.BoardName, a.BoardType, a.ArticleID, a.Title, a.Content, a.Link, a.Author, a.CreatedAt.Format("2006-01-02 15:04:05"))
}

// Repository 게시글 데이터의 저장 및 조회를 추상화한 인터페이스입니다.
// 비즈니스 로직이 특정 저장소 기술(예: SQLite)에 의존하지 않도록 의존성을 역전(DIP)시키며, 저장소 교체 시 이 인터페이스만 새로 구현하면 됩니다.
type Repository interface {
	// InsertArticles 지정한 providerID에 속하는 게시글 목록을 저장소에 저장합니다.
	// 개별 게시글 저장에 실패하더라도 나머지 게시글의 처리는 계속 진행되며, 반환값으로 실제로 작성에 성공한 게시글 수를 돌려줍니다.
	InsertArticles(ctx context.Context, providerID string, articles []*Article) (int, error)

	// GetArticles 지정한 providerID와 boardIDs에 해당하는 게시글을 최신 작성일시 순으로 최대 제한 개수(limit)만큼 반환합니다.
	GetArticles(ctx context.Context, providerID string, boardIDs []string, limit uint) ([]*Article, error)

	// GetLatestCrawledInfo 지정된 사이트(providerID)의 게시판(boardID)에서 이전에 수집한 가장 최신 게시글의 ID와 작성일시를 조회합니다.
	// 만약 boardID가 빈 문자열("")인 경우, 게시판을 구분하지 않고 해당 사이트 전체에서 가장 최신 게시글 정보를 반환합니다.
	GetLatestCrawledInfo(ctx context.Context, providerID, boardID string) (string, time.Time, error)

	// UpdateLatestCrawledArticleID 새로운 게시글 수집이 끝난 후, 가장 마지막에 수집한 게시글의 ID를 저장합니다.
	// 다음 크롤링 때 이전에 수집한 글을 중복해서 가져오지 않도록 반드시 호출해야 합니다.
	UpdateLatestCrawledArticleID(ctx context.Context, providerID, boardID, articleID string) error
}
