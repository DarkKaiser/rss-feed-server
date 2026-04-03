package provider

import (
	"errors"
	"testing"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestErrContentUnavailable_Sentinel 은 ErrContentUnavailable 의 핵심 계약 사항을 검증합니다.
//
// 이 에러는 여러 parser.go 에서 반환되고, base.go 의 재시도 루프가
// errors.Is() 로 감지하여 분기하는 핵심 센티넬 값이므로:
//  1. nil 이 아닌 구체적인 에러 값을 보유해야 하고
//  2. errors.Is() 동등 비교가 정상 동작해야 하며
//  3. apperrors.ExecutionFailed 타입으로 분류되어야 하고
//  4. 래핑(Wrap)되어도 식별이 가능해야 합니다.
func TestErrContentUnavailable_Sentinel(t *testing.T) {
	t.Run("ErrContentUnavailable 는 nil 이 아닌 에러 값이어야 합니다", func(t *testing.T) {
		assert.Error(t, ErrContentUnavailable)
	})

	t.Run("errors.Is() 를 통해 ErrContentUnavailable 자신과 동등 비교되어야 합니다 (센티넬 동작 보장)", func(t *testing.T) {
		// base.go 의 `errors.Is(err, ErrContentUnavailable)` 분기가 올바르게 동작하려면
		// 이 조건이 반드시 true 여야 합니다.
		assert.True(t, errors.Is(ErrContentUnavailable, ErrContentUnavailable))
	})

	t.Run("apperrors.Is() 를 통해 apperrors.ExecutionFailed 타입으로 분류되어야 합니다", func(t *testing.T) {
		// ErrContentUnavailable 은 apperrors.New(apperrors.ExecutionFailed, ...) 로 생성됩니다.
		// apperrors 패키지의 타입 기반 분류 시스템이 올바르게 동작하는지 검증합니다.
		assert.True(t, apperrors.Is(ErrContentUnavailable, apperrors.ExecutionFailed),
			"ErrContentUnavailable 은 apperrors.ExecutionFailed 타입으로 분류되어야 합니다")
	})

	t.Run("fmt.Errorf('%%w') 로 래핑되어도 errors.Is() 로 식별되어야 합니다 (에러 체이닝 호환성)", func(t *testing.T) {
		// 구현체에서 ErrContentUnavailable 을 그대로 반환하지 않고
		// 추가 컨텍스트와 함께 래핑할 수 있으므로, 체이닝 후에도 식별 가능해야 합니다.
		wrapped := errors.Join(errors.New("상위 컨텍스트"), ErrContentUnavailable)
		assert.True(t, errors.Is(wrapped, ErrContentUnavailable),
			"래핑된 에러에서도 ErrContentUnavailable 이 식별되어야 합니다")
	})

	t.Run("관련 없는 에러는 ErrContentUnavailable 과 동등 비교되지 않아야 합니다", func(t *testing.T) {
		unrelated := errors.New("무관한 에러")
		assert.False(t, errors.Is(unrelated, ErrContentUnavailable),
			"무관한 에러는 ErrContentUnavailable 와 동등해서는 안 됩니다")
	})

	t.Run("nil 에러는 ErrContentUnavailable 과 동등 비교되지 않아야 합니다", func(t *testing.T) {
		assert.False(t, errors.Is(nil, ErrContentUnavailable))
	})

	t.Run("에러 메시지가 비어있지 않아야 합니다", func(t *testing.T) {
		require.NotEmpty(t, ErrContentUnavailable.Error(),
			"센티넬 에러는 디버깅을 위해 비어있지 않은 메시지를 가져야 합니다")
	})

	t.Run("다른 apperrors 타입과 혼동되지 않아야 합니다", func(t *testing.T) {
		// ErrContentUnavailable 은 ExecutionFailed 이지만, ParsingFailed 나 System 으로는
		// 분류되어서는 안 됩니다. 타입 오분류 버그를 방지합니다.
		assert.False(t, apperrors.Is(ErrContentUnavailable, apperrors.ParsingFailed),
			"ErrContentUnavailable 은 ParsingFailed 타입이어서는 안 됩니다")
		assert.False(t, apperrors.Is(ErrContentUnavailable, apperrors.System),
			"ErrContentUnavailable 은 System 타입이어서는 안 됩니다")
		assert.False(t, apperrors.Is(ErrContentUnavailable, apperrors.Unauthorized),
			"ErrContentUnavailable 은 Unauthorized 타입이어서는 안 됩니다")
	})
}
