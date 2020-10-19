package log

import (
	"fmt"
	"github.com/darkkaiser/rss-server/utils"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"math"
	"os"
	"runtime"
	"strings"
	"time"
)

const (
	logFileDir       string = "logs"
	logFileExtension string = "log"
)

func init() {
	log.SetLevel(log.TraceLevel)
	log.SetReportCaller(true)
	log.SetFormatter(&log.TextFormatter{
		CallerPrettyfier: func(frame *runtime.Frame) (function string, file string) {
			const shortPath = "github.com/darkkaiser/rss-server"

			function = fmt.Sprintf("%s(line:%d)", frame.Function, frame.Line)
			if strings.HasPrefix(function, shortPath) == true {
				function = "..." + function[len(shortPath):]
			}

			return
		},
	})
}

func InitLog(debug bool, appName string, checkDaysAgo float64) {
	if debug == true {
		return
	}

	// 로그 파일이 쌓이는 폴더를 생성한다.
	_, err := os.Stat(logFileDir)
	if os.IsNotExist(err) == true {
		utils.CheckErr(os.MkdirAll(logFileDir, 0755))
	}

	// 로그 파일을 생성한다.
	t := time.Now()
	filePath := fmt.Sprintf("%s%s%s-%d%02d%02d%02d%02d%02d.%s", logFileDir, string(os.PathSeparator), appName, t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), logFileExtension)
	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	utils.CheckErr(err)

	log.SetOutput(file)

	// 일정 시간이 지난 로그 파일을 모두 삭제한다.
	cleanOutOfLogFiles(appName, checkDaysAgo)
}

func cleanOutOfLogFiles(appName string, checkDaysAgo float64) {
	fiList, err := ioutil.ReadDir(logFileDir)
	if err != nil {
		return
	}

	t := time.Now()
	for _, fi := range fiList {
		fileName := fi.Name()
		if strings.HasPrefix(fileName, appName) == false || strings.HasSuffix(fileName, logFileExtension) == false {
			continue
		}

		daysAgo := math.Abs(t.Sub(fi.ModTime()).Hours()) / 24
		if daysAgo >= checkDaysAgo {
			filePath := logFileDir + string(os.PathSeparator) + fileName

			err = os.Remove(filePath)
			if err == nil {
				log.Infof("오래된 로그파일 삭제 성공(%s)", filePath)
			} else {
				log.Infof("오래된 로그파일 삭제 실패(%s), %s", filePath, err)
			}
		}
	}
}
