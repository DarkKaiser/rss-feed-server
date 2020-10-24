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
	return nil
}

// @@@@@
// 객체가 처음 생성될때 수행
func (f *NaverCafeRSSFeed) CreateTables() interface{} {
	return 0
}

// @@@@@
// 객체가 처음 생성될때 수행
func (f *NaverCafeRSSFeed) GenerateNaverCafeData() interface{} {
	return 0
}

// @@@@@
// 스케쥴러에서 등록된 시간이 도래했을때 수행
func (f *NaverCafeRSSFeed) DeleteAfterDays() interface{} {
	return 0
}

// @@@@@
// 크롤러에서 수행
func (f *NaverCafeRSSFeed) LatestArticleID() interface{} {
	return 0
}

// @@@@@
// 크롤러에서 수행
func (f *NaverCafeRSSFeed) AddArticle() interface{} {
	return 0
}

// @@@@@
// 웹서버에서 수행
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
