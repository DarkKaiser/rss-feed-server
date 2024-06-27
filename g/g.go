package g

import (
	"encoding/json"
	"fmt"
	"github.com/darkkaiser/rss-feed-server/utils"
	log "github.com/sirupsen/logrus"
	"os"
	"strings"
)

const (
	AppName    string = "rss-feed-server"
	AppVersion string = "0.0.3"

	appConfigFileName = AppName + ".json"
)

type RssFeedProviderSite string

const (
	RssFeedProviderSiteNaverCafe       RssFeedProviderSite = "NaverCafe"
	RssFeedProviderSiteYeosuCityHall   RssFeedProviderSite = "YeosuCityHall"
	RssFeedProviderSiteSsangbongSchool RssFeedProviderSite = "SsangbongSchool"
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
		TLSServer   bool   `json:"tls_server"`
		TLSCertFile string `json:"tls_cert_file"`
		TLSKeyFile  string `json:"tls_key_file"`
		ListenPort  int    `json:"listen_port"`
	} `json:"ws"`
	NotifyAPI struct {
		Url           string `json:"url"`
		AppKey        string `json:"app_key"`
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

func (c *AppConfig) validation() {
	var providerIDs = make([]string, 0)
	var providerSiteNaverCafeIDs = make([]string, 0)
	var providerSiteNaverCafeClubIDs = make([]string, 0)
	var providerSiteYeosuCityHallIDs = make([]string, 0)
	var providerSiteSsangbongSchoolIDs = make([]string, 0)

	for _, p := range c.RssFeed.Providers {
		panicIfContains(providerIDs, p.ID, fmt.Sprintf("RSS Feed Provider의 ID('%s')가 중복되었습니다.", p.ID))
		providerIDs = append(providerIDs, p.ID)

		panicIfEmpty(p.Site, "RSS Feed Provider Site가 입력되지 않았습니다.")

		switch RssFeedProviderSite(p.Site) {
		case RssFeedProviderSiteNaverCafe:
			site := "네이버 카페"

			c.validationRssFeedProviderConfig(site, p.Config, &providerSiteNaverCafeIDs)

			clubID, ok := p.Config.Data["club_id"].(string)
			if ok == false {
				log.Panicf("%s 파일의 내용이 유효하지 않습니다. '%s' %s의 ClubID가 입력되지 않았거나 타입이 유효하지 않습니다.", appConfigFileName, p.Config.ID, site)
			}
			panicIfEmpty(clubID, fmt.Sprintf("%s 파일의 내용이 유효하지 않습니다. '%s' %s의 ClubID가 입력되지 않았습니다.", appConfigFileName, p.Config.ID, site))
			panicIfContains(providerSiteNaverCafeClubIDs, clubID, fmt.Sprintf("%s의 ClubID('%s')가 중복되었습니다.", site, clubID))
			providerSiteNaverCafeClubIDs = append(providerSiteNaverCafeClubIDs, clubID)

		case RssFeedProviderSiteYeosuCityHall:
			c.validationRssFeedProviderConfig("여수시청 홈페이지", p.Config, &providerSiteYeosuCityHallIDs)

		case RssFeedProviderSiteSsangbongSchool:
			c.validationRssFeedProviderConfig("쌍봉초등학교 홈페이지", p.Config, &providerSiteSsangbongSchoolIDs)

		default:
			log.Panicf("%s 파일의 내용이 유효하지 않습니다. 지원하지 않는 RSS Feed Provider Site('%s')입니다.", appConfigFileName, p.Site)
		}
	}

	if c.WS.TLSServer == true {
		panicIfEmpty(c.WS.TLSCertFile, "웹서버의 Cert 파일 경로가 입력되지 않았습니다.")
		panicIfEmpty(c.WS.TLSKeyFile, "웹서버의 Key 파일 경로가 입력되지 않았습니다.")
	}
}

func (c *AppConfig) validationRssFeedProviderConfig(provider string, providerConfig *ProviderConfig, validatedProviderSiteIDs *[]string) {
	panicIfContains(*validatedProviderSiteIDs, providerConfig.ID, fmt.Sprintf("%s의 ID('%s')가 중복되었습니다.", provider, providerConfig.ID))
	*validatedProviderSiteIDs = append(*validatedProviderSiteIDs, providerConfig.ID)

	panicIfEmpty(providerConfig.Name, fmt.Sprintf("%s(ID:%s)의 Name이 입력되지 않았습니다.", provider, providerConfig.ID))
	panicIfEmpty(providerConfig.Url, fmt.Sprintf("%s(ID:%s)의 URL이 입력되지 않았습니다.", provider, providerConfig.ID))

	if strings.HasSuffix(providerConfig.Url, "/") == true {
		providerConfig.Url = providerConfig.Url[:len(providerConfig.Url)-1]
	}

	var boardIDs []string
	for _, b := range providerConfig.Boards {
		panicIfContains(boardIDs, b.ID, fmt.Sprintf("%s(ID:%s)의 게시판 ID('%s')가 중복되었습니다.", provider, providerConfig.ID, b.ID))
		boardIDs = append(boardIDs, b.ID)

		panicIfEmpty(b.Name, fmt.Sprintf("%s(ID:%s)의 게시판 Name이 입력되지 않았습니다.", provider, providerConfig.ID))
	}
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
	data, err := os.ReadFile(appConfigFileName)
	utils.CheckErr(err)

	var config AppConfig
	err = json.Unmarshal(data, &config)
	utils.CheckErr(err)

	config.validation()

	return &config
}

func panicIfEmpty(value, message string) {
	if strings.TrimSpace(value) == "" {
		log.Panicf("%s 파일의 내용이 유효하지 않습니다. %s", appConfigFileName, message)
	}
}

func panicIfContains(s []string, e, message string) {
	if utils.Contains(s, e) == true {
		log.Panicf("%s 파일의 내용이 유효하지 않습니다. %s", appConfigFileName, message)
	}
}
