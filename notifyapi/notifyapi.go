package notifyapi

import (
	"bytes"
	"encoding/json"
	"github.com/darkkaiser/rss-feed-server/g"
	log "github.com/sirupsen/logrus"
	"net/http"
)

var (
	url           string
	apiKey        string
	applicationID string
)

type notifyMessage struct {
	Message       string `json:"message"`
	ErrorOccurred bool   `json:"error_occurred"`
	ApplicationID string `json:"application_id"`
}

func Init(config *g.AppConfig) {
	url = config.NotifyAPI.Url
	apiKey = config.NotifyAPI.APIKey
	applicationID = config.NotifyAPI.ApplicationID
}

//noinspection GoUnhandledErrorResult
func SendNotifyMessage(message string, errorOccurred bool) bool {
	notifyMessage := notifyMessage{
		ApplicationID: applicationID,
		Message:       message,
		ErrorOccurred: errorOccurred,
	}

	jsonBytes, err := json.Marshal(notifyMessage)
	if err != nil {
		log.Errorf("NotifyAPI 서비스 호출이 실패하였습니다. (error:%s)", err)
		return false
	}
	reqBody := bytes.NewBuffer(jsonBytes)

	req, err := http.NewRequest("POST", url, reqBody)
	if err != nil {
		log.Errorf("NotifyAPI 서비스 호출이 실패하였습니다. (error:%s)", err)
		return false
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Cache-Control", "no-cache")

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		log.Errorf("NotifyAPI 서비스 호출이 실패하였습니다. (error:%s)", err)
		return false
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		log.Errorf("NotifyAPI 서비스 호출이 실패하였습니다. (HTTP 상태코드:%d)", res.StatusCode)
	}

	return true
}
