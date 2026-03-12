package middleware

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"io"
	"strconv"
	"time"
)

type Logger struct {
	*applog.Logger
}

func (l Logger) Output() io.Writer {
	return l.Out
}

func (l Logger) SetOutput(w io.Writer) {
	applog.SetOutput(w)
}

func (l Logger) Prefix() string {
	return ""
}

func (l Logger) SetPrefix(string) {
	// do nothing
}

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
	case applog.PanicLevel:
		return log.OFF
	case applog.FatalLevel:
		return log.OFF
	case applog.TraceLevel:
		return log.OFF
	}

	return log.OFF
}

func (l Logger) SetLevel(lvl log.Lvl) {
	switch lvl {
	case log.DEBUG:
		applog.SetLevel(applog.DebugLevel)
	case log.WARN:
		applog.SetLevel(applog.WarnLevel)
	case log.ERROR:
		applog.SetLevel(applog.ErrorLevel)
	case log.INFO:
		applog.SetLevel(applog.InfoLevel)
	}
}

func (l Logger) SetHeader(string) {
	// do nothing
}

func (l Logger) Print(i ...interface{}) {
	applog.Info(i...)
}

func (l Logger) Printf(format string, args ...interface{}) {
	applog.Infof(format, args...)
}

func (l Logger) Printj(j log.JSON) {
	applog.WithFields(applog.Fields(j)).Print()
}

func (l Logger) Debug(i ...interface{}) {
	applog.Debug(i...)
}

func (l Logger) Debugf(format string, args ...interface{}) {
	applog.Debugf(format, args...)
}

func (l Logger) Debugj(j log.JSON) {
	applog.WithFields(applog.Fields(j)).Debug()
}

func (l Logger) Info(i ...interface{}) {
	applog.Info(i...)
}

func (l Logger) Infof(format string, args ...interface{}) {
	applog.Infof(format, args...)
}

func (l Logger) Infoj(j log.JSON) {
	applog.WithFields(applog.Fields(j)).Info()
}

func (l Logger) Warn(i ...interface{}) {
	applog.Warn(i...)
}

func (l Logger) Warnf(format string, args ...interface{}) {
	applog.Warnf(format, args...)
}

func (l Logger) Warnj(j log.JSON) {
	applog.WithFields(applog.Fields(j)).Warn()
}

func (l Logger) Error(i ...interface{}) {
	applog.Error(i...)
}

func (l Logger) Errorf(format string, args ...interface{}) {
	applog.Errorf(format, args...)
}

func (l Logger) Errorj(j log.JSON) {
	applog.WithFields(applog.Fields(j)).Error()
}

func (l Logger) Fatal(i ...interface{}) {
	applog.Fatal(i...)
}

func (l Logger) Fatalf(format string, args ...interface{}) {
	applog.Fatalf(format, args...)
}

func (l Logger) Fatalj(j log.JSON) {
	applog.WithFields(applog.Fields(j)).Fatal()
}

func (l Logger) Panic(i ...interface{}) {
	applog.Panic(i...)
}

func (l Logger) Panicf(format string, args ...interface{}) {
	applog.Panicf(format, args...)
}

func (l Logger) Panicj(j log.JSON) {
	applog.WithFields(applog.Fields(j)).Panic()
}

func logrusMiddlewareHandler(c echo.Context, next echo.HandlerFunc) error {
	req := c.Request()
	res := c.Response()
	start := time.Now()
	if err := next(c); err != nil {
		c.Error(err)
	}
	stop := time.Now()

	p := req.URL.Path
	if p == "" {
		p = "/"
	}

	bytesIn := req.Header.Get(echo.HeaderContentLength)
	if bytesIn == "" {
		bytesIn = "0"
	}

	applog.WithFields(map[string]interface{}{
		"time_rfc3339":  time.Now().Format(time.RFC3339),
		"remote_ip":     c.RealIP(),
		"host":          req.Host,
		"uri":           req.RequestURI,
		"method":        req.Method,
		"path":          p,
		"referer":       req.Referer(),
		"user_agent":    req.UserAgent(),
		"status":        res.Status,
		"latency":       strconv.FormatInt(stop.Sub(start).Nanoseconds()/1000, 10),
		"latency_human": stop.Sub(start).String(),
		"bytes_in":      bytesIn,
		"bytes_out":     strconv.FormatInt(res.Size, 10),
	}).Info("echo log")

	return nil
}

func logrusLogger(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		return logrusMiddlewareHandler(c, next)
	}
}

func LogrusLogger() echo.MiddlewareFunc {
	return logrusLogger
}
