package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoad_Success(t *testing.T) {
	assert := assert.New(t)

	wd, _ := os.Getwd()
	// 테스트 환경에서는 config 패키지 경로에서 설정파일을 읽어들이므로 강제로 부모 폴더로 경로를 변경해준다.
	assert.NoError(os.Chdir("../.."))

	config, warnings, err := Load()
	assert.NoError(err)
	assert.NotNil(config)
	assert.Len(warnings, 1)
	assert.Contains(warnings[0], "시스템 예약 포트(1-1023)를 사용하도록 설정되었습니다")

	// 테스트를 위해 최소 2개 이상의 RSS Feed Provider가 등록되어져 있어야 한다.
	assert.Greater(len(config.RssFeed.Providers), 1)

	// 변경된 경로를 다시 원래대로 복구한다.
	assert.NoError(os.Chdir(wd))
}

func TestLoad_FileNotFound(t *testing.T) {
	assert := assert.New(t)

	config, warnings, err := LoadWithFile("non_existent_file.json")
	assert.Error(err)
	assert.Nil(config)
	assert.Empty(warnings)
	assert.Contains(err.Error(), "설정 파일을 찾을 수 없습니다")
}

func TestAppConfig_Validation(t *testing.T) {
	assert := assert.New(t)

	// 올바른 구조체 생성
	config := newDefaultConfig()
	config.NotifyAPI = NotifyAPIConfig{
		Url:           "http://example.com/api",
		AppKey:        "test_app_key",
		ApplicationID: "test_app_id",
	}
	config.WS = WSConfig{
		ListenPort: 8080,
	}
	config.RssFeed = RssFeedConfig{
		MaxItemCount: 10,
		Providers: []*ProviderConfig{
			{
				ID:   "provider1",
				Site: string(ProviderSiteYeosuCityHall),
				Config: &ProviderDetailConfig{
					ID:   "provider_conf_1",
					Name: "Provider 1",
					URL:  "http://example.com/1",
				},
				Scheduler: SchedulerConfig{TimeSpec: "@every 5m"},
			},
			{
				ID:   "provider2",
				Site: string(ProviderSiteYeosuCityHall),
				Config: &ProviderDetailConfig{
					ID:   "provider_conf_2",
					Name: "Provider 2",
					URL:  "http://example.com/2",
				},
				Scheduler: SchedulerConfig{TimeSpec: "@every 10m"},
			},
		},
	}

	validator := newValidator()

	t.Run("Valid Config", func(t *testing.T) {
		err := config.validate(validator)
		assert.NoError(err)
	})

	t.Run("Duplicate Provider ID", func(t *testing.T) {
		config.RssFeed.Providers[1].ID = config.RssFeed.Providers[0].ID
		err := config.validate(validator)
		assert.Error(err)
		assert.Contains(err.Error(), "RSS 피드 설정 내에 중복된 RSS 피드 공급자(Provider) ID가 존재합니다 (설정 값을 확인해주세요)")
		config.RssFeed.Providers[1].ID = "provider2" // restore
	})

	t.Run("Empty Provider Site", func(t *testing.T) {
		config.RssFeed.Providers[0].Site = ""
		err := config.validate(validator)
		assert.Error(err)
		assert.Contains(err.Error(), "site (조건: required)")
		config.RssFeed.Providers[0].Site = string(ProviderSiteYeosuCityHall) // restore
	})

	t.Run("Naver Cafe Missing ClubID", func(t *testing.T) {
		config.RssFeed.Providers[0].Site = string(ProviderSiteNaverCafe)
		// Data is nil, so ClubID is missing
		err := config.validate(validator)
		assert.Error(err)
		assert.Contains(err.Error(), "club_id가 입력되지 않았거나 문자열 타입이 아닙니다")

		// Set ClubID
		config.RssFeed.Providers[0].Config.Data = map[string]interface{}{"club_id": "123456"}
		err = config.validate(validator)
		assert.NoError(err)

		config.RssFeed.Providers[0].Site = string(ProviderSiteYeosuCityHall) // restore
		config.RssFeed.Providers[0].Config.Data = nil                        // restore
	})

	t.Run("Unknown Provider Site", func(t *testing.T) {
		config.RssFeed.Providers[0].Site = "UnknownSite"
		err := config.validate(validator)
		assert.Error(err)
		assert.Contains(err.Error(), "지원하지 않는 사이트('UnknownSite')가 설정되었습니다")
		config.RssFeed.Providers[0].Site = string(ProviderSiteYeosuCityHall) // restore
	})

	t.Run("Duplicate NaverCafe ClubID", func(t *testing.T) {
		config.RssFeed.Providers[0].Site = string(ProviderSiteNaverCafe)
		config.RssFeed.Providers[0].Config.Data = map[string]interface{}{"club_id": "same_id"}
		config.RssFeed.Providers[1].Site = string(ProviderSiteNaverCafe)
		config.RssFeed.Providers[1].Config.Data = map[string]interface{}{"club_id": "same_id"}

		err := config.validate(validator)
		assert.Error(err)
		assert.Contains(err.Error(), "club_id('same_id')가 중복되었습니다")

		// restore
		config.RssFeed.Providers[0].Site = string(ProviderSiteYeosuCityHall)
		config.RssFeed.Providers[0].Config.Data = nil
		config.RssFeed.Providers[1].Site = string(ProviderSiteYeosuCityHall)
		config.RssFeed.Providers[1].Config.Data = nil
	})

	t.Run("WS TLS Validation", func(t *testing.T) {
		config.WS.TLSServer = true

		// Missing both
		err := config.validate(validator)
		assert.Error(err)
		assert.Contains(err.Error(), "TLS 서버 활성화 시 TLS 인증서 파일 경로(tls_cert_file)는 필수입니다")

		// Mock files to bypass file existence check or test the "not found" logic
		config.WS.TLSCertFile = "dummy.crt"
		config.WS.TLSKeyFile = "dummy.key"
		err = config.validate(validator)
		assert.Error(err)
		assert.Contains(err.Error(), "지정된 TLS 인증서 파일(tls_cert_file)을 찾을 수 없습니다: 'dummy.crt'")

		config.WS.TLSServer = false // restore
		config.WS.TLSCertFile = ""
		config.WS.TLSKeyFile = ""
	})

	t.Run("WS ListenPort Invalid", func(t *testing.T) {
		config.WS.ListenPort = 0
		err := config.validate(validator)
		assert.Error(err)
		assert.Contains(err.Error(), "웹 서비스 포트(listen_port)는 1에서 65535 사이의 값이어야 합니다")

		config.WS.ListenPort = 8080 // restore
	})
}

func TestProviderConfig_ContainsBoard(t *testing.T) {
	assert := assert.New(t)

	var providerConfig ProviderDetailConfig
	providerConfig.Boards = append(providerConfig.Boards, &BoardConfig{
		ID:       "1",
		Name:     "이름",
		Type:     "",
		Category: "카테고리",
	})

	assert.True(providerConfig.HasBoard("1"))
	assert.False(providerConfig.HasBoard(""))
	assert.False(providerConfig.HasBoard("2"))
	assert.False(providerConfig.HasBoard("11"))
	assert.False(providerConfig.HasBoard("1 "))
}

func TestAppConfig_Lint(t *testing.T) {
	assert := assert.New(t)

	config := newDefaultConfig()
	config.WS.ListenPort = 80 // < 1024

	warnings := config.lint()
	assert.Len(warnings, 1)
	assert.Contains(warnings[0], "시스템 예약 포트(1-1023)를 사용하도록 설정되었습니다")
}
