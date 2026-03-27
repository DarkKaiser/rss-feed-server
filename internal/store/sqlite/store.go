package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	_ "github.com/mattn/go-sqlite3"
)

// Store RSS 피드 도메인의 데이터를 영속성(Persistence) 있게 관리하기 위한 SQLite 전용 저장소입니다.
// feed.Repository 인터페이스를 구현하고 있으며, 프로바이더, 보드, 게시글 및 크롤링 메타데이터에 대한
// 모든 데이터베이스 조작(삽입, 조회, 갱신, 삭제)을 담당합니다.
//
// 내부적으로 Go 표준 라이브러리의 *sql.DB 커넥션 풀을 활용하므로, 다중 고루틴(Goroutine)
// 요청 환경에서도 안전한 동시성(Concurrency) 접근 및 제어를 보장합니다.
type Store struct {
	// db 활성화된 SQLite 데이터베이스 커넥션 풀입니다.
	// 이 커넥션의 라이프사이클(Open/Close)은 Store의 책임이 아니며, 최상위 호출자(Caller) 측에서 관리해야 합니다.
	db *sql.DB
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ feed.Repository = (*Store)(nil)

// New 주입받은 SQLite 데이터베이스 커넥션(*sql.DB)을 기반으로 새로운 Store 인스턴스를 생성하여 반환합니다.
//
// 의존성 및 초기화 관련 주의사항:
//   - 이 팩토리(Factory) 함수는 단순히 Store 인스턴스를 생성하고 의존성을 주입하는 역할만 수행합니다.
//   - 내부 데이터베이스 스키마의 생성이나 마이그레이션에는 관여하지 않으므로,
//     반환된 Store를 서비스에 연동하기 전 반드시 Initialize() 메서드를 호출하여 필요한 테이블과 인덱스를 준비해야 합니다.
func New(db *sql.DB) (*Store, error) {
	s := &Store{
		db: db,
	}

	return s, nil
}

// Initialize 어플리케이션 시작 시 데이터베이스가 정상적으로 동작하기 위한 필수 준비 작업을 수행합니다.
// 크게 두 가지 작업을 처리합니다:
//  1. autoMigrate: 시스템 구동에 필요한 테이블과 인덱스를 확인하고, 누락된 구성요소를 자동으로 생성하여 스키마를 최신 상태로 구성합니다.
//  2. vacuum: 보관 기간이 만료되어 삭제된 게시글들이 남긴 빈 공간(Free block)을 회수하고 데이터베이스 파편화를 최적화합니다.
//
// 주의: 이 프로세스는 무거운 I/O를 동반할 수 있으므로, 통상적으로 어플리케이션(Store) 초기화 단계에서 1회 호출하는 것을 권장합니다.
func (s *Store) Initialize(ctx context.Context) error {
	// 단계 1: 데이터베이스 스키마 마이그레이션 (테이블/인덱스 점검 및 생성)
	if err := s.autoMigrate(ctx); err != nil {
		return fmt.Errorf("데이터베이스 스키마 초기화 및 마이그레이션 실패: %w", err)
	}

	// 단계 2: OS에 사용하지 않는 파일 빈 공간을 반환하여 단편화를 해소하고 I/O 성능 저하를 방지
	if err := s.vacuum(ctx); err != nil {
		return fmt.Errorf("데이터베이스 저장 공간 회수 및 최적화 실패: %w", err)
	}

	return nil
}

// autoMigrate 시스템 구동에 필요한 데이터베이스 테이블 및 인덱스의 존재 여부를 확인하고, 누락된 항목을 일괄 생성합니다.
// 내부적으로 도메인 엔티티(프로바이더, 보드, 게시글, 크롤링 메타데이터)를 위한 스키마 DDL 스크립트를 단일 트랜잭션 단위로 실행하여,
// 'IF NOT EXISTS' 제약 조건을 통해 안전하고 멱등성(Idempotence)이 보장된 스키마 초기화를 수행합니다.
//
// 주의: DDL 쿼리 실행 중 오류가 발생할 경우, 데이터베이스의 파편화 및 불일치를 막기 위해
// 생성 과정에서 변경된 내역은 즉시 롤백(Rollback) 처리됩니다.
func (s *Store) autoMigrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("데이터베이스 스키마 마이그레이션 트랜잭션 시작 실패: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	query := `
		CREATE TABLE IF NOT EXISTS rss_provider (
			id            VARCHAR( 50) PRIMARY KEY NOT NULL UNIQUE,
			site          VARCHAR( 50) NOT NULL,
			s_id          VARCHAR( 50) NOT NULL,
			s_name        VARCHAR(130) NOT NULL,
			s_description VARCHAR(200),
			s_url         VARCHAR(100) NOT NULL
		);

		CREATE INDEX IF NOT EXISTS rss_provider_index01 ON rss_provider(s_id);

		CREATE TABLE IF NOT EXISTS rss_provider_board (
			p_id VARCHAR( 50) NOT NULL,
			id   VARCHAR( 50) NOT NULL,
			name VARCHAR(130) NOT NULL,
			PRIMARY KEY (p_id, id),
			FOREIGN KEY (p_id) REFERENCES rss_provider(id) ON DELETE CASCADE
		);

		CREATE TABLE IF NOT EXISTS rss_provider_article (
			p_id         VARCHAR( 50) NOT NULL,
			b_id         VARCHAR( 50) NOT NULL,
			id           VARCHAR( 50) NOT NULL,
			title        VARCHAR(400) NOT NULL,
			content      TEXT,
			link         VARCHAR(1000) NOT NULL,
			author       VARCHAR(50),
			created_date DATETIME,
			PRIMARY KEY (p_id, b_id, id),
			FOREIGN KEY (p_id, b_id) REFERENCES rss_provider_board(p_id, id) ON DELETE CASCADE
		);

		CREATE INDEX IF NOT EXISTS rss_provider_article_index01 ON rss_provider_article(p_id, created_date DESC);

		CREATE INDEX IF NOT EXISTS rss_provider_article_index02 ON rss_provider_article(p_id, b_id, created_date DESC);

		CREATE TABLE IF NOT EXISTS rss_provider_site_crawled_data (
			p_id                      VARCHAR( 50) NOT NULL,
			b_id                      VARCHAR( 50) NOT NULL,
			latest_crawled_article_id VARCHAR( 50) NOT NULL,
			PRIMARY KEY (p_id, b_id),
			FOREIGN KEY (p_id) REFERENCES rss_provider(id) ON DELETE CASCADE
		);
	`

	if _, err := tx.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("데이터베이스 스키마 마이그레이션 쿼리(DDL) 실행 실패: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("데이터베이스 스키마 마이그레이션 트랜잭션 커밋 실패: %w", err)
	}

	return nil
}

// vacuum SQLite 고유의 최적화 명령어인 'VACUUM'을 실행합니다.
// DELETE 작업 등으로 비워진 레코드 공간을 실제로 모아 파일 크기를 줄여주며,
// 내부 B-Tree 구조의 단편화(Fragmentation)를 재정렬해 쿼리 성능을 일정하게 유지합니다.
func (s *Store) vacuum(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "VACUUM")
	return err
}

// SyncProviders 애플리케이션 설정(providers)을 단일 정보 원천(Source of Truth)으로 삼아,
// 데이터베이스에 저장된 공급자(Provider) 및 게시판(Board) 마스터 데이터를 최신 상태로 완전 동기화합니다.
//
// 동기화는 다음 세 가지 동작을 하나의 트랜잭션 안에서 원자적(Atomic)으로 처리합니다.
//   - 신규 공급자/게시판 → 데이터베이스에 추가
//   - 기존 공급자/게시판의 정보 변경 → 최신 설정값으로 덮어쓰기
//   - 설정 파일에서 제거된 공급자/게시판 → 관련 데이터 일괄 삭제
//
// 도중에 오류가 발생하면 트랜잭션 전체가 롤백되므로, DB는 항상 일관된 상태를 유지합니다.
func (s *Store) SyncProviders(ctx context.Context, providers []*config.ProviderConfig) error {
	// [단계 1] 트랜잭션 시작 — 이후 모든 DB 작업은 이 트랜잭션 안에서 진행됩니다.
	// 중간에 에러가 발생하면 defer로 등록된 Rollback이 자동 실행되어 DB가 이전 상태로 완벽히 복구됩니다.
	// (Commit이 먼저 성공하면 Rollback은 no-op으로 안전하게 무시됩니다.)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("공급자 동기화 트랜잭션 시작(BeginTx) 실패: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// [단계 2] 공급자 Upsert 쿼리 사전 컴파일(Prepare) — 반복 루프 이전에 SQL을 미리 파싱해 두어
	// 매 반복마다 쿼리를 재해석하는 오버헤드 없이 바인딩 인자만 교체하여 빠르게 실행할 수 있습니다.
	providerStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO
			rss_provider (id, site, s_id, s_name, s_description, s_url)
		VALUES
			(?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			site          = excluded.site,
			s_id          = excluded.s_id,
			s_name        = excluded.s_name,
			s_description = excluded.s_description,
			s_url         = excluded.s_url
	`)
	if err != nil {
		return fmt.Errorf("공급자(Provider) Upsert 쿼리 사전 컴파일(PrepareContext) 실패: %w", err)
	}
	defer providerStmt.Close()

	// [단계 3] 게시판 Upsert 쿼리 사전 컴파일(Prepare) — 공급자(Provider)당 N개의 게시판이 존재하므로
	// 이 쿼리는 단계 2보다 더 많이 실행됩니다. 사전 컴파일로 반복 비용을 최소화합니다.
	boardStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO
			rss_provider_board (p_id, id, name)
		VALUES
			(?, ?, ?)
		ON CONFLICT(p_id, id) DO UPDATE SET
			name = excluded.name
	`)
	if err != nil {
		return fmt.Errorf("게시판(Board) Upsert 쿼리 사전 컴파일(PrepareContext) 실패: %w", err)
	}
	defer boardStmt.Close()

	// [단계 4] 설정 목록 순회 및 Upsert — 각 공급자와 그 하위 게시판 데이터를 DB에 반영합니다.
	// 루프 시작 시 컨텍스트 취소 여부를 먼저 확인하여, 상위 레벨의 타임아웃/취소 신호를 즉시 반영합니다.
	for _, p := range providers {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("공급자 동기화 루프 실행 중 컨텍스트 취소 감지: %w", err)
		}

		// 공급자 마스터 레코드를 먼저 반영합니다 (게시판은 공급자 레코드에 외래 키로 종속됩니다).
		if err := s.upsertProvider(ctx, providerStmt, p); err != nil {
			return fmt.Errorf("공급자 Upsert 실패 (providerID: %s): %w", p.ID, err)
		}

		// 공급자에 속한 게시판 목록을 순회하며 각 게시판 레코드를 DB에 반영합니다.
		for _, b := range p.Config.Boards {
			if err := s.upsertBoard(ctx, boardStmt, p.ID, b); err != nil {
				return fmt.Errorf("게시판 Upsert 실패 (providerID: %s, boardID: %s): %w", p.ID, b.ID, err)
			}
		}
	}

	// [단계 5] 잔여 데이터 제거 — 설정에서 삭제된 공급자/게시판의 레코드를 DB에서 정리합니다.
	if err := s.pruneStaleData(ctx, tx, providers); err != nil {
		return err
	}

	// [단계 6] 트랜잭션 커밋 — 단계 1~5가 모두 성공한 경우에만 변경 사항을 DB에 영구 반영합니다.
	// 이 시점 이전에는 어떤 변경도 실제 DB에 기록되지 않았으며, 여기서 실패해도 자동 롤백됩니다.
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("공급자 동기화 트랜잭션 영구 반영(Commit) 실패: %w", err)
	}

	return nil
}

// upsertProvider 설정에 정의된 단일 공급자(Provider) 정보를 데이터베이스에 추가하거나, 이미 존재하면 최신 내용으로 업데이트(Upsert)합니다.
func (s *Store) upsertProvider(ctx context.Context, stmt *sql.Stmt, p *config.ProviderConfig) error {
	if _, err := stmt.ExecContext(ctx, p.ID, p.Site, p.Config.ID, p.Config.Name, p.Config.Description, p.Config.URL); err != nil {
		return fmt.Errorf("공급자 Upsert 쿼리 실행 실패 (providerID: %s): %w", p.ID, err)
	}

	return nil
}

// upsertBoard 특정 공급자(Provider)에 속한 게시판(Board) 정보를 데이터베이스에 추가하거나, 이미 존재하면 최신 이름으로 업데이트(Upsert)합니다.
func (s *Store) upsertBoard(ctx context.Context, stmt *sql.Stmt, providerID string, b *config.BoardConfig) error {
	if _, err := stmt.ExecContext(ctx, providerID, b.ID, b.Name); err != nil {
		return fmt.Errorf("게시판 Upsert 쿼리 실행 실패 (providerID: %s, boardID: %s): %w", providerID, b.ID, err)
	}

	return nil
}

// pruneStaleData 설정(providers)에서 빠진 공급자(Provider)와 게시판(Board) 레코드를 데이터베이스에서 찾아서
// 삭제하는 내부 헬퍼 함수입니다.
//
// 공급자 레코드 하나를 삭제하면, 그 아래에 딸린 게시판(Board)과 게시글(Article)이 FK ON DELETE CASCADE 설정에 의해
// 자동으로 함께 삭제됩니다.
//
// 주의: SQLite는 기본적으로 외래 키 규칙을 비활성화합니다.
// 위의 연쇄 삭제가 실제로 동작하려면, DB 연결 시 `?_fk=1` 옵션이나 `PRAGMA foreign_keys = ON;`을 반드시 켜 두어야 합니다.
func (s *Store) pruneStaleData(ctx context.Context, tx *sql.Tx, providers []*config.ProviderConfig) error {
	// [Short-Circuit] 살려둘 공급자가 하나도 없으면, IN 절 조건 없이 테이블 전체를 비웁니다.
	// (SQL은 IN () 처럼 빈 목록을 허용하지 않으므로, 이 경우를 별도 분기로 처리합니다.)
	if len(providers) == 0 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM rss_provider"); err != nil {
			return fmt.Errorf("전체 공급자(Provider) 레코드 일괄 삭제(Delete) 쿼리 실행 실패: %w", err)
		}

		return nil
	}

	// [단계 1] DELETE ... WHERE id NOT IN (?) 쿼리에 넘길 바인딩 인자 목록을 구성합니다.
	// 설정에 남아 있는 공급자 ID를 추출하고, 해당 수만큼 '?' 플레이스홀더를 생성합니다.
	activeProviderIDs := make([]any, 0, len(providers))
	for _, p := range providers {
		activeProviderIDs = append(activeProviderIDs, p.ID)
	}

	providerPlaceholders := make([]string, len(activeProviderIDs))
	for i := range providerPlaceholders {
		providerPlaceholders[i] = "?"
	}
	providerBindStr := strings.Join(providerPlaceholders, ", ")

	// [단계 2] 설정에서 사라진 공급자 레코드를 DB에서 삭제합니다.
	// 삭제된 공급자에 딸린 게시판(Board)과 게시글(Article)은 FK Cascade에 의해 자동으로 함께 삭제됩니다.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM rss_provider WHERE id NOT IN (%s)", providerBindStr), activeProviderIDs...); err != nil {
		return fmt.Errorf("고립된 공급자(Provider) 레코드 삭제(Delete) 쿼리 실행 실패: %w", err)
	}

	// [단계 3] 각 공급자를 순회하며, 설정에서 사라진 게시판 레코드를 DB에서 정리합니다.
	for _, p := range providers {
		if len(p.Config.Boards) == 0 {
			// 이 공급자에 설정된 게시판이 하나도 없다면, 소속 게시판 레코드 전체를 삭제합니다.
			// (삭제된 게시판에 속한 게시글(Article)은 FK Cascade로 자동 삭제됩니다.)
			if _, err := tx.ExecContext(ctx, "DELETE FROM rss_provider_board WHERE p_id = ?", p.ID); err != nil {
				return fmt.Errorf("공급자(Provider) 하위 전체 게시판(Board) 레코드 삭제(Delete) 쿼리 실행 실패 (providerID: %s): %w", p.ID, err)
			}

			// 각 게시판에 연결된 크롤링 메타데이터도 함께 삭제합니다.
			// 단, b_id=''(이 공급자 전체에 적용되는 글로벌 메타데이터)는 삭제하지 않고 보존합니다.
			if _, err := tx.ExecContext(ctx, "DELETE FROM rss_provider_site_crawled_data WHERE p_id = ? AND b_id != ''", p.ID); err != nil {
				return fmt.Errorf("공급자(Provider) 하위 전체 크롤링 메타데이터 삭제(Delete) 쿼리 실행 실패 (providerID: %s): %w", p.ID, err)
			}

			continue
		}

		// 게시판 IN 절 바인딩 인자를 구성합니다. (공급자 ID 목록 구성과 동일한 방식)
		activeBoardIDs := make([]any, 0, len(p.Config.Boards))
		for _, b := range p.Config.Boards {
			activeBoardIDs = append(activeBoardIDs, b.ID)
		}

		boardPlaceholders := make([]string, len(activeBoardIDs))
		for i := range boardPlaceholders {
			boardPlaceholders[i] = "?"
		}
		boardBindStr := strings.Join(boardPlaceholders, ", ")

		// 이 공급자에 속하지만 설정에서 사라진 게시판 레코드를 삭제합니다.
		// (삭제된 게시판에 속한 게시글(Article)은 FK Cascade로 자동 삭제됩니다.)
		boardDeleteArgs := append([]any{p.ID}, activeBoardIDs...)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM rss_provider_board WHERE p_id = ? AND id NOT IN (%s)", boardBindStr), boardDeleteArgs...); err != nil {
			return fmt.Errorf("고립된 게시판(Board) 레코드 삭제(Delete) 쿼리 실행 실패 (providerID: %s): %w", p.ID, err)
		}

		// 삭제된 게시판에 연결된 크롤링 메타데이터도 함께 삭제합니다.
		// b_id=''(공급자 전체에 적용되는 글로벌 메타데이터)는 게시판과 무관하므로 보존합니다.
		metaDeleteArgs := append([]any{p.ID, ""}, activeBoardIDs...)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM rss_provider_site_crawled_data WHERE p_id = ? AND b_id != ? AND b_id NOT IN (%s)", boardBindStr), metaDeleteArgs...); err != nil {
			return fmt.Errorf("고립된 게시판(Board) 크롤링 메타데이터 삭제(Delete) 쿼리 실행 실패 (providerID: %s): %w", p.ID, err)
		}
	}

	return nil
}

// PurgeOldArticles 환경 설정(config.ProviderConfig)에 정의된 보관 기한(ArchiveDays)을 기준으로,
// 유효 기간이 만료된 과거 크롤링 게시글 레코드들을 데이터베이스에서 일괄 삭제(Purge)합니다.
//
// 주로 데이터베이스 테이블 용량의 무한 증식을 막기 위한 주기적 생명주기 관리(Lifecycle Management) 용도로 호출됩니다.
// 하나의 거대한 트랜잭션으로 전체 데이터를 한 번에 삭제할 경우 발생하는 테이블 잠금(Lock)의 장기화 및
// 성능 병목(Bottleneck) 현상을 방지하기 위해 다음의 세 가지 핵심 설계가 적용되어 있습니다.
//
//  1. 작업 단위 격리(Isolation): 전체를 통째로 지우지 않고 개별 사이트(공급자) 단위로 대상을 나누어 처리합니다.
//  2. 단기 트랜잭션(Short-lived Tx): 익명 함수를 활용하여 각 공급자 단위마다 전용 트랜잭션을 빠르게 맺고 종료합니다.
//  3. 부분 실패 허용(Fault Tolerant): 특정 공급자의 삭제 쿼리가 실패하더라도 전체 정지(Panic/Return) 없이
//     나머지 작업을 마저 수행하며, 수집된 모든 에러 궤적은 `errors.Join`으로 묶어 최종 보고합니다.
func (s *Store) PurgeOldArticles(ctx context.Context, providers []*config.ProviderConfig) error {
	var errs []error

	for _, p := range providers {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("보관 기한 초과 레코드 일괄 삭제 작업 중 실행 컨텍스트가 취소되었습니다: %w", err)
		}

		// DB 점유율(Lock) 최소화를 위해 각 Provider 별로 트랜잭션을 분리하여 삭제합니다.
		err := func() error {
			tx, err := s.db.BeginTx(ctx, nil)
			if err != nil {
				return fmt.Errorf("보관 기한 초과 레코드 삭제를 위한 독립 트랜잭션 시작(BeginTx) 실패: %w", err)
			}
			defer func() {
				_ = tx.Rollback()
			}()

			if err := s.deleteOldArticles(ctx, tx, p.ID, p.Config.ArchiveDays); err != nil {
				return err
			}

			if err := tx.Commit(); err != nil {
				return fmt.Errorf("보관 기한 초과 레코드 삭제 트랜잭션의 영구 반영(Commit) 실패: %w", err)
			}

			return nil
		}()

		if err != nil {
			errs = append(errs, fmt.Errorf("단일 프로바이더(providerID: %s)의 보관 기한 초과 레코드 일괄 삭제 실패: %w", p.ID, err))
		}
	}

	return errors.Join(errs...)
}

// deleteOldArticles 진행 중인 트랜잭션(tx) 내에서 특정 Provider의 설정된 보관 기한(archiveDays)이 지난
// 과거 게시글들을 물리적으로 영구 삭제합니다.
//
// 외부 API인 PurgeOldArticles에서 각 Provider별 격리된 트랜잭션과 함께 호출되며,
// SQLite 내장 함수인 `strftime`을 사용하여 현재 시각('now')을 기준으로 만료 대상 레코드를 데이터베이스
// 엔진 레벨에서 효율적으로 필터링 후 삭제(DELETE)합니다.
//
// 최적화 포인트:
// archiveDays 값이 0으로 전달되는 경우는 데이터 보관 기한이 '무제한'임을 뜻합니다.
// 이 때는 어떠한 불필요한 I/O 작업이나 쿼리도 실행하지 않고 즉시 성공(nil)을 반환합니다.
func (s *Store) deleteOldArticles(ctx context.Context, tx *sql.Tx, providerID string, archiveDays uint) error {
	// archiveDays가 0인 경우는 보관 기간 '무제한(보존)'을 의미하므로, 불필요한 DB 쿼리 오버헤드를 막기 위해 즉시 반환합니다.
	if archiveDays == 0 {
		return nil
	}

	query := `
		DELETE 
		  FROM rss_provider_article
		 WHERE p_id = ?
		   AND created_date < strftime('%Y-%m-%dT%H:%M:%SZ', 'now', ?)
	`

	if _, err := tx.ExecContext(ctx, query, providerID, fmt.Sprintf("-%d days", archiveDays)); err != nil {
		return fmt.Errorf("보관 기한을 초과한 게시글 레코드의 영구 삭제(DELETE) 쿼리 실행 실패 (providerID: %s, archiveDays: %d): %w", providerID, archiveDays, err)
	}

	return nil
}

// SaveArticles 게시글 목록을 데이터베이스에 저장하고, 실제로 저장에 성공한 게시글 수를 반환합니다.
// 이미 있는 게시글(p_id, b_id, id 중복)이면 최신 내용으로 덮어쓰고, 다음 게시글로 계속 진행합니다.
// 개별 게시글 저장에 실패하더라도 나머지는 계속 처리되며, 실패한 내역은 반환되는 error에 통합되어 전달됩니다.
func (s *Store) SaveArticles(ctx context.Context, providerID string, articles []*feed.Article) (int, error) {
	// 저장할 게시글이 없으면 바로 반환합니다.
	if len(articles) == 0 {
		return 0, nil
	}

	// 전체 저장 작업을 하나의 트랜잭션으로 묶어 원자성을 보장합니다.
	// defer로 등록된 Rollback()은 Commit()이 먼저 성공하면 자동으로 무시됩니다.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("게시글 저장(SaveArticles) 트랜잭션 BeginTx 실패: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// 쿼리를 미리 컴파일(PrepareContext)하여 루프 코스트를 줄입니다.
	// 새 게시글은 삽입하고, 이미 있는 게시글은 최신 내용으로 덮어씁니다. (Upsert)
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO
			rss_provider_article (p_id, b_id, id, title, content, link, author, created_date)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(p_id, b_id, id) DO UPDATE SET
			title        = excluded.title,
			content      = excluded.content,
			link         = excluded.link,
			author       = excluded.author,
			created_date = excluded.created_date
	`)
	if err != nil {
		return 0, fmt.Errorf("게시글(Article) Upsert PrepareContext 실패 (providerID: %s): %w", providerID, err)
	}
	defer stmt.Close()

	var errs []error
	var savedCount int

	// 게시글을 한 건씩 순회하며 저장합니다.
	// 컨텍스트가 취소되면 나머지 작업을 중단하고, 단일 실패는 기록하고 다음으로 넘어갑니다.
	for _, article := range articles {
		if err := ctx.Err(); err != nil {
			errs = append(errs, fmt.Errorf("게시글 저장(SaveArticles) 컨텍스트 취소로 중단: %w", err))
			break
		}

		if _, err := stmt.ExecContext(ctx, providerID, article.BoardID, article.ArticleID, article.Title, article.Content, article.Link, article.Author, article.CreatedAt.UTC().Format(time.RFC3339)); err != nil {
			errs = append(errs, fmt.Errorf("게시글(Article) Upsert 쿼리 실행 실패 (providerID: %s, articleID: %s): %w", providerID, article.ArticleID, err))
			continue
		}

		savedCount++
	}

	// 루프가 끝나면 트랜잭션을 커밋합니다.
	// 커밋 실패 시에는 루프 중 누적된 에러도 함께 통합하여 반환합니다.
	if err := tx.Commit(); err != nil {
		if len(errs) > 0 {
			errs = append(errs, fmt.Errorf("게시글 저장(SaveArticles) 트랜잭션 Commit 실패: %w", err))
			return 0, errors.Join(errs...)
		}

		return 0, fmt.Errorf("게시글 저장(SaveArticles) 트랜잭션 Commit 실패: %w", err)
	}

	// 루프 중 일부 실패가 있었더라도 커밋은 성공했으므로, 성공 건수와 함께 실패 내역을 두 값 모두 반환합니다.
	if len(errs) > 0 {
		return savedCount, fmt.Errorf("게시글(Article) 부분 저장 완료 — 일부 Upsert 실패 포함: %w", errors.Join(errs...))
	}

	return savedCount, nil
}

// GetArticles 지정한 공급자(providerID)의 게시판들(boardIDs)에서 게시글을 최신순으로 최대 limit개 반환합니다.
// boardIDs가 비어 있으면 DB를 조회하지 않고 빈 목록을 반환합니다.
func (s *Store) GetArticles(ctx context.Context, providerID string, boardIDs []string, limit uint) ([]*feed.Article, error) {
	// 조회할 게시판이 없으면 DB 통신 없이 즉시 빈 목록을 반환합니다.
	if len(boardIDs) == 0 {
		return make([]*feed.Article, 0), nil
	}

	// boardIDs 개수만큼 `?, ?, ?` 자리표시자를 동적으로 만들어 IN 절에 끌어 넣습니다.
	placeholders := make([]string, len(boardIDs))
	for i := range boardIDs {
		placeholders[i] = "?"
	}

	query := fmt.Sprintf(`
		SELECT a.b_id
		     , b.name AS b_name
		     , a.id
		     , a.title
		     , IFNULL(a.content, "") AS content
		     , a.link
		     , IFNULL(a.author, "") AS author
		     , a.created_date
		  FROM rss_provider_article a
		       INNER JOIN rss_provider_board b ON ( a.p_id = b.p_id AND a.b_id = b.id )
		 WHERE a.p_id = ?
		   AND a.b_id IN (%s)
		 ORDER BY a.created_date DESC
		 LIMIT ?
	`, strings.Join(placeholders, ", "))

	// 쿼리 실행에 바인딩할 인자를 순서대로 조립합니다: providerID → boardIDs → limit
	args := make([]any, 0, 2+len(boardIDs))
	args = append(args, providerID)
	for _, id := range boardIDs {
		args = append(args, id)
	}
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("GetArticles 쿼리 실행 실패 (providerID: %s): %w", providerID, err)
	}
	defer rows.Close()

	articles := make([]*feed.Article, 0, limit)

	// 조회 결과를 한 행씩 순회하며 Article 구조체로 변환합니다.
	for rows.Next() {
		var article feed.Article
		var rawCreatedDate sql.NullString

		if err = rows.Scan(&article.BoardID, &article.BoardName, &article.ArticleID, &article.Title, &article.Content, &article.Link, &article.Author, &rawCreatedDate); err != nil {
			return nil, fmt.Errorf("GetArticles 결과 스캔 실패: %w", err)
		}
		if rawCreatedDate.Valid {
			if parsed, err := time.Parse(time.RFC3339, rawCreatedDate.String); err == nil {
				article.CreatedAt = parsed.Local()
			}
		}

		articles = append(articles, &article)
	}

	// rows.Next() 루프 종료 후, 루프 도중 발생한 내부 에러를 확인합니다.
	// rows.Scan 에러와는 다른 종류의 실패이므로 반드시 체크해야 합니다.
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("GetArticles 결과 행 반복 중 에러 발생: %w", err)
	}

	return articles, nil
}

// GetCrawlingCursor 이전 크롤링에서 마지막으로 수집한 게시글의 ID와 작성일시를 반환합니다.
// 결과가 없으면 ID는 빈 문자열(""), 작성일시는 zero value(time.Time{})를 반환합니다.
//
// boardID가 빈 문자열("")이면 특정 게시판이 아닌 해당 공급자(Provider) 전체를 대상으로 조회합니다.
//
// 참고: 내부 쿼리는 `SELECT (서브쿼리1), (서브쿼리2)` 구조를 사용합니다.
// 이 방식은 결과가 없어도 항상 (NULL, NULL) 한 행을 반환하므로, sql.ErrNoRows가 발생하지 않으며 sql.Null* 타입으로 안전하게 처리됩니다.
func (s *Store) GetCrawlingCursor(ctx context.Context, providerID, boardID string) (string, time.Time, error) {
	var err error
	var rawArticleID sql.NullString
	var rawCreatedDate sql.NullString

	if boardID == "" {
		// boardID가 빈 문자열일 때, 크롤링 커서 ID는 b_id=''에서 조회하지만
		// 최신 게시글 날짜는 b_id 구분 없이 이 공급자 전체에서 가장 최신 값을 가져옵니다.
		// (네이버 카페처럼 여러 게시판을 통합 관리하는 공급자에서, 어느 게시판에 가장 최신 글이 있는지 빠르게 파악하기 위한 의도된 동작입니다.)
		err = s.db.QueryRowContext(ctx, `
			SELECT ( SELECT latest_crawled_article_id
					   FROM rss_provider_site_crawled_data
					  WHERE p_id = ?
					    AND b_id = '' )
			     , ( SELECT created_date 
					   FROM rss_provider_article
					  WHERE p_id = ?
					  ORDER BY created_date DESC
					  LIMIT 1 )
		`, providerID, providerID).Scan(&rawArticleID, &rawCreatedDate)
	} else {
		err = s.db.QueryRowContext(ctx, `
			SELECT ( SELECT latest_crawled_article_id
					   FROM rss_provider_site_crawled_data
					  WHERE p_id = ?
					    AND b_id = ? )
			     , ( SELECT created_date 
					   FROM rss_provider_article
					  WHERE p_id = ?
					    AND b_id = ?
					  ORDER BY created_date DESC
					  LIMIT 1 )
		`, providerID, boardID, providerID, boardID).Scan(&rawArticleID, &rawCreatedDate)
	}

	var articleID string
	var createdDate time.Time

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", createdDate, fmt.Errorf("최신 크롤링 메타데이터 조회(Select) 쿼리 실행 실패 (providerID: %s, boardID: %s): %w", providerID, boardID, err)
	}

	// DB에서 가져온 Null 허용 타입(sql.Null*)을 실제 반환할 도메인 타입으로 변환합니다.
	// Null이면 각각 빈 문자열과 zero time을 그대로 반환합니다.
	if rawArticleID.Valid {
		articleID = rawArticleID.String
	}
	if rawCreatedDate.Valid {
		if parsed, err := time.Parse(time.RFC3339, rawCreatedDate.String); err == nil {
			createdDate = parsed.Local()
		}
	}

	return articleID, createdDate, nil
}

// UpsertLatestCrawledArticleID 이번 크롤링에서 수집한 가장 최신 게시글의 ID를 저장합니다.
// 다음번 크롤링 때 이 ID 이후 게시글부터 수집하므로, 이미 가져온 게시글을 다시 가져오는 일이 없어집니다.
//
// boardID를 빈 문자열("")로 전달하면 특정 게시판이 아닌 공급자 전체에 대한 기준 ID로 저장됩니다.
func (s *Store) UpsertLatestCrawledArticleID(ctx context.Context, providerID, boardID, articleID string) error {
	query := `
		INSERT INTO
			rss_provider_site_crawled_data (p_id, b_id, latest_crawled_article_id)
		VALUES
			(?, ?, ?)
		ON CONFLICT(p_id, b_id) DO UPDATE SET
			latest_crawled_article_id = excluded.latest_crawled_article_id
	`

	if _, err := s.db.ExecContext(ctx, query, providerID, boardID, articleID); err != nil {
		return fmt.Errorf("크롤링 커서(Cursor) Upsert 쿼리 실행 실패 (providerID: %s, boardID: %s): %w", providerID, boardID, err)
	}

	return nil
}
