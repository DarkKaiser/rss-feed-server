package config

import (
	"fmt"
	"strings"

	"github.com/darkkaiser/notify-server/pkg/cronx"
	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/go-playground/validator/v10"
)

// ProviderSite RSS 피드를 수집할 대상 사이트를 나타내는 타입입니다.
type ProviderSite string

// 현재 서비스가 지원하는 크롤링 대상 사이트 목록입니다.
const (
	ProviderSiteNaverCafe                 ProviderSite = "NaverCafe"                 // 네이버 카페
	ProviderSiteYeosuCityHall             ProviderSite = "YeosuCityHall"             // 여수시청 홈페이지
	ProviderSiteSsangbongElementarySchool ProviderSite = "SsangbongElementarySchool" // 쌍봉초등학교 홈페이지
)

// AppConfig 애플리케이션의 모든 설정을 포함하는 최상위 구조체
type AppConfig struct {
	Debug     bool            `json:"debug"`
	RSSFeed   RSSFeedConfig   `json:"rss_feed"`
	WS        WSConfig        `json:"ws"`
	NotifyAPI NotifyAPIConfig `json:"notify_api"`
}

// validate 설정 파일 로드 직후, 각 설정 항목의 정합성과 필수 값의 유효성을 검증합니다.
func (c *AppConfig) validate(v *validator.Validate) error {
	if err := c.RSSFeed.validate(v); err != nil {
		return err
	}

	if err := c.WS.validate(v); err != nil {
		return err
	}

	if err := c.NotifyAPI.validate(v); err != nil {
		return err
	}

	return nil
}

// lint 서비스 운영의 안정성과 보안을 위해 권장되는 설정 준수 여부를 진단합니다.
// 강제적인 에러를 발생시키지는 않으나, 잠재적 위험 요소(예: Well-known Port 사용)에 대한 경고 메시지를 반환합니다.
func (c *AppConfig) lint() []string {
	var warnings []string
	warnings = append(warnings, c.WS.lint()...)
	return warnings
}

// RSSFeedConfig RSS 피드 관련 설정을 정의하는 구조체
type RSSFeedConfig struct {
	MaxItemCount uint              `json:"max_item_count" validate:"gt=0"`
	Providers    []*ProviderConfig `json:"providers" validate:"unique=ID"`
}

func (c *RSSFeedConfig) validate(v *validator.Validate) error {
	if err := checkStruct(v, c, "RSS 피드 설정"); err != nil {
		return err
	}

	// 네이버 카페 club_id 중복 여부를 추적하기 위한 맵
	seenClubIDs := make(map[string]string)

	for _, p := range c.Providers {
		if err := p.validate(v, seenClubIDs); err != nil {
			return err
		}
	}

	return nil
}

// ProviderConfig 개별 RSS 피드 공급자(사이트)에 대한 설정을 정의하는 구조체
type ProviderConfig struct {
	ID        string                `json:"id" validate:"required"`
	Site      string                `json:"site" validate:"required"`
	Config    *ProviderDetailConfig `json:"config" validate:"required"`
	Scheduler SchedulerConfig       `json:"scheduler"`
}

func (c *ProviderConfig) validate(v *validator.Validate, seenClubIDs map[string]string) error {
	if err := checkStruct(v, c, fmt.Sprintf("RSS 피드 공급자(ID: %s, Site: %s)", c.ID, c.Site)); err != nil {
		return err
	}

	switch ProviderSite(c.Site) {
	case ProviderSiteNaverCafe:
		if err := c.Config.validate(v, "네이버 카페"); err != nil {
			return err
		}

		clubID, ok := c.Config.Data["club_id"].(string)
		if !ok || strings.TrimSpace(clubID) == "" {
			return apperrors.Newf(apperrors.InvalidInput, "RSS 피드 공급자(ID: %s, Site: %s)의 club_id가 입력되지 않았거나 문자열 타입이 아닙니다", c.ID, c.Site)
		}

		// 동일한 club_id가 여러 공급자에 중복 설정되면 크롤링 대상이 불분명해지므로 에러 처리한다.
		if existingProviderID, exists := seenClubIDs[clubID]; exists {
			return apperrors.Newf(apperrors.InvalidInput, "네이버 카페 club_id('%s')가 중복되었습니다 (RSS 피드 공급자 ID: '%s', '%s')", clubID, existingProviderID, c.ID)
		}
		seenClubIDs[clubID] = c.ID

	case ProviderSiteYeosuCityHall:
		if err := c.Config.validate(v, "여수시청 홈페이지"); err != nil {
			return err
		}

	case ProviderSiteSsangbongElementarySchool:
		if err := c.Config.validate(v, "쌍봉초등학교 홈페이지"); err != nil {
			return err
		}

	default:
		return apperrors.Newf(apperrors.InvalidInput, "RSS 피드 공급자(ID: %s)에 지원하지 않는 사이트('%s')가 설정되었습니다", c.ID, c.Site)
	}

	// Scheduler 구조체 검증
	if err := checkStruct(v, c.Scheduler, fmt.Sprintf("RSS 피드 공급자(ID: %s, Site: %s) 스케줄러", c.ID, c.Site)); err != nil {
		return err
	}
	if err := cronx.Validate(c.Scheduler.TimeSpec); err != nil {
		return apperrors.Wrap(err, apperrors.InvalidInput, fmt.Sprintf("RSS 피드 공급자(ID: %s, Site: %s)의 스케줄러 time_spec 설정이 유효하지 않습니다", c.ID, c.Site))
	}

	return nil
}

// ProviderDetailConfig RSS 피드 공급자의 상세 정보를 담는 설정 구조체
type ProviderDetailConfig struct {
	ID          string         `json:"id" validate:"required"`
	Name        string         `json:"name" validate:"required"`
	Description string         `json:"description"`
	URL         string         `json:"url" validate:"required"`
	Boards      []*BoardConfig `json:"boards" validate:"unique=ID"`
	ArchiveDays uint           `json:"archive_days"`
	Data        map[string]any `json:"data"`
}

func (c *ProviderDetailConfig) validate(v *validator.Validate, providerName string) error {
	if err := checkStruct(v, c, fmt.Sprintf("%s(ID: %s)의 상세 설정", providerName, c.ID)); err != nil {
		return err
	}

	c.URL = strings.TrimSuffix(c.URL, "/")

	for _, board := range c.Boards {
		if err := board.validate(v, c.ID, providerName); err != nil {
			return err
		}
	}

	return nil
}

func (c *ProviderDetailConfig) HasBoard(boardID string) bool {
	for _, board := range c.Boards {
		if board.ID == boardID {
			return true
		}
	}
	return false
}

// BoardConfig RSS 피드 공급자 내 개별 게시판을 정의하는 구조체
type BoardConfig struct {
	ID       string `json:"id" validate:"required"`
	Name     string `json:"name" validate:"required"`
	Type     string `json:"type"`
	Category string `json:"category"`
}

func (c *BoardConfig) validate(v *validator.Validate, providerID, providerName string) error {
	if err := checkStruct(v, c, fmt.Sprintf("%s(ID: %s)의 게시판(ID: %s)", providerName, providerID, c.ID)); err != nil {
		return err
	}
	return nil
}

// SchedulerConfig 스케줄링 설정을 정의하는 구조체
type SchedulerConfig struct {
	TimeSpec string `json:"time_spec" validate:"required"`
}

// WSConfig 웹 서비스의 포트 및 TLS(HTTPS) 보안 설정을 정의하는 구조체
type WSConfig struct {
	TLSServer   bool   `json:"tls_server"`
	TLSCertFile string `json:"tls_cert_file" validate:"required_if=TLSServer true,omitempty,file"`
	TLSKeyFile  string `json:"tls_key_file" validate:"required_if=TLSServer true,omitempty,file"`
	ListenPort  int    `json:"listen_port" validate:"min=1,max=65535"`
}

func (c *WSConfig) validate(v *validator.Validate) error {
	// 웹 서비스(포트, TLS) 설정 유효성 검사
	if err := checkStruct(v, c, "웹 서비스 설정"); err != nil {
		return err
	}
	return nil
}

func (c *WSConfig) lint() []string {
	var warnings []string

	// 시스템 예약 포트(1024 미만) 사용 경고
	if c.ListenPort < 1024 {
		warnings = append(warnings, fmt.Sprintf("시스템 예약 포트(1-1023)를 사용하도록 설정되었습니다(포트: %d). 이 경우 서버 구동 시 관리자 권한이 필요할 수 있습니다", c.ListenPort))
	}

	return warnings
}

// NotifyAPIConfig 알림 발송을 위한 REST API 클라이언트 설정 구조체
type NotifyAPIConfig struct {
	URL           string `json:"url"`
	AppKey        string `json:"app_key"`
	ApplicationID string `json:"application_id"`
}

func (c *NotifyAPIConfig) validate(v *validator.Validate) error {
	if err := checkStruct(v, c, "Notify API 클라이언트 설정"); err != nil {
		return err
	}
	return nil
}
