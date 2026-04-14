package navercafe

import (
	"errors"
	"testing"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/stretchr/testify/assert"
)

func TestCrawlerSettings_ApplyDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		initialDelayMinutes  int
		expectedDelayMinutes int
	}{
		{
			name:                 "지연 시간이 0인 경우 기본값(40)이 적용된다",
			initialDelayMinutes:  0,
			expectedDelayMinutes: 40,
		},
		{
			name:                 "지연 시간이 음수인 경우 기본값(40)이 적용된다",
			initialDelayMinutes:  -10,
			expectedDelayMinutes: 40,
		},
		{
			name:                 "지연 시간이 명시적으로 양수로 설정된 경우 해당 값을 유지한다",
			initialDelayMinutes:  15,
			expectedDelayMinutes: 15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settings := &crawlerSettings{
				CrawlingDelayMinutes: tt.initialDelayMinutes,
			}

			settings.ApplyDefaults()

			assert.Equal(t, tt.expectedDelayMinutes, settings.CrawlingDelayMinutes)
		})
	}
}

func TestCrawlerSettings_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		initialClubID string
		expectedError error
		expectedValue string
	}{
		{
			name:          "ClubID가 정상적인 문자열인 경우 검증을 통과한다",
			initialClubID: "my_cafe_id",
			expectedError: nil,
			expectedValue: "my_cafe_id",
		},
		{
			name:          "ClubID 앞뒤 공백은 제거 처리되며 검증을 통과한다",
			initialClubID: "  my_cafe_id  ",
			expectedError: nil,
			expectedValue: "my_cafe_id",
		},
		{
			name:          "ClubID가 빈 문자열이면 에러를 반환한다",
			initialClubID: "",
			expectedError: apperrors.New(apperrors.InvalidInput, ""),
		},
		{
			name:          "ClubID가 공백 문자열로만 구성된 경우 에러를 반환한다",
			initialClubID: "   ",
			expectedError: apperrors.New(apperrors.InvalidInput, ""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settings := &crawlerSettings{
				ClubID: tt.initialClubID,
			}

			err := settings.Validate()

			if tt.expectedError != nil {
				var appErr *apperrors.AppError
				if assert.True(t, errors.As(err, &appErr)) {
					expectedAppErr := tt.expectedError.(*apperrors.AppError)
					assert.Equal(t, expectedAppErr.Type(), appErr.Type())
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedValue, settings.ClubID)
			}
		})
	}
}
