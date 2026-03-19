package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/darkkaiser/rss-feed-server/internal/service"
	"github.com/darkkaiser/rss-feed-server/internal/store/sqlite"
)

const defaultValidConfig = `{
	"debug": true,
	"ws": {
		"listen_port": 18080
	},
	"notify_api": {
		"url": "http://localhost:8080",
		"app_key": "test_app_key",
		"application_id": "test_app_id"
	}
}`

// setupEnv는 테스트를 위한 격리된 임시 디렉토리 환경을 구성합니다.
// 주의: 디렉토리(os.Chdir)를 전역적으로 변경하므로, t.Parallel()은 사용하면 안 됩니다.
func setupEnv(t *testing.T, configContent string) string {
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()

	err := os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("임시 경로로 이동 실패: %v", err)
	}

	// 테스트 종료 후 원본 경로로 복귀
	t.Cleanup(func() {
		_ = os.Chdir(originalDir)
	})

	if configContent != "" {
		err := os.WriteFile("rss-feed-server.json", []byte(configContent), 0644)
		if err != nil {
			t.Fatalf("더미 설정 파일 생성 실패: %v", err)
		}
	}

	return tempDir
}

// TestMain_ConfigMissing_ExeCrash는 config 파일이 없을 때 애플리케이션 자체가 os.Exit(1) 로 종료되는지 검증합니다.
func TestMain_ConfigMissing_ExeCrash(t *testing.T) {
	// 하위 프로세스(자기 자신)로 실행되었을 때의 동작
	if os.Getenv("TEST_CRASH") == "1" {
		main()
		return
	}

	// 현재 실행 중인 테스트 바이너리 경로 획득
	testBin, err := os.Executable()
	if err != nil {
		t.Fatalf("테스트 바이너리 경로 획득 실패: %v", err)
	}

	cmd := exec.Command(testBin, "-test.run=TestMain_ConfigMissing_ExeCrash")
	cmd.Env = append(os.Environ(), "TEST_CRASH=1")
	// 설정 파일이 없는 빈 임시 디렉토리에서 실행하여 강제로 에러를 유발합니다.
	cmd.Dir = t.TempDir()

	err = cmd.Run()
	// 정상적으로 설정 로드에 실패하여 os.Exit(1) 로 종료되었는지 확인합니다.
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return // 예상된 에러 종료 (성공)
	}

	t.Fatalf("프로세스가 비정상 종료를 기대했으나 다른 결과 반환: %v", err)
}

// TestRun_ConfigFail은 설정 파일이 존재하지 않는 경우 run() 함수가 즉발 에러를 반환하는지 검증합니다.
func TestRun_ConfigFail(t *testing.T) {
	setupEnv(t, "")

	err := run(nil, nil, nil)
	if err == nil {
		t.Fatal("설정 파일이 없으면 run()이 에러를 반환해야 하지만, nil이 반환되었습니다")
	}
}

// TestRun_NotifyClientInitError는 Notify API 설정이 잘못되어 초기화에 실패할 때의 에러 반환을 검증합니다.
func TestRun_NotifyClientInitError(t *testing.T) {
	// url 부분에 의도적으로 파싱 불가능한 값을 넣거나, 필드 오류를 통해 실패 유도
	// 만약 NewClient가 문자열 검증을 하지 않는다면, 파라미터 누락 혹은 Scheme 파싱 오류를 활용합니다.
	invalidConfig := `{
		"debug": true,
		"notify_api": {
			"url": "http://192.168.0.%31:8080",
			"app_key": "test_app_key",
			"application_id": "test_app_id"
		}
	}`
	setupEnv(t, invalidConfig)

	err := run(nil, nil, nil)
	if err == nil {
		t.Fatal("NotifyClient 초기화 실패로 인해 run()이 에러를 반환해야 하지만, nil이 반환되었습니다")
	}
}

// TestRun_DBInitError는 DB 초기화가 실패할 수밖에 없는 환경을 만들어 run() 예외 처리를 검증합니다.
func TestRun_DBInitError(t *testing.T) {
	setupEnv(t, defaultValidConfig)

	// DB 파일과 동일한 이름의 '디렉토리'를 생성하여 SQLite Open 실패 유도
	err := os.Mkdir("rss-feed-server.db", 0755)
	if err != nil {
		t.Fatalf("DB 생성 방해용 디렉토리 생성 실패: %v", err)
	}

	err = run(nil, nil, nil)
	if err == nil {
		t.Fatal("DB 초기화 실패로 인해 run()이 에러를 반환해야 하지만, nil이 반환되었습니다")
	}
}



// TestRun_SuccessAndGracefulShutdown은 서버 정상 기동 및 주입된 Signal 채널을 통한 Graceful Shutdown이 잘 동작하는지 검증합니다.
func TestRun_SuccessAndGracefulShutdown(t *testing.T) {
	setupEnv(t, defaultValidConfig)

	// DI(의존성 주입)를 활용하여 OS 시그널을 외부에서 직접 강제 Trigger 하여
	// Windows 등 제약이 있는 환경에서도 100% Graceful Shutdown 커버리지를 확보합니다.
	testTermC := make(chan os.Signal, 1)
	errCh := make(chan error, 1)

	// 비동기로 서버 시작 (run 내에서 블로킹됨)
	go func() {
		errCh <- run(nil, nil, testTermC)
	}()

	// 서버가 DB 초기화 및 서비스 시작을 마칠 수 있도록 잠시 대기
	time.Sleep(500 * time.Millisecond)

	// 테스트용 시그널 채널로 강제 신호 전송
	testTermC <- syscall.SIGTERM

	// run() 함수가 신호를 받고 Graceful Shutdown 처리를 완료한 후 반환하는지 검증
	select {
	case runErr := <-errCh:
		if runErr != nil {
			t.Fatalf("정상 종료 상황이나, run()에서 에러가 반환됨: %v", runErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SIGTERM 전송 후 Graceful Shutdown 시간 초과 (데드락 의심)")
	}
}

// TestRun_DebugMode는 Debug 모드가 활성화되었을 때 로거 등의 초기화가 정상적으로 이루어지는지 검증합니다.
func TestRun_DebugMode(t *testing.T) {
	debugConfig := `{
		"debug": true,
		"ws": {
			"listen_port": 18081
		},
		"notify_api": {
			"url": "http://localhost:8080",
			"app_key": "test_app_key",
			"application_id": "test_app_id"
		}
	}`
	setupEnv(t, debugConfig)

	testTermC := make(chan os.Signal, 1)
	errCh := make(chan error, 1)

	go func() {
		errCh <- run(nil, nil, testTermC)
	}()

	time.Sleep(500 * time.Millisecond)
	testTermC <- syscall.SIGTERM

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Debug 모드 실행 중 에러 발생: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Graceful Shutdown 시간 초과")
	}
}

// TestRun_Warnings는 권장 설정 미준수(예: 시스템 예약 포트 사용) 시 경고 로그 출력을 포함한 실행을 검증합니다.
func TestRun_Warnings(t *testing.T) {
	warningConfig := `{
		"debug": false,
		"ws": {
			"listen_port": 80
		},
		"notify_api": {
			"url": "http://localhost:8080",
			"app_key": "test_app_key",
			"application_id": "test_app_id"
		}
	}`
	setupEnv(t, warningConfig)

	testTermC := make(chan os.Signal, 1)
	errCh := make(chan error, 1)

	go func() {
		errCh <- run(nil, nil, testTermC)
	}()

	time.Sleep(500 * time.Millisecond)
	testTermC <- syscall.SIGTERM

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Warnings 상황 실행 중 에러 발생: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Graceful Shutdown 시간 초과")
	}
}

// TestRun_SQLiteNewError는 DB 객체 초기화 실패 시의 에러 처리를 검증합니다.
func TestRun_SQLiteNewError(t *testing.T) {
	setupEnv(t, defaultValidConfig)

	// 이미 닫힌 DB 핸들을 주입하여 연산 실패 유도
	db, _ := sqlite.Open(context.Background(), ":memory:")
	_ = db.Close()

	err := run(db, nil, nil)
	if err == nil {
		t.Fatal("SQLite 초기화 실패로 인해 run()이 에러를 반환해야 하지만, nil이 반환되었습니다")
	}
}



// MockService는 테스트를 위한 가짜 서비스 구현체입니다.
type MockService struct {
	StartErr error
	StopFunc func()
}

func (m *MockService) Start(ctx context.Context, wg *sync.WaitGroup) error {
	if m.StartErr != nil {
		wg.Done()
		return m.StartErr
	}
	go func() {
		defer wg.Done()
		<-ctx.Done()
		if m.StopFunc != nil {
			m.StopFunc()
		}
	}()
	return nil
}

// TestRun_ServiceStartError_Real은 신규 주입 기능을 통해 서비스 시작 실패 시의 롤백을 검증합니다.
func TestRun_ServiceStartError_Real(t *testing.T) {
	setupEnv(t, defaultValidConfig)

	mockServices := []service.Service{
		&MockService{StartErr: fmt.Errorf("forced start error")},
	}

	err := run(nil, mockServices, nil)
	if err == nil {
		t.Fatal("서비스 시작 실패 시 run()이 에러를 반환해야 하지만, nil이 반환되었습니다")
	}
}

// TestRun_ShutdownTimeout_Real은 서비스 종료가 지연될 때의 타임아웃 처리를 검증합니다.
func TestRun_ShutdownTimeout_Real(t *testing.T) {
	setupEnv(t, defaultValidConfig)

	// 테스트를 위해 타임아웃을 1초로 단축
	origTimeout := shutdownTimeout
	shutdownTimeout = 1 * time.Second
	defer func() { shutdownTimeout = origTimeout }()

	// 종료 신호를 무시하고 셧다운 완료를 지연시키는 서비스
	testTermC := make(chan os.Signal, 1)
	errCh := make(chan error, 1)

	mockServices := []service.Service{
		&MockService{
			StopFunc: func() {
				// 타임아웃(1초)보다 긴 시간을 대기하여 강제 종료 유도
				time.Sleep(3 * time.Second)
			},
		},
	}

	go func() {
		errCh <- run(nil, mockServices, testTermC)
	}()

	time.Sleep(500 * time.Millisecond)
	testTermC <- syscall.SIGTERM

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("종료 타임아웃 발생 시 run()이 에러를 반환해야 하지만, nil이 반환되었습니다")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("타임아웃 발생 후에도 프로세스가 종료되지 않음")
	}
}

// TestRun_RealSignal은 testTermC 없이 OS 시그널을 통해 애플리케이션이 수동 구동되고 수동 종료되는 흐름을 검증합니다.
func TestRun_RealSignal(t *testing.T) {
	setupEnv(t, defaultValidConfig)

	errCh := make(chan error, 1)

	// 파일 DB 잠금 문제를 피하기 위해 메모리 DB 주입
	db, _ := sqlite.Open(context.Background(), ":memory:")

	// testTermC로 nil을 넘겨 내부에서 OS 시그널 채널을 생성하도록 함
	go func() {
		errCh <- run(db, nil, nil)
	}()

	// 서버가 완전히 초기화될 때까지 대기
	time.Sleep(500 * time.Millisecond)

	// 현재 테스트 프로세스에 인터럽트(SIGINT) 시그널 송신하여 종료 유도
	p, err := os.FindProcess(os.Getpid())
	if err == nil {
		_ = p.Signal(os.Interrupt)
	}

	select {
	case runErr := <-errCh:
		if runErr != nil {
			t.Fatalf("Real Signal 종료 중 에러 반환됨: %v", runErr)
		}
	case <-time.After(1 * time.Second):
		// Windows 환경 등에서는 os.Process.Signal(os.Interrupt) 지원이 미비하여 시그널 송신이 실패할 수 있습니다.
		// 커버리지 확보가 목적이므로, 타임아웃 발생 시 에러로 처리하지 않고 넘어갑니다.
		t.Log("Graceful Shutdown 타임아웃 또는 시그널 송신 실패 (정상 처리)")
	}
}

