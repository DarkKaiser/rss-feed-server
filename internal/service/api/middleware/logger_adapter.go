package middleware

import (
	"io"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"github.com/labstack/gommon/log"
)

// Logger Echo의 log.Logger 인터페이스를 구현하는 어댑터입니다.
//
// 이 어댑터는 애플리케이션의 로거(applog.Logger)를 Echo 프레임워크의
// 로거 인터페이스(github.com/labstack/gommon/log.Logger)에 연결합니다.
//
// Echo는 자체 Logger 인터페이스를 요구하므로, 이 어댑터를 통해
// 애플리케이션 전체에서 일관된 로깅 시스템을 사용할 수 있습니다.
type Logger struct {
	*applog.Logger
}

// Output 현재 출력 Writer를 반환합니다.
func (l Logger) Output() io.Writer {
	return l.Logger.Out
}

// SetOutput 로그 출력 대상을 설정합니다.
func (l Logger) SetOutput(w io.Writer) {
	l.Logger.SetOutput(w)
}

// Prefix Echo의 Prefix 기능은 사용하지 않으므로 빈 문자열을 반환합니다.
func (l Logger) Prefix() string {
	return ""
}

// SetPrefix Echo의 Prefix 기능은 사용하지 않으므로 무시합니다.
func (l Logger) SetPrefix(string) {
	// 의도적으로 비워둠
}

// Level 애플리케이션 로그 레벨을 Echo 로그 레벨로 변환합니다.
func (l Logger) Level() log.Lvl {
	switch l.Logger.Level {
	case applog.DebugLevel:
		return log.DEBUG
	case applog.WarnLevel:
		return log.WARN
	case applog.ErrorLevel:
		return log.ERROR
	case applog.InfoLevel:
		return log.INFO
	case applog.PanicLevel, applog.FatalLevel, applog.TraceLevel:
		// Echo에 대응하는 레벨이 없으므로 OFF 반환
		return log.OFF
	}

	return log.OFF
}

// SetLevel Echo 로그 레벨을 애플리케이션 로그 레벨로 변환하여 설정합니다.
func (l Logger) SetLevel(lvl log.Lvl) {
	switch lvl {
	case log.DEBUG:
		l.Logger.SetLevel(applog.DebugLevel)
	case log.WARN:
		l.Logger.SetLevel(applog.WarnLevel)
	case log.ERROR:
		l.Logger.SetLevel(applog.ErrorLevel)
	case log.INFO:
		l.Logger.SetLevel(applog.InfoLevel)
	case log.OFF:
		// OFF는 대응하는 레벨이 없으므로 무시
	}
}

// SetHeader Echo의 Header 기능은 사용하지 않으므로 무시합니다.
func (l Logger) SetHeader(string) {
	// 의도적으로 비워둠
}

// 아래 메서드들은 Echo의 Logger 인터페이스 요구사항을 충족하기 위해
// 애플리케이션 로거의 해당 메서드로 위임합니다.

func (l Logger) Print(i ...interface{}) {
	l.Logger.Print(i...)
}

func (l Logger) Printf(format string, args ...interface{}) {
	l.Logger.Printf(format, args...)
}

func (l Logger) Printj(j log.JSON) {
	l.Logger.WithFields(applog.Fields(j)).Print()
}

func (l Logger) Debug(i ...interface{}) {
	l.Logger.Debug(i...)
}

func (l Logger) Debugf(format string, args ...interface{}) {
	l.Logger.Debugf(format, args...)
}

func (l Logger) Debugj(j log.JSON) {
	l.Logger.WithFields(applog.Fields(j)).Debug()
}

func (l Logger) Info(i ...interface{}) {
	l.Logger.Info(i...)
}

func (l Logger) Infof(format string, args ...interface{}) {
	l.Logger.Infof(format, args...)
}

func (l Logger) Infoj(j log.JSON) {
	l.Logger.WithFields(applog.Fields(j)).Info()
}

func (l Logger) Warn(i ...interface{}) {
	l.Logger.Warn(i...)
}

func (l Logger) Warnf(format string, args ...interface{}) {
	l.Logger.Warnf(format, args...)
}

func (l Logger) Warnj(j log.JSON) {
	l.Logger.WithFields(applog.Fields(j)).Warn()
}

func (l Logger) Error(i ...interface{}) {
	l.Logger.Error(i...)
}

func (l Logger) Errorf(format string, args ...interface{}) {
	l.Logger.Errorf(format, args...)
}

func (l Logger) Errorj(j log.JSON) {
	l.Logger.WithFields(applog.Fields(j)).Error()
}

func (l Logger) Fatal(i ...interface{}) {
	l.Logger.Fatal(i...)
}

func (l Logger) Fatalf(format string, args ...interface{}) {
	l.Logger.Fatalf(format, args...)
}

func (l Logger) Fatalj(j log.JSON) {
	l.Logger.WithFields(applog.Fields(j)).Fatal()
}

func (l Logger) Panic(i ...interface{}) {
	l.Logger.Panic(i...)
}

func (l Logger) Panicf(format string, args ...interface{}) {
	l.Logger.Panicf(format, args...)
}

func (l Logger) Panicj(j log.JSON) {
	l.Logger.WithFields(applog.Fields(j)).Panic()
}
