package notifyapi

import (
	"encoding/json"
	"fmt"
	"github.com/darkkaiser/rss-server/g"
	log "github.com/sirupsen/logrus"
	"os/exec"
)

var (
	url           string
	apiKey        string
	applicationID string
)

type notifyMessage struct {
	Message       string `json:"message"`
	ErrorOccured  bool   `json:"error_occured"`
	ApplicationID string `json:"application_id"`
}

func Init(config *g.AppConfig) {
	url = config.NotifyAPI.Url
	apiKey = config.NotifyAPI.APIKey
	applicationID = config.NotifyAPI.ApplicationID
}

func SendNotifyMessage(message string, errorOccured bool) {
	notifyMessage := notifyMessage{
		Message:       message,
		ErrorOccured:  errorOccured,
		ApplicationID: applicationID,
	}

	jsonBytes, err := json.Marshal(notifyMessage)
	if err != nil {
		log.Printf("NotifyAPI 서비스 호출이 실패하였습니다. (error:%s)", err)
		return
	}
	/* WD My Cloud에서 gcc가 설치되지 않아 http 패키지를 사용할 수 없는 현상이 발생하여 curl을 이용한 방법으로 변경한다.
	 * (http 패키지 내부에서 gcc를 사용하는 것 같음)
	 *
	reqBody := bytes.NewBuffer(jsonBytes)

	req, err := http.NewRequest("POST", url, reqBody)
	if err != nil {
		log.Printf("NotifyAPI 서비스 호출이 실패하였습니다. (error:%s)", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Cache-Control", "no-cache")

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		log.Printf("NotifyAPI 서비스 호출이 실패하였습니다. (error:%s)", err)
		return
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		log.Printf("NotifyAPI 서비스 호출이 실패하였습니다. (HTTP 상태코드:%d)", res.StatusCode)
	}
	*/

	// ※ 윈도우에서 curl을 호출하게 되면 텔레그램에서 한글이 깨지는 현상이 발생한다. 하지만 리눅스에서는 그런 현상이 발생하지 않늗다.
	// 윈도우에서도 한글이 깨지지 않도록 하려면 커맨드창에 chcp 65001을 입력하고 사용하면 된다고 한다.(확인까지는 해보지 않았음)
	cmd := exec.Command("curl", "-X", "POST", "-H", "Content-Type: application/json", "-H", fmt.Sprintf("Authorization: Bearer %s", apiKey), "-d", string(jsonBytes), url)
	err = cmd.Run()
	if err != nil {
		log.Printf("NotifyAPI 서비스 호출이 실패하였습니다. (error:%s)", err)
	}
}
