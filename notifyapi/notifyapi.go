package notifyapi

import (
	"bytes"
	"encoding/json"
	log "github.com/sirupsen/logrus"
	"net/http"
	"strings"
)

type Config struct {
	valid bool

	Url           string
	APIKey        string
	ApplicationID string
}

func (c *Config) validation() bool {
	c.valid = false

	if strings.TrimSpace(c.Url) == "" {
		log.Warn("NotifyAPI의 Url이 입력되지 않았습니다. NotifyAPI를 사용할 수 없습니다.")
		return false
	}
	if strings.HasPrefix(c.Url, "http://") == false && strings.HasPrefix(c.Url, "https://") == false {
		log.Warn("유효하지 않은 NotifyAPI의 Url이 입력되었습니다. NotifyAPI를 사용할 수 없습니다.")
		return false
	}
	if strings.TrimSpace(c.APIKey) == "" {
		log.Warn("NotifyAPI의 APIKey가 입력되지 않았습니다. NotifyAPI를 사용할 수 없습니다.")
		return false
	}
	if strings.TrimSpace(c.ApplicationID) == "" {
		log.Warn("NotifyAPI의 ApplicationID가 입력되지 않았습니다. NotifyAPI를 사용할 수 없습니다.")
		return false
	}

	c.valid = true

	return true
}

var config *Config

func Init(c *Config) {
	config = c
	config.validation()
}

type notifyMessage struct {
	ApplicationID string `json:"application_id"`
	Message       string `json:"message"`
	ErrorOccurred bool   `json:"error_occurred"`
}

//goland:noinspection GoUnhandledErrorResult
func SendNotifyMessage(message string, errorOccurred bool) bool {
	if config.valid == false {
		log.Warn("NotifyAPI의 설정값이 유효하지 않습니다. NotifyAPI를 사용할 수 없습니다.")
		return false
	}
	if strings.TrimSpace(message) == "" {
		log.Warn("NotifyAPI로 전달하려는 메시지가 빈 문자열입니다. NotifyAPI를 사용할 수 없습니다.")
		return false
	}

	m := notifyMessage{
		ApplicationID: config.ApplicationID,
		Message:       message,
		ErrorOccurred: errorOccurred,
	}

	data, err := json.Marshal(m)
	if err != nil {
		log.Errorf("NotifyAPI 호출이 실패하였습니다. (error:%s)", err)
		return false
	}
	reqBody := bytes.NewBuffer(data)

	req, err := http.NewRequest("POST", config.Url, reqBody)
	if err != nil {
		log.Errorf("NotifyAPI 호출이 실패하였습니다. (error:%s)", err)
		return false
	}

	req.Header.Set("Authorization", "Bearer "+config.APIKey)
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		log.Errorf("NotifyAPI 호출이 실패하였습니다. (error:%s)", err)
		return false
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		log.Errorf("NotifyAPI 호출이 실패하였습니다. (HTTP 상태코드:%d)", res.StatusCode)
		return false
	}

	return true
}
