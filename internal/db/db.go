package db

import (
	"database/sql"
	"fmt"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/notifyapi"
	_ "github.com/go-sql-driver/mysql"
)

func New() *sql.DB {
	db, err := sql.Open("sqlite3", fmt.Sprintf("./%s.db", config.AppName))
	if err != nil {
		m := "DB를 여는 중에 치명적인 오류가 발생하였습니다."

		notifyapi.Send(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)

		applog.Panicf("%s (error:%s)", m, err)
	}

	return db
}
