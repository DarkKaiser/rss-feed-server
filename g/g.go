package g

import (
	"encoding/json"
	"github.com/darkkaiser/rss-server/utils"
	"io/ioutil"
)

const (
	AppName    string = "rss-server"
	AppVersion string = "0.1.0"

	AppConfigFileName = AppName + ".json"
)

// Convert JSON to Go struct : https://mholt.github.io/json-to-go/
type AppConfig struct {
	Debug     bool `json:"debug"`
	NotifyAPI struct {
		Url           string `json:"url"`
		APIKey        string `json:"api_key"`
		ApplicationID string `json:"application_id"`
	} `json:"notify_api"`
	// @@@@@
	Notifiers struct {
		DefaultNotifierID string `json:"default_notifier_id"`
		Telegrams         []struct {
			ID     string `json:"id"`
			Token  string `json:"token"`
			ChatID int64  `json:"chat_id"`
		} `json:"telegrams"`
	} `json:"notifiers"`
	// @@@@@
	Tasks []struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Commands []struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Scheduler   struct {
				Runnable bool   `json:"runnable"`
				TimeSpec string `json:"time_spec"`
			} `json:"scheduler"`
			Notifier struct {
				Usable bool `json:"usable"`
			} `json:"notifier"`
			DefaultNotifierID string                 `json:"default_notifier_id"`
			Data              map[string]interface{} `json:"data"`
		} `json:"commands"`
		Data map[string]interface{} `json:"data"`
	} `json:"tasks"`
}

func InitAppConfig() *AppConfig {
	data, err := ioutil.ReadFile(AppConfigFileName)
	utils.CheckErr(err)

	var config AppConfig
	err = json.Unmarshal(data, &config)
	utils.CheckErr(err)

	// @@@@@
	////
	//// 파일 내용에 대해 유효성 검사를 한다.
	////
	//var notifierIDs []string
	//for _, telegram := range config.Notifiers.Telegrams {
	//	if utils.Contains(notifierIDs, telegram.ID) == true {
	//		log.Panicf("%s 파일의 내용이 유효하지 않습니다. NotifierID(%s)가 중복되었습니다.", AppConfigFileName, telegram.ID)
	//	}
	//	notifierIDs = append(notifierIDs, telegram.ID)
	//}
	//if utils.Contains(notifierIDs, config.Notifiers.DefaultNotifierID) == false {
	//	log.Panicf("%s 파일의 내용이 유효하지 않습니다. 전체 NotifierID 목록에서 기본 NotifierID(%s)가 존재하지 않습니다.", AppConfigFileName, config.Notifiers.DefaultNotifierID)
	//}
	//
	//var taskIDs []string
	//for _, t := range config.Tasks {
	//	if utils.Contains(taskIDs, t.ID) == true {
	//		log.Panicf("%s 파일의 내용이 유효하지 않습니다. TaskID(%s)가 중복되었습니다.", AppConfigFileName, t.ID)
	//	}
	//	taskIDs = append(taskIDs, t.ID)
	//
	//	var commandIDs []string
	//	for _, c := range t.Commands {
	//		if utils.Contains(commandIDs, c.ID) == true {
	//			log.Panicf("%s 파일의 내용이 유효하지 않습니다. CommandID(%s)가 중복되었습니다.", AppConfigFileName, c.ID)
	//		}
	//		commandIDs = append(commandIDs, c.ID)
	//
	//		if utils.Contains(notifierIDs, c.DefaultNotifierID) == false {
	//			log.Panicf("%s 파일의 내용이 유효하지 않습니다. 전체 NotifierID 목록에서 %s::%s Task의 기본 NotifierID(%s)가 존재하지 않습니다.", AppConfigFileName, t.ID, c.ID, c.DefaultNotifierID)
	//		}
	//	}
	//}
	//
	//if len(config.NotifyAPI.APIKey) == 0 {
	//	log.Panicf("%s 파일의 내용이 유효하지 않습니다. NotifyAPI의 APIKey가 입력되지 않았습니다.", AppConfigFileName)
	//}
	//
	//var applicationIDs []string
	//for _, app := range config.NotifyAPI.Applications {
	//	if utils.Contains(applicationIDs, app.ID) == true {
	//		log.Panicf("%s 파일의 내용이 유효하지 않습니다. ApplicationID(%s)가 중복되었습니다.", AppConfigFileName, app.ID)
	//	}
	//	applicationIDs = append(applicationIDs, app.ID)
	//
	//	if utils.Contains(notifierIDs, app.DefaultNotifierID) == false {
	//		log.Panicf("%s 파일의 내용이 유효하지 않습니다. 전체 NotifierID 목록에서 %s Application의 기본 NotifierID(%s)가 존재하지 않습니다.", AppConfigFileName, app.ID, app.DefaultNotifierID)
	//	}
	//}

	return &config
}
