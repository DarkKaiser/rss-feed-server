package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDB는 각 테스트 환경에 독립적인 인메모리 SQLite DB와 초기화된 Store를 반환합니다.
func setupTestDB(t *testing.T) (*sql.DB, *Store) {
	t.Helper()

	// t.Parallel() 환경에서 각 테스트가 완전히 분리된 인메모리 DB를 갖도록 테스트 함수명을 사용합니다.
	// cache=shared를 사용해야 *sql.DB 커넥션 풀 내부의 모든 루틴이 동일한 메모리 DB를 바라봅니다.
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", t.Name())

	// PRAGMA foreign_keys=ON 옵션을 명시하여 외래키 Cascade 삭제 기능 활성화
	db, err := Open(context.Background(), dsn)
	require.NoError(t, err)

	store, err := New(db)
	require.NoError(t, err)

	// 스키마(AutoMigrate) 및 초기화(Vacuum) 수행
	err = store.Initialize(context.Background())
	require.NoError(t, err)

	return db, store
}

// TestStore_Initialize는 멱등성 및 정상 동작을 검증합니다.
func TestStore_Initialize(t *testing.T) {
	t.Parallel()
	db, store := setupTestDB(t)
	defer db.Close()

	// 이미 setupTestDB에서 1회 호출되었으나, 한 번 더 호출해서 멱등성(Idempotent) 검증
	err := store.Initialize(context.Background())
	assert.NoError(t, err, "Initialize 메서드는 여러 번 호출되어도 에러가 발생하지 않아야 합니다.")
}

// TestStore_SyncProviders는 Config -> DB 동기화 파이프라인의 삽입/수정 및 연쇄 삭제 로직을 검증합니다.
func TestStore_SyncProviders(t *testing.T) {
	t.Parallel()
	db, store := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// 1. 최초 데이터 삽입 검증
	initialProviders := []*config.ProviderConfig{
		{
			ID:   "naver_cafe_1",
			Site: "NaverCafe",
			Config: &config.ProviderDetailConfig{
				ID:          "c_1",
				Name:        "Test Club",
				Description: "Test Desc",
				URL:         "https://cafe.naver.com/test",
				Boards: []*config.BoardConfig{
					{ID: "board_1", Name: "Board 1"},
					{ID: "board_2", Name: "Board 2"},
				},
			},
		},
	}

	err := store.SyncProviders(ctx, initialProviders)
	require.NoError(t, err)

	// 삽입 데이터 확인
	var pCount, bCount int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM rss_provider").Scan(&pCount)
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM rss_provider_board").Scan(&bCount)
	assert.Equal(t, 1, pCount)
	assert.Equal(t, 2, bCount)

	// 가상의 게시글(Article) 추가 (Cascading 삭제 테스트를 위함)
	_, err = store.SaveArticles(ctx, "naver_cafe_1", []*feed.Article{
		{BoardID: "board_1", ArticleID: "art_1", Title: "Title 1", Link: "Link 1", CreatedAt: time.Now()},
		{BoardID: "board_2", ArticleID: "art_2", Title: "Title 2", Link: "Link 2", CreatedAt: time.Now()},
	})
	require.NoError(t, err)

	// 2. 수정 및 부분 삭제 검증 (board_2 제거 및 새로운 board_3 추가, Description 변경)
	updatedProviders := []*config.ProviderConfig{
		{
			ID:   "naver_cafe_1",
			Site: "NaverCafe",
			Config: &config.ProviderDetailConfig{
				ID:          "c_1",
				Name:        "Test Club Updated",
				Description: "Updated Desc",
				URL:         "https://cafe.naver.com/test",
				Boards: []*config.BoardConfig{
					{ID: "board_1", Name: "Board 1 Updated"},
					{ID: "board_3", Name: "Board 3"},
				},
			},
		},
	}

	err = store.SyncProviders(ctx, updatedProviders)
	require.NoError(t, err)

	// 수정 확인 (이름이 변경되어야 함)
	var updatedDesc string
	db.QueryRowContext(ctx, "SELECT s_description FROM rss_provider WHERE id = 'naver_cafe_1'").Scan(&updatedDesc)
	assert.Equal(t, "Updated Desc", updatedDesc)

	// Board Cascade 및 pruneStaleData 확인
	// board_2가 날아갔으므로 소속된 게시글(art_2)도 삭제되어 있어야 함
	var artCount int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM rss_provider_article").Scan(&artCount)
	assert.Equal(t, 1, artCount, "board_2 삭제 시 소속된 art_2 게시글도 Cascade Delete 되어야 합니다.")

	// 3. 공급자 자체 삭제 검증
	err = store.SyncProviders(ctx, []*config.ProviderConfig{}) // 모든 공급자 제거
	require.NoError(t, err)

	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM rss_provider").Scan(&pCount)
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM rss_provider_article").Scan(&artCount)
	assert.Equal(t, 0, pCount)
	assert.Equal(t, 0, artCount, "공급자 삭제 시 엮인 게시글 전부가 완전 삭제되어야 합니다.")
}

// TestStore_SaveAndGetArticles는 부분 성공(Best-Effort) 보장 다건 저장과 최신순 조회/Limit 동작을 검증합니다.
func TestStore_SaveAndGetArticles(t *testing.T) {
	t.Parallel()
	db, store := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// 사전 준비: Provider 및 Board 구성
	providers := []*config.ProviderConfig{
		{
			ID: "p_1", Site: "NaverCafe",
			Config: &config.ProviderDetailConfig{
				ID: "c_1", Name: "N1", URL: "U",
				Boards: []*config.BoardConfig{
					{ID: "b_1", Name: "Board 1"},
					{ID: "b_2", Name: "Board 2"},
				},
			},
		},
	}
	require.NoError(t, store.SyncProviders(ctx, providers))

	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// 정상 삽입 검증
	articles := []*feed.Article{
		{BoardID: "b_1", ArticleID: "a1", Title: "Title 1", Link: "1", CreatedAt: baseTime},
		{BoardID: "b_1", ArticleID: "a2", Title: "Title 2", Link: "2", CreatedAt: baseTime.Add(1 * time.Hour)},
		{BoardID: "b_2", ArticleID: "a3", Title: "Title 3", Link: "3", CreatedAt: baseTime.Add(2 * time.Hour)},
	}

	savedCnt, err := store.SaveArticles(ctx, "p_1", articles)
	require.NoError(t, err)
	assert.Equal(t, 3, savedCnt)

	// 중복 시 업데이트(Upsert) 검증 (에러 없이 내용만 덮어씀)
	articles[0].Title = "Updated Title 1"
	savedCnt, err = store.SaveArticles(ctx, "p_1", []*feed.Article{articles[0]})
	require.NoError(t, err)
	assert.Equal(t, 1, savedCnt)

	// 조회(GetArticles) 검증
	// 보드가 없을 때 빈 배열 반환
	res, err := store.GetArticles(ctx, "p_1", []string{}, 10)
	require.NoError(t, err)
	assert.Empty(t, res)

	// 단일 보드 조회 및 정렬(최신순 1시간 뒤가 앞으로) 확인
	res, err = store.GetArticles(ctx, "p_1", []string{"b_1"}, 10)
	require.NoError(t, err)
	require.Len(t, res, 2)
	assert.Equal(t, "a2", res[0].ArticleID, "내림차순 정렬이 보장되어야 합니다.")
	assert.Equal(t, "Updated Title 1", res[1].Title, "Upsert 갱신 처리가 반영되어 있어야 합니다.")

	// 다중 보드 조회 및 Limit 테스트
	res, err = store.GetArticles(ctx, "p_1", []string{"b_1", "b_2"}, 2) // 총 3개지만 2개만 Limit
	require.NoError(t, err)
	require.Len(t, res, 2)
	assert.Equal(t, "a3", res[0].ArticleID) // 가장 최신
	assert.Equal(t, "a2", res[1].ArticleID) // 그 다음
}

// TestStore_CrawlingCursor는 최신 수집 커서(ID, Date)의 Upsert 및 Get 동작 대칭성을 검증합니다.
func TestStore_CrawlingCursor(t *testing.T) {
	t.Parallel()
	db, store := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// 사전 구성 (p_1 아래 b_1, b_2)
	providers := []*config.ProviderConfig{
		{
			ID: "p_1", Site: "NaverCafe",
			Config: &config.ProviderDetailConfig{
				ID: "c_1", Name: "N", URL: "U",
				Boards: []*config.BoardConfig{{ID: "b_1", Name: "B1"}, {ID: "b_2", Name: "B2"}},
			},
		},
	}
	require.NoError(t, store.SyncProviders(ctx, providers))

	// 데이터가 없을 때의 Get 동작 검증 (NULL 커서)
	cursorID, cursorDate, err := store.GetCrawlingCursor(ctx, "p_1", "b_1")
	require.NoError(t, err)
	assert.Empty(t, cursorID)
	assert.True(t, cursorDate.IsZero())

	// 게시글 및 커서 저장 (Upsert)
	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	_, err = store.SaveArticles(ctx, "p_1", []*feed.Article{
		{BoardID: "b_1", ArticleID: "art_first", Title: "T1", Link: "1", CreatedAt: baseTime},
		{BoardID: "b_2", ArticleID: "art_second", Title: "T2", Link: "2", CreatedAt: baseTime.Add(time.Hour)},
	})
	require.NoError(t, err)

	err = store.UpsertLatestCrawledArticleID(ctx, "p_1", "b_1", "art_first")
	require.NoError(t, err)
	err = store.UpsertLatestCrawledArticleID(ctx, "p_1", "b_2", "art_second")
	require.NoError(t, err)

	// 특정 보드(b_1)의 커서 조회 검증
	cursorID, cursorDate, err = store.GetCrawlingCursor(ctx, "p_1", "b_1")
	require.NoError(t, err)
	assert.Equal(t, "art_first", cursorID)
	// SQLite DATETIME 저장 과정에서 로컬이나 포맷이 깎일 수 있으므로 포맷하여 비교하거나, 객체가 존재하는지만 확인
	assert.False(t, cursorDate.IsZero(), "생성 날짜가 함께 반환되어야 합니다.")

	// Global 보드 조회 동작 검증 (boardID == "")
	// b_1 커서(art_first)가 latest_crawled로 등록되긴 했지만, boardID == "" (루트) 커서는 b_id='' 로 따로 기록하므로 비어있어야 함.
	// (비즈니스 코드 참조: boardID가 ""일 때, 최신 크롤링 ID는 b_id=''를 필터링하지만, 날짜는 b_id 필터링 없이 통틀어 최대값을 가져옵니다)
	err = store.UpsertLatestCrawledArticleID(ctx, "p_1", "", "global_cursor")
	require.NoError(t, err)

	globalCursorID, globalCursorDate, err := store.GetCrawlingCursor(ctx, "p_1", "")
	require.NoError(t, err)
	assert.Equal(t, "global_cursor", globalCursorID)

	// 전체 보드(b_1, b_2) 중 가장 늦은 시간은 art_second(b_2)의 시간(1시간 후)이므로,
	// b_id='' 여도 날짜 서브쿼리는 전체 중 최대 시간(globalCursorDate)을 잡아와야 합니다.
	assert.True(t, globalCursorDate.After(cursorDate), "글로벌 조회 시 개별 보드를 통틀어 가장 최근 Created_Date를 가져와야 합니다.")
}
