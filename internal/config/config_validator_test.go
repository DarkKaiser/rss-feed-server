package config

import (
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
)

func TestNewValidator_TagName(t *testing.T) {
	assert := assert.New(t)
	v := newValidator()

	// 테스트용 구조체: json:"-" 태그와 정상적인 json 태그 포함
	type TestStruct struct {
		HiddenField  string `json:"-" validate:"required"`
		VisibleField string `json:"visible_field" validate:"required"`
		NormalField  string `validate:"required"`
	}

	testData := TestStruct{}
	err := v.Struct(testData)
	assert.Error(err)

	validationErrs, ok := err.(validator.ValidationErrors)
	assert.True(ok)

	var hiddenFieldName, visibleFieldName, normalFieldName string
	for _, e := range validationErrs {
		switch e.StructField() {
		case "HiddenField":
			hiddenFieldName = e.Field()
		case "VisibleField":
			visibleFieldName = e.Field()
		case "NormalField":
			normalFieldName = e.Field()
		}
	}

	// name == "-" 일 때 ""를 반환하므로 필드 이름이 공백처리 됨 (Validator 라이브러리의 Field() 값)
	assert.Equal("HiddenField", hiddenFieldName) // json 태그가 "-" 이면 validator는 원래 필드명(StructField)을 Field()에도 유지하거나 빈값으로 둠
	assert.Equal("visible_field", visibleFieldName) // json 태그의 첫 번째 값이 반환되어야 함
	assert.Equal("NormalField", normalFieldName)    // json 태그가 없으면 원래 필드명
}

// CustomValidator로 에러 강제 발생을 위한 인터페이스 구현
func TestCheckStruct_ValidationErrors_CastFail(t *testing.T) {
	assert := assert.New(t)

	// 일반적인 validator.ValidationErrors가 아닌 에러(nil 검증 자체도 validator 라이브러리가 InvalidValidationError로 반환함) 검증
	v := newValidator()
	err := checkStruct(v, nil, "Nil 검증 테스트")
	
	assert.Error(err)
	// validator.InvalidValidationError가 발생하여 캐스팅 실패(기본 오류 반환) 분기로 진입함
	assert.Contains(err.Error(), "Nil 검증 테스트 유효성 검증에 실패했습니다")
}

func TestCheckStruct_TLSCertFile_DefaultError(t *testing.T) {
	assert := assert.New(t)
	v := newValidator()

	// TLSCertFile 이 required_if나 file 태그가 아닌 다른 태그(예: email) 위반으로 에러를 뱉게 만듭니다.
	type TestConfig struct {
		TLSCertFile string `json:"tls_cert_file" validate:"email"`
	}
	
	testData := TestConfig{TLSCertFile: "not-an-email"}
	err := checkStruct(v, &testData, "TLS 테스트")
	
	assert.Error(err)
	assert.Contains(err.Error(), "TLS 인증서 파일 경로(tls_cert_file) 설정이 올바르지 않습니다")
}

func TestCheckStruct_TLSKeyFile_DefaultError(t *testing.T) {
	assert := assert.New(t)
	v := newValidator()

	// TLSKeyFile 이 required_if나 file 태그가 아닌 다른 태그(예: url) 위반으로 에러를 뱉게 만듭니다.
	type TestConfig struct {
		TLSKeyFile string `json:"tls_key_file" validate:"url"`
	}
	
	testData := TestConfig{TLSKeyFile: "not-a-url"}
	err := checkStruct(v, &testData, "TLS 키 테스트")
	
	assert.Error(err)
	assert.Contains(err.Error(), "TLS 키 파일 경로(tls_key_file) 설정이 올바르지 않습니다")
}

func TestCheckStruct_UniqueTag_DefaultTarget(t *testing.T) {
	assert := assert.New(t)
	v := newValidator()

	// target == "providers" 나 "boards" 가 아닌 일반 슬라이스에서 unique 태그 위반
	type TestConfig struct {
		Items []string `json:"items" validate:"unique"`
	}

	testData := TestConfig{Items: []string{"A", "A"}}
	err := checkStruct(v, &testData, "일반 항목 테스트")

	assert.Error(err)
	assert.Contains(err.Error(), "일반 항목 테스트 내에 중복된 items ID가 존재합니다 (설정 값을 확인해주세요)")
}

func TestCheckStruct_UniqueTag_BoardsTarget(t *testing.T) {
	assert := assert.New(t)
	v := newValidator()

	// target == "boards" 분기 테스트
	type Board struct {
		ID string `validate:"required"`
	}
	type TestConfig struct {
		Boards []Board `json:"boards" validate:"unique=ID"`
	}

	testData := TestConfig{Boards: []Board{{ID: "1"}, {ID: "1"}}}
	err := checkStruct(v, &testData, "게시판 항목 테스트")

	assert.Error(err)
	assert.Contains(err.Error(), "게시판 항목 테스트 내에 중복된 게시판(Board) ID가 존재합니다 (설정 값을 확인해주세요)")
}

func TestCheckStruct_UniqueTag_ProvidersTarget(t *testing.T) {
	assert := assert.New(t)
	v := newValidator()

	// target == "providers" 분기 테스트
	type Provider struct {
		ID string `validate:"required"`
	}
	type TestConfig struct {
		Providers []Provider `json:"providers" validate:"unique=ID"`
	}

	testData := TestConfig{Providers: []Provider{{ID: "prov1"}, {ID: "prov1"}}}
	err := checkStruct(v, &testData, "제공자 항목 테스트")

	assert.Error(err)
	assert.Contains(err.Error(), "제공자 항목 테스트 내에 중복된 RSS 피드 공급자(Provider) ID가 존재합니다 (설정 값을 확인해주세요)")
}

func TestCheckStruct_GenericTagError(t *testing.T) {
	assert := assert.New(t)
	v := newValidator()

	// switch 구문에 들어가지 않는 일반적인 태그 (예: required)
	type TestConfig struct {
		SimpleField string `json:"simple_field" validate:"required"`
	}

	testData := TestConfig{}
	err := checkStruct(v, &testData, "일반 구조체 테스트")

	assert.Error(err)
	assert.Contains(err.Error(), "일반 구조체 테스트의 설정이 올바르지 않습니다: simple_field (조건: required)")
}

func TestCheckStruct_NilError(t *testing.T) {
	assert := assert.New(t)
	v := newValidator()

	type TestConfig struct {
		ValidField string `validate:"required"`
	}
	
	// 유효성 에러가 없는 정상 통과 시나리오
	err := checkStruct(v, &TestConfig{ValidField: "Valid"}, "정상 테스트")
	assert.NoError(err)
}

func TestCheckStruct_TLSCertFile_FileError(t *testing.T) {
	assert := assert.New(t)
	v := newValidator()

	// 57-58라인: TLSCertFile 필드에서, 태그가 "file" 인 경우 파일 로드 에러
	// 해당 에러 문구를 유도하려면 필드명이 "TLSCertFile"이어야 하고 에러 태그가 "file" 이어야 합니다.
	type TestConfig struct {
		TLSCertFile string `validate:"file"`
	}

	testData := TestConfig{TLSCertFile: "not-found-cert.crt"}
	err := checkStruct(v, &testData, "TLS 파일 조회 테스트")

	assert.Error(err)
	assert.Contains(err.Error(), "지정된 TLS 인증서 파일(tls_cert_file)을 찾을 수 없습니다: 'not-found-cert.crt'")
}

func TestCheckStruct_TLSKeyFile_FileError(t *testing.T) {
	assert := assert.New(t)
	v := newValidator()

	// 66-67라인: TLSKeyFile 필드에서, 태그가 "file" 인 경우 파일 로드 에러
	type TestConfig struct {
		TLSKeyFile string `validate:"file"`
	}

	testData := TestConfig{TLSKeyFile: "not-found-key.key"}
	err := checkStruct(v, &testData, "TLS 키 파일 조회 테스트")

	assert.Error(err)
	assert.Contains(err.Error(), "지정된 TLS 키 파일(tls_key_file)을 찾을 수 없습니다: 'not-found-key.key'")
}

// Struct 필드가 아닌 단순 변수나 포인터 검증 시 Struct()에서 ValidationErrors가 아닌 일반 예외가 발생할 경우 Wrap(88라인)
func TestCheckStruct_WrapError(t *testing.T) {
	assert := assert.New(t)
	v := newValidator()

	// struct가 아닌 원시 타입을 검증할 경우 InvalidValidationError가 아닌 일반 에러(구조체가 아니라는 에러)가 발생합니다.
	notAStruct := 123
	err := v.Struct(notAStruct)
	assert.Error(err)
	// 이 에러가 ValidationErrors인지 확인
	_, ok := err.(validator.ValidationErrors)
	assert.False(ok) // ok가 false여야 else 분기(Wrap)로 빠짐

	errToCheck := checkStruct(v, notAStruct, "일반 검증 텍스트")
	assert.Error(errToCheck)
	assert.Contains(errToCheck.Error(), "일반 검증 텍스트 유효성 검증에 실패했습니다")
}
