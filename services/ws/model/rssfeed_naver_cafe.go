package model

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
)

const NaverCafeRSSFeedModel ModelType = "naver_cafe_rss_feed"

type NaverCafeRSSFeed struct {
	db *sql.DB
}

func NewNaverCafeRSSFeed(db *sql.DB) *NaverCafeRSSFeed {
	rssFeed := &NaverCafeRSSFeed{
		db: db,
	}

	// @@@@@
	rssFeed.init()

	return rssFeed
}

// @@@@@
func (f *NaverCafeRSSFeed) init() interface{} {
	// db만들고 일정기간 시간이 지난 데이터 모두 삭제
	// config에 등록된 카페 정보를 이때 등록???
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

// @@@@@
func InitDB(file string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", file)
	if err != nil {
		return nil, err
	}

	createTableQuery := `
			create table IF NOT EXISTS useraccount (
			id integer PRIMARY KEY autoincrement,
			userId text,
			password text,
			UNIQUE (id, userId)
			)
		`
	_, e := db.Exec(createTableQuery)
	if e != nil {
		return nil, e
	}

	return db, nil
}

// @@@@@
func AddUser(db *sql.DB, id string, password string) error {

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare("insert into useraccount (userId,password) values (?,?)")
	_, err := stmt.Exec(id, password)
	if err != nil {
		log.Println(err.Error())
		return err
	}
	tx.Commit()
	return nil
}

// @@@@@
//func GetUser(db *sql.DB, userId string) (User, error) {
//	var user User
//	rows := db.QueryRow("select * from useraccount where userId = $1", userId)
//	err := rows.Scan(&user.Id, &user.UserId, &user.Password)
//	if err != nil {
//		return User{}, err
//	}
//
//	return user, nil
//}
