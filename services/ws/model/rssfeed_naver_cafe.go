package model

import (
	"database/sql"
	"github.com/darkkaiser/rss-feed-server/g"
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

	// @@@@@
	rssFeed.init(config)

	return rssFeed
}

// @@@@@
func (f *NaverCafeRSSFeed) init(config *g.AppConfig) interface{} {
	// db만들고 일정기간 시간이 지난 데이터 모두 삭제
	// config에 등록된 카페 정보를 이때 등록???

	f.createTable(config)

	return nil
}

// @@@@@
func (f *NaverCafeRSSFeed) createTable(config *g.AppConfig) error {
	//
	// naver_cafe_info 테이블
	//
	stmt, err := f.db.Prepare(`
		CREATE TABLE IF NOT EXISTS naver_cafe_info (
			id 			VARCHAR( 30) PRIMARY KEY NOT NULL UNIQUE, 
			clubId 		VARCHAR( 30) NOT NULL, 
			name 		VARCHAR(100) NOT NULL, 
			description VARCHAR(200), 
			url 		VARCHAR( 50) NOT NULL
		)
	`)
	if err != nil {
		// @@@@@
		log.Fatal(err.Error())
		return err
	}
	_, err = stmt.Exec()
	stmt.Close()

	stmt, err = f.db.Prepare(`
		CREATE UNIQUE INDEX IF NOT EXISTS naver_cafe_info_index01
						 ON naver_cafe_info(clubId)
	`)
	if err != nil {
		// @@@@@
		log.Fatal(err.Error())
		return err
	}
	_, err = stmt.Exec()

	//
	// naver_cafe_board_info 테이블
	//
	stmt, err = f.db.Prepare(`
		CREATE TABLE IF NOT EXISTS naver_cafe_board_info (
			id 			VARCHAR(  7) PRIMARY KEY NOT NULL UNIQUE,
			name 		VARCHAR(100) NOT NULL,
			cafeId 		VARCHAR( 30) NOT NULL,
			FOREIGN KEY (cafeId) REFERENCES naver_cafe_info(id)
		)
	`)
	if err != nil {
		// @@@@@
		log.Fatal(err.Error())
		return err
	}
	_, err = stmt.Exec()

	// 기초 데이터 추가
	for _, c := range config.RSSFeed.NaverCafes {
		f.AddCafeInfo(c.ID, c.ClubID, c.Name, c.Description, c.Url)

		for _, b := range c.Boards {
			f.AddCafeBoardInfo(b.ID, b.Name, c.ID)
		}
	}

	//
	// naver_cafe_article 테이블
	//

	return nil
}

// @@@@@
func (f *NaverCafeRSSFeed) AddCafeInfo(id, clubId, name, description, url string) error {
	tx, _ := f.db.Begin()
	stmt, _ := tx.Prepare("INSERT OR REPLACE INTO naver_cafe_info (id, clubId, name, description, url) values (?, ?, ?, ?, ?)")
	_, err := stmt.Exec(id, clubId, name, description, url)
	if err != nil {
		tx.Rollback()
		log.Println(err.Error())
		return err
	}
	tx.Commit()

	return nil
}

// @@@@@
func (f *NaverCafeRSSFeed) AddCafeBoardInfo(id, name, cafeId string) error {
	tx, _ := f.db.Begin()
	stmt, _ := tx.Prepare("INSERT OR REPLACE INTO naver_cafe_board_info (id, name, cafeId) values (?, ?, ?)")
	_, err := stmt.Exec(id, name, cafeId)
	if err != nil {
		log.Println(err.Error())
		return err
	}
	tx.Commit()

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
