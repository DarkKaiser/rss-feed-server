package provider

import (
	"testing"
	"time"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedNow는 테스트 시각 의존성을 없애기 위해 ParseCreatedAt 내부의 time.Now()와
// 동일한 정밀도(초 단위 이하 제거)로 맞춘 고정 기준 시각입니다.
// 단, ParseCreatedAt은 time.Now()를 직접 호출하므로, 시간 포맷 테스트에서는
// 테스트 시작 직전 now를 캡처하여 결과 검증에 사용합니다.

func TestParseCreatedAt_TimeFormat_HHMMSS(t *testing.T) {
	t.Run("정상: 현재 시각보다 과거인 시간 문자열 (HH:MM:SS)", func(t *testing.T) {
		// 과거 시각을 보장하기 위해 자정(00:00:01)으로 고정
		// 실제 테스트 환경에서는 함수 내부 time.Now()를 제어할 수 없으므로,
		// 자정 이후 1초 후라는 가장 안전한 과거 케이스를 선택합니다.
		now := time.Now()
		// 현재 시각의 HH:MM:SS 보다 1초 앞선 과거 시각을 생성합니다.
		pastTime := now.Add(-1 * time.Hour)
		input := pastTime.Format("15:04:05")

		got, err := ParseCreatedAt(input)

		require.NoError(t, err)
		// 날짜가 오늘 또는 어제(자정 경계 교정)인지 확인
		assert.False(t, got.IsZero())
		// 반환된 시각이 현재보다 미래가 아닌지 확인 (핵심 불변조건)
		assert.False(t, got.After(time.Now()), "반환된 시각이 현재 시각보다 미래여서는 안 됩니다")
		// 시/분/초가 입력값과 일치하는지 확인
		assert.Equal(t, input, got.Format("15:04:05"))
	})

	t.Run("정상: 자정 경계 교정 — 미래 시각은 전날로 보정 (HH:MM:SS)", func(t *testing.T) {
		// 현재보다 1시간 뒤 시각 문자열을 입력 → 미래로 판정되어 -24h 교정되어야 합니다.
		futureTime := time.Now().Add(1 * time.Hour)
		input := futureTime.Format("15:04:05")

		got, err := ParseCreatedAt(input)

		require.NoError(t, err)
		// 교정 후에는 현재 시각보다 반드시 과거여야 합니다.
		assert.True(t, got.Before(time.Now()), "미래 시각은 24시간 차감 후 현재보다 과거여야 합니다")
	})

	t.Run("실패: 유효하지 않은 시각 값 (HH:MM:SS 형식이나 값이 범위 초과)", func(t *testing.T) {
		// 패턴은 정규식을 통과하지만 time.ParseInLocation 에서 실패하는 케이스
		_, err := ParseCreatedAt("25:61:99")

		assert.Error(t, err)
	})
}

func TestParseCreatedAt_TimeFormat_HHMM(t *testing.T) {
	t.Run("정상: 현재 시각보다 과거인 시간 문자열 (HH:MM)", func(t *testing.T) {
		pastTime := time.Now().Add(-1 * time.Hour)
		input := pastTime.Format("15:04")

		got, err := ParseCreatedAt(input)

		require.NoError(t, err)
		assert.False(t, got.IsZero())
		assert.False(t, got.After(time.Now()), "반환된 시각이 현재 시각보다 미래여서는 안 됩니다")
		// 초는 항상 00으로 고정됨
		assert.Equal(t, "00", got.Format("05"), "초(second)는 항상 00이어야 합니다")
		assert.Equal(t, input, got.Format("15:04"))
	})

	t.Run("정상: 자정 경계 교정 — 미래 시각은 전날로 보정 (HH:MM)", func(t *testing.T) {
		futureTime := time.Now().Add(1 * time.Hour)
		input := futureTime.Format("15:04")

		got, err := ParseCreatedAt(input)

		require.NoError(t, err)
		assert.True(t, got.Before(time.Now()), "미래 시각은 24시간 차감 후 현재보다 과거여야 합니다")
	})

	t.Run("실패: 유효하지 않은 시각 값 (HH:MM 형식이나 값이 범위 초과)", func(t *testing.T) {
		_, err := ParseCreatedAt("25:61")

		assert.Error(t, err)
	})
}

func TestParseCreatedAt_DateFormat_YyyyMMDD_Dash(t *testing.T) {
	t.Run("정상: 과거 날짜 (yyyy-MM-dd)", func(t *testing.T) {
		got, err := ParseCreatedAt("2024-03-15")

		require.NoError(t, err)
		assert.Equal(t, 2024, got.Year())
		assert.Equal(t, time.March, got.Month())
		assert.Equal(t, 15, got.Day())
	})

	t.Run("정상: 시각은 항상 00:00:00 으로 고정 (결정론적 보장)", func(t *testing.T) {
		// 동일 날짜는 크롤링 시점에 관계없이 항상 같은 시각을 반환해야 합니다.
		got1, err1 := ParseCreatedAt("2024-03-15")
		got2, err2 := ParseCreatedAt("2024-03-15")

		require.NoError(t, err1)
		require.NoError(t, err2)
		assert.Equal(t, got1, got2, "동일 날짜 입력은 항상 동일한 time.Time을 반환해야 합니다")
		assert.Equal(t, "00:00:00", got1.Format("15:04:05"), "시각은 항상 00:00:00 이어야 합니다")
	})

	t.Run("정상: 로컬 시간대(time.Local) 적용 확인", func(t *testing.T) {
		got, err := ParseCreatedAt("2024-03-15")

		require.NoError(t, err)
		assert.Equal(t, time.Local, got.Location(), "로컬 시간대가 적용되어야 합니다")
	})

	t.Run("실패: 유효하지 않은 날짜 값 (yyyy-MM-dd 형식이나 존재하지 않는 날짜)", func(t *testing.T) {
		// 패턴은 통과하나 존재하지 않는 날짜
		_, err := ParseCreatedAt("2024-13-45")

		assert.Error(t, err)
	})
}

func TestParseCreatedAt_DateFormat_YyyyMMDD_Dot(t *testing.T) {
	t.Run("정상: 과거 날짜 (yyyy.MM.dd.)", func(t *testing.T) {
		got, err := ParseCreatedAt("2024.03.15.")

		require.NoError(t, err)
		assert.Equal(t, 2024, got.Year())
		assert.Equal(t, time.March, got.Month())
		assert.Equal(t, 15, got.Day())
	})

	t.Run("정상: 시각은 항상 00:00:00 으로 고정 (결정론적 보장)", func(t *testing.T) {
		got1, err1 := ParseCreatedAt("2024.03.15.")
		got2, err2 := ParseCreatedAt("2024.03.15.")

		require.NoError(t, err1)
		require.NoError(t, err2)
		assert.Equal(t, got1, got2, "동일 날짜 입력은 항상 동일한 time.Time을 반환해야 합니다")
		assert.Equal(t, "00:00:00", got1.Format("15:04:05"), "시각은 항상 00:00:00 이어야 합니다")
	})

	t.Run("정상: 로컬 시간대(time.Local) 적용 확인", func(t *testing.T) {
		got, err := ParseCreatedAt("2024.03.15.")

		require.NoError(t, err)
		assert.Equal(t, time.Local, got.Location(), "로컬 시간대가 적용되어야 합니다")
	})

	t.Run("실패: 유효하지 않은 날짜 값 (yyyy.MM.dd. 형식이나 존재하지 않는 날짜)", func(t *testing.T) {
		_, err := ParseCreatedAt("2024.13.45.")

		assert.Error(t, err)
	})
}

func TestParseCreatedAt_DateFormatEquivalence(t *testing.T) {
	t.Run("두 날짜 포맷(대시/점)은 동일 날짜에 대해 동등한 time.Time을 반환해야 합니다", func(t *testing.T) {
		// yyyy-MM-dd 와 yyyy.MM.dd. 는 동일 날짜를 표현하므로 결과가 같아야 합니다.
		dashResult, err1 := ParseCreatedAt("2024-03-15")
		dotResult, err2 := ParseCreatedAt("2024.03.15.")

		require.NoError(t, err1)
		require.NoError(t, err2)
		assert.True(t, dashResult.Equal(dotResult), "yyyy-MM-dd 와 yyyy.MM.dd. 포맷은 동일한 time.Time을 반환해야 합니다")
	})
}

func TestParseCreatedAt_UnsupportedFormat(t *testing.T) {
	unsupportedCases := []struct {
		name  string
		input string
	}{
		{"빈 문자열", ""},
		{"공백 문자열", "   "},
		{"날짜-시간 복합 형식 (지원 안 함)", "2024-03-15 14:30:00"},
		{"구분자 없는 숫자열", "20240315"},
		{"후행 점 없는 yyyy.MM.dd 형식", "2024.03.15"},
		{"슬래시 구분자 날짜", "2024/03/15"},
		{"한국어 날짜 형식", "2024년 03월 15일"},
		{"초가 포함된 HH:MM:SS:ms 형식", "14:30:00:123"},
	}

	for _, tc := range unsupportedCases {
		tc := tc // 루프 변수 캡처
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCreatedAt(tc.input)

			assert.Error(t, err, "지원하지 않는 포맷은 반드시 에러를 반환해야 합니다")
			assert.True(t, got.IsZero(), "에러 시 반환되는 time.Time은 zero value여야 합니다")
		})
	}
}

func TestParseCreatedAt_UnsupportedFormat_ErrorType(t *testing.T) {
	t.Run("지원하지 않는 포맷 에러는 apperrors.ParsingFailed 타입이어야 합니다", func(t *testing.T) {
		_, err := ParseCreatedAt("invalid-format")

		require.Error(t, err)
		assert.True(t,
			apperrors.Is(err, apperrors.ParsingFailed),
			"반환된 에러는 apperrors.ParsingFailed 타입이어야 합니다",
		)
	})
}
