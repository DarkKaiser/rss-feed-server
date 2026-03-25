package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	_ "github.com/mattn/go-sqlite3"
)

// @@@@@
// Store RSS Feed Provider의 DB 접근을 담당하는 Store
type Store struct {
	db *sql.DB
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ feed.Repository = (*Store)(nil)

// @@@@@
// New Store를 초기화하여 반환한다.
func New(db *sql.DB) (*Store, error) {
	s := &Store{
		db: db,
	}

	return s, nil
}

// @@@@@
// AutoMigrate 어플리케이션에 필요한 DB 테이블 및 인덱스가 없다면 생성한다.
func (s *Store) AutoMigrate() error {
	if err := s.createTables(); err != nil {
		return fmt.Errorf("초기 테이블 및 인덱스 생성 실패: %w", err)
	}

	// 어제(과거) 지워진 게시글의 빈 공간(Free Space)을 물리적으로 완전히 회수하고 압축합니다.
	// (새벽 4시 서버 재실행 메커니즘과 맞물려 매일 1회 초기화 시점에 안전하게 용량이 관리됩니다.)
	if _, err := s.db.Exec("VACUUM"); err != nil {
		return fmt.Errorf("DB 용량 압축(VACUUM) 실패: %w", err)
	}

	return nil
}

// @@@@@
// SyncProviders 환경 설정에 정의된 내용으로 RSS Feed Provider 마스터 데이터를 DB에 동기화한다.
func (s *Store) SyncProviders(providers []*config.ProviderConfig) error {
	var errs []error

	for _, c := range providers {
		if err := s.insertRSSFeedProvider(c.ID, c.Site, c.Config.ID, c.Config.Name, c.Config.Description, c.Config.URL); err != nil {
			errs = append(errs, fmt.Errorf("RSS Feed Provider 정보 추가 실패 (providerID: %s): %w", c.ID, err))
		}

		for _, b := range c.Config.Boards {
			if err := s.insertRSSFeedProviderBoard(c.ID, b.ID, b.Name); err != nil {
				errs = append(errs, fmt.Errorf("RSS Feed Provider Board 정보 추가 실패 (providerID: %s, boardID: %s): %w", c.ID, b.ID, err))
			}
		}
	}

	return errors.Join(errs...)
}

// @@@@@
// PurgeOldArticles 환경 설정에 정의된 보관 기한(ArchiveDays)이 지난 오래된 크롤링 게시글을 모두 삭제한다.
func (s *Store) PurgeOldArticles(providers []*config.ProviderConfig) error {
	var errs []error

	for _, c := range providers {
		if err := s.deleteOutOfDateArticles(c.ID, c.Config.ArchiveDays); err != nil {
			errs = append(errs, fmt.Errorf("보관 기간이 지난 게시글 삭제 실패 (providerID: %s): %w", c.ID, err))
		}
	}

	return errors.Join(errs...)
}

// @@@@@
// noinspection GoUnhandledErrorResult
func (s *Store) createTables() error {
	//
	// rss_provider 테이블
	//
	stmt1, err := s.db.Prepare(`
		CREATE TABLE IF NOT EXISTS rss_provider (
			id            VARCHAR( 50) PRIMARY KEY NOT NULL UNIQUE,
			site          VARCHAR( 50) NOT NULL,
			s_id          VARCHAR( 50) NOT NULL,
			s_name        VARCHAR(130) NOT NULL,
			s_description VARCHAR(200),
			s_url         VARCHAR(100) NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("rss_provider 테이블 생성 쿼리 준비 실패: %w", err)
	}
	defer stmt1.Close()
	if _, err = stmt1.Exec(); err != nil {
		return fmt.Errorf("rss_provider 테이블 생성 쿼리 실행 실패: %w", err)
	}

	stmt2, err := s.db.Prepare(`
		CREATE INDEX IF NOT EXISTS rss_provider_index01 ON rss_provider(s_id)
	`)
	if err != nil {
		return fmt.Errorf("rss_provider_index01 인덱스 생성 쿼리 준비 실패: %w", err)
	}
	defer stmt2.Close()
	if _, err = stmt2.Exec(); err != nil {
		return fmt.Errorf("rss_provider_index01 인덱스 생성 쿼리 실행 실패: %w", err)
	}

	//
	// rss_provider_board 테이블
	//
	stmt3, err := s.db.Prepare(`
		CREATE TABLE IF NOT EXISTS rss_provider_board (
			p_id VARCHAR( 50) NOT NULL,
			id   VARCHAR( 50) NOT NULL,
			name VARCHAR(130) NOT NULL,
			PRIMARY KEY (p_id, id),
			FOREIGN KEY (p_id) REFERENCES rss_provider(id)
		)
	`)
	if err != nil {
		return fmt.Errorf("rss_provider_board 테이블 생성 쿼리 준비 실패: %w", err)
	}
	defer stmt3.Close()
	if _, err = stmt3.Exec(); err != nil {
		return fmt.Errorf("rss_provider_board 테이블 생성 쿼리 실행 실패: %w", err)
	}

	stmt4, err := s.db.Prepare(`
		CREATE INDEX IF NOT EXISTS rss_provider_board_index01 ON rss_provider_board(p_id)
	`)
	if err != nil {
		return fmt.Errorf("rss_provider_board_index01 인덱스 생성 쿼리 준비 실패: %w", err)
	}
	defer stmt4.Close()
	if _, err = stmt4.Exec(); err != nil {
		return fmt.Errorf("rss_provider_board_index01 인덱스 생성 쿼리 실행 실패: %w", err)
	}

	//
	// rss_provider_article 테이블
	//
	stmt5, err := s.db.Prepare(`
		CREATE TABLE IF NOT EXISTS rss_provider_article (
			p_id         VARCHAR( 50) NOT NULL,
			b_id         VARCHAR( 50) NOT NULL,
			id           VARCHAR( 50) NOT NULL,
			title        VARCHAR(400) NOT NULL,
			content      TEXT,
			link         VARCHAR(1000) NOT NULL,
			author       VARCHAR(50),
			created_date DATETIME,
			PRIMARY KEY (p_id, b_id, id),
			FOREIGN KEY (p_id) REFERENCES rss_provider(id),
			FOREIGN KEY (b_id) REFERENCES rss_provider_board(id)
		)
	`)
	if err != nil {
		return fmt.Errorf("rss_provider_article 테이블 생성 쿼리 준비 실패: %w", err)
	}
	defer stmt5.Close()
	if _, err = stmt5.Exec(); err != nil {
		return fmt.Errorf("rss_provider_article 테이블 생성 쿼리 실행 실패: %w", err)
	}

	stmt6, err := s.db.Prepare(`
		CREATE INDEX IF NOT EXISTS rss_provider_article_index01 ON rss_provider_article(created_date)
	`)
	if err != nil {
		return fmt.Errorf("rss_provider_article_index01 인덱스 생성 쿼리 준비 실패: %w", err)
	}
	defer stmt6.Close()
	if _, err = stmt6.Exec(); err != nil {
		return fmt.Errorf("rss_provider_article_index01 인덱스 생성 쿼리 실행 실패: %w", err)
	}

	//
	// rss_provider_site_crawled_data 테이블
	//
	stmt7, err := s.db.Prepare(`
		CREATE TABLE IF NOT EXISTS rss_provider_site_crawled_data (
			p_id                      VARCHAR( 50) NOT NULL,
			b_id                      VARCHAR( 50) NOT NULL,
			latest_crawled_article_id VARCHAR( 50) NOT NULL,
			PRIMARY KEY (p_id, b_id),
			FOREIGN KEY (p_id) REFERENCES rss_provider(id)
		)
	`)
	if err != nil {
		return fmt.Errorf("rss_provider_site_crawled_data 테이블 생성 쿼리 준비 실패: %w", err)
	}
	defer stmt7.Close()
	if _, err = stmt7.Exec(); err != nil {
		return fmt.Errorf("rss_provider_site_crawled_data 테이블 생성 쿼리 실행 실패: %w", err)
	}

	return nil
}

// @@@@@
// noinspection GoUnhandledErrorResult
func (s *Store) insertRSSFeedProvider(providerID, site, sourceID, sourceName, sourceDescription, sourceURL string) error {
	stmt, err := s.db.Prepare(`
		INSERT OR REPLACE
		  INTO rss_provider (id, site, s_id, s_name, s_description, s_url) 
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("rss_provider 삽입/갱신 쿼리 준비 실패 (providerID: %s): %w", providerID, err)
	}
	defer stmt.Close()
	if _, err = stmt.Exec(providerID, site, sourceID, sourceName, sourceDescription, sourceURL); err != nil {
		return fmt.Errorf("rss_provider 삽입/갱신 쿼리 실행 실패 (providerID: %s): %w", providerID, err)
	}

	return nil
}

// @@@@@
// noinspection GoUnhandledErrorResult
func (s *Store) insertRSSFeedProviderBoard(providerID, boardID, boardName string) error {
	stmt, err := s.db.Prepare(`
		INSERT OR REPLACE
		  INTO rss_provider_board (p_id, id, name) 
		VALUES (?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("rss_provider_board 삽입/갱신 쿼리 준비 실패 (providerID: %s, boardID: %s): %w", providerID, boardID, err)
	}
	defer stmt.Close()
	if _, err = stmt.Exec(providerID, boardID, boardName); err != nil {
		return fmt.Errorf("rss_provider_board 삽입/갱신 쿼리 실행 실패 (providerID: %s, boardID: %s): %w", providerID, boardID, err)
	}

	return nil
}

// @@@@@
// InsertArticles 게시글 목록을 DB에 추가하고 실제로 삽입된 건수와 통합 에러를 반환한다.
// 개별 행 삽입 실패는 건너뛰고 계속 진행하며, 실패한 행들은 단일 slice로 모여서 반환된다.
//
// noinspection GoUnhandledErrorResult
func (s *Store) InsertArticles(ctx context.Context, providerID string, articles []*feed.Article) (int, error) {
	stmt, err := s.db.PrepareContext(ctx, `
		INSERT OR REPLACE
		  INTO rss_provider_article (p_id, b_id, id, title, content, link, author, created_date)
		VALUES (?, ?, ?, ?, ?, ?, ?, DATETIME(?))
	`)
	if err != nil {
		return 0, fmt.Errorf("rss_provider_article 삽입/갱신 쿼리 준비 실패 (providerID: %s): %w", providerID, err)
	}
	defer stmt.Close()

	var insertedCnt int
	var errs []error

	for _, article := range articles {
		if _, err := stmt.ExecContext(ctx, providerID, article.BoardID, article.ArticleID, article.Title, article.Content, article.Link, article.Author, article.CreatedAt.UTC().Format("2006-01-02 15:04:05")); err != nil {
			errs = append(errs, fmt.Errorf("RSS Feed DB 게시글 등록 실패 (p_id: %s, article: %+v): %w", providerID, article, err))
		} else {
			insertedCnt++
		}
	}

	return insertedCnt, errors.Join(errs...)
}

// @@@@@
// GetArticles 지정한 provider/board의 게시글 목록을 최신순으로 반환한다.
//
// noinspection GoUnhandledErrorResult
func (s *Store) GetArticles(ctx context.Context, providerID string, boardIDs []string, limit uint) ([]*feed.Article, error) {
	// 플레이스홀더 (?, ?, ?) 생성
	placeholders := make([]string, len(boardIDs))
	for i := range boardIDs {
		placeholders[i] = "?"
	}

	query := fmt.Sprintf(`
		SELECT a.b_id
		     , b.name AS b_name
		     , a.id
		     , a.title
		     , IFNULL(a.content, "") AS content
		     , a.link
		     , IFNULL(a.author, "") AS author
		     , a.created_date
		  FROM rss_provider_article a
		       INNER JOIN rss_provider_board b ON ( a.p_id = b.p_id AND a.b_id = b.id )
		 WHERE a.p_id = ?
		   AND a.b_id IN (%s)
		 ORDER BY a.created_date DESC
		        , a.rowid DESC
		 LIMIT ?
	`, strings.Join(placeholders, ", "))

	stmt, err := s.db.PrepareContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("GetArticles 쿼리 준비 실패 (providerID: %s): %w", providerID, err)
	}
	defer stmt.Close()

	// 쿼리 파라미터 구성: providerID + boardIDs + maxArticleCount
	args := make([]any, 0, 2+len(boardIDs))
	args = append(args, providerID)
	for _, id := range boardIDs {
		args = append(args, id)
	}
	args = append(args, limit)

	rows, err := stmt.QueryContext(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("GetArticles 쿼리 실행 실패 (providerID: %s): %w", providerID, err)
	}
	defer rows.Close()

	articles := make([]*feed.Article, 0)

	for rows.Next() {
		var createdDate sql.NullTime
		var article feed.Article
		if err = rows.Scan(&article.BoardID, &article.BoardName, &article.ArticleID, &article.Title, &article.Content, &article.Link, &article.Author, &createdDate); err != nil {
			return nil, fmt.Errorf("GetArticles 결과 스캔 실패: %w", err)
		}
		if createdDate.Valid {
			article.CreatedAt = createdDate.Time.Local()
		}

		articles = append(articles, &article)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("GetArticles 결과 행 반복 중 에러 발생: %w", err)
	}

	return articles, nil
}

// @@@@@
// deleteOutOfDateArticles 보관 기간이 지난 게시글을 삭제한다.
//
// noinspection GoUnhandledErrorResult
func (s *Store) deleteOutOfDateArticles(providerID string, archiveDays uint) error {
	stmt, err := s.db.Prepare(fmt.Sprintf(`
		DELETE 
		  FROM rss_provider_article
		 WHERE p_id = ?
		   AND created_date < DATE(DATETIME('now', 'utc'), '-%d days')
	`, archiveDays))
	if err != nil {
		return fmt.Errorf("오래된 게시글 삭제 쿼리 준비 실패 (providerID: %s, days: %d): %w", providerID, archiveDays, err)
	}
	defer stmt.Close()
	if _, err = stmt.Exec(providerID); err != nil {
		return fmt.Errorf("오래된 게시글 삭제 쿼리 실행 실패 (providerID: %s, days: %d): %w", providerID, archiveDays, err)
	}

	return nil
}

// @@@@@
// GetLatestCrawledInfo 마지막으로 크롤링한 게시글 ID와 작성일시를 반환한다.
//
// noinspection GoUnhandledErrorResult,GoSnakeCaseUsage
func (s *Store) GetLatestCrawledInfo(ctx context.Context, providerID, boardID string) (string, time.Time, error) {
	var err error
	var articleID sql.NullString
	var createdDate sql.NullTime

	if boardID == "" {
		err = s.db.QueryRowContext(ctx, `
			SELECT ( SELECT latest_crawled_article_id
					   FROM rss_provider_site_crawled_data
					  WHERE p_id = ?
					    AND b_id = '' )
			     , ( SELECT created_date 
					   FROM rss_provider_article
					  WHERE p_id = ?
					  ORDER BY created_date DESC
					         , rowid DESC
					  LIMIT 1 )
		`, providerID, providerID).Scan(&articleID, &createdDate)
	} else {
		err = s.db.QueryRowContext(ctx, `
			SELECT ( SELECT latest_crawled_article_id
					   FROM rss_provider_site_crawled_data
					  WHERE p_id = ?
					    AND b_id = ? )
			     , ( SELECT created_date 
					   FROM rss_provider_article
					  WHERE p_id = ?
					    AND b_id = ?
					  ORDER BY created_date DESC
					         , rowid DESC
					  LIMIT 1 )
		`, providerID, boardID, providerID, boardID).Scan(&articleID, &createdDate)
	}

	var latestCrawledArticleID string
	var latestCrawledCreatedDate time.Time

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", latestCrawledCreatedDate, fmt.Errorf("최신 크롤링 정보 조회 실패 (providerID: %s, boardID: %s): %w", providerID, boardID, err)
	}

	if articleID.Valid {
		latestCrawledArticleID = articleID.String
	}
	if createdDate.Valid {
		latestCrawledCreatedDate = createdDate.Time.Local()
	}

	return latestCrawledArticleID, latestCrawledCreatedDate, nil
}

// @@@@@
// UpdateLatestCrawledArticleID 마지막으로 크롤링한 게시글 ID를 갱신한다.
//
// noinspection GoUnhandledErrorResult,GoSnakeCaseUsage
func (s *Store) UpdateLatestCrawledArticleID(ctx context.Context, providerID, boardID, articleID string) error {
	stmt, err := s.db.PrepareContext(ctx, `
		INSERT OR REPLACE
		  INTO rss_provider_site_crawled_data (p_id, b_id, latest_crawled_article_id) 
		VALUES (?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("최신 크롤링 게시글 ID 갱신 쿼리 준비 실패 (providerID: %s, boardID: %s): %w", providerID, boardID, err)
	}
	defer stmt.Close()
	if _, err = stmt.ExecContext(ctx, providerID, boardID, articleID); err != nil {
		return fmt.Errorf("최신 크롤링 게시글 ID 갱신 쿼리 실행 실패 (providerID: %s, boardID: %s): %w", providerID, boardID, err)
	}

	return nil
}
