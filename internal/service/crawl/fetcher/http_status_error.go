package fetcher

import (
	"fmt"
	"net/http"
)

// HTTPStatusError HTTP 요청 실패 시 상태 코드와 응답 정보를 포함하는 구조화된 에러입니다.
//
// # 개요
//
// 이 에러 타입은 HTTP 요청이 실패했을 때 단순한 에러 메시지 대신 풍부한 컨텍스트 정보를 제공합니다.
// 상태 코드, URL, 응답 헤더, 응답 본문 일부 등을 구조화된 필드로 제공하여,
// 호출자가 에러 상황을 정확히 파악하고 적절한 대응(재시도, 로깅, 알림 등)을 할 수 있도록 돕습니다.
//
// # 사용 예시
//
//	resp, err := fetcher.Do(req)
//	if err != nil {
//	    var httpErr *HTTPStatusError
//	    if errors.As(err, &httpErr) {
//	        // HTTP 상태 코드별 처리
//	        switch httpErr.StatusCode {
//	        case 404:
//	            log.Warn("리소스를 찾을 수 없음", "url", httpErr.URL)
//	        case 500:
//	            log.Error("서버 에러 발생", "body", httpErr.BodySnippet)
//	        }
//	    }
//	}
//
// # 에러 체이닝 지원
//
// Cause 필드에 apperrors.AppError를 포함하여 에러 타입 분류와 체이닝을 지원합니다.
// 표준 Unwrap() 메서드를 통해 errors.Is 및 errors.As와 같은 Go 표준 에러 체이닝 기능을 활용할 수 있습니다.
type HTTPStatusError struct {
	// ========================================
	// HTTP 응답 정보
	// ========================================

	// StatusCode 서버가 반환한 HTTP 상태 코드입니다.
	// 예: 200 (성공), 404 (Not Found), 500 (Internal Server Error)
	StatusCode int

	// Status HTTP 상태 코드에 대응하는 텍스트 설명입니다.
	// 예: "200 OK", "404 Not Found", "500 Internal Server Error"
	Status string

	// URL 요청을 보낸 대상 URL입니다.
	// 민감한 정보(비밀번호, 토큰 등)는 자동으로 마스킹됩니다.
	URL string

	// Header 서버가 반환한 HTTP 응답 헤더입니다.
	// 민감한 헤더(Authorization, Cookie 등)는 자동으로 마스킹됩니다.
	// 디버깅 및 문제 분석 시 유용한 정보를 제공합니다.
	Header http.Header

	// BodySnippet 응답 본문의 일부(최대 4KB)입니다.
	// 전체 본문을 저장하지 않고 앞부분만 캡처하여 메모리 효율성을 유지합니다.
	// 에러 원인 파악 및 디버깅 용도로 사용됩니다.
	BodySnippet string

	// ========================================
	// 에러 원인 정보
	// ========================================

	// Cause 이 HTTP 에러의 근본 원인이 되는 내부 도메인 에러입니다.
	// 주로 apperrors.AppError 타입의 에러가 저장되며,
	// Unwrap() 메서드를 통해 표준 에러 체이닝 패턴을 지원합니다.
	Cause error
}

// Error 표준 error 인터페이스를 구현합니다.
//
// HTTP 상태 코드와 함께 URL, 응답 본문 일부, 원인 에러 등의 상세 정보를 포함한
// 사람이 읽기 쉬운 형태의 에러 메시지를 반환합니다.
//
// 반환 형식:
//
//	"HTTP {상태코드} ({상태텍스트}) URL: {URL}, Body: {본문일부}: {원인에러}"
//
// 예시:
//
//	"HTTP 404 (Not Found) URL: https://example.com/api/users/123, Body: {\"error\":\"user not found\"}: 사용자를 찾을 수 없습니다"
func (e *HTTPStatusError) Error() string {
	msg := fmt.Sprintf("HTTP %d (%s)", e.StatusCode, e.Status)
	if e.URL != "" {
		msg += fmt.Sprintf(" URL: %s", e.URL)
	}
	if e.BodySnippet != "" {
		msg += fmt.Sprintf(", Body: %s", e.BodySnippet)
	}
	if e.Cause != nil {
		msg += fmt.Sprintf(": %v", e.Cause)
	}
	return msg
}

// Unwrap 표준 errors 패키지의 Unwrap 인터페이스를 구현합니다.
//
// 이 메서드는 래핑된 원인 에러(Cause)를 반환하여, errors.Is() 및 errors.As()와 같은
// Go 표준 에러 체이닝 기능을 사용할 수 있게 합니다.
//
// 반환값:
//   - Cause 필드에 저장된 원인 에러 (nil일 수 있음)
func (e *HTTPStatusError) Unwrap() error {
	return e.Cause
}
