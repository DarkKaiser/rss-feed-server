package log

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLog(t *testing.T) {
	// 로그가 생성되는 폴더를 임시폴더로 설정한다.
	logDirParentPath = fmt.Sprintf("%s%s", t.TempDir(), string(os.PathSeparator))

	var checkDaysAgo = 10.
	var logDirPath = fmt.Sprintf("%s%s", logDirParentPath, logDirName)
	var appName = "log-package-testing"

	assert := assert.New(t)

	//
	// 디버그 모드로 초기화하면, 로그폴더 및 파일은 생성되지 않아야 한다.
	//
	assert.Nil(Init(true, appName, checkDaysAgo))

	_, err := os.Stat(logDirPath)
	assert.Equal(true, os.IsNotExist(err))

	//
	// 운영 모드로 초기화하면, 로그폴더 및 1개의 파일이 생성되어져야 한다.
	//
	lf := Init(false, appName, checkDaysAgo)
	assert.NotNil(lf)

	_, err = os.Stat(logDirPath)
	assert.Equal(false, os.IsNotExist(err))

	fiList, _ := ioutil.ReadDir(logDirPath)
	assert.Equal(1, len(fiList))

	lfName := fiList[0].Name()
	assert.True(strings.HasPrefix(lfName, appName))
	assert.True(strings.HasSuffix(lfName, logFileExtension))

	// 로그파일이 현재 열려있는 상태이므로 테스트를 위해 강제로 닫아준다.
	// 임시폴더는 테스트 종료 이후에 자동으로 삭제가 되는데, 로그파일이 열려있으면 임시폴더가 삭제되지 않는 문제가 발생하기도 한다.
	_ = lf.Close()

	// 로그파일을 강제로 닫아주었으므로 로거의 출력을 기본값으로 재설정한다.
	log.SetOutput(os.Stderr)

	//
	// 로그파일의 생성일자를 현재-(삭제기한+1)로 변경하여, 기한이 지난 로그파일이 삭제되는지 테스트한다.
	//
	mtime := time.Now().Add(time.Hour * 24 * (-1 * (time.Duration(checkDaysAgo) + 1))).Local()
	err = os.Chtimes(fmt.Sprintf("%s%s%s", logDirPath, string(os.PathSeparator), lfName), mtime, mtime)
	assert.Nil(err)

	// 삭제기한을 임의로 +2해서 로그파일이 삭제되지 않는지 확인한다.
	cleanOutOfLogFiles(appName, checkDaysAgo+2)

	fiList, _ = ioutil.ReadDir(logDirPath)
	assert.Equal(1, len(fiList))

	// 원래의 삭제기한으로 했을때 로그파일이 삭제되는지 확인한다.
	cleanOutOfLogFiles(appName, checkDaysAgo)

	fiList, _ = ioutil.ReadDir(logDirPath)
	assert.Equal(0, len(fiList))
}
