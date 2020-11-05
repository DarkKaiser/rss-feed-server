package model

import (
	"database/sql"
	"fmt"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
	"strings"
	"time"
)

// @@@@@
const (
	RssProviderModel ModelType = "rss_provider_model"

	RssProviderSupportedTypeNaverCafe = "naver_cafe"
)

// @@@@@ RssProviderPosts
type RssProviderPosts struct {
	BoardID   string
	BoardName string
	PostsID   string
	Title     string
	Content   string
	Link      string
	Author    string
	CreatedAt time.Time
}

// @@@@@
func (p RssProviderPosts) String() string {
	return fmt.Sprintf("[%s, %s, %s, %s, %s, %s, %s, %s]", p.BoardID, p.BoardName, p.PostsID, p.Title, p.Content, p.Link, p.Author, p.CreatedAt.Format("2006-10-02 15:04:05"))
}

// @@@@@
type RssProvider struct {
	db *sql.DB
}

// @@@@@
func NewRssProvider(config *g.AppConfig, db *sql.DB) *RssProvider {
	nc := &RssProvider{
		db: db,
	}

	if err := nc.init(config); err != nil {
		m := "네이버 카페 DB를 초기화하는 중에 치명적인 오류가 발생하였습니다."

		notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)

		log.Panicf("%s (error:%s)", m, err)
	}

	return nc
}

// @@@@@
func (nc *RssProvider) init(config *g.AppConfig) error {
	if err := nc.createTables(); err != nil {
		return err
	}

	for _, c := range config.RssFeed.Providers {
		// 기초 데이터를 추가한다.
		if err := nc.insertNaverCafeInfo(c.ID, c.Name, c.Description, c.Url); err != nil {
			return err
		}

		for _, b := range c.Boards {
			if err := nc.insertNaverCafeBoardInfo(c.ID, b.ID, b.Name); err != nil {
				return err
			}
		}

		// 일정 시간이 지난 게시글 자료를 모두 삭제한다.
		if err := nc.deleteOutOfDatePosts(c.ID, c.PostsArchiveDate); err != nil {
			return err
		}
	}

	return nil
}

// @@@@@
//noinspection GoUnhandledErrorResult
func (nc *RssProvider) createTables() error {
	//
	// naver_cafe_info 테이블
	//
	stmt1, err := nc.db.Prepare(`
		CREATE TABLE IF NOT EXISTS naver_cafe_info (
			cafeId 					VARCHAR( 30) PRIMARY KEY NOT NULL UNIQUE,
			clubId 					VARCHAR( 30) NOT NULL,
			name 					VARCHAR(130) NOT NULL,
			description 			VARCHAR(200),
			url 					VARCHAR( 50) NOT NULL,
			crawledLatestArticleId	INTEGER DEFAULT 0
		)
	`)
	if err != nil {
		return err
	}
	defer stmt1.Close()
	if _, err = stmt1.Exec(); err != nil {
		return err
	}

	stmt2, err := nc.db.Prepare(`
		CREATE INDEX IF NOT EXISTS naver_cafe_info_index01 ON naver_cafe_info(clubId)
	`)
	if err != nil {
		return err
	}
	defer stmt2.Close()
	if _, err = stmt2.Exec(); err != nil {
		return err
	}

	//
	// naver_cafe_board_info 테이블
	//
	stmt3, err := nc.db.Prepare(`
		CREATE TABLE IF NOT EXISTS naver_cafe_board_info (
			cafeId 		VARCHAR( 30) NOT NULL,
			boardId		VARCHAR(  5) PRIMARY KEY NOT NULL UNIQUE,
			name 		VARCHAR(130) NOT NULL,
			FOREIGN KEY (cafeId) REFERENCES naver_cafe_info(cafeId)
		)
	`)
	if err != nil {
		return err
	}
	defer stmt3.Close()
	if _, err = stmt3.Exec(); err != nil {
		return err
	}

	stmt4, err := nc.db.Prepare(`
		CREATE INDEX IF NOT EXISTS naver_cafe_board_info_index01 ON naver_cafe_board_info(cafeId)
	`)
	if err != nil {
		return err
	}
	defer stmt4.Close()
	if _, err = stmt4.Exec(); err != nil {
		return err
	}

	//
	// naver_cafe_article 테이블
	//
	stmt5, err := nc.db.Prepare(`
		CREATE TABLE IF NOT EXISTS naver_cafe_article (
			cafeId 		VARCHAR( 30) NOT NULL,
			boardId 	VARCHAR(  5) NOT NULL,
			articleId 	INTEGER NOT NULL,
			title 		VARCHAR(400) NOT NULL,
			content		TEXT,
			link 		VARCHAR(1000) NOT NULL,
			author 		VARCHAR(50),
			createdAt	DATETIME,
			PRIMARY KEY (cafeId, boardId, articleId)
			FOREIGN KEY (cafeId) REFERENCES naver_cafe_info(cafeId)
			FOREIGN KEY (boardId) REFERENCES naver_cafe_board_info(boardId)
		)
	`)
	if err != nil {
		return err
	}
	defer stmt5.Close()
	if _, err = stmt5.Exec(); err != nil {
		return err
	}

	stmt6, err := nc.db.Prepare(`
		CREATE INDEX IF NOT EXISTS naver_cafe_article_index01 ON naver_cafe_article(createdAt)
	`)
	if err != nil {
		return err
	}
	defer stmt6.Close()
	if _, err = stmt6.Exec(); err != nil {
		return err
	}

	return nil
}

// @@@@@
//noinspection GoUnhandledErrorResult
func (nc *RssProvider) insertNaverCafeInfo(cafeID, name, description, url string) error {
	stmt, err := nc.db.Prepare(`
		INSERT OR REPLACE
		  INTO naver_cafe_info (cafeId, clubId, name, description, url, crawledLatestArticleId) 
	    VALUES (?, ?, ?, ?, ?, ( SELECT crawledLatestArticleId
	                               FROM naver_cafe_info
	                              WHERE cafeId = ? ) )
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	if _, err = stmt.Exec(cafeID, "@@@@@제거", name, description, url, cafeID); err != nil {
		return err
	}

	return nil
}

// @@@@@
//noinspection GoUnhandledErrorResult
func (nc *RssProvider) insertNaverCafeBoardInfo(cafeID, boardID, name string) error {
	stmt, err := nc.db.Prepare("INSERT OR REPLACE INTO naver_cafe_board_info (cafeId, boardId, name) VALUES (?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	if _, err = stmt.Exec(cafeID, boardID, name); err != nil {
		return err
	}

	return nil
}

// @@@@@
//noinspection GoUnhandledErrorResult
func (nc *RssProvider) CrawledLatestArticleID(cafeID string) (int64, error) {
	var crawledLatestArticleID int64
	err := nc.db.QueryRow(`
		SELECT MAX(articleId) 
		  FROM (    SELECT IFNULL(crawledLatestArticleId, 0) articleId
		              FROM naver_cafe_info
		             WHERE cafeId = ?
                 UNION ALL
                    SELECT IFNULL(MAX(articleId), 0) articleId
		              FROM naver_cafe_article
		             WHERE cafeId = ?
		       )
	`, cafeID, cafeID).Scan(&crawledLatestArticleID)

	if err != nil {
		return 0, err
	}

	return crawledLatestArticleID, nil
}

// @@@@@
//noinspection GoUnhandledErrorResult
func (nc *RssProvider) UpdateCrawledLatestArticleID(cafeID string, articleID int64) error {
	stmt, err := nc.db.Prepare("UPDATE naver_cafe_info SET crawledLatestArticleId = ? WHERE cafeId = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()
	if _, err = stmt.Exec(articleID, cafeID); err != nil {
		return err
	}

	return nil
}

// @@@@@
//noinspection GoUnhandledErrorResult
func (nc *RssProvider) InsertArticles(cafeID string, articles []*RssProviderPosts) (int, error) {
	stmt, err := nc.db.Prepare(`
		INSERT OR REPLACE
		  INTO naver_cafe_article (cafeId, boardId, articleId, title, content, link, author, createdAt)
	    VALUES (?, ?, ?, ?, ?, ?, ?, datetime(?))
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	var insertedCnt int
	var sentNotifyMessage = false
	for _, article := range articles {
		if _, err := stmt.Exec(cafeID, article.BoardID, article.ArticleID, article.Title, article.Content, article.Link, article.Author, article.CreatedAt.UTC().Format("2006-01-02 15:04:05")); err != nil {
			m := fmt.Sprintf("네이버 카페('%s > %s')의 게시글 등록이 실패하였습니다.", cafeID, article.BoardName)

			log.Errorf("%s (게시글정보:%s) (error:%s)", m, article, err)

			// 너무 많은 알림 메시지가 발송될 수 있으므로, 동시에 입력되는 게시글 중 최초 오류건에 대해서만 알림 메시지를 보낸다.
			if sentNotifyMessage == false {
				sentNotifyMessage = true
				notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)
			}
		} else {
			insertedCnt += 1
		}
	}

	return insertedCnt, nil
}

// @@@@@
//noinspection GoUnhandledErrorResult
func (nc *RssProvider) Articles(cafeID string, boardIDs []string, maxArticleCount uint) ([]*RssProviderPosts, error) {
	stmt, err := nc.db.Prepare(fmt.Sprintf(`
		SELECT a.boardId
             , b.name boardName
		     , a.articleId
		     , a.title
		     , IFNULL(a.content, "") content
		     , a.link
		     , IFNULL(a.author, "") author
		     , a.createdAt
		  FROM naver_cafe_article a
               INNER JOIN naver_cafe_board_info b ON ( a.cafeId = b.cafeId AND a.boardId = b.boardId )
		 WHERE a.cafeId = ?
		   AND a.boardId IN (%s)
      ORDER BY a.articleId DESC
         LIMIT ?
	`, fmt.Sprintf("'%s'", strings.Join(boardIDs, "', '"))))
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	rows, err := stmt.Query(cafeID, maxArticleCount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	articles := make([]*RssProviderPosts, 0)

	for rows.Next() {
		var article RssProviderPosts

		var createdAt sql.NullTime
		if err = rows.Scan(&article.BoardID, &article.BoardName, &article.ArticleID, &article.Title, &article.Content, &article.Link, &article.Author, &createdAt); err != nil {
			return nil, err
		}
		if createdAt.Valid == true {
			article.CreatedAt = createdAt.Time.Local()
		}

		articles = append(articles, &article)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return articles, nil
}

// @@@@@
//noinspection GoUnhandledErrorResult
func (nc *RssProvider) deleteOutOfDatePosts(cafeID string, postsArchiveDate uint) error {
	stmt, err := nc.db.Prepare(fmt.Sprintf(`
		DELETE 
		  FROM naver_cafe_article
		 WHERE cafeId = ?
		   AND createdAt < date(datetime('now', 'utc'), '-%d days')
	`, postsArchiveDate))
	if err != nil {
		return err
	}
	defer stmt.Close()
	if _, err = stmt.Exec(cafeID); err != nil {
		return err
	}

	return nil
}
