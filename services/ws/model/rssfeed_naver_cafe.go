package model

import (
	"database/sql"
	"fmt"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
)

const NaverCafeRSSFeedModel ModelType = "naver_cafe_rss_feed"

type NaverCafeRSSFeed struct {
	db *sql.DB
}

func NewNaverCafeRSSFeed(config *g.AppConfig, db *sql.DB) *NaverCafeRSSFeed {
	rssFeed := &NaverCafeRSSFeed{
		db: db,
	}

	if err := rssFeed.init(config); err != nil {
		m := fmt.Sprintf("네이버 카페 관련 DB를 초기화하는 중에 치명적인 오류가 발생하였습니다.\r\n\r\n%s", err)

		notifyapi.SendNotifyMessage(m, true)

		log.Panic(m)
	}

	return rssFeed
}

func (f *NaverCafeRSSFeed) init(config *g.AppConfig) error {
	if err := f.createTables(); err != nil {
		return err
	}

	// 기초 데이터를 추가한다.
	for _, c := range config.RSSFeed.NaverCafes {
		if err := f.insertNaverCafeInfo(c.ID, c.ClubID, c.Name, c.Description, c.Url); err != nil {
			return err
		}

		for _, b := range c.Boards {
			if err := f.insertNaverCafeBoardInfo(c.ID, b.ID, b.Name); err != nil {
				return err
			}
		}
	}

	// 일정 시간이 지난 게시물 자료를 모두 삭제한다.@@@@@ 10
	if err := f.deleteNaverCafeArticle(10); err != nil {
		return err
	}

	return nil
}

//noinspection GoUnhandledErrorResult
func (f *NaverCafeRSSFeed) createTables() error {
	//
	// naver_cafe_info 테이블
	//
	stmt1, err := f.db.Prepare(`
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

	stmt2, err := f.db.Prepare(`
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
	stmt3, err := f.db.Prepare(`
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

	stmt4, err := f.db.Prepare(`
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
	stmt5, err := f.db.Prepare(`
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

	stmt6, err := f.db.Prepare(`
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
func (f *NaverCafeRSSFeed) insertNaverCafeInfo(cafeId, clubId, name, description, url string) error {
	stmt, err := f.db.Prepare("INSERT OR REPLACE INTO naver_cafe_info (cafeId, clubId, name, description, url) values (?, ?, ?, ?, ?)")
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
func (f *NaverCafeRSSFeed) insertNaverCafeBoardInfo(cafeId, boardId, name string) error {
	stmt, err := f.db.Prepare("INSERT OR REPLACE INTO naver_cafe_board_info (cafeId, boardId, name) values (?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	if _, err = stmt.Exec(cafeId, boardId, name); err != nil {
		return err
	}

	return nil
}

func (f *NaverCafeRSSFeed) deleteNaverCafeArticle(checkDaysAgo int) error {
	// @@@@@ cleanOutOfLogFiles
	return nil
}

// @@@@@
func (f *NaverCafeRSSFeed) LatestArticleID() interface{} {
	return 0
}

// @@@@@
func (f *NaverCafeRSSFeed) AddArticle() interface{} {
	return 0
}

// @@@@@
func (f *NaverCafeRSSFeed) GetArticles() interface{} {
	return 0
}
