package middleware

import (
	"github.com/labstack/echo"
	"github.com/labstack/gommon/log"
	"github.com/sirupsen/logrus"
	"io"
	"strconv"
	"time"
)

type Logger struct {
	*logrus.Logger
}

func (l Logger) Output() io.Writer {
	return l.Out
}

func (l Logger) SetOutput(w io.Writer) {
	logrus.SetOutput(w)
}

func (l Logger) Prefix() string {
	return ""
}

func (l Logger) SetPrefix(string) {
	// do nothing
}

func (l Logger) Level() log.Lvl {
	switch l.Logger.Level {
	case logrus.DebugLevel:
		return log.DEBUG
	case logrus.WarnLevel:
		return log.WARN
	case logrus.ErrorLevel:
		return log.ERROR
	case logrus.InfoLevel:
		return log.INFO
	}

	return log.OFF
}

func (l Logger) SetLevel(lvl log.Lvl) {
	switch lvl {
	case log.DEBUG:
		logrus.SetLevel(logrus.DebugLevel)
	case log.WARN:
		logrus.SetLevel(logrus.WarnLevel)
	case log.ERROR:
		logrus.SetLevel(logrus.ErrorLevel)
	case log.INFO:
		logrus.SetLevel(logrus.InfoLevel)
	}
}

func (l Logger) SetHeader(string) {
	// do nothing
}

func (l Logger) Print(i ...interface{}) {
	logrus.Print(i...)
}

func (l Logger) Printf(format string, args ...interface{}) {
	logrus.Printf(format, args...)
}

func (l Logger) Printj(j log.JSON) {
	logrus.WithFields(logrus.Fields(j)).Print()
}

func (l Logger) Debug(i ...interface{}) {
	logrus.Debug(i...)
}

func (l Logger) Debugf(format string, args ...interface{}) {
	logrus.Debugf(format, args...)
}

func (l Logger) Debugj(j log.JSON) {
	logrus.WithFields(logrus.Fields(j)).Debug()
}

func (l Logger) Info(i ...interface{}) {
	logrus.Info(i...)
}

func (l Logger) Infof(format string, args ...interface{}) {
	logrus.Infof(format, args...)
}

func (l Logger) Infoj(j log.JSON) {
	logrus.WithFields(logrus.Fields(j)).Info()
}

func (l Logger) Warn(i ...interface{}) {
	logrus.Warn(i...)
}

func (l Logger) Warnf(format string, args ...interface{}) {
	logrus.Warnf(format, args...)
}

func (l Logger) Warnj(j log.JSON) {
	logrus.WithFields(logrus.Fields(j)).Warn()
}

func (l Logger) Error(i ...interface{}) {
	logrus.Error(i...)
}

func (l Logger) Errorf(format string, args ...interface{}) {
	logrus.Errorf(format, args...)
}

func (l Logger) Errorj(j log.JSON) {
	logrus.WithFields(logrus.Fields(j)).Error()
}

func (l Logger) Fatal(i ...interface{}) {
	logrus.Fatal(i...)
}

func (l Logger) Fatalf(format string, args ...interface{}) {
	logrus.Fatalf(format, args...)
}

func (l Logger) Fatalj(j log.JSON) {
	logrus.WithFields(logrus.Fields(j)).Fatal()
}

func (l Logger) Panic(i ...interface{}) {
	logrus.Panic(i...)
}

func (l Logger) Panicf(format string, args ...interface{}) {
	logrus.Panicf(format, args...)
}

func (l Logger) Panicj(j log.JSON) {
	logrus.WithFields(logrus.Fields(j)).Panic()
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

	logrus.WithFields(map[string]interface{}{
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
