package g

import (
	"encoding/json"
	"github.com/darkkaiser/rss-feed-server/utils"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
)

const (
	AppName    string = "rss-feed-server"
	AppVersion string = "0.2.0"

	AppConfigFileName = AppName + ".json"
)

type AppConfig struct {
	Debug   bool `json:"debug"`
	RssFeed struct {
		MaxItemCount uint `json:"max_item_count"`
		Providers    []*struct {
			ID                string          `json:"id"`
			Site              string          `json:"site"`
			Config            *ProviderConfig `json:"config"`
			CrawlingScheduler struct {
				TimeSpec string `json:"time_spec"`
			} `json:"crawling_scheduler"`
		} `json:"providers"`
	} `json:"rss_feed"`
	WS struct {
		TLSServer    bool   `json:"tls_server"`
		CertFilePath string `json:"certfile_path"`
		KeyFilePath  string `json:"keyfile_path"`
		ListenPort   int    `json:"listen_port"`
	} `json:"ws"`
	NotifyAPI struct {
		Url           string `json:"url"`
		APIKey        string `json:"api_key"`
		ApplicationID string `json:"application_id"`
	} `json:"notify_api"`
}

type ProviderConfig struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Url         string `jsin:"url"`
	Boards      []*struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Category string `json:"category"`
	} `json:"boards"`
	ArticleArchiveDate uint                   `json:"article_archive_date"`
	Data               map[string]interface{} `json:"data"`
}

func (c *ProviderConfig) ContainsBoard(boardID string) bool {
	for _, board := range c.Boards {
		if board.ID == boardID {
			return true
		}
	}

	return false
}

func InitAppConfig() *AppConfig {
	data, err := ioutil.ReadFile(AppConfigFileName)
	utils.CheckErr(err)

	var config AppConfig
	err = json.Unmarshal(data, &config)
	utils.CheckErr(err)

	// @@@@@ url 마지막에 / 있는지 체크
	//////////////////////////
	//
	// 파일 내용에 대해 유효성 검사를 한다.
	//
	var naverCafeIDs []string
	//var naverCafeClubIDs []string
	for _, c := range config.RssFeed.Providers {
		if utils.Contains(naverCafeIDs, c.ID) == true {
			log.Panicf("%s 파일의 내용이 유효하지 않습니다. 네이버 카페 ID(%s)가 중복되었습니다.", AppConfigFileName, c.ID)
		}
		naverCafeIDs = append(naverCafeIDs, c.ID)

		//@@@@@
		//if utils.Contains(naverCafeClubIDs, c.ClubID) == true {
		//	log.Panicf("%s 파일의 내용이 유효하지 않습니다. 네이버 카페 ClubID(%s)가 중복되었습니다.", AppConfigFileName, c.ClubID)
		//}
		//naverCafeClubIDs = append(naverCafeClubIDs, c.ClubID)

		//if c.Name == "" {
		//	log.Panicf("%s 파일의 내용이 유효하지 않습니다. '%s' 네이버 카페의 Name이 입력되지 않았습니다.", AppConfigFileName, c.ID)
		//}
		//
		//var boardIDs []string
		//for _, b := range c.Boards {
		//	if utils.Contains(boardIDs, b.ID) == true {
		//		log.Panicf("%s 파일의 내용이 유효하지 않습니다. '%s' 네이버 카페의 게시판 ID(%s)가 중복되었습니다.", AppConfigFileName, c.Name, b.ID)
		//	}
		//	boardIDs = append(boardIDs, b.ID)
		//
		//	if b.Name == "" {
		//		log.Panicf("%s 파일의 내용이 유효하지 않습니다. '%s' 네이버 카페의 게시판 Name이 입력되지 않았습니다.", AppConfigFileName, c.Name)
		//	}
		//}
	}
	//////////////////////////

	if config.WS.TLSServer == true {
		if config.WS.CertFilePath == "" {
			log.Panicf("%s 파일의 내용이 유효하지 않습니다. 웹서버의 Cert 파일 경로가 입력되지 않았습니다.", AppConfigFileName)
		}
		if config.WS.KeyFilePath == "" {
			log.Panicf("%s 파일의 내용이 유효하지 않습니다. 웹서버의 Key 파일 경로가 입력되지 않았습니다.", AppConfigFileName)
		}
	}

	if config.NotifyAPI.Url == "" {
		log.Panicf("%s 파일의 내용이 유효하지 않습니다. NotifyAPI의 Url이 입력되지 않았습니다.", AppConfigFileName)
	}
	if config.NotifyAPI.APIKey == "" {
		log.Panicf("%s 파일의 내용이 유효하지 않습니다. NotifyAPI의 APIKey가 입력되지 않았습니다.", AppConfigFileName)
	}
	if config.NotifyAPI.ApplicationID == "" {
		log.Panicf("%s 파일의 내용이 유효하지 않습니다. NotifyAPI의 ApplicationID가 입력되지 않았습니다.", AppConfigFileName)
	}

	return &config
}
