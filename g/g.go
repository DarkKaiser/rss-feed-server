package g

import (
	"encoding/json"
	"fmt"
	"github.com/darkkaiser/rss-feed-server/utils"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"strings"
)

const (
	AppName    string = "rss-feed-server"
	AppVersion string = "0.3.0"

	AppConfigFileName = AppName + ".json"
)

type RssFeedSupportedSite string

const (
	// RSS Feed 서비스가 지원 가능한 사이트
	RssFeedSupportedSiteNaverCafe RssFeedSupportedSite = "NaverCafe"
	RssFeedSupportedSiteYeosuCity RssFeedSupportedSite = "YeosuCity"
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
		Type     string `json:"type"`
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

	//
	// 파일 내용에 대해 유효성 검사를 한다.
	//
	var rssFeedProviderIDs = make([]string, 0)
	var siteYeosuCityIDs = make([]string, 0)
	var siteNaverCafeIDs = make([]string, 0)
	var siteNaverCafeClubIDs = make([]string, 0)

	for _, p := range config.RssFeed.Providers {
		panicIfContains(rssFeedProviderIDs, p.ID, fmt.Sprintf("RSS Feed Provider의 ID('%s')가 중복되었습니다.", p.ID))
		rssFeedProviderIDs = append(rssFeedProviderIDs, p.ID)

		panicIfEmpty(p.Site, "RSS Feed Provider의 Site가 입력되지 않았습니다.")

		switch RssFeedSupportedSite(p.Site) {
		case RssFeedSupportedSiteNaverCafe:
			validationCheckRssFeedSupportedSiteConfig("네이버 카페", p.Config, &siteNaverCafeIDs)

			clubID, ok := p.Config.Data["club_id"].(string)
			if ok == false {
				log.Panicf("%s 파일의 내용이 유효하지 않습니다. '%s' 네이버 카페의 ClubID가 입력되지 않았거나 타입이 유효하지 않습니다.", AppConfigFileName, p.Config.ID)
			}
			panicIfContains(siteNaverCafeClubIDs, clubID, fmt.Sprintf("네이버 카페의 ClubID('%s')가 중복되었습니다.", clubID))
			siteNaverCafeClubIDs = append(siteNaverCafeClubIDs, clubID)

		case RssFeedSupportedSiteYeosuCity:
			validationCheckRssFeedSupportedSiteConfig("여수시 홈페이지", p.Config, &siteYeosuCityIDs)

		default:
			log.Panicf("%s 파일의 내용이 유효하지 않습니다. 지원되지 않는 RSS Feed Provider의 Site('%s')입니다.", AppConfigFileName, p.Site)
		}
	}

	if config.WS.TLSServer == true {
		panicIfEmpty(config.WS.CertFilePath, "웹서버의 Cert 파일 경로가 입력되지 않았습니다.")
		panicIfEmpty(config.WS.KeyFilePath, "웹서버의 Key 파일 경로가 입력되지 않았습니다.")
	}

	panicIfEmpty(config.NotifyAPI.Url, "NotifyAPI의 Url이 입력되지 않았습니다.")
	panicIfEmpty(config.NotifyAPI.APIKey, "NotifyAPI의 APIKey가 입력되지 않았습니다.")
	panicIfEmpty(config.NotifyAPI.ApplicationID, "NotifyAPI의 ApplicationID가 입력되지 않았습니다.")

	return &config
}

func validationCheckRssFeedSupportedSiteConfig(site string, siteConfig *ProviderConfig, siteIDs *[]string) {
	panicIfContains(*siteIDs, siteConfig.ID, fmt.Sprintf("%s의 ID('%s')가 중복되었습니다.", site, siteConfig.ID))
	*siteIDs = append(*siteIDs, siteConfig.ID)

	panicIfEmpty(siteConfig.Name, fmt.Sprintf("%s(ID:%s)의 Name이 입력되지 않았습니다.", site, siteConfig.ID))
	panicIfEmpty(siteConfig.Url, fmt.Sprintf("%s(ID:%s)의 URL이 입력되지 않았습니다.", site, siteConfig.ID))

	if strings.HasSuffix(siteConfig.Url, "/") == true {
		siteConfig.Url = siteConfig.Url[:len(siteConfig.Url)-1]
	}

	var boardIDs []string
	for _, b := range siteConfig.Boards {
		panicIfContains(boardIDs, b.ID, fmt.Sprintf("%s(ID:%s)의 게시판 ID('%s')가 중복되었습니다.", site, siteConfig.ID, b.ID))
		boardIDs = append(boardIDs, b.ID)

		panicIfEmpty(b.Name, fmt.Sprintf("%s(ID:%s)의 게시판 Name이 입력되지 않았습니다.", site, siteConfig.ID))
	}
}

func panicIfEmpty(value, message string) {
	if value == "" {
		log.Panicf("%s 파일의 내용이 유효하지 않습니다. %s", AppConfigFileName, message)
	}
}

func panicIfContains(s []string, e, message string) {
	if utils.Contains(s, e) == true {
		log.Panicf("%s 파일의 내용이 유효하지 않습니다. %s", AppConfigFileName, message)
	}
}
