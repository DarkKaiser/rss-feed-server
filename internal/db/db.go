package db

import (
	"context"
	"database/sql"
	"fmt"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	_ "github.com/go-sql-driver/mysql"
)

func New(notifyClient *notify.Client) *sql.DB {
	db, err := sql.Open("sqlite3", fmt.Sprintf("./%s.db", config.AppName))
	if err != nil {
		m := "DB를 여는 중에 치명적인 오류가 발생하였습니다."

		if notifyClient != nil {
			notifyClient.NotifyError(context.Background(), fmt.Sprintf("%s\r\n\r\n%s", m, err))
		}

		applog.Panicf("%s (error:%s)", m, err)
	}

	return db
}
