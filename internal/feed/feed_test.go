package feed_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/darkkaiser/rss-feed-server/internal/feed"
)

// =============================================================================
// Article.String() Tests
// =============================================================================

// TestArticle_String_Format은 String() 메서드의 출력 포맷을 검증합니다.
// 테이블 기반 테스트로 정상 케이스와 경계 케이스를 모두 다룹니다.
func TestArticle_String_Format(t *testing.T) {
	t.Parallel()

	fixedTime := time.Date(2025, 1, 15, 9, 30, 0, 0, time.UTC)

	tests := []struct {
		name    string
		article feed.Article
		want    string
	}{
		{
			name: "모든 필드가 채워진 완전한 게시글",
			article: feed.Article{
				BoardID:   "board-001",
				BoardName: "공지사항",
				BoardType: "naver-cafe-board",
				ArticleID: "article-999",
				Title:     "안녕하세요",
				Content:   "본문 내용입니다.",
				Link:      "https://example.com/article/999",
				Author:    "홍길동",
				CreatedAt: fixedTime,
			},
			want: "[board-001, 공지사항, naver-cafe-board, article-999, 안녕하세요, 본문 내용입니다., https://example.com/article/999, 홍길동, 2025-01-15 09:30:00]",
		},
		{
			name: "선택적 필드(Content, Author)가 빈 게시글",
			article: feed.Article{
				BoardID:   "board-002",
				BoardName: "자유게시판",
				BoardType: "yeosu-board",
				ArticleID: "article-1",
				Title:     "제목만 있는 글",
				Content:   "",
				Link:      "https://example.com/article/1",
				Author:    "",
				CreatedAt: fixedTime,
			},
			want: "[board-002, 자유게시판, yeosu-board, article-1, 제목만 있는 글, , https://example.com/article/1, , 2025-01-15 09:30:00]",
		},
		{
			name:    "모든 필드가 비어있는 제로값 게시글",
			article: feed.Article{},
			// CreatedAt의 제로값은 "0001-01-01 00:00:00"으로 포맷됩니다.
			want: "[, , , , , , , , 0001-01-01 00:00:00]",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.article.String())
		})
	}
}

// TestArticle_String_TimezoneFormat은 CreatedAt의 시각이 지정된 포맷("2006-01-02 15:04:05")으로
// 시간대와 무관하게 안정적으로 출력되는지 확인합니다.
func TestArticle_String_TimezoneFormat(t *testing.T) {
	t.Parallel()

	// UTC 기준 2025-06-01 00:00:00 → KST(UTC+9)로 표현하면 2025-06-01 09:00:00
	kst := time.FixedZone("KST", 9*60*60)
	articleUTC := feed.Article{CreatedAt: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)}
	articleKST := feed.Article{CreatedAt: time.Date(2025, 6, 1, 9, 0, 0, 0, kst)}

	// UTC와 KST의 절대 시각이 같더라도 String()은 각 time.Time의 로컬 표현을 사용합니다.
	// 이 테스트는 포맷 자체가 올바른지 확인하며, 타임존 변환의 책임은 호출부에 있음을 문서화합니다.
	assert.Contains(t, articleUTC.String(), "2025-06-01 00:00:00")
	assert.Contains(t, articleKST.String(), "2025-06-01 09:00:00")
}

// TestArticle_String_ImplementsStringer는 Article이 fmt.Stringer 인터페이스를
// 올바르게 구현하고 있음을 컴파일 타임에 보장하기 위한 테스트입니다.
func TestArticle_String_ImplementsStringer(t *testing.T) {
	t.Parallel()

	// 이 줄이 컴파일되면 Article이 fmt.Stringer를 만족한다는 의미입니다.
	var _ interface{ String() string } = feed.Article{}
	t.Log("Article이 fmt.Stringer 인터페이스를 올바르게 구현하고 있습니다.")
}

// =============================================================================
// Repository Interface Contract Tests
// =============================================================================

// mockRepository는 테스트 전용 feed.Repository 구현체입니다.
// 인터페이스에 새 메서드가 추가될 경우 여기에서 컴파일 에러가 발생하여
// 구현체 업데이트를 강제합니다.
type mockRepository struct {
	insertArticlesFn               func(ctx context.Context, providerID string, articles []*feed.Article) (int, error)
	getArticlesFn                  func(ctx context.Context, providerID string, boardIDs []string, limit uint) ([]*feed.Article, error)
	getLatestCrawledInfoFn         func(ctx context.Context, providerID, boardID string) (string, time.Time, error)
	updateLatestCrawledArticleIDFn func(ctx context.Context, providerID, boardID, articleID string) error
}

// 컴파일 타임 인터페이스 준수 검증
var _ feed.Repository = (*mockRepository)(nil)

func (m *mockRepository) SaveArticles(ctx context.Context, providerID string, articles []*feed.Article) (int, error) {
	return m.insertArticlesFn(ctx, providerID, articles)
}

func (m *mockRepository) GetArticles(ctx context.Context, providerID string, boardIDs []string, limit uint) ([]*feed.Article, error) {
	return m.getArticlesFn(ctx, providerID, boardIDs, limit)
}

func (m *mockRepository) GetCrawlingCursor(ctx context.Context, providerID, boardID string) (string, time.Time, error) {
	return m.getLatestCrawledInfoFn(ctx, providerID, boardID)
}

func (m *mockRepository) UpsertLatestCrawledArticleID(ctx context.Context, providerID, boardID, articleID string) error {
	return m.updateLatestCrawledArticleIDFn(ctx, providerID, boardID, articleID)
}

// TestRepository_InterfaceContract은 mockRepository를 통해 Repository 인터페이스의
// 각 메서드가 올바른 시그니처를 갖고 있는지 계약을 검증합니다.
func TestRepository_InterfaceContract(t *testing.T) {
	t.Parallel()

	fixedTime := time.Date(2025, 3, 17, 12, 0, 0, 0, time.UTC)
	articles := []*feed.Article{
		{BoardID: "b1", ArticleID: "a1", Title: "테스트 게시글", CreatedAt: fixedTime},
	}

	repo := &mockRepository{
		insertArticlesFn: func(ctx context.Context, providerID string, in []*feed.Article) (int, error) {
			return len(in), nil
		},
		getArticlesFn: func(ctx context.Context, providerID string, boardIDs []string, limit uint) ([]*feed.Article, error) {
			return articles, nil
		},
		getLatestCrawledInfoFn: func(ctx context.Context, providerID, boardID string) (string, time.Time, error) {
			return "a1", fixedTime, nil
		},
		updateLatestCrawledArticleIDFn: func(ctx context.Context, providerID, boardID, articleID string) error {
			return nil
		},
	}

	t.Run("InsertArticles: 삽입 성공 수를 올바르게 반환한다", func(t *testing.T) {
		t.Parallel()
		n, err := repo.SaveArticles(context.Background(), "provider-1", articles)
		assert.NoError(t, err)
		assert.Equal(t, 1, n)
	})

	t.Run("GetArticles: 게시글 목록을 올바르게 반환한다", func(t *testing.T) {
		t.Parallel()
		got, err := repo.GetArticles(context.Background(), "provider-1", []string{"b1"}, 10)
		assert.NoError(t, err)
		assert.Len(t, got, 1)
		assert.Equal(t, "a1", got[0].ArticleID)
	})

	t.Run("GetLatestCrawledInfo: 마지막 크롤링 정보를 올바르게 반환한다", func(t *testing.T) {
		t.Parallel()
		id, at, err := repo.GetCrawlingCursor(context.Background(), "provider-1", "b1")
		assert.NoError(t, err)
		assert.Equal(t, "a1", id)
		assert.Equal(t, fixedTime, at)
	})

	t.Run("UpdateLatestCrawledArticleID: 오류 없이 완료된다", func(t *testing.T) {
		t.Parallel()
		err := repo.UpsertLatestCrawledArticleID(context.Background(), "provider-1", "b1", "a1")
		assert.NoError(t, err)
	})
}
