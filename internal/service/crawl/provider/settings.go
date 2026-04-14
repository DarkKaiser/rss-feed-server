package provider

import (
	"github.com/darkkaiser/notify-server/pkg/maputil"
)

// Defaulter 설정 정보 구조체가 누락된 선택적 필드에 안전한 기본값을 스스로 주입할 수 있도록 하는 인터페이스입니다.
//
// ParseSettings 파이프라인에서 Validator보다 먼저 자동으로 호출되며, 이를 통해 Validate() 내부에서는
// 기본값이 이미 채워진 안전한 상태를 전제하고 필수 항목 위주의 검증 로직만 간결하게 작성할 수 있습니다.
//
// 적용 대상:
//   - 값이 0 이하(int)이거나 비어있는(string) 선택적 필드
//   - 생략 시 서비스 운영에 지장이 없는 "권장 기본값"이 존재하는 필드
//
// 적용 제외:
//   - 반드시 명시적으로 입력해야 하는 필수(Required) 필드 (해당 검증은 Validate에서 수행)
type Defaulter interface {
	ApplyDefaults()
}

// Validator 설정 정보 구조체가 스스로 유효성을 검증할 수 있도록 하는 인터페이스입니다.
//
// ParseSettings 파이프라인에서 ApplyDefaults() 호출 이후에 자동으로 실행됩니다.
// 이 메서드는 치명적인 설정 오류(필수 필드 누락, 값 범위 위반 등)를 발견하면 에러를 반환하여
// 크롤러 초기화 전체를 즉시 중단시킵니다.
// 이를 통해 잘못된 설정 정보로 인한 런타임 오류 및 데이터 수집 불능 상태를 사전에 방지합니다.
type Validator interface {
	Validate() error
}

// ParseSettings 설정 파일의 "data" 항목에서 넘어온 비구조화된 맵(map[string]any) 데이터를
// 제네릭 타입 T로 지정된 설정 구조체로 변환하는 3단계 파이프라인 함수입니다.
//
// 실행 순서:
//  1. [디코딩] maputil 라이브러리를 사용하여 비구조화된 맵 데이터를 구조체 객체로 안전하게 매핑(디코딩)합니다.
//     타입 자동 보정(Weakly Typed)을 지원하여 설정 값의 유연한 바인딩을 보장합니다.
//  2. [기본값] 타입 T가 Defaulter 인터페이스를 구현한 경우, ApplyDefaults()를 호출하여
//     누락된 선택적 필드에 안전한 기본값을 자동으로 채워 넣습니다.
//  3. [검증] 타입 T가 Validator 인터페이스를 구현한 경우, Validate()를 호출하여 필수 항목 누락이나
//     잘못된 값 범위 등 치명적인 설정 오류를 검사합니다.
//
// 반환값:
//   - *T: 기본값이 채워지고 유효성이 검증된 설정 구조체의 포인터
//   - error: 데이터 매핑(디코딩) 실패 또는 Validate() 과정에서 유효성 오류가 감지된 상태
func ParseSettings[T any](rawSettings map[string]any) (*T, error) {
	// [1단계: 디코딩] map 데이터를 타입 T의 구조체로 디코딩
	settings, err := maputil.Decode[T](rawSettings)
	if err != nil {
		return nil, err
	}

	// [2단계: 기본값 주입]
	// T가 Defaulter 인터페이스를 구현한 경우, ApplyDefaults()를 먼저 호출합니다.
	// 반드시 Validate()보다 먼저 실행되어야 합니다. 검증 단계에서는 기본값이 이미 적용된
	// 상태임을 전제하므로, 순서가 바뀌면 선택적 필드를 필수 필드로 오판할 수 있습니다.
	if d, ok := any(settings).(Defaulter); ok {
		d.ApplyDefaults()
	}

	// [3단계: 유효성 검증]
	// T가 Validator 인터페이스를 구현한 경우, Validate()를 호출합니다.
	// 검증 실패 시 에러를 반환하여 크롤러 초기화 전체를 즉시 중단시킵니다.
	if v, ok := any(settings).(Validator); ok {
		if err := v.Validate(); err != nil {
			return nil, err
		}
	}

	return settings, nil
}
