package provider

import (
	"fmt"
	"regexp"
	"time"
)

// ParseCreatedDate 게시글 목록에서 추출한 날짜 문자열을 time.Time으로 변환합니다.
//
// 지원 형식:
//   - "HH:MM:SS"      : 오늘 날짜 + 해당 시각으로 설정
//   - "yyyy-MM-dd"    : 오늘이면 현재 시각, 과거이면 23:59:59로 설정
//
// 배경:
//
//	게시판 목록 페이지에서 날짜가 오늘이면 "HH:MM:SS" 형식으로,
//	과거이면 "yyyy-MM-dd" 형식으로 표시되는 경우가 일반적입니다.
//	오늘 날짜가 아닌 게시글은 정확한 시각 정보가 없으므로
//	하루의 마지막 시각인 23:59:59를 기본값으로 설정합니다.
func ParseCreatedDate(dateStr string) (time.Time, error) {
	now := time.Now()

	// HH:MM:SS 형식
	if matched, _ := regexp.MatchString(`^[0-9]{2}:[0-9]{2}:[0-9]{2}$`, dateStr); matched {
		t, err := time.ParseInLocation("2006-01-02 15:04:05",
			fmt.Sprintf("%04d-%02d-%02d %s", now.Year(), now.Month(), now.Day(), dateStr),
			time.Local)
		if err == nil && t.After(now) {
			t = t.Add(-24 * time.Hour)
		}
		return t, err
	}

	// HH:MM 형식 (초 정보 없음, 0초로 설정)
	if matched, _ := regexp.MatchString(`^[0-9]{2}:[0-9]{2}$`, dateStr); matched {
		t, err := time.ParseInLocation("2006-01-02 15:04:05",
			fmt.Sprintf("%04d-%02d-%02d %s:00", now.Year(), now.Month(), now.Day(), dateStr),
			time.Local)
		if err == nil && t.After(now) {
			t = t.Add(-24 * time.Hour)
		}
		return t, err
	}

	// yyyy-MM-dd 형식
	if matched, _ := regexp.MatchString(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`, dateStr); matched {
		// 미래시간 노출 및 파싱 시점 차이에 따른 정렬 붕괴(역전)를 방지하기 위해
		// 오늘과 과거 구분 없이 모두 "00:00:00"으로 통일하여 결정론적인 값을 부여합니다.
		dateTimeStr := fmt.Sprintf("%s 00:00:00", dateStr)
		return time.ParseInLocation("2006-01-02 15:04:05", dateTimeStr, time.Local)
	}

	// yyyy.MM.dd. 형식 (네이버 카페 등 점(.) 구분자 사용 사이트)
	if matched, _ := regexp.MatchString(`^[0-9]{4}\.[0-9]{2}\.[0-9]{2}\.$`, dateStr); matched {
		// 결정론적인 시간 보장을 위해 "00:00:00"으로 통일합니다.
		dateTimeStr := fmt.Sprintf("%s 00:00:00", dateStr)
		return time.ParseInLocation("2006.01.02. 15:04:05", dateTimeStr, time.Local)
	}

	return time.Time{}, fmt.Errorf("게시글에서 등록일('%s') 파싱이 실패하였습니다.", dateStr)
}
