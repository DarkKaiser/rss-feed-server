package provider

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- 테스트용 더미 구조체 정의 ---

// 1. 기본 설정 (인터페이스 미구현)
type basicSettings struct {
	Name  string `mapstructure:"name"`
	Value int    `mapstructure:"value"`
}

// 2. Defaulter 구현 설정
type defaulterSettings struct {
	Name           string `mapstructure:"name"`
	Value          int    `mapstructure:"value"`
	DefaultApplied bool
}

func (s *defaulterSettings) ApplyDefaults() {
	if s.Value == 0 {
		s.Value = 42
	}
	s.DefaultApplied = true
}

// 3. Validator 구현 설정
type validatorSettings struct {
	Name string `mapstructure:"name"`
}

func (s *validatorSettings) Validate() error {
	if s.Name == "" {
		return errors.New("이름은 필수 항목입니다")
	}
	return nil
}

// 4. Defaulter 및 Validator 모두 구현 설정 (파이프라인 순서 검증용)
type fullPipelineSettings struct {
	Name  string `mapstructure:"name"`
	Value int    `mapstructure:"value"`
}

func (s *fullPipelineSettings) ApplyDefaults() {
	if s.Name == "" {
		s.Name = "default_name"
	}
}

func (s *fullPipelineSettings) Validate() error {
	// ApplyDefaults가 먼저 호출되었다면 Name이 비어있지 않아야 합니다.
	// 순서가 어긋났다면 이 검증을 통과하지 못합니다.
	if s.Name == "" {
		return errors.New("이름이 설정되지 않았습니다 (기본값 주입 실패)")
	}
	if s.Value < 0 {
		return errors.New("값이 0 이상이어야 합니다")
	}
	return nil
}

// --- 테스트 슈트 ---

func TestParseSettings_Basic(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"name":  "test_app",
		"value": "100", // maputil의 Weakly Typed 기능에 의해 string "100"이 int 100으로 자동 캐스팅됨을 확인
	}

	settings, err := ParseSettings[basicSettings](raw)

	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, "test_app", settings.Name)
	assert.Equal(t, 100, settings.Value)
}

func TestParseSettings_ApplyDefaults(t *testing.T) {
	t.Parallel()

	// 값이 주어지지 않아 0 초기값을 갖는 경우, ApplyDefaults가 발동하는지 확인
	raw := map[string]any{
		"name": "defaulter_app",
	}

	settings, err := ParseSettings[defaulterSettings](raw)

	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, "defaulter_app", settings.Name)
	assert.Equal(t, 42, settings.Value) // 기본값 42가 주입되었는지 확인
	assert.True(t, settings.DefaultApplied)
}

func TestParseSettings_Validate_Success(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"name": "valid_app",
	}

	settings, err := ParseSettings[validatorSettings](raw)

	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, "valid_app", settings.Name)
}

func TestParseSettings_Validate_Failure(t *testing.T) {
	t.Parallel()

	// Name이 빈 문자열인 경우 Validate() 에러 발생 여부 확인
	raw := map[string]any{
		"name": "",
	}

	settings, err := ParseSettings[validatorSettings](raw)

	require.Error(t, err)
	assert.Nil(t, settings)
	assert.Contains(t, err.Error(), "이름은 필수 항목입니다")
}

func TestParseSettings_FullPipeline(t *testing.T) {
	t.Parallel()

	t.Run("성공: 기본값이 주입되어 검증을 통과한다", func(t *testing.T) {
		t.Parallel()

		// Name 필드가 누락되었지만, ApplyDefaults()에서 채워주어 Validate()를 통과해야 합니다.
		raw := map[string]any{
			"value": 10,
		}

		settings, err := ParseSettings[fullPipelineSettings](raw)

		require.NoError(t, err)
		require.NotNil(t, settings)
		assert.Equal(t, "default_name", settings.Name)
		assert.Equal(t, 10, settings.Value)
	})

	t.Run("실패: 채워졌어도 논리적 검증 실패 시 에러가 반환된다", func(t *testing.T) {
		t.Parallel()

		// 부정적인 Value를 주어 Validate()에서 차단되도록 합니다.
		raw := map[string]any{
			"name":  "custom_name",
			"value": -5,
		}

		settings, err := ParseSettings[fullPipelineSettings](raw)

		require.Error(t, err)
		assert.Nil(t, settings)
		assert.Contains(t, err.Error(), "값이 0 이상이어야 합니다")
	})
}

// 5. Decoding 실패 테스트 (예외적 상황)
func TestParseSettings_Decode_Failure(t *testing.T) {
	t.Parallel()

	// maputil 변환 시 실패할 수 있는 의도적인 잘못된 타입 주입 (구조체를 통째로 넣는 등)
	// 예방 차원의 테스트
	raw := map[string]any{
		"value": struct{}{}, // int 자리에 구조체를 주입하여 강제 에러 유도
	}

	settings, err := ParseSettings[basicSettings](raw)

	require.Error(t, err)
	assert.Nil(t, settings)
}
