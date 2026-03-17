package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// Open 주어진 DSN(Data Source Name)을 사용하여 SQLite 데이터베이스에 연결하고,
// 네트워크 또는 파일 접근의 유효성을 검증한 후 초기화된 DB 핸들을 반환한다.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("SQLite 연결 초기화 실패: %w", err)
	}

	// sql.Open은 드라이버 설정만 초기화할 뿐 실제 연결을 맺지 않습니다.
	// PingContext로 DB 파일 접근과 실제 연결 상태를 명시적으로 검증합니다.
	if err = db.PingContext(ctx); err != nil {
		_ = db.Close() // 핸들 누수 방지
		return nil, fmt.Errorf("SQLite 연결 상태 검증 실패: %w", err)
	}

	return db, nil
}
