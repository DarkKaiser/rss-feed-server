package notifyapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	log "github.com/sirupsen/logrus"
	"net/http"
	"strings"
)

type Config struct {
	valid bool

	Url           string
	AppKey        string
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
	if strings.TrimSpace(c.AppKey) == "" {
		log.Warn("NotifyAPI의 APP_KEY가 입력되지 않았습니다. NotifyAPI를 사용할 수 없습니다.")
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

type sendMessage struct {
	ApplicationID string `json:"application_id"`
	Message       string `json:"message"`
	ErrorOccurred bool   `json:"error_occurred"`
}

//goland:noinspection GoUnhandledErrorResult
func Send(message string, errorOccurred bool) bool {
	if config.valid == false {
		return false
	}

	if strings.TrimSpace(message) == "" {
		log.Warn("NotifyAPI로 전달하려는 메시지가 빈 문자열입니다. NotifyAPI를 사용할 수 없습니다.")
		return false
	}

	data, err := json.Marshal(sendMessage{
		ApplicationID: config.ApplicationID,
		Message:       message,
		ErrorOccurred: errorOccurred,
	})
	if err != nil {
		log.Errorf("NotifyAPI 호출이 실패하였습니다. (error:%s)", err)
		return false
	}
	reqBody := bytes.NewBuffer(data)
	req, err := http.NewRequest("POST", fmt.Sprintf("%s?app_key=%s", config.Url, config.AppKey), reqBody)
	if err != nil {
		log.Errorf("NotifyAPI 호출이 실패하였습니다. (error:%s)", err)
		return false
	}
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
