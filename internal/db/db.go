package db

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// Open sqlite3 DB 연결을 열고 연결 유효성을 검증하여 반환한다.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("DB를 여는 중에 오류가 발생하였습니다: %w", err)
	}

	// sql.Open은 실제 커넥션을 생성하지 않으므로 PingContext로 연결 유효성을 검증한다.
	if err = db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("DB 연결을 확인하는 중에 오류가 발생하였습니다: %w", err)
	}

	return db, nil
}
