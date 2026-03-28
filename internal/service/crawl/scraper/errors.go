package scraper

import (
	"fmt"
	"net/http"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// JSON 응답 처리 에러 (JSON Response Handling)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// newErrJSONParseFailed JSON 바이트 스트림을 Go 구조체로 역직렬화(Unmarshal)하는 과정에서 구문 오류가 발생했을 때 에러를 생성합니다.
//
// 에러 발생 위치(Offset)와 주변 문맥(Snippet)을 포함하여, API 응답 형식 변경이나 문자 인코딩 문제를 즉시 파악할 수 있도록 지원합니다.
//
// 매개변수:
//   - cause: json.Decoder.Decode()가 반환한 원본 에러 (json.SyntaxError 등)
//   - url: 요청을 보낸 대상 URL (에러 발생 위치 추적용)
//   - offset: JSON 구문 오류가 발생한 바이트 위치 (json.SyntaxError.Offset, 0이면 위치 정보 없음)
//   - snippet: 에러 발생 위치 주변의 문맥 데이터 (빈 문자열이면 문맥 정보 없음)
//
// 반환값: apperrors.ParsingFailed 타입의 에러
func newErrJSONParseFailed(cause error, url string, offset int, snippet string) error {
	m := fmt.Sprintf("JSON 파싱 실패: 구문 오류로 인해 응답 데이터를 변환할 수 없습니다 (URL: %s)", url)

	if len(snippet) > 0 {
		m += fmt.Sprintf(" - 오류 위치: %d, 주변 문맥: ...%s...", offset, snippet)
	}

	return apperrors.Wrap(cause, apperrors.ParsingFailed, m)
}

// newErrJSONUnexpectedToken 유효한 JSON 객체 파싱 완료 후에도 데이터 스트림에 처리되지 않은 잔여 데이터(Extra Data)가 존재할 때 발생하는 에러를 생성합니다.
//
// 표준 JSON 디코더는 첫 번째 유효한 객체 파싱 후 작업을 멈추지만, 본 스크래퍼는 데이터 무결성 보장을 위해 스트림 끝(EOF)까지 완전히 비어있는지 검증합니다.
// 이는 오염된 응답 데이터로 인한 비즈니스 로직 오작동을 사전에 차단하는 핵심 안전장치입니다.
//
// 매개변수:
//   - url: 요청을 보낸 대상 URL (에러 발생 위치 추적용)
//
// 반환값: apperrors.ParsingFailed 타입의 에러
func newErrJSONUnexpectedToken(url string) error {
	return apperrors.New(apperrors.ParsingFailed, fmt.Sprintf("JSON 파싱 실패: 응답 데이터에 유효한 JSON 이후 불필요한 토큰이 포함되어 있습니다 (URL: %s)", url))
}

// newErrUnexpectedHTMLResponse JSON API 호출 시 서버가 HTML 응답(에러 페이지, 로그인 페이지 등)을 반환했을 때 발생하는 에러를 생성합니다.
//
// 주요 원인: API 엔드포인트 오류(URL 오타, 경로 변경), 인증 실패(세션 만료로 인한 로그인 페이지 리다이렉트), 서버 에러(500/403 등의 HTML 에러 페이지 응답)
//
// 매개변수:
//   - url: 요청을 보낸 대상 URL (에러 발생 위치 추적용)
//   - contentType: 응답의 Content-Type 헤더 값 (디버깅용)
//
// 반환값: apperrors.InvalidInput 타입의 에러
func newErrUnexpectedHTMLResponse(url, contentType string) error {
	return apperrors.New(apperrors.InvalidInput, fmt.Sprintf("응답 형식 오류: JSON 대신 HTML이 반환되었습니다 (URL: %s, Content-Type: %s)", url, contentType))
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// HTML 파싱 에러 (HTML Parsing)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// newErrHTMLParseFailed HTML 바이트 스트림을 DOM 트리(goquery.Document)로 파싱하는 과정에서 치명적인 오류가 발생했을 때 에러를 생성합니다.
//
// 마이너 문법 오류는 브라우저처럼 관대하게 처리되지만, 스트림 손상이나 메모리 할당 실패 등 심각한 문제 발생 시 이 에러가 반환됩니다.
//
// 매개변수:
//   - cause: goquery.NewDocumentFromReader()가 반환한 원본 에러
//   - url: 요청을 보낸 대상 URL (에러 발생 위치 추적용)
//
// 반환값: apperrors.ParsingFailed 타입의 에러
func newErrHTMLParseFailed(cause error, url string) error {
	return apperrors.Wrap(cause, apperrors.ParsingFailed, fmt.Sprintf("HTML 파싱 실패: DOM 트리 생성 중 오류가 발생했습니다 (URL: %s)", url))
}

// NewErrHTMLStructureChanged 대상 페이지의 HTML 구조 변경으로 인해 예상 요소를 찾을 수 없을 때 발생하는 '논리적 파싱 실패' 에러를 생성합니다.
//
// 이 에러는 네트워크 장애가 아닌 사이트 개편(UI/UX 변경, 프론트엔드 프레임워크 교체)을 감지하는 핵심 관측 지표로, CSS 셀렉터 업데이트가 필요함을 의미합니다.
//
// 매개변수:
//   - url: 구조 변경이 감지된 페이지 URL (빈 문자열 가능)
//   - message: 구조 변경에 대한 설명 메시지
//
// 반환값: apperrors.ExecutionFailed 타입의 에러
func NewErrHTMLStructureChanged(url, message string) error {
	if url != "" {
		return apperrors.New(apperrors.ExecutionFailed, fmt.Sprintf("HTML 구조 변경: %s (URL: %s)", message, url))
	}
	return apperrors.New(apperrors.ExecutionFailed, fmt.Sprintf("HTML 구조 변경: %s", message))
}

// newErrReadHTMLInput HTML 입력 데이터 스트림(io.Reader)을 메모리로 읽어들이는 과정에서 I/O 오류가 발생했을 때 에러를 생성합니다.
//
// ParseHTML 함수에서 범용 io.Reader를 읽는 중 네트워크 연결 중단, 디스크 읽기 오류, 컨텍스트 취소 등의 오류가 발생한 경우입니다.
//
// 매개변수:
//   - cause: io.ReadAll()이 반환한 원본 I/O 에러
//
// 반환값: apperrors.Unavailable 타입의 에러 (재시도 가능)
func newErrReadHTMLInput(cause error) error {
	return apperrors.Wrap(cause, apperrors.Unavailable, "HTML 입력 데이터 읽기 실패: 데이터 스트림을 읽는 중 I/O 오류가 발생했습니다")
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// HTTP 요청 및 네트워크 에러 (HTTP Request & Network)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// newErrHTTPRequestFailed HTTP 응답 상태 코드(4xx, 5xx)에 따라 적절한 에러 타입을 자동 분류하여 재시도 정책을 지원하는 에러를 생성합니다.
//
// 재시도 정책: Unavailable(재시도 가능) - 5xx 서버 에러, 408 Timeout, 429 Rate Limit 등 일시적 장애 / ExecutionFailed(재시도 불필요) - 400 Bad Request, 404 Not Found 등 영구적 클라이언트 오류
//
// 매개변수:
//   - cause: HTTP 요청 실패의 원본 에러
//   - url: 요청을 보낸 대상 URL (에러 발생 위치 추적용)
//   - statusCode: HTTP 응답 상태 코드 (에러 타입 분류 기준)
//   - body: 응답 본문의 일부 (디버깅용, 빈 문자열 가능)
//
// 반환값: 상태 코드에 따라 분류된 에러 타입(Unavailable 또는 ExecutionFailed)
func newErrHTTPRequestFailed(cause error, url string, statusCode int, body string) error {
	// HTTP 상태 코드에 따라 에러 타입을 분류합니다:
	//
	// 4xx 클라이언트 에러:
	//   - 기본: ExecutionFailed (재시도 불필요)
	//   - 예외: 408 Request Timeout, 429 Too Many Requests
	//     → Unavailable (일시적일 수 있으므로 재시도 가능)
	//
	// 5xx 서버 에러:
	//   - Unavailable (서버 문제이므로 재시도 가능)
	//
	// 기타 (3xx, 1xx 등):
	//   - Unavailable (기본값)
	errType := apperrors.Unavailable
	if statusCode >= 400 && statusCode < 500 {
		if statusCode != http.StatusRequestTimeout && statusCode != http.StatusTooManyRequests {
			errType = apperrors.ExecutionFailed
		}
	}

	// 에러 메시지 생성
	msg := fmt.Sprintf("HTTP 요청 실패: %d %s (URL: %s)", statusCode, http.StatusText(statusCode), url)
	if body != "" {
		msg += fmt.Sprintf(" - 응답: %s", body)
	}

	return apperrors.Wrap(cause, errType, msg)
}

// newErrCreateHTTPRequest HTTP 요청 객체 생성 실패 시 에러를 생성합니다.
//
// 네트워크 통신 전 초기화 단계에서 발생하는 오류로, 주요 원인은 URL 형식 오류, 잘못된 HTTP 메서드, 컨텍스트 주입 실패 등입니다.
//
// 매개변수:
//   - cause: http.NewRequestWithContext()가 반환한 원본 에러
//   - url: 요청을 생성하려던 대상 URL (에러 발생 위치 추적용)
//
// 반환값: apperrors.ExecutionFailed 타입의 에러
func newErrCreateHTTPRequest(cause error, url string) error {
	return apperrors.Wrap(cause, apperrors.ExecutionFailed, fmt.Sprintf("HTTP 요청 생성 실패: 요청 객체 초기화 중 오류 발생 (URL: %s)", url))
}

// newErrNetworkError 네트워크 통신 장애(DNS 조회 실패, TCP 연결 타임아웃, 서버 거부 등) 발생 시 에러를 생성합니다.
//
// 일시적 네트워크 장애와 영구적 장애를 구분하기 위한 핵심 진단 지점입니다.
//
// 매개변수:
//   - cause: fetcher.Do()가 반환한 원본 네트워크 에러
//   - url: 요청을 보낸 대상 URL (에러 발생 위치 추적용)
//
// 반환값: apperrors.Unavailable 타입의 에러 (재시도 가능)
func newErrNetworkError(cause error, url string) error {
	return apperrors.Wrap(cause, apperrors.Unavailable, fmt.Sprintf("네트워크 오류: 연결 실패 (URL: %s)", url))
}

// newErrHTTPRequestCanceled 컨텍스트 취소 또는 타임아웃으로 인해 HTTP 요청이 중단되었을 때 에러를 생성합니다.
//
// 불필요한 네트워크 I/O를 조기 종료하여 고루틴 누수를 방지하고 시스템 부하를 관리합니다.
//
// 매개변수:
//   - cause: 컨텍스트 취소/타임아웃 원본 에러 (context.Canceled 또는 context.DeadlineExceeded)
//   - url: 요청을 보낸 대상 URL (에러 발생 위치 추적용)
//
// 반환값: apperrors.Unavailable 타입의 에러 (재시도 가능)
func newErrHTTPRequestCanceled(cause error, url string) error {
	return apperrors.Wrap(cause, apperrors.Unavailable, fmt.Sprintf("요청 중단: 컨텍스트 취소 또는 타임아웃 (URL: %s)", url))
}

// ErrContextCanceled 스크래핑 프로세스에서 컨텍스트 취소 또는 타임아웃 발생 시 사용되는 공통 에러 인스턴스입니다.
// ParseHTML 함수 등에서 컨텍스트 상태를 확인하여 이미 취소된 경우 즉시 반환되며, 불필요한 작업을 조기에 중단합니다.
var ErrContextCanceled = apperrors.New(apperrors.Unavailable, "작업 중단: 컨텍스트 취소 또는 타임아웃")

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 요청 본문 처리 에러 (Request Body Processing)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// newErrPrepareRequestBody HTTP 요청 전송을 위해 본문 데이터(io.Reader)를 메모리로 읽어들이는 과정에서 I/O 오류가 발생했을 때 에러를 생성합니다.
//
// 네트워크 전송 이전 단계로, 주요 원인은 컨텍스트 취소/타임아웃, 스트림 읽기 실패, 메모리 부족 등입니다.
//
// 매개변수:
//   - cause: io.ReadAll()이 반환한 원본 I/O 에러
//
// 반환값: apperrors.ExecutionFailed 타입의 에러
func newErrPrepareRequestBody(cause error) error {
	return apperrors.Wrap(cause, apperrors.ExecutionFailed, "요청 본문 준비 실패: 데이터 스트림을 읽는 중 오류가 발생했습니다")
}

// newErrEncodeJSONBody HTTP 요청 본문으로 전송할 데이터를 JSON 형식으로 직렬화(json.Marshal)하는 과정에서 오류가 발생했을 때 에러를 생성합니다.
//
// 네트워크 전송 이전 단계로, 주요 원인은 순환 참조, 직렬화 불가능한 타입(chan, func 등), 잘못된 구조체 태그 등 코드 레벨 버그입니다.
//
// 매개변수:
//   - cause: json.Marshal()이 반환한 원본 직렬화 에러
//
// 반환값: apperrors.Internal 타입의 에러
func newErrEncodeJSONBody(cause error) error {
	return apperrors.Wrap(cause, apperrors.Internal, "요청 본문 JSON 인코딩 실패: 데이터를 직렬화할 수 없습니다")
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 응답 본문 처리 에러 (Response Body Processing)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// newErrReadResponseBody 서버로부터 수신된 HTTP 응답 본문을 메모리로 읽어들이는 과정에서 I/O 오류가 발생했을 때 에러를 생성합니다.
//
// HTTP 응답 헤더는 정상적으로 수신되었으나 본문 데이터를 읽는 중 네트워크 연결 중단, 타임아웃, 서버 측 연결 종료 등의 오류가 발생한 경우입니다.
//
// 매개변수:
//   - cause: io.ReadAll()이 반환한 원본 I/O 에러
//
// 반환값: apperrors.Unavailable 타입의 에러 (재시도 가능)
func newErrReadResponseBody(cause error) error {
	return apperrors.Wrap(cause, apperrors.Unavailable, "응답 본문 데이터 수신 실패: 데이터 스트림을 읽는 중 I/O 오류가 발생했습니다")
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 크기 제한 초과 에러 (Size Limit Exceeded)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// newErrRequestBodySizeLimitExceeded HTTP 요청 본문의 크기가 설정된 제한을 초과하여 전송을 차단할 때 발생하는 에러를 생성합니다.
//
// DoS 공격 방어를 위해 악의적인 대용량 요청으로 인한 메모리 고갈 및 네트워크 대역폭 낭비를 사전에 차단하는 보안 메커니즘입니다.
//
// 매개변수:
//   - limit: 설정된 최대 요청 본문 크기 (바이트 단위)
//   - contentType: 요청 본문의 Content-Type (디버깅용, 빈 문자열 가능)
//
// 반환값: apperrors.InvalidInput 타입의 에러
func newErrRequestBodySizeLimitExceeded(limit int64, contentType string) error {
	msg := fmt.Sprintf("요청 본문 크기 초과: 전송 데이터가 허용 제한(%d 바이트)을 초과했습니다", limit)
	if contentType != "" {
		msg += fmt.Sprintf(" (Content-Type: %s)", contentType)
	}
	return apperrors.New(apperrors.InvalidInput, msg)
}

// newErrResponseBodySizeLimitExceeded 서버로부터 수신된 응답 본문의 크기가 허용된 최대 크기를 초과하여 파싱을 중단해야 할 때 발생하는 에러를 생성합니다.
//
// DoS 공격 방지 및 시스템 안정성을 위해 개별 요청의 메모리 사용량을 제한하는 보안 메커니즘입니다.
//
// 매개변수:
//   - limit: 설정된 최대 응답 본문 크기 (바이트 단위)
//   - url: 요청을 보낸 대상 URL (에러 발생 위치 추적용)
//   - contentType: 응답 본문의 Content-Type (디버깅용, 빈 문자열 가능)
//
// 반환값: apperrors.InvalidInput 타입의 에러
func newErrResponseBodySizeLimitExceeded(limit int64, url, contentType string) error {
	msg := fmt.Sprintf("응답 본문 크기 초과: 수신 데이터가 허용 제한(%d 바이트)을 초과했습니다 (URL: %s)", limit, url)
	if contentType != "" {
		msg += fmt.Sprintf(" (Content-Type: %s)", contentType)
	}
	return apperrors.New(apperrors.InvalidInput, msg)
}

// newErrInputDataSizeLimitExceeded 입력 데이터 스트림의 크기가 설정된 제한을 초과하여 파싱을 중단해야 할 때 발생하는 에러를 생성합니다.
//
// DoS 공격 방지 및 시스템 안정성을 위해 개별 파싱 작업의 메모리 사용량을 제한하는 보안 메커니즘입니다.
//
// 매개변수:
//   - limit: 설정된 최대 입력 데이터 크기 (바이트 단위)
//   - dataType: 입력 데이터 타입 (예: "HTML", "JSON", 디버깅용, 빈 문자열 가능)
//
// 반환값: apperrors.InvalidInput 타입의 에러
func newErrInputDataSizeLimitExceeded(limit int64, dataType string) error {
	msg := fmt.Sprintf("입력 데이터 크기 초과: 허용 제한(%d 바이트)을 초과했습니다", limit)
	if dataType != "" {
		msg += fmt.Sprintf(" (타입: %s)", dataType)
	}
	return apperrors.New(apperrors.InvalidInput, msg)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 입력 검증 에러 (Input Validation)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ErrDecodeTargetNil JSON 응답을 디코딩할 대상 변수가 nil일 때 반환되는 에러입니다.
// FetchJSON 함수 호출 시 디코딩 대상 변수 v가 nil인 경우 즉시 반환되며, json.Decoder.Decode(nil) 호출로 인한 런타임 패닉을 사전에 차단합니다.
var ErrDecodeTargetNil = apperrors.New(apperrors.Internal, "JSON 디코딩 실패: 결과를 저장할 변수가 nil입니다")

// newErrDecodeTargetInvalidType JSON 응답을 디코딩할 대상 변수의 타입이 유효하지 않을 때 발생하는 에러를 생성합니다.
//
// json.Unmarshal은 반드시 nil이 아닌 포인터를 요구하므로, 포인터가 아닌 타입이나 Typed Nil 포인터를 사전 검증하여 런타임 패닉을 방지합니다.
//
// 매개변수:
//   - v: 검증에 실패한 디코딩 대상 변수 (타입 정보 출력용)
//
// 반환값: apperrors.Internal 타입의 에러
func newErrDecodeTargetInvalidType(v any) error {
	return apperrors.New(apperrors.Internal, fmt.Sprintf("JSON 디코딩 실패: 결과를 저장할 변수는 nil이 아닌 포인터여야 합니다 (전달된 타입: %T)", v))
}

// ErrInputReaderNil HTML 파싱을 위한 입력 데이터 스트림이 제공되지 않았을 때 반환되는 에러입니다.
// ParseHTML 함수 호출 시 io.Reader 파라미터 r이 nil인 경우 입력 검증 단계에서 즉시 반환되며, API 계약 위반을 명확히 통보합니다.
var ErrInputReaderNil = apperrors.New(apperrors.Internal, "HTML 파싱 실패: 입력 데이터 스트림이 nil입니다")

// ErrInputReaderTypedNil HTML 파싱을 위한 입력 데이터 스트림이 Typed Nil 포인터일 때 반환되는 에러입니다.
// ParseHTML 함수 호출 시 io.Reader 파라미터 r이 포인터 타입이면서 nil 값을 가진 경우 입력 검증 단계에서 즉시 반환되며, 런타임 패닉을 방지합니다.
var ErrInputReaderTypedNil = apperrors.New(apperrors.Internal, "HTML 파싱 실패: 입력 데이터 스트림이 Typed Nil입니다")

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 응답 검증 에러 (Response Validation)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// newErrValidationFailed 사용자 정의 Validator 함수가 HTTP 응답 검증에 실패했을 때 발생하는 에러를 생성합니다.
//
// HTTP 프로토콜 수준의 검증(상태 코드, Content-Type 등)을 통과한 응답에 대해 비즈니스 로직 차원에서 정의한 추가 검증 조건이 충족되지 않았을 때 호출됩니다.
// 원본 에러가 apperrors.AppError 타입인 경우 해당 에러 타입을 보존하여 재시도 정책이 상위 레이어까지 전달되도록 합니다.
//
// 매개변수:
//   - cause: Validator 함수가 반환한 원본 에러 (검증 실패 이유 포함)
//   - url: 요청을 보낸 대상 URL (에러 발생 위치 추적용)
//   - preview: 응답 본문의 일부 미리보기 문자열 (디버깅용, 빈 문자열 가능)
//
// 반환값: 원본 에러를 래핑한 새로운 에러 (URL과 응답 본문 미리보기 포함)
func newErrValidationFailed(cause error, url, preview string) error {
	// 원본 에러의 타입 정보 보존
	errType := apperrors.ExecutionFailed
	if appErr, ok := cause.(*apperrors.AppError); ok {
		errType = appErr.Type()
	}

	msg := fmt.Sprintf("응답 검증 실패 (URL: %s)", url)
	if preview != "" {
		msg += fmt.Sprintf(" - Body Snippet: %s", preview)
	}

	return apperrors.Wrap(cause, errType, msg)
}
