package model

import (
	"database/sql"
	"fmt"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
	"time"
)

const (
	NaverCafeModel ModelType = "naver_cafe_model"

	NaverCafeHomeUrl = "https://cafe.naver.com"
)

type NaverCafeArticle struct {
	BoardID   string
	BoardName string
	ArticleID int64
	Title     string
	Content   string
	Link      string
	Author    string
	CreatedAt time.Time
}

func (a NaverCafeArticle) String() string {
	return fmt.Sprintf("[%s, %s, %d, %s, %s, %s, %s, %s]", a.BoardID, a.BoardName, a.ArticleID, a.Title, a.Content, a.Link, a.Author, a.CreatedAt.Format("2006-10-02 15:04:05"))
}

type NaverCafe struct {
	db *sql.DB
}

func NewNaverCafe(config *g.AppConfig, db *sql.DB) *NaverCafe {
	nc := &NaverCafe{
		db: db,
	}

	if err := nc.init(config); err != nil {
		m := "네이버 카페 DB를 초기화하는 중에 치명적인 오류가 발생하였습니다."

		notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)

		log.Panic(fmt.Sprintf("%s (error:%s)", m, err))
	}

	return nc
}

func (nc *NaverCafe) init(config *g.AppConfig) error {
	if err := nc.createTables(); err != nil {
		return err
	}

	for _, c := range config.RSSFeed.NaverCafes {
		// 기초 데이터를 추가한다.
		if err := nc.insertNaverCafeInfo(c.ID, c.ClubID, c.Name, c.Description, fmt.Sprintf("%s/%s", NaverCafeHomeUrl, c.ID)); err != nil {
			return err
		}

		for _, b := range c.Boards {
			if err := nc.insertNaverCafeBoardInfo(c.ID, b.ID, b.Name); err != nil {
				return err
			}
		}

		// 일정 시간이 지난 게시글 자료를 모두 삭제한다.
		if err := nc.deleteOutOfDateArticles(c.ID, c.ArticleArchiveDate); err != nil {
			return err
		}
	}

	return nil
}

//noinspection GoUnhandledErrorResult
func (nc *NaverCafe) createTables() error {
	//
	// naver_cafe_info 테이블
	//
	stmt1, err := nc.db.Prepare(`
		CREATE TABLE IF NOT EXISTS naver_cafe_info (
			cafeId 		VARCHAR( 30) PRIMARY KEY NOT NULL UNIQUE,
			clubId 		VARCHAR( 30) NOT NULL,
			name 		VARCHAR(130) NOT NULL,
			description VARCHAR(200),
			url 		VARCHAR( 50) NOT NULL
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

//noinspection GoUnhandledErrorResult
func (nc *NaverCafe) insertNaverCafeInfo(cafeId, clubId, name, description, url string) error {
	stmt, err := nc.db.Prepare("INSERT OR REPLACE INTO naver_cafe_info (cafeId, clubId, name, description, url) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	if _, err = stmt.Exec(cafeId, clubId, name, description, url); err != nil {
		return err
	}

	return nil
}

//noinspection GoUnhandledErrorResult
func (nc *NaverCafe) insertNaverCafeBoardInfo(cafeId, boardId, name string) error {
	stmt, err := nc.db.Prepare("INSERT OR REPLACE INTO naver_cafe_board_info (cafeId, boardId, name) VALUES (?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	if _, err = stmt.Exec(cafeId, boardId, name); err != nil {
		return err
	}

	return nil
}

//noinspection GoUnhandledErrorResult
func (nc *NaverCafe) GetLatestArticleID(cafeId string) (int64, error) {
	var articleId int64
	err := nc.db.QueryRow(`
		SELECT IFNULL(MAX(articleId), 0)
		  FROM naver_cafe_article
		 WHERE cafeId = ?
	`, cafeId).Scan(&articleId)

	if err != nil {
		return 0, err
	}

	return articleId, nil
}

//noinspection GoUnhandledErrorResult
func (nc *NaverCafe) InsertArticles(cafeId string, articles []*NaverCafeArticle) (int64, error) {
	stmt, err := nc.db.Prepare(`
		INSERT OR REPLACE
		  INTO naver_cafe_article (cafeId, boardId, articleId, title, content, link, author, createdAt)
	    VALUES (?, ?, ?, ?, ?, ?, ?, datetime(?))
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	var insertedCnt int64
	var sentNotification = false
	for _, article := range articles {
		if _, err := stmt.Exec(cafeId, article.BoardID, article.ArticleID, article.Title, article.Content, article.Link, article.Author, article.CreatedAt.Format("2006-10-02 15:04:05")); err != nil {
			m := fmt.Sprintf("네이버 카페('%s > %s')의 게시글 등록이 실패하였습니다.", cafeId, article.BoardName)

			log.Errorf("%s (게시글정보:%s) (error:%s)", m, article, err)

			// 너무 많은 알림 메시지가 발송될 수 있으므로, 동시에 입력되는 게시글 중 최초 오류건에 대해서만 알림 메시지를 보낸다.
			if sentNotification == false {
				sentNotification = true
				notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)
			}
		} else {
			insertedCnt += 1
		}
	}

	return insertedCnt, nil
}

//noinspection GoUnhandledErrorResult
func (nc *NaverCafe) GetArticles(cafeId string, maxArticleCount uint) ([]*NaverCafeArticle, error) {
	stmt, err := nc.db.Prepare(`
		SELECT boardId
		     , articleId
		     , title
		     , IFNULL(content, "")
		     , link
		     , IFNULL(author, "")
		     , createdAt
		  FROM naver_cafe_article
		 WHERE cafeId = ?
      ORDER BY articleId DESC
         LIMIT ?
	`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	rows, err := stmt.Query(cafeId, maxArticleCount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	articles := make([]*NaverCafeArticle, 0)

	for rows.Next() {
		var article NaverCafeArticle

		var createdAt sql.NullTime
		if err = rows.Scan(&article.BoardID, &article.ArticleID, &article.Title, &article.Content, &article.Link, &article.Author, &createdAt); err != nil {
			return nil, err
		}
		if createdAt.Valid == true {
			article.CreatedAt = createdAt.Time
		}

		articles = append(articles, &article)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return articles, nil
}

//noinspection GoUnhandledErrorResult
func (nc *NaverCafe) deleteOutOfDateArticles(cafeId string, articleArchiveDate uint) error {
	stmt, err := nc.db.Prepare(fmt.Sprintf(`
		DELETE 
		  FROM naver_cafe_article
		 WHERE cafeId = ?
		   AND createdAt < date('now', '-%d days')
	`, articleArchiveDate))
	if err != nil {
		return err
	}
	defer stmt.Close()
	if _, err = stmt.Exec(cafeId); err != nil {
		return err
	}

	return nil
}
