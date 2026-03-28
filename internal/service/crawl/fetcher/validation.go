package fetcher

import (
	"bytes"
	"io"
	"net/http"
	"slices"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
)

// CheckResponseStatus HTTP 응답의 상태 코드를 검증하고, 실패 시 구조화된 에러를 반환합니다.
//
// # 목적
//
// HTTP 응답 상태 코드를 검사하여 성공/실패를 판단하고, 실패한 경우 상태 코드에 맞는
// 도메인 에러 타입(apperrors.ErrorType)으로 변환하여 HTTPStatusError를 생성합니다.
//
// # 사용 예시
//
//	resp, _ := http.Get("https://api.example.com/users")
//	if err := CheckResponseStatus(resp); err != nil {
//	    var statusErr *HTTPStatusError
//	    if errors.As(err, &statusErr) {
//	        log.Error("API 호출 실패", "status", statusErr.StatusCode, "body", statusErr.BodySnippet)
//	    }
//	}
//
// 매개변수:
//   - resp: 검증할 HTTP 응답 객체
//   - allowedStatusCodes: 허용할 상태 코드 목록 (비어있으면 200 OK만 허용)
//
// 반환값:
//   - 상태 코드가 허용되면 nil, 그렇지 않으면 HTTPStatusError
//
// 주의사항:
//   - 이 함수는 응답 객체의 Body를 재구성하므로, 호출 후에도 Body를 읽을 수 있습니다.
//   - Body를 즉시 닫을 예정이라면 CheckResponseStatusWithoutReconstruct를 사용하세요.
func CheckResponseStatus(resp *http.Response, allowedStatusCodes ...int) error {
	return checkResponseStatus(resp, true, allowedStatusCodes...)
}

// CheckResponseStatusWithoutReconstruct HTTP 응답의 상태 코드를 검증하고, 실패 시 Body 재구성 없이 구조화된 에러를 반환합니다.
//
// # 목적
//
// CheckResponseStatus와 동일한 검증을 수행하지만, 응답 객체의 Body를 재구성하지 않습니다.
// 에러 발생 시 즉시 Body를 닫을 계획이 있는 경우 이 함수를 사용하면 불필요한
// Body 재구성 오버헤드를 피할 수 있습니다.
//
// # 사용 시나리오
//
//   - StatusCodeFetcher처럼 에러 발생 시 즉시 drainAndCloseBody()를 호출하는 경우
//   - Body 내용을 다시 읽을 필요가 없는 경우
//   - 성능 최적화가 중요한 경우
//
// # 사용 예시
//
//	resp, _ := fetcher.Do(req)
//	if err := CheckResponseStatusWithoutReconstruct(resp, 200, 201); err != nil {
//	    drainAndCloseBody(resp.Body) // Body를 즉시 닫음
//	    return nil, err
//	}
//
// 매개변수:
//   - resp: 검증할 HTTP 응답 객체
//   - allowedStatusCodes: 허용할 상태 코드 목록
//
// 반환값:
//   - 상태 코드가 허용되면 nil, 그렇지 않으면 HTTPStatusError
//
// 주의사항:
//   - 이 함수 호출 후 resp.Body는 일부가 읽힌 상태이므로, 에러 시 Body를 즉시 닫아야 합니다.
func CheckResponseStatusWithoutReconstruct(resp *http.Response, allowedStatusCodes ...int) error {
	return checkResponseStatus(resp, false, allowedStatusCodes...)
}

// checkResponseStatus 상태 코드 검증의 실제 구현 로직입니다.
//
// 이 함수는 내부 전용(unexported)이며, CheckResponseStatus와 CheckResponseStatusWithoutReconstruct의
// 공통 로직을 담당합니다. reconstruct 플래그에 따라 Body 재구성 여부를 결정합니다.
//
// 매개변수:
//   - resp: 검증할 HTTP 응답 객체
//   - reconstruct: true면 Body를 재구성, false면 재구성하지 않음
//   - allowedStatusCodes: 허용할 상태 코드 목록
//
// 반환값:
//   - 상태 코드가 허용되면 nil, 그렇지 않으면 HTTPStatusError
func checkResponseStatus(resp *http.Response, reconstruct bool, allowedStatusCodes ...int) error {
	// 1. 응답 상태 코드가 허용할 상태코드 목록에 있는지 확인
	isAllowed := false
	if len(allowedStatusCodes) == 0 {
		// 허용할 상태코드 목록이 비어있으면 200 OK만 허용
		isAllowed = resp.StatusCode == http.StatusOK
	} else {
		if slices.Contains(allowedStatusCodes, resp.StatusCode) {
			isAllowed = true
		}
	}

	if isAllowed {
		return nil
	}

	// 2. 응답 상태 코드를 도메인 에러 타입으로 매핑
	errType := apperrors.ExecutionFailed

	switch resp.StatusCode {
	case http.StatusNotFound:
		errType = apperrors.NotFound

	case http.StatusForbidden, http.StatusUnauthorized:
		errType = apperrors.Forbidden

	case http.StatusBadRequest:
		errType = apperrors.InvalidInput

	case http.StatusTooManyRequests, http.StatusRequestTimeout:
		errType = apperrors.Unavailable

	default:
		if resp.StatusCode >= 500 {
			errType = apperrors.Unavailable
		}
	}

	// 3. 요청 URL 추출 및 민감 정보 마스킹
	urlStr := ""
	if resp.Request != nil && resp.Request.URL != nil {
		urlStr = redactURL(resp.Request.URL)
	}

	// 4. 응답 객체의 Body 일부만 읽기 (디버깅 정보용)
	var bodySnippet string
	if resp.Body != nil {
		// 응답 객체의 Body 일부만 읽어서 메모리 효율성 유지
		lr := io.LimitReader(resp.Body, 4096)
		bodyBytes, err := io.ReadAll(lr)
		if err == nil && len(bodyBytes) > 0 {
			bodySnippet = string(bodyBytes)

			if reconstruct {
				// 읽은 부분을 다시 채워넣어 호출자가 응답 본문을 온전히 읽을 수 있게 함!
				// 익명 구조체를 사용하여 원본 응답 객체 Body의 Close() 메서드를 보존!
				resp.Body = struct {
					io.Reader
					io.Closer
				}{
					Reader: io.MultiReader(bytes.NewReader(bodyBytes), resp.Body),
					Closer: resp.Body,
				}
			}
		}
	}

	return &HTTPStatusError{
		StatusCode:  resp.StatusCode,
		Status:      resp.Status,
		URL:         urlStr,
		Header:      redactHeaders(resp.Header),
		BodySnippet: bodySnippet,
		Cause:       newErrUnexpectedHTTPStatus(errType, resp.Status, urlStr),
	}
}
