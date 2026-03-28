package fetcher

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
)

var (
	// sensitiveExactKeys 전체 문자열이 정확히 일치(Exact Match)해야 마스킹되는 쿼리 파라미터 키워드 목록입니다.
	//
	// "key", "token"과 같이 일반적인 단어를 부분 일치로 검사하면, "monkey", "broken" 같은
	// 무해한 단어까지 마스킹되는 오탐(False Positive)이 발생할 수 있습니다.
	// 이를 방지하기 위해, 이곳에 정의된 키워드들은 대소문자 구분 없이 전체 문자열이 일치할 때만 민감한 정보로 처리합니다.
	sensitiveExactKeys = []string{
		// 1. 일반적인 보안 키워드
		"token", "auth", "key", "secret", "pass", "credential", "signature", "password", "passwd",

		// 2. 널리 사용되는 API 키 및 토큰
		// "_key"와 같은 포괄적인 접미사 매칭은 오탐 가능성이 높으므로, 자주 쓰이는 조합을 명시적으로 등록합니다.
		"access_token", "api_key", "client_secret", "refresh_token", "id_token",
		"access_key", "secret_key", "private_key", "public_key",
		"client_id", "client_key", "app_key", "auth_key",
	}

	// sensitiveSuffixes 특정 접미사(Suffix)로 끝나야 마스킹되는 쿼리 파라미터 키워드 목록입니다.
	//
	// "_token", "_secret"과 같이 구조화된 명명 규칙을 따르는 변수들을 포괄적으로 감지하기 위해 사용합니다.
	// 예: 이 목록에 "_secret"이 있으면, "client_secret", "app_secret" 등이 모두 자동으로 마스킹됩니다.
	sensitiveSuffixes = []string{
		"_token", "_secret", "_cred", "_sig", "_password", "_passwd",
	}
)

// redactHeaders HTTP 응답 헤더에서 민감한 정보를 마스킹하여 안전한 복사본을 반환합니다.
//
// # 목적
//
// 로깅이나 에러 메시지에 HTTP 헤더를 포함할 때, 인증 토큰이나 쿠키 같은 민감한 정보가
// 노출되지 않도록 보호합니다. 원본 헤더는 변경하지 않고 복사본을 생성하여 마스킹합니다.
//
// # 마스킹 대상 헤더
//
//   - Authorization: Bearer 토큰, Basic 인증 정보 등
//   - Proxy-Authorization: 프록시 인증 정보
//   - Cookie: 세션 쿠키 등 클라이언트 측 인증 정보
//   - Set-Cookie: 서버가 설정하는 쿠키 정보
//
// 매개변수:
//   - h: 마스킹할 HTTP 헤더 (nil 허용)
//
// 반환값:
//   - 민감한 정보가 마스킹된 헤더 복사본 (입력이 nil이면 nil 반환)
func redactHeaders(h http.Header) http.Header {
	if h == nil {
		return nil
	}

	masked := h.Clone()

	sensitive := []string{"Authorization", "Proxy-Authorization", "Cookie", "Set-Cookie"}
	for _, key := range sensitive {
		if masked.Get(key) != "" {
			masked.Set(key, "***")
		}
	}

	return masked
}

// redactURL URL에서 민감한 정보를 마스킹하여 안전한 문자열로 반환합니다.
//
// # 목적
//
// 로깅이나 에러 메시지에 URL을 포함할 때, 비밀번호, API 키, 토큰, 프록시 인증 정보 등의 민감한 정보가
// 노출되지 않도록 보호합니다. URL의 구조는 유지하면서 민감한 값만 마스킹합니다.
//
// # 마스킹 대상
//
// 1. **사용자 인증 정보**: `https://user:password@example.com` → `https://user:xxxxx@example.com`
// 2. **민감한 쿼리 파라미터 값**: `?token=secret&id=123` → `?token=xxxxx&id=123`
//
// # 동작 방식
//
// 1. URL의 User 정보(Username, Password)가 존재하면 "xxxxx"로 마스킹
// 2. 쿼리 파라미터 중 지정된 민감한 키(Sensitive Key)의 값만 선별적으로 "xxxxx"로 치환
// 3. 그 외 Path나 Fragment(#) 정보는 변경 없이 그대로 유지
//
// # 사용 예시
//
//	u, _ := url.Parse("https://admin:secret@api.example.com/v1/users?token=abc123&id=456")
//	safe := redactURL(u)
//	// 결과: "https://admin:xxxxx@api.example.com/v1/users?token=xxxxx&id=456"
//
// 매개변수:
//   - u: 마스킹할 URL (nil 허용)
//
// 반환값:
//   - 민감한 정보가 마스킹된 URL 문자열 (입력이 nil이면 빈 문자열 반환)
//
// 주의사항:
//   - 원본 URL 객체는 변경되지 않습니다 (불변성 보장)
//   - 파싱 실패 시 기본 마스킹 결과(Redacted())를 반환합니다
func redactURL(u *url.URL) string {
	if u == nil {
		return ""
	}

	// 1. URL 구조체 복제 (얕은 복사)
	// 원본 URL을 변경하지 않기 위해 복사본을 만들어 작업합니다.
	// url.URL은 구조체이므로 값 복사가 일어납니다. User 등의 포인터 필드는 아래에서 새로 할당하므로 안전합니다.
	ru := *u

	// 2. Password 마스킹
	// User 정보가 있는 경우(비밀번호 포함 또는 토큰 단독 사용) 마스킹을 수행합니다.
	if u.User != nil {
		if _, has := u.User.Password(); has {
			ru.User = url.UserPassword(u.User.Username(), "xxxxx")
		} else if u.User.Username() != "" {
			// 비밀번호 없이 사용자명만 있는 경우 (예: https://token@api.com)
			// 사용자명을 토큰으로 간주하여 마스킹합니다.
			ru.User = url.User("xxxxx")
		}
	}

	// 3. 쿼리 파라미터 값 선별적 마스킹 (민감한 키만 마스킹)
	if u.RawQuery != "" {
		query := ru.Query()
		for key := range query {
			if isSensitiveKey(key) {
				query.Set(key, "xxxxx")
			}
		}

		ru.RawQuery = query.Encode()
	}

	return ru.String()
}

// redactRawURL URL 문자열에서 민감한 정보를 마스킹하여 안전한 문자열로 반환합니다.
//
// # 목적
//
// 로깅이나 에러 메시지에 URL을 포함할 때, 비밀번호, API 키, 토큰, 프록시 인증 정보 등의 민감한 정보가
// 노출되지 않도록 보호합니다. URL의 구조는 유지하면서 민감한 값만 마스킹합니다.
//
// # 처리 전략
//
// 1. **정상 파싱 가능한 경우**: `redactURL` 함수를 통해 표준적인 마스킹 수행
//
// 2. **파싱 실패 시 폴백(Fallback)**: 문자열 조작으로 최선의 마스킹 수행
//   - 비표준 형식의 URL(스킴 없는 프록시 주소 등)도 안전하게 처리
//   - @ 기호를 기준으로 인증 정보 부분을 감지하여 마스킹
//
// # 사용 예시
//
//	redactRawURL("http://admin:secret@api.example.com/v1?token=abc123&id=456")
//	// 결과: "http://admin:xxxxx@api.example.com/v1?token=xxxxx&id=456"
//
//	redactRawURL("user:pass@proxy.internal.com:8080")  // 스킴 없는 프록시 주소
//	// 결과: "xxxxx:xxxxx@proxy.internal.com:8080"
//
//	redactRawURL("https://example.com/public")  // 인증 정보 없음
//	// 결과: "https://example.com/public" (변경 없음)
//
// 매개변수:
//   - rawURL: 마스킹할 URL 문자열
//
// 반환값:
//   - 민감한 정보가 마스킹된 URL 문자열
//
// 주의사항:
//   - 원본 URL 문자열은 변경되지 않습니다 (불변성 보장)
//   - 파싱 불가능한 URL도 최선의 노력으로 마스킹을 시도합니다
func redactRawURL(rawURL string) string {
	u, err := url.Parse(rawURL)

	// 파싱에 실패하였거나, "://"를 포함하지 않는 비표준 형태(예: "user:pass@host")이면서 '@'가 포함된 경우 폴백 로직을 수행합니다.
	// "://"가 있는 정상 URL은 redactURL이 처리하도록 합니다.
	// 주의: "user:pass@host"는 url.Parse 시 "user"를 스킴으로 오인할 수 있어 u.Scheme 확인만으로는 부족합니다.
	if err != nil || (!strings.Contains(rawURL, "://") && strings.Contains(rawURL, "@")) {
		// [파싱 실패 또는 불완전 파싱 시 폴백 전략]
		// 비표준 형식의 URL(예: 스킴 없는 프록시 주소)도 안전하게 처리하기 위해
		// 문자열 조작을 통해 최소한의 마스킹을 수행합니다.

		// 검색 범위 제한: 쿼리(?)나 프래그먼트(#) 시작 전까지로 제한
		// 이는 쿼리 파라미터(예: email=user@test.com)에 포함된 @를 인증 정보 구분자로 오인하는 것을 방지합니다.
		authSearchLimit := len(rawURL)
		if idx := strings.IndexAny(rawURL, "?#"); idx != -1 {
			authSearchLimit = idx
		}

		// 제한된 범위 내에서 @ 기호를 찾아 인증 정보 존재 여부 확인
		if authSplitIdx := strings.LastIndex(rawURL[:authSearchLimit], "@"); authSplitIdx != -1 {
			// 스킴(http://, https:// 등)이 있는지 확인
			if schemeSplitIdx := strings.Index(rawURL[:authSplitIdx], "://"); schemeSplitIdx != -1 {
				// 스킴이 있는 경우
				// 스킴 부분은 유지하고, @ 앞의 인증 정보만 마스킹 ("http://user:pass@host" → "http://xxxxx:xxxxx@host")
				return rawURL[:schemeSplitIdx+3] + "xxxxx:xxxxx" + rawURL[authSplitIdx:]
			}

			// 스킴이 없는 경우
			// 프록시 설정 등에서 스킴 없이 입력되는 경우 처리 ("user:pass@host" → "xxxxx:xxxxx@host")
			return "xxxxx:xxxxx" + rawURL[authSplitIdx:]
		}

		// @ 기호가 없으면 인증 정보가 없는 것으로 간주하여 원본 그대로 반환
		return rawURL
	}

	return redactURL(u)
}

// redactRefererURL URL에서 사용자 자격 증명(Userinfo)을 제거하고,
// 쿼리 파라미터 내 민감한 정보를 마스킹하여 Referer 헤더에 안전하게 사용할 수 있는 문자열을 반환합니다.
//
// # 목적
//
// HTTP 리다이렉트 시 Referer 헤더를 설정할 때, RFC 7231 섹션 5.5.2를 준수하고
// 민감한 정보가 노출되지 않도록 URL을 정제합니다.
//
// # 마스킹 대상
//
// 1. **사용자 인증 정보**: `https://user:password@example.com` → `https://example.com` (완전 제거)
// 2. **민감한 쿼리 파라미터 값**: `?token=secret&id=123` → `?token=xxxxx&id=123`
//
// # 동작 방식
//
// 1. URL의 User 정보(Username, Password)를 완전히 제거 (RFC 7231 준수)
// 2. 쿼리 파라미터 중 지정된 민감한 키(Sensitive Key)의 값만 선별적으로 "xxxxx"로 치환
// 3. 그 외 Path나 Fragment(#) 정보는 변경 없이 그대로 유지
//
// # 사용 예시
//
//	u, _ := url.Parse("https://admin:secret@api.example.com/v1/users?token=abc123&id=456")
//	safe := redactRefererURL(u)
//	// 결과: "https://api.example.com/v1/users?token=xxxxx&id=456"
//
// 매개변수:
//   - u: 마스킹할 URL (nil 허용)
//
// 반환값:
//   - Referer 헤더에 안전하게 사용할 수 있는 URL 문자열 (입력이 nil이면 빈 문자열 반환)
//
// 주의사항:
//   - 원본 URL 객체는 변경되지 않습니다 (불변성 보장)
func redactRefererURL(u *url.URL) string {
	if u == nil {
		return ""
	}

	// 1. URL 구조체 복제 (얕은 복사)
	// 원본 URL을 변경하지 않기 위해 복사본을 만들어 작업합니다.
	ru := *u

	// 2. 사용자 자격 증명(Userinfo) 완전 제거 (RFC 7231 준수)
	ru.User = nil

	// 3. 쿼리 파라미터 값 선별적 마스킹 (민감한 키만 마스킹)
	if u.RawQuery != "" {
		masked := false

		query := ru.Query()
		for key := range query {
			if isSensitiveKey(key) {
				query.Set(key, "xxxxx")

				masked = true
			}
		}

		if masked {
			ru.RawQuery = query.Encode()
		}
	}

	return ru.String()
}

// isSensitiveKey 주어진 키가 민감한 정보를 나타내는 키워드인지 확인합니다.
//
// 매개변수:
//   - key: 검사할 쿼리 파라미터 키 이름
//
// 반환값:
//   - 민감한 정보를 나타내는 키워드이면 true, 그렇지 않으면 false
func isSensitiveKey(key string) bool {
	lowerKey := strings.ToLower(key)

	// 1. 정확히 일치하는 경우...
	if slices.Contains(sensitiveExactKeys, lowerKey) {
		return true
	}

	// 2. 특정 접미사로 끝나는 경우...
	for _, suffix := range sensitiveSuffixes {
		if strings.HasSuffix(lowerKey, suffix) {
			return true
		}
	}

	return false
}
