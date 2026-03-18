package config

import (
	"fmt"
	"reflect"
	"strings"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/go-playground/validator/v10"
)

// newValidator 새로운 Validator 인스턴스를 생성하고 커스텀 유효성 검사 함수를 등록합니다.
func newValidator() *validator.Validate {
	v := validator.New()

	// 검증 에러가 났을 때, 에러 메시지에 Go 구조체 필드명 대신 JSON 이름을 보여주도록 설정합니다.
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})

	return v
}

// checkStruct 구조체 인스턴스의 유효성을 태그 규칙에 따라 검증하고, 발생한 오류를 사용자 친화적인 도메인 에러로 변환합니다.
//
// 선택적 인자인 fields를 제공하면 해당 필드 범위 내에서만 부분 검증(Partial Validation)을 수행합니다.
// 이는 복합적인 중첩 구조체 검증 시, 특정 필드 집합에 대한 검증 로직을 격리하여 제어할 때 유용합니다.
func checkStruct(v *validator.Validate, s interface{}, contextName string, fields ...string) error {
	var err error
	if len(fields) > 0 {
		err = v.StructPartial(s, fields...)
	} else {
		err = v.Struct(s)
	}

	if err != nil {
		if validationErrors, ok := err.(validator.ValidationErrors); ok {
			// 첫 번째 에러만 상세히 보고
			firstErr := validationErrors[0]

			// 필드별(Field) 커스텀 에러 처리
			switch firstErr.StructField() {
			case "MaxItemCount":
				return apperrors.New(apperrors.InvalidInput, fmt.Sprintf("RSS 피드 최대 수집 개수(max_item_count)는 0보다 커야 합니다: '%v'", firstErr.Value()))
			case "ListenPort":
				return apperrors.New(apperrors.InvalidInput, "웹 서비스 포트(listen_port)는 1에서 65535 사이의 값이어야 합니다")
			case "TLSCertFile":
				switch firstErr.Tag() {
				case "required_if":
					return apperrors.New(apperrors.InvalidInput, "TLS 서버 활성화 시 TLS 인증서 파일 경로(tls_cert_file)는 필수입니다")
				case "file":
					return apperrors.New(apperrors.InvalidInput, fmt.Sprintf("지정된 TLS 인증서 파일(tls_cert_file)을 찾을 수 없습니다: '%v'", firstErr.Value()))
				default:
					return apperrors.New(apperrors.InvalidInput, "TLS 인증서 파일 경로(tls_cert_file) 설정이 올바르지 않습니다")
				}
			case "TLSKeyFile":
				switch firstErr.Tag() {
				case "required_if":
					return apperrors.New(apperrors.InvalidInput, "TLS 서버 활성화 시 TLS 키 파일 경로(tls_key_file)는 필수입니다")
				case "file":
					return apperrors.New(apperrors.InvalidInput, fmt.Sprintf("지정된 TLS 키 파일(tls_key_file)을 찾을 수 없습니다: '%v'", firstErr.Value()))
				default:
					return apperrors.New(apperrors.InvalidInput, "TLS 키 파일 경로(tls_key_file) 설정이 올바르지 않습니다")
				}
			}

			// 태그별(Tag) 커스텀 에러 처리 (범용)
			switch firstErr.Tag() {
			case "unique":
				target := firstErr.Field()
				switch target {
				case "providers":
					target = "RSS 피드 공급자(Provider)"
				case "boards":
					target = "게시판(Board)"
				}

				// unique 태그 에러는 "중복된 {Target} ID가 존재합니다" 형태로 통일 (전체 슬라이스 덤프 방지)
				return apperrors.New(apperrors.InvalidInput, fmt.Sprintf("%s 내에 중복된 %s ID가 존재합니다 (설정 값을 확인해주세요)", contextName, target))
			}

			return apperrors.New(apperrors.InvalidInput, fmt.Sprintf("%s의 설정이 올바르지 않습니다: %s (조건: %s)", contextName, firstErr.Field(), firstErr.Tag()))
		}
		return apperrors.Wrap(err, apperrors.InvalidInput, fmt.Sprintf("%s 유효성 검증에 실패했습니다", contextName))
	}
	return nil
}
