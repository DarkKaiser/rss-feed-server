package config

import (
	"fmt"
	"os"
	"strings"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

const (
	// AppName 애플리케이션의 전역 고유 식별자입니다.
	AppName string = "rss-feed-server"

	// DefaultFilename 애플리케이션 초기화 시 참조하는 기본 설정 파일명입니다.
	// 실행 인자를 통해 명시적인 경로가 제공되지 않을 경우, 시스템은 이 파일을 탐색하여 구성을 로드합니다.
	DefaultFilename = AppName + ".json"

	// ------------------------------------------------------------------------------------------------
	// RSS 피드 설정
	// ------------------------------------------------------------------------------------------------

	// DefaultMaxItemCount RSS 피드 수집 시 최대로 유지할 아이템(게시글) 개수의 기본값입니다.
	DefaultMaxItemCount = 10

	// ------------------------------------------------------------------------------------------------
	// 웹 서비스 설정
	// ------------------------------------------------------------------------------------------------

	// DefaultListenPort 웹 서비스가 수신 대기할 기본 포트입니다.
	DefaultListenPort = 8080
)

// newDefaultConfig 애플리케이션의 모든 설정에 대한 '기본값'을 정의하고 초기화합니다.
// 사용자 설정이 누락되더라도 안전하게 실행될 수 있도록 미리 값을 채워주는 역할을 합니다.
func newDefaultConfig() AppConfig {
	return AppConfig{
		Debug: false,
		RssFeed: RssFeedConfig{
			MaxItemCount: DefaultMaxItemCount,
		},
		WS: WSConfig{
			ListenPort: DefaultListenPort,
		},
	}
}

// Load 기본 설정 파일을 읽어 애플리케이션 설정을 로드합니다.
func Load() (*AppConfig, []string, error) {
	return LoadWithFile(DefaultFilename)
}

// LoadWithFile 지정된 경로의 설정 파일을 읽어 AppConfig 객체를 생성합니다.
func LoadWithFile(filename string) (*AppConfig, []string, error) {
	k := koanf.New(".")

	// 1. 기본값 로드
	err := k.Load(structs.Provider(newDefaultConfig(), "json"), nil)
	if err != nil {
		return nil, nil, apperrors.Wrap(err, apperrors.System, "기본값 로드 중 오류가 발생하였습니다")
	}

	// 2. 설정 파일 로드 (기본값 덮어쓰기)
	if err := k.Load(file.Provider(filename), json.Parser()); err != nil {
		if os.IsNotExist(err) {
			return nil, nil, apperrors.Wrap(err, apperrors.System, fmt.Sprintf("설정 파일을 찾을 수 없습니다: '%s'", filename))
		}
		return nil, nil, apperrors.Wrap(err, apperrors.InvalidInput, fmt.Sprintf("설정 파일 로드 중 오류가 발생하였습니다: '%s'", filename))
	}

	// 3. 환경 변수 로드 (JSON 설정 덮어쓰기)
	//  - 접두사: RSSFEED_
	//  - 구분자: 이중 언더스코어(__)를 점(.)으로 변환 (계층 구조 표현)
	//  - 예: RSSFEED_HTTP_RETRY__MAX_RETRIES -> http_retry.max_retries
	if err := k.Load(env.Provider("RSSFEED_", ".", normalizeEnvKey), nil); err != nil {
		return nil, nil, apperrors.Wrap(err, apperrors.System, "환경 변수 로드 중 오류가 발생하였습니다")
	}

	// 4. 구조체 언마샬링
	unmarshalConf := koanf.UnmarshalConf{
		Tag: "json",
		DecoderConfig: &mapstructure.DecoderConfig{
			ErrorUnused:      false, // 파일에 존재하지만 구조체에 없는 필드가 있어도 무시함 (운영 환경 안정성)
			WeaklyTypedInput: true,
			DecodeHook: mapstructure.ComposeDecodeHookFunc(
				mapstructure.StringToTimeDurationHookFunc(),
			),
		},
	}

	var appConfig AppConfig
	if err := k.UnmarshalWithConf("", &appConfig, unmarshalConf); err != nil {
		return nil, nil, apperrors.Wrap(err, apperrors.System, "설정값을 구조체에 매핑하지 못하였습니다. 데이터 타입(숫자/문자 등)을 확인해주세요")
	}

	// 5. 유효성 검사 수행
	if err := appConfig.validate(newValidator()); err != nil {
		return nil, nil, apperrors.Wrap(err, apperrors.InvalidInput, fmt.Sprintf("설정 파일('%s')의 유효성 검증에 실패하였습니다", filename))
	}

	// 6. 권장 설정 검사 (유효성 검사 통과 후 수행)
	warnings := appConfig.lint()

	return &appConfig, warnings, nil
}

// normalizeEnvKey 환경 변수 키를 내부 설정 구조체에 매핑하기 위해 표준화된 키 형식으로 변환합니다.
func normalizeEnvKey(key string) string {
	key = strings.ToLower(strings.TrimPrefix(key, "RSSFEED_"))
	return strings.ReplaceAll(key, "__", ".")
}
