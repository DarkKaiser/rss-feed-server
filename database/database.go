package database

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
)

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
