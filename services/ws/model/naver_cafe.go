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

const NaverCafeModel ModelType = "naver_cafe_model"

// @@@@@
type NaverCafeArticle struct {
	BoardID   string
	ArticleID int
	Title     string
	Content   string
	Link      string
	Author    string
	CreatedAt *time.Time
}

type NaverCafe struct {
	db *sql.DB
}

func NewNaverCafe(config *g.AppConfig, db *sql.DB) *NaverCafe {
	nc := &NaverCafe{
		db: db,
	}

	if err := nc.init(config); err != nil {
		m := fmt.Sprintf("네이버 카페 관련 DB를 초기화하는 중에 치명적인 오류가 발생하였습니다.\r\n\r\n%s", err)

		notifyapi.SendNotifyMessage(m, true)

		log.Panic(m)
	}

	return nc
}

func (nc *NaverCafe) init(config *g.AppConfig) error {
	if err := nc.createTables(); err != nil {
		return err
	}

	for _, c := range config.RSSFeed.NaverCafes {
		// 기초 데이터를 추가한다.
		if err := nc.insertNaverCafeInfo(c.ID, c.ClubID, c.Name, c.Description, c.Url); err != nil {
			return err
		}

		for _, b := range c.Boards {
			if err := nc.insertNaverCafeBoardInfo(c.ID, b.ID, b.Name); err != nil {
				return err
			}
		}

		// 일정 시간이 지난 게시물 자료를 모두 삭제한다.
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
func (nc *NaverCafe) GetLatestArticleID(cafeId string) (int, error) {
	var articleId int
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
func (nc *NaverCafe) InsertArticles(cafeId string, articles []*NaverCafeArticle) (int, error) {
	// @@@@@ datetime yyyy-MM-dd HH:mm:ss
	// insert into naver_cafe_article2 (cafeId, boardId, id, createdAt) values ('a', 'a', 6, datetime('2020-12-10 21:44:59'))
	stmt, err := nc.db.Prepare(`
		INSERT OR REPLACE INTO naver_cafe_article (
		            		   cafeId
		            		 , boardId
		            		 , articleId
		          			 , title
		          			 , link
		          			 , author
		          			 , createdAt
		          			 )
	         		    VALUES (
		            		   ?
		          			 , ?
		          			 , ?
		          			 , ?
		          			 , ?
		          			 , ?
		          			 , datetime('now')
		          			 )
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	count := 0
	for _, article := range articles {
		if result, err := stmt.Exec(cafeId, article.BoardID, article.ArticleID, article.Title, article.Link, article.Author); err != nil {
			// @@@@@
			m := fmt.Sprintf("ddd")

			log.Error(m)

			notifyapi.SendNotifyMessage(m, true)
		} else {
			n, err := result.RowsAffected()
			if err != nil {

			}
			if n == 1 {
				// @@@@@
				count += 1
			}
		}
	}

	return count, nil
}

// @@@@@
func (nc *NaverCafe) GetArticles(cafeId string) []*NaverCafeArticle {
	rows, err := nc.db.Query(fmt.Sprintf("SELECT boardId, articleId, title, IFNULL(content, ''), link, author, createdAt FROM naver_cafe_article WHERE cafeId = '%s'", cafeId))
	if err != nil {
		panic(err)
	}

	var articleId int
	var boardId, title, content, link, author string
	var dt *time.Time

	var articles []*NaverCafeArticle
	for rows.Next() {
		err = rows.Scan(&boardId, &articleId, &title, &content, &link, &author, &dt)
		if err != nil {
			panic(err)
		}

		article := &NaverCafeArticle{
			BoardID:   boardId,
			ArticleID: articleId,
			Title:     title,
			Content:   content,
			Link:      link,
			Author:    author,
			CreatedAt: dt,
		}

		articles = append(articles, article)
	}
	err = rows.Err()
	if err != nil {

	}
	rows.Close() //good habit to close

	return articles
}

func (nc *NaverCafe) deleteOutOfDateArticles(cafeId string, articleArchiveDate int) error {
	// @@@@@
	return nil
}
