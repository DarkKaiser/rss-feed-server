package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Open() Tests
// =============================================================================

// TestOpen_Success_InMemory는 인메모리 SQLite DB에 정상적으로 연결되는 경우를 검증합니다.
// 인메모리 DSN은 실제 파일을 생성하지 않아 테스트 격리에 적합합니다.
func TestOpen_Success_InMemory(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), ":memory:")
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()

	// 반환된 핸들이 실제로 사용 가능한 상태인지 PingContext로 재확인합니다.
	assert.NoError(t, db.PingContext(context.Background()))
}

// TestOpen_Success_SharedCache는 shared cache 인메모리 DSN으로도 연결이 성공하는지 검증합니다.
// 여러 연결이 동일한 인메모리 DB를 공유해야 하는 시나리오에서 사용되는 DSN 형태입니다.
func TestOpen_Success_SharedCache(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), "file::memory:?cache=shared")
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()

	assert.NoError(t, db.PingContext(context.Background()))
}

// TestOpen_Failure_InvalidDSN은 접근 불가능한 경로의 DSN을 지정했을 때
// 적절한 에러가 반환되는지 검증합니다.
func TestOpen_Failure_InvalidDSN(t *testing.T) {
	t.Parallel()

	// SQLite 드라이버는 sql.Open 단계에서는 에러를 내지 않고,
	// PingContext 단계에서 파일 시스템 접근을 시도하다 실패하는 특성이 있습니다.
	// 존재하지 않는 디렉터리 경로를 사용하여 Ping 실패를 유도합니다.
	db, err := Open(context.Background(), "/nonexistent/path/that/cannot/exist/rss.db")
	require.Error(t, err)
	require.Nil(t, db)

	// 에러 메시지에 의도한 컨텍스트("검증 실패")가 포함되어 있는지 확인합니다.
	assert.Contains(t, err.Error(), "SQLite 연결 상태 검증 실패")
}

// TestOpen_Failure_CancelledContext는 이미 취소된 ctx를 전달했을 때
// PingContext가 실패하고 에러가 올바르게 래핑되어 반환되는지 검증합니다.
func TestOpen_Failure_CancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 즉시 취소하여 Ping 단계에서 context.Canceled 에러를 유도합니다.

	db, err := Open(ctx, ":memory:")
	require.Error(t, err)
	require.Nil(t, db)

	assert.Contains(t, err.Error(), "SQLite 연결 상태 검증 실패")
}

// TestOpen_Failure_DeadlineExceeded는 만료된 Deadline을 가진 ctx를 전달했을 때
// PingContext가 실패하고 에러가 올바르게 래핑되어 반환되는지 검증합니다.
func TestOpen_Failure_DeadlineExceeded(t *testing.T) {
	t.Parallel()

	// 이미 지나간 시각으로 Deadline을 설정하면 PingContext는 즉시 실패합니다.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	db, err := Open(ctx, ":memory:")
	require.Error(t, err)
	require.Nil(t, db)

	assert.Contains(t, err.Error(), "SQLite 연결 상태 검증 실패")
}
