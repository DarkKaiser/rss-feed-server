package provider

import (
	"fmt"
	"regexp"
	"time"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
)

// ParseCreatedAt 크롤링으로 수집한 게시글 작성일 문자열을 time.Time 객체로 변환합니다.
//
// 각 사이트는 작성일을 표시하는 방식이 제각각입니다.
// 오늘 작성된 글은 "14:30"처럼 시간만, 과거 글은 "2024-03-15"처럼 날짜만 표시하는 것이 대표적입니다.
// 이 함수는 그러한 파편화된 포맷들을 하나의 일관된 time.Time 값으로 정규화하는 역할을 합니다.
//
// # 지원 포맷
//
// 시간 포맷 ("HH:MM:SS" 또는 "HH:MM"):
//   - 오늘 날짜를 기준으로 시각을 합성합니다.
//   - 합성 결과가 현재 시각보다 미래인 경우(예: 자정 직후 전날 23:50분 글 수집 시),
//     자정 경계를 넘어 날짜가 오늘로 잘못 합성된 것이므로 24시간을 차감해 교정합니다.
//
// 날짜 포맷 ("yyyy-MM-dd" 또는 "yyyy.MM.dd."):
//   - 시/분/초가 없는 날짜에 현재 시간을 그대로 붙이면, 크롤링 실행 시점에 따라 동일 날짜의
//     게시글이 서로 다른 시각을 갖게 됩니다. 이는 재크롤링 시 게시글 순서 역전(Sorting Inversion)을
//     일으켜 커서(Cursor) 기반 중복 수집 방지 로직을 깨뜨릴 수 있습니다.
//   - 이를 방지하기 위해 날짜 포맷 입력은 항상 "00:00:00"을 고정하여, 크롤링 시점과
//     무관하게 결정론적(Deterministic)인 값을 보장합니다.
func ParseCreatedAt(s string) (time.Time, error) {
	now := time.Now()

	// HH:MM:SS 형식
	if matched, _ := regexp.MatchString(`^[0-9]{2}:[0-9]{2}:[0-9]{2}$`, s); matched {
		t, err := time.ParseInLocation("2006-01-02 15:04:05", fmt.Sprintf("%04d-%02d-%02d %s", now.Year(), now.Month(), now.Day(), s), time.Local)
		if err == nil && t.After(now) {
			t = t.Add(-24 * time.Hour)
		}

		return t, err
	}

	// HH:MM 형식 (초 정보 없음, 0초로 설정)
	if matched, _ := regexp.MatchString(`^[0-9]{2}:[0-9]{2}$`, s); matched {
		t, err := time.ParseInLocation("2006-01-02 15:04:05", fmt.Sprintf("%04d-%02d-%02d %s:00", now.Year(), now.Month(), now.Day(), s), time.Local)
		if err == nil && t.After(now) {
			t = t.Add(-24 * time.Hour)
		}

		return t, err
	}

	// yyyy-MM-dd 형식
	if matched, _ := regexp.MatchString(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`, s); matched {
		return time.ParseInLocation("2006-01-02 15:04:05", s+" 00:00:00", time.Local)
	}

	// yyyy.MM.dd. 형식 (네이버 카페 등 점(.) 구분자 사용 사이트)
	if matched, _ := regexp.MatchString(`^[0-9]{4}\.[0-9]{2}\.[0-9]{2}\.$`, s); matched {
		return time.ParseInLocation("2006.01.02. 15:04:05", s+" 00:00:00", time.Local)
	}

	return time.Time{}, apperrors.Newf(apperrors.ParsingFailed, "지원되지 않는 작성일 데이터 포맷('%s')이 감지되어 시간 변환에 실패하였습니다.", s)
}
