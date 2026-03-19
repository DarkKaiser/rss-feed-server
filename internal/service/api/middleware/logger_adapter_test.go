package middleware

import (
	"bytes"
	"encoding/json"
	"testing"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/labstack/gommon/log"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// =============================================================================
// Logger Adapter 테스트
// =============================================================================

// TestLoggerAdapter_Level_Table은 애플리케이션 로그 레벨이 Echo 로그 레벨로
// 올바르게 변환되는지 검증합니다.
func TestLoggerAdapter_Level_Table(t *testing.T) {
	tests := []struct {
		name          string
		appLogLevel   applog.Level
		expectedLevel log.Lvl
	}{
		{"성공: Debug 레벨 변환", applog.DebugLevel, log.DEBUG},
		{"성공: Info 레벨 변환", applog.InfoLevel, log.INFO},
		{"성공: Warn 레벨 변환", applog.WarnLevel, log.WARN},
		{"성공: Error 레벨 변환", applog.ErrorLevel, log.ERROR},
		{"성공: Panic 레벨 변환 (미지원 -> OFF)", applog.PanicLevel, log.OFF},
		{"성공: Fatal 레벨 변환 (미지원 -> OFF)", applog.FatalLevel, log.OFF},
		{"성공: Trace 레벨 변환 (미지원 -> OFF)", applog.TraceLevel, log.OFF},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := logrus.New()
			logger := Logger{l}
			l.SetLevel(tt.appLogLevel)
			assert.Equal(t, tt.expectedLevel, logger.Level())
		})
	}
}

// TestLoggerAdapter_SetLevel_Table은 Echo 로그 레벨 설정이 애플리케이션 로거에
// 올바르게 반영되는지 검증합니다.
func TestLoggerAdapter_SetLevel_Table(t *testing.T) {
	tests := []struct {
		name          string
		inputLevel    log.Lvl
		expectedLevel applog.Level
	}{
		{"성공: Debug 레벨 설정", log.DEBUG, applog.DebugLevel},
		{"성공: Info 레벨 설정", log.INFO, applog.InfoLevel},
		{"성공: Warn 레벨 설정", log.WARN, applog.WarnLevel},
		{"성공: Error 레벨 설정", log.ERROR, applog.ErrorLevel},
		{"성공: OFF 레벨 설정 (무시됨)", log.OFF, applog.InfoLevel}, // 기본값 유지
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := logrus.New()
			l.SetLevel(applog.InfoLevel) // 초기값 설정
			logger := Logger{l}

			logger.SetLevel(tt.inputLevel)

			if tt.inputLevel != log.OFF {
				assert.Equal(t, tt.expectedLevel, l.Level)
			} else {
				// OFF인 경우 변경되지 않아야 함
				assert.Equal(t, applog.InfoLevel, l.Level)
			}
		})
	}
}

// TestLoggerAdapter_Methods_Table은 모든 로깅 메서드(Print, Info, Warn 등)가
// 올바르게 위임되어 로그를 남기는지 검증합니다.
func TestLoggerAdapter_Methods_Table(t *testing.T) {
	tests := []struct {
		name             string
		action           func(*Logger)
		expectLogContent string
		expectLevel      string
	}{
		{
			name:             "성공: Print 메서드",
			action:           func(l *Logger) { l.Print("테스트 메시지") },
			expectLogContent: "테스트 메시지",
			expectLevel:      "info", // Print는 기본적으로 info 레벨
		},
		{
			name:             "성공: Printf 메서드",
			action:           func(l *Logger) { l.Printf("테스트 %s", "포맷") },
			expectLogContent: "테스트 포맷",
			expectLevel:      "info",
		},
		{
			name:             "성공: Printj 메서드",
			action:           func(l *Logger) { l.Printj(log.JSON{"key": "value"}) },
			expectLogContent: "value",
			expectLevel:      "info",
		},
		{
			name:             "성공: Info 메서드",
			action:           func(l *Logger) { l.Info("테스트 정보") },
			expectLogContent: "테스트 정보",
			expectLevel:      "info",
		},
		{
			name:             "성공: Infof 메서드",
			action:           func(l *Logger) { l.Infof("정보 %d", 123) },
			expectLogContent: "정보 123",
			expectLevel:      "info",
		},
		{
			name:             "성공: Infoj 메서드",
			action:           func(l *Logger) { l.Infoj(log.JSON{"user": "admin"}) },
			expectLogContent: "admin",
			expectLevel:      "info",
		},
		{
			name:             "성공: Warn 메서드",
			action:           func(l *Logger) { l.Warn("테스트 경고") },
			expectLogContent: "테스트 경고",
			expectLevel:      "warning",
		},
		{
			name:             "성공: Warnf 메서드",
			action:           func(l *Logger) { l.Warnf("경고 %v", true) },
			expectLogContent: "경고 true",
			expectLevel:      "warning",
		},
		{
			name:             "성공: Warnj 메서드",
			action:           func(l *Logger) { l.Warnj(log.JSON{"risk": "high"}) },
			expectLogContent: "high",
			expectLevel:      "warning",
		},
		{
			name:             "성공: Error 메서드",
			action:           func(l *Logger) { l.Error("테스트 에러") },
			expectLogContent: "테스트 에러",
			expectLevel:      "error",
		},
		{
			name:             "성공: Errorf 메서드",
			action:           func(l *Logger) { l.Errorf("에러 코드 %d", 500) },
			expectLogContent: "에러 코드 500",
			expectLevel:      "error",
		},
		{
			name:             "성공: Errorj 메서드",
			action:           func(l *Logger) { l.Errorj(log.JSON{"err": "failed"}) },
			expectLogContent: "failed",
			expectLevel:      "error",
		},
		{
			name:             "성공: Debug 메서드",
			action:           func(l *Logger) { l.Debug("테스트 디버그") },
			expectLogContent: "테스트 디버그",
			expectLevel:      "debug",
		},
		{
			name:             "성공: Debugf 메서드",
			action:           func(l *Logger) { l.Debugf("디버그 %s", "mode") },
			expectLogContent: "디버그 mode",
			expectLevel:      "debug",
		},
		{
			name:             "성공: Debugj 메서드",
			action:           func(l *Logger) { l.Debugj(log.JSON{"trace": "123"}) },
			expectLogContent: "123",
			expectLevel:      "debug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := logrus.New()
			l.SetOutput(&buf)
			l.SetLevel(applog.DebugLevel)           // 모든 레벨 로깅 허용
			l.SetFormatter(&applog.JSONFormatter{}) // 검증을 위해 JSON 포맷터 사용

			logger := &Logger{l}
			tt.action(logger)

			// 로그 파싱 및 검증
			var logEntry map[string]interface{}
			err := json.Unmarshal(buf.Bytes(), &logEntry)
			assert.NoError(t, err, "로그 파싱 실패")

			// 레벨 확인
			assert.Equal(t, tt.expectLevel, logEntry["level"], "로그 레벨이 일치해야 합니다")

			// 메시지 또는 필드 내용 확인
			logContent := buf.String()
			assert.Contains(t, logContent, tt.expectLogContent, "로그 내용이 포함되어야 합니다")
		})
	}
}

// TestLoggerAdapter_Methods_Panic은 Panic 메서드가 실제로 패닉을 유발하는지 검증합니다.
func TestLoggerAdapter_Methods_Panic(t *testing.T) {
	l := logrus.New()
	// Panic 발생 시 로그 출력을 억제하기 위해 버퍼 사용
	l.SetOutput(&bytes.Buffer{})
	logger := &Logger{l}

	assert.Panics(t, func() {
		logger.Panic("패닉 테스트")
	}, "Panic 메서드는 패닉을 발생시켜야 합니다")

	assert.Panics(t, func() {
		logger.Panicf("패닉 %d", 1)
	}, "Panicf 메서드는 패닉을 발생시켜야 합니다")

	assert.Panics(t, func() {
		logger.Panicj(log.JSON{"reason": "test"})
	}, "Panicj 메서드는 패닉을 발생시켜야 합니다")
}

// TestLoggerAdapter_OutputVerifier는 Output 및 SetOutput 메서드를 검증합니다.
func TestLoggerAdapter_OutputVerifier(t *testing.T) {
	l := logrus.New()
	logger := Logger{l}

	var buf bytes.Buffer
	logger.SetOutput(&buf)

	assert.Equal(t, &buf, logger.Output(), "설정된 Output Writer가 반환되어야 합니다")
}

// TestLoggerAdapter_PrefixVerifier는 Prefix 관련 메서드가 의도대로 무시(no-op)되는지 검증합니다.
func TestLoggerAdapter_PrefixVerifier(t *testing.T) {
	l := logrus.New()
	logger := Logger{l}

	logger.SetPrefix("test-prefix")
	assert.Equal(t, "", logger.Prefix(), "Prefix는 지원하지 않으므로 빈 문자열을 반환해야 합니다")
}

// TestLoggerAdapter_HeaderVerifier는 SetHeader 메서드가 안전하게 호출 가능한지(panic 없음) 검증합니다.
func TestLoggerAdapter_HeaderVerifier(t *testing.T) {
	l := logrus.New()
	logger := Logger{l}

	assert.NotPanics(t, func() {
		logger.SetHeader("test-header")
	}, "SetHeader는 호출 시 아무 동작도 하지 않아야 합니다 (panic 없음)")
}
