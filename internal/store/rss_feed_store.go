package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/model"
	_ "github.com/mattn/go-sqlite3"
)

// RSSFeedStore RSS Feed Provider의 DB 접근을 담당하는 RSSFeedStore
type RSSFeedStore struct {
	db *sql.DB
}

// NewRSSFeed Store를 초기화하여 반환한다.
func NewRSSFeed(config *config.AppConfig, db *sql.DB) (*RSSFeedStore, error) {
	s := &RSSFeedStore{
		db: db,
	}

	return s, nil
}

// Initialize RSS Feed DB를 초기화(테이블 생성 및 기초 데이터 추가)한다.
func (s *RSSFeedStore) Initialize(cfg *config.AppConfig) error {
	if err := s.createTables(); err != nil {
		return err
	}

	for _, c := range cfg.RssFeed.Providers {
		// 기초 데이터를 추가한다.
		if err := s.insertRSSFeedProvider(c.ID, c.Site, c.Config.ID, c.Config.Name, c.Config.Description, c.Config.URL); err != nil {
			return err
		}

		for _, b := range c.Config.Boards {
			if err := s.insertRSSFeedProviderBoard(c.ID, b.ID, b.Name); err != nil {
				return err
			}
		}

		// 일정 시간이 지난 게시글 자료를 모두 삭제한다.
		if err := s.deleteOutOfDateArticles(c.ID, c.Config.ArticleArchiveDate); err != nil {
			return err
		}
	}

	return nil
}

// noinspection GoUnhandledErrorResult
func (s *RSSFeedStore) createTables() error {
	//
	// rss_provider 테이블
	//
	stmt1, err := s.db.Prepare(`
		CREATE TABLE IF NOT EXISTS rss_provider (
			id 					VARCHAR( 50) PRIMARY KEY NOT NULL UNIQUE,
			site 				VARCHAR( 50) NOT NULL,
			s_id 				VARCHAR( 50) NOT NULL,
			s_name 				VARCHAR(130) NOT NULL,
			s_description 		VARCHAR(200),
			s_url 				VARCHAR(100) NOT NULL
		)
	`)
	if err != nil {
		return err
	}
	defer stmt1.Close()
	if _, err = stmt1.Exec(); err != nil {
		return err
	}

	stmt2, err := s.db.Prepare(`
		CREATE INDEX IF NOT EXISTS rss_provider_index01 ON rss_provider(s_id)
	`)
	if err != nil {
		return err
	}
	defer stmt2.Close()
	if _, err = stmt2.Exec(); err != nil {
		return err
	}

	//
	// rss_provider_board 테이블
	//
	stmt3, err := s.db.Prepare(`
		CREATE TABLE IF NOT EXISTS rss_provider_board (
			p_id 		VARCHAR( 50) NOT NULL,
			id			VARCHAR( 50) NOT NULL,
			name 		VARCHAR(130) NOT NULL,
			PRIMARY KEY (p_id, id)
			FOREIGN KEY (p_id) REFERENCES rss_provider(id)
		)
	`)
	if err != nil {
		return err
	}
	defer stmt3.Close()
	if _, err = stmt3.Exec(); err != nil {
		return err
	}

	stmt4, err := s.db.Prepare(`
		CREATE INDEX IF NOT EXISTS rss_provider_board_index01 ON rss_provider_board(p_id)
	`)
	if err != nil {
		return err
	}
	defer stmt4.Close()
	if _, err = stmt4.Exec(); err != nil {
		return err
	}

	//
	// rss_provider_article 테이블
	//
	stmt5, err := s.db.Prepare(`
		CREATE TABLE IF NOT EXISTS rss_provider_article (
			p_id 			VARCHAR( 50) NOT NULL,
			b_id 			VARCHAR( 50) NOT NULL,
			id 				VARCHAR( 50) NOT NULL,
			title 			VARCHAR(400) NOT NULL,
			content			TEXT,
			link 			VARCHAR(1000) NOT NULL,
			author 			VARCHAR(50),
			created_date	DATETIME,
			PRIMARY KEY (p_id, b_id, id)
			FOREIGN KEY (p_id) REFERENCES rss_provider(id)
			FOREIGN KEY (b_id) REFERENCES rss_provider_board(id)
		)
	`)
	if err != nil {
		return err
	}
	defer stmt5.Close()
	if _, err = stmt5.Exec(); err != nil {
		return err
	}

	stmt6, err := s.db.Prepare(`
		CREATE INDEX IF NOT EXISTS rss_provider_article_index01 ON rss_provider_article(created_date)
	`)
	if err != nil {
		return err
	}
	defer stmt6.Close()
	if _, err = stmt6.Exec(); err != nil {
		return err
	}

	//
	// rss_provider_site_crawled_data 테이블
	//
	stmt7, err := s.db.Prepare(`
		CREATE TABLE IF NOT EXISTS rss_provider_site_crawled_data (
			p_id 						VARCHAR( 50) NOT NULL,
			b_id 						VARCHAR( 50) NOT NULL,
			latest_crawled_article_id	VARCHAR( 50) NOT NULL,
			PRIMARY KEY (p_id, b_id)
			FOREIGN KEY (p_id) REFERENCES rss_provider(id)
		)
	`)
	if err != nil {
		return err
	}
	defer stmt7.Close()
	if _, err = stmt7.Exec(); err != nil {
		return err
	}

	return nil
}

// noinspection GoUnhandledErrorResult
func (s *RSSFeedStore) insertRSSFeedProvider(providerID, site, sourceID, sourceName, sourceDescription, sourceURL string) error {
	stmt, err := s.db.Prepare(`
		INSERT OR REPLACE
		  INTO rss_provider (id, site, s_id, s_name, s_description, s_url) 
	    VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	if _, err = stmt.Exec(providerID, site, sourceID, sourceName, sourceDescription, sourceURL); err != nil {
		return err
	}

	return nil
}

// noinspection GoUnhandledErrorResult
func (s *RSSFeedStore) insertRSSFeedProviderBoard(providerID, boardID, boardName string) error {
	stmt, err := s.db.Prepare("INSERT OR REPLACE INTO rss_provider_board (p_id, id, name) VALUES (?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	if _, err = stmt.Exec(providerID, boardID, boardName); err != nil {
		return err
	}

	return nil
}

// InsertArticles 게시글 목록을 DB에 추가하고 실제로 삽입된 건수를 반환한다.
// 개별 행 삽입 실패는 건너뛰고 계속 진행하며, 실패한 행은 에러 로그로 기록된다.
//
// noinspection GoUnhandledErrorResult
func (s *RSSFeedStore) InsertArticles(providerID string, articles []*model.Article) (int, error) {
	stmt, err := s.db.Prepare(`
		INSERT OR REPLACE
		  INTO rss_provider_article (p_id, b_id, id, title, content, link, author, created_date)
	    VALUES (?, ?, ?, ?, ?, ?, ?, datetime(?))
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	var insertedCnt int
	for _, article := range articles {
		if _, err := stmt.Exec(providerID, article.BoardID, article.ArticleID, article.Title, article.Content, article.Link, article.Author, article.CreatedAt.UTC().Format("2006-01-02 15:04:05")); err != nil {
			applog.Errorf("RSS Feed DB에 게시글 등록이 실패하였습니다. (p_id:%s) (게시글정보:%s) (error:%s)", providerID, article, err)
			// @@@@@ 알림은 삭제했음.
			// 너무 많은 알림 메시지가 발송될 수 있으므로, 동시에 입력되는 게시글 중 최초 오류건에 대해서만 알림 메시지를 보낸다.
			// if sentNotifyMessage == false && p.notifyClient != nil {
			// 	sentNotifyMessage = true
			// 	_ = p.notifyClient.NotifyError(context.Background(), fmt.Sprintf("%s\r\n\r\n%s", m, err))
			// }
		} else {
			insertedCnt++
		}
	}

	return insertedCnt, nil
}

// GetArticles 지정한 provider/board의 게시글 목록을 최신순으로 반환한다.
//
// noinspection GoUnhandledErrorResult
func (s *RSSFeedStore) GetArticles(providerID string, boardIDs []string, maxArticleCount uint) ([]*model.Article, error) {
	// 플레이스홀더 (?, ?, ?) 생성
	placeholders := make([]string, len(boardIDs))
	for i := range boardIDs {
		placeholders[i] = "?"
	}

	query := fmt.Sprintf(`
		SELECT a.b_id
             , b.name b_name
		     , a.id
		     , a.title
		     , IFNULL(a.content, "") content
		     , a.link
		     , IFNULL(a.author, "") author
		     , a.created_date
		  FROM rss_provider_article a
               INNER JOIN rss_provider_board b ON ( a.p_id = b.p_id AND a.b_id = b.id )
		 WHERE a.p_id = ?
		   AND a.b_id IN (%s)
      ORDER BY a.created_date DESC
             , a.rowid DESC
         LIMIT ?
	`, strings.Join(placeholders, ", "))

	stmt, err := s.db.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	// 쿼리 파라미터 구성: providerID + boardIDs + maxArticleCount
	args := make([]any, 0, 2+len(boardIDs))
	args = append(args, providerID)
	for _, id := range boardIDs {
		args = append(args, id)
	}
	args = append(args, maxArticleCount)

	rows, err := stmt.Query(args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	articles := make([]*model.Article, 0)

	for rows.Next() {
		var createdDate sql.NullTime
		var article model.Article
		if err = rows.Scan(&article.BoardID, &article.BoardName, &article.ArticleID, &article.Title, &article.Content, &article.Link, &article.Author, &createdDate); err != nil {
			return nil, err
		}
		if createdDate.Valid {
			article.CreatedAt = createdDate.Time.Local()
		}

		articles = append(articles, &article)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return articles, nil
}

// deleteOutOfDateArticles 보관 기간이 지난 게시글을 삭제한다.
//
// noinspection GoUnhandledErrorResult
func (s *RSSFeedStore) deleteOutOfDateArticles(providerID string, archiveDays uint) error {
	stmt, err := s.db.Prepare(fmt.Sprintf(`
		DELETE 
		  FROM rss_provider_article
		 WHERE p_id = ?
		   AND created_date < date(datetime('now', 'utc'), '-%d days')
	`, archiveDays))
	if err != nil {
		return err
	}
	defer stmt.Close()
	if _, err = stmt.Exec(providerID); err != nil {
		return err
	}

	return nil
}

// LatestCrawledInfo 마지막으로 크롤링한 게시글 ID와 작성일시를 반환한다.
//
// noinspection GoUnhandledErrorResult,GoSnakeCaseUsage
func (s *RSSFeedStore) LatestCrawledInfo(providerID, boardID string) (string, time.Time, error) {
	var err error
	var articleID sql.NullString
	var createdDate sql.NullTime

	if boardID == "" {
		err = s.db.QueryRow(`
			 SELECT ( SELECT latest_crawled_article_id
					  	FROM rss_provider_site_crawled_data
					   WHERE p_id = ?
						AND b_id = '' ),
					( SELECT created_date 
						FROM rss_provider_article
					   WHERE p_id = ?
					ORDER BY created_date DESC
					   		, rowid DESC
					   LIMIT 1 )
		`, providerID, providerID).Scan(&articleID, &createdDate)
	} else {
		err = s.db.QueryRow(`
			 SELECT ( SELECT latest_crawled_article_id
					  	FROM rss_provider_site_crawled_data
					   WHERE p_id = ?
						AND b_id = ? ),
					( SELECT created_date 
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
		return "", latestCrawledCreatedDate, err
	}

	if articleID.Valid {
		latestCrawledArticleID = articleID.String
	}
	if createdDate.Valid {
		latestCrawledCreatedDate = createdDate.Time.Local()
	}

	return latestCrawledArticleID, latestCrawledCreatedDate, nil
}

// UpdateLatestCrawledArticleID 마지막으로 크롤링한 게시글 ID를 갱신한다.
//
// noinspection GoUnhandledErrorResult,GoSnakeCaseUsage
func (s *RSSFeedStore) UpdateLatestCrawledArticleID(providerID, boardID, latestCrawledArticleID string) error {
	stmt, err := s.db.Prepare("INSERT OR REPLACE INTO rss_provider_site_crawled_data (p_id, b_id, latest_crawled_article_id) VALUES (?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	if _, err = stmt.Exec(providerID, boardID, latestCrawledArticleID); err != nil {
		return err
	}

	return nil
}
