package g

import (
	"fmt"
	"github.com/darkkaiser/rss-feed-server/utils"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

func TestAppConfig_Validation(t *testing.T) {
	assert := assert.New(t)

	// 테스트 환경에서는 g 패키지 경로에서 설정파일을 읽어들이므로 강제로 부모 폴더로 경로를 변경해준다.
	assert.NoError(os.Chdir(".."))

	var config *AppConfig
	assert.NotPanics(func() { config = InitAppConfig() })
	assert.NotNil(config)
	assert.NotPanics(func() { config.validation() })

	// 테스트를 위해 최소 2개 이상의 RSS Feed Provider가 등록되어져 있어야 한다.
	assert.Greater(len(config.RssFeed.Providers), 1)

	var tempBool bool
	var tempString1, tempString2 string

	// RSS Feed Provider의 ID가 중복된 경우 패닉 발생
	tempString1 = config.RssFeed.Providers[0].ID
	config.RssFeed.Providers[0].ID = config.RssFeed.Providers[1].ID
	assert.Panics(func() { config.validation() })
	config.RssFeed.Providers[0].ID = tempString1

	// RSS Feed Provider Site가 비어있는 경우 패닉 발생
	tempString1 = config.RssFeed.Providers[0].Site
	for _, v := range []string{"", "   "} {
		config.RssFeed.Providers[0].Site = v
		assert.Panics(func() { config.validation() })
	}
	config.RssFeed.Providers[0].Site = tempString1

	// 각각의 RSS Feed Provider Site에 대한 테스트 진행
	for _, p := range config.RssFeed.Providers {
		switch RssFeedProviderSite(p.Site) {
		case RssFeedProviderSiteNaverCafe:
			// 네이버 카페의 ClubID가 비어있는 경우 패닉 발생
			tempString1, _ = p.Config.Data["club_id"].(string)
			for _, v := range []string{"", "   "} {
				p.Config.Data["club_id"] = v
				assert.Panics(func() { config.validation() })
			}
			p.Config.Data["club_id"] = tempString1

		case RssFeedProviderSiteYeosuCityHall:
			// pass
		}
	}

	// 지원하지 않는 RSS Feed Provider Site인 경우 패닉 발생
	tempString1 = config.RssFeed.Providers[0].Site
	config.RssFeed.Providers[0].Site = "UnknownSite"
	assert.Panics(func() {
		config.validation()
	})
	config.RssFeed.Providers[0].Site = tempString1

	// 웹서버 설정정보 확인
	tempBool = config.WS.TLSServer
	tempString1 = config.WS.TLSCertFile
	tempString2 = config.WS.TLSKeyFile
	{
		config.WS.TLSServer = true
		{
			// 웹서버의 Cert 파일 경로가 비어있는 경우 패닉 발생
			config.WS.TLSKeyFile = "/etc/letsencrypt/privkey.pem"
			for _, v := range []string{"", "   "} {
				config.WS.TLSCertFile = v
				assert.Panics(func() {
					config.validation()
				})
			}

			// 웹서버의 Key 파일 경로가 비어있는 경우 패닉 발생
			config.WS.TLSCertFile = "/etc/letsencrypt/fullchain.pem"
			for _, v := range []string{"", "   "} {
				config.WS.TLSKeyFile = v
				assert.Panics(func() {
					config.validation()
				})
			}
		}

		config.WS.TLSServer = false
		{
			config.WS.TLSCertFile = ""
			config.WS.TLSKeyFile = ""
			assert.NotPanics(func() {
				config.validation()
			})
		}
	}
	config.WS.TLSServer = tempBool
	config.WS.TLSCertFile = tempString1
	config.WS.TLSKeyFile = tempString2

	// NotifyAPI의 URL이 비어있는 경우 패닉 발생
	tempString1 = config.NotifyAPI.Url
	for _, v := range []string{"", "   "} {
		config.NotifyAPI.Url = v
		assert.Panics(func() {
			config.validation()
		})
	}
	config.NotifyAPI.Url = tempString1

	// NotifyAPI의 APIKey가 비어있는 경우 패닉 발생
	tempString1 = config.NotifyAPI.APIKey
	for _, v := range []string{"", "   "} {
		config.NotifyAPI.APIKey = v
		assert.Panics(func() {
			config.validation()
		})
	}
	config.NotifyAPI.APIKey = tempString1

	// NotifyAPI의 ApplicationID가 비어있는 경우 패닉 발생
	tempString1 = config.NotifyAPI.ApplicationID
	for _, v := range []string{"", "   "} {
		config.NotifyAPI.ApplicationID = v
		assert.Panics(func() {
			config.validation()
		})
	}
	config.NotifyAPI.ApplicationID = tempString1

	// 변경된 경로를 다시 원래대로 복구한다.
	assert.NoError(os.Chdir("g"))
}

func TestAppConfig_ValidationRssFeedProviderConfig(t *testing.T) {
	assert := assert.New(t)

	const provider = "네이버"
	const providerUrl = "http://www.naver.com"

	var providerConfig = ProviderConfig{
		ID:                 "TEST_ID1",
		Name:               "테스트 이름1",
		Description:        "설명",
		Url:                providerUrl,
		Boards:             nil,
		ArticleArchiveDate: 10,
		Data:               nil,
	}
	for i := 1; i <= 3; i++ {
		providerConfig.Boards = append(providerConfig.Boards, &struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Type     string `json:"type"`
			Category string `json:"category"`
		}{
			ID:       fmt.Sprintf("보드%d", i),
			Name:     fmt.Sprintf("이름%d", i),
			Type:     "",
			Category: fmt.Sprintf("카테고리%d", i),
		})
	}

	var config *AppConfig

	// 입력값이 정상적인 경우...
	assert.NotPanics(func() {
		config.validationRssFeedProviderConfig(provider, &providerConfig, &[]string{"TEST_ID0"})
	})

	// 이미 유효성 검사를 진행한 Provider Site의 ID가 전달되는 경우 패닉 발생
	assert.Panics(func() {
		config.validationRssFeedProviderConfig(provider, &providerConfig, &[]string{"TEST_ID1"})
	})

	// 유효성 검사를 처음 진행하는 Provider Site의 ID가 전달되는 경우는 패닉이 발생하지 않음
	var validatedProviderSiteIDs = []string{"TEST_ID0"}
	assert.NotPanics(func() {
		config.validationRssFeedProviderConfig(provider, &providerConfig, &validatedProviderSiteIDs)
	})
	// 유효성 검사를 마친 Provider Site의 ID는 등록되어져 있다.
	assert.Equal(2, len(validatedProviderSiteIDs))
	assert.True(utils.Contains(validatedProviderSiteIDs, providerConfig.ID))
	// 이미 유효성 검사를 마친 Provider Site의 ID가 또 전달되는 경우는 패닉 발생
	assert.Panics(func() {
		config.validationRssFeedProviderConfig(provider, &providerConfig, &validatedProviderSiteIDs)
	})

	// Provider의 Name이 비어있는 경우 패닉 발생
	for _, v := range []string{"", "   "} {
		providerConfig.Name = v
		assert.Panics(func() {
			config.validationRssFeedProviderConfig(provider, &providerConfig, &[]string{"TEST_ID0"})
		})
	}
	providerConfig.Name = "테스트 이름"

	// Provider의 URL이 비어있는 경우 패닉 발생
	for _, v := range []string{"", "   "} {
		providerConfig.Url = v
		assert.Panics(func() {
			config.validationRssFeedProviderConfig(provider, &providerConfig, &[]string{"TEST_ID0"})
		})
	}
	providerConfig.Url = providerUrl

	// ProviderConfig의 Board ID가 중복되는 경우 패닉 발생
	providerConfig.Boards[0].ID = "보드2"
	assert.Panics(func() {
		config.validationRssFeedProviderConfig(provider, &providerConfig, &[]string{"TEST_ID0"})
	})
	providerConfig.Boards[0].ID = "보드1"

	// ProviderConfig의 Board Name이 비어있는 경우 패닉 발생
	for _, v := range []string{"", "   "} {
		providerConfig.Boards[0].Name = v
		assert.Panics(func() {
			config.validationRssFeedProviderConfig(provider, &providerConfig, &[]string{"TEST_ID0"})
		})
	}
	providerConfig.Boards[0].Name = "이름1"
}

func TestProviderConfig_ContainsBoard(t *testing.T) {
	assert := assert.New(t)

	var providerConfig ProviderConfig
	providerConfig.Boards = append(providerConfig.Boards, &struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Type     string `json:"type"`
		Category string `json:"category"`
	}{
		ID:       "1",
		Name:     "이름",
		Type:     "",
		Category: "카테고리",
	})

	assert.True(providerConfig.ContainsBoard("1"))
	assert.False(providerConfig.ContainsBoard(""))
	assert.False(providerConfig.ContainsBoard("2"))
	assert.False(providerConfig.ContainsBoard("11"))
	assert.False(providerConfig.ContainsBoard("1 "))
}

func TestInitAppConfig(t *testing.T) {
	assert := assert.New(t)

	// 테스트 환경에서는 g 패키지 경로에서 설정파일을 읽어들이므로 강제로 부모 폴더로 경로를 변경해준다.
	assert.NoError(os.Chdir(".."))

	var config *AppConfig
	assert.NotPanics(func() { config = InitAppConfig() })
	assert.NotNil(config)

	// 변경된 경로를 다시 원래대로 복구한다.
	assert.NoError(os.Chdir("g"))
}

func TestPanicIfEmpty(t *testing.T) {
	assert := assert.New(t)

	assert.Panics(func() { panicIfEmpty("", "추가 메시지") })
	assert.Panics(func() { panicIfEmpty("   ", "추가 메시지") })
	assert.NotPanics(func() { panicIfEmpty("value", "추가 메시지") })
	assert.NotPanics(func() { panicIfEmpty("   value   ", "추가 메시지") })
}

func TestPanicIfContains(t *testing.T) {
	assert := assert.New(t)

	s := []string{"A1", "B1", "C1"}
	assert.Panics(func() { panicIfContains(s, "A1", "추가 메시지") })
	assert.NotPanics(func() { panicIfContains(s, "", "추가 메시지") })
	assert.NotPanics(func() { panicIfContains(s, "a1", "추가 메시지") })
	assert.NotPanics(func() { panicIfContains(s, "A1 ", "추가 메시지") })
	assert.NotPanics(func() { panicIfContains(s, "A12", "추가 메시지") })

	s = []string{"A1", "B1", "C1", ""}
	assert.Panics(func() { panicIfContains(s, "", "추가 메시지") })
}
