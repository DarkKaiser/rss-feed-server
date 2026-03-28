package scraper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"

	applog "github.com/darkkaiser/notify-server/pkg/log"
	"golang.org/x/net/html/charset"
)

// FetchJSON 지정된 URL로 HTTP 요청을 보내 JSON 응답을 가져오고, 지정된 구조체로 디코딩합니다.
//
// 이 함수는 RESTful API 호출에 최적화되어 있으며, 다음과 같은 주요 기능을 제공합니다:
//   - 요청 본문 자동 처리: 구조체를 전달하면 자동으로 JSON 마샬링하여 전송
//   - 응답 검증: Content-Type 확인 및 HTML 응답 감지
//   - 메모리 무결성: maxResponseBodySize 초과 시 에러를 반환하여 불완전한 파싱 방지
//   - 자동 재시도 지원: 네트워크 오류 시 Fetcher가 요청을 재시도할 수 있도록 본문을 메모리 버퍼링
//
// 매개변수:
//   - ctx: 요청의 생명주기를 제어하는 컨텍스트 (취소, 타임아웃 등)
//   - method: HTTP 메서드 (예: "GET", "POST")
//   - rawURL: 요청할 URL
//   - body: 요청 본문 데이터 (nil 가능, GET 요청 시 일반적으로 nil)
//   - header: 추가 HTTP 헤더 (nil 가능, 예: User-Agent, Cookie 등)
//   - v: JSON 응답을 디코딩할 대상 구조체의 포인터 (반드시 nil이 아닌 포인터여야 함)
//
// 반환값:
//   - error: 네트워크 오류, JSON 파싱 오류, 또는 응답 크기 초과 시 에러 반환
func (s *scraper) FetchJSON(ctx context.Context, method, rawURL string, body any, header http.Header, v any) error {
	// 0단계: 디코딩 대상(v) 검증
	// JSON 디코딩을 위해서는 결과를 담을 '구조체의 포인터'가 필요합니다.
	// 만약 v가 nil이거나 포인터가 아니라면, 디코딩된 데이터를 저장할 수 없으므로
	// 네트워크 요청 전에 즉시 에러를 반환하여 개발자의 실수를 조기에 알립니다.
	if v == nil {
		return ErrDecodeTargetNil
	}
	if rv := reflect.ValueOf(v); rv.Kind() != reflect.Ptr || rv.IsNil() {
		return newErrDecodeTargetInvalidType(v)
	}

	// 1단계: 요청 본문(Body) 처리
	// prepareBody는 전달받은 body를 메모리 버퍼로 읽어들여 재사용 가능한 리더로 변환합니다.
	// 이를 통해 네트워크 오류 발생 시 Fetcher가 동일한 본문으로 요청을 재시도할 수 있습니다.
	// 또한 maxRequestBodySize를 초과하는 본문은 이 단계에서 거부됩니다.
	reqBody, err := s.prepareBody(ctx, body)
	if err != nil {
		return err
	}

	// 2단계: HTTP 헤더 구성
	// 요청 본문이 존재하는 경우, 올바른 처리를 위해 Content-Type 헤더를 설정합니다.
	// 사용자가 명시적으로 헤더를 제공한 경우 이를 존중하되, 필수 헤더가 누락된 경우 기본값을 적용합니다.
	if reqBody != nil {
		if header == nil {
			header = make(http.Header) // 헤더가 없는 경우 새로 생성
		} else {
			header = header.Clone() // 호출자가 전달한 원본 헤더가 변경되지 않도록 복사본을 사용
		}

		// Content-Type이 명시되지 않은 경우, JSON API 호출의 표준인 "application/json"을 기본값으로 설정합니다.
		if header.Get("Content-Type") == "" {
			header.Set("Content-Type", "application/json")
		}
	}

	// 3단계: HTTP 요청 실행을 위한 파라미터 구성
	// executeRequest 함수가 실제 네트워크 요청을 수행할 수 있도록 필요한 정보들을 requestParams 구조체에 담습니다.
	params := requestParams{
		Method:        method,
		URL:           rawURL,
		Body:          reqBody,
		Header:        header,
		DefaultAccept: "application/json", // 서버에 JSON 응답을 선호함을 알립니다.
		Validator: func(resp *http.Response, logger *applog.Entry) error {
			// 응답 검증: 상태 코드 확인(checkResponse) 외에 추가적으로
			// 응답 헤더의 Content-Type이 JSON인지, 혹은 HTML 페이지가 잘못 반환되었는지 검사합니다.
			// (REST API 요청 시 종종 발생하는 에러 페이지 반환 케이스를 감지하기 위함)
			return s.verifyJSONContentType(resp, rawURL, logger)
		},
	}

	// 4단계: HTTP 요청 실행 및 응답 수신
	// executeRequest를 통해 실제 네트워크 요청을 수행하고, 응답 본문을 메모리 버퍼(result.Body)로 읽어들입니다.
	// 이때 result.Response.Body는 이미 NopCloser로 교체된 상태이므로,
	// 이후의 Close 호출은 실질적인 네트워크 리소스 해제가 아닌, API 규약을 준수하기 위한 관례적 명시입니다.
	// (실제 네트워크 연결 해제는 executeRequest 내부에서 이미 처리되었습니다)
	result, logger, err := s.executeRequest(ctx, params)
	if err != nil {
		return err
	}
	defer result.Response.Body.Close()

	// 5단계: JSON 디코딩 및 데이터 매핑
	// 메모리에 확보된 응답 본문(result.Body)을 디코딩하여 대상 구조체(v)에 저장합니다.
	// 이 과정에서 문자열 인코딩 변환(Charset)과 JSON 문법 검사(Strict Mode)가 수행됩니다.
	return s.decodeJSONResponse(ctx, result, v, rawURL, logger)
}

// verifyJSONContentType JSON API 응답의 Content-Type 헤더를 검증합니다.
//
// 이 함수는 JSON 응답을 기대하는 API 호출에서 다음과 같은 문제를 조기에 감지합니다:
//  1. 에러 페이지 감지: API 엔드포인트가 잘못되었거나 인증 실패 시 서버가 HTML 에러 페이지를 반환하는 경우
//  2. 비표준 Content-Type 경고: API가 올바른 JSON을 반환하지만 Content-Type 헤더를 잘못 설정한 경우
//
// 검증 전략:
//   - HTML 응답: 즉시 에러 반환 (JSON 파싱이 불가능하므로 조기 실패)
//   - 비표준 Content-Type: 경고 로그만 남기고 파싱 계속 진행 (실제 많은 API가 잘못된 헤더를 사용하므로 관대하게 처리)
//   - 204 No Content: 본문이 없는 정상 응답이므로 검증 생략
//
// 매개변수:
//   - resp: 검증할 HTTP 응답 객체
//   - url: 요청을 보낸 대상 URL (에러 발생 시 어느 엔드포인트에서 문제가 생겼는지 추적하기 위한 용도)
//   - logger: 검증 과정의 특이 사항이나 비표준 헤더 등을 기록할 로거 객체
//
// 반환값:
//   - error: HTML 응답이 감지된 경우 에러 반환, 그 외에는 nil
func (s *scraper) verifyJSONContentType(resp *http.Response, url string, logger *applog.Entry) error {
	// 204 No Content 응답은 본문이 없는 정상적인 응답입니다.
	// 이 경우 Content-Type 헤더가 없거나 의미가 없으므로 검증을 건너뜁니다.
	// (예: DELETE 요청 성공 시 서버가 204를 반환하는 경우)
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	contentType := resp.Header.Get("Content-Type")

	// 검증 1단계: HTML 응답 감지 (치명적 오류)
	//
	// JSON API를 호출했는데 HTML 페이지가 반환되는 경우는 다음과 같은 심각한 문제를 의미합니다:
	//   - 잘못된 API 엔드포인트 호출 (URL 오타, 버전 불일치 등)
	//   - 인증 실패로 인한 로그인 페이지 리다이렉트
	//   - 서버 에러로 인한 에러 페이지 반환 (500 Internal Server Error 등)
	//
	// 이러한 경우 JSON 파싱이 불가능하므로, 명확한 에러 메시지와 함께 즉시 실패 처리합니다.
	if isHTMLContentType(contentType) {
		return newErrUnexpectedHTMLResponse(url, contentType)
	}

	// 검증 2단계: 비표준 Content-Type 경고 (관대한 처리)
	//
	// 실무에서는 많은 API가 올바른 JSON 데이터를 반환하면서도 Content-Type 헤더를 잘못 설정하는 경우가 있습니다:
	//   - "text/plain"으로 설정된 JSON 응답
	//   - "application/octet-stream"으로 설정된 JSON 응답
	//   - Content-Type 헤더가 아예 누락된 경우
	//
	// 이러한 경우 실제 데이터는 유효한 JSON이므로, 엄격한 검증(Strict Mode)으로 에러를 발생시키면
	// 정상적으로 동작하는 API와의 통신이 차단될 수 있습니다.
	//
	// 따라서 경고 로그만 남기고 JSON 파싱을 계속 진행하여, 실제 데이터 유효성은 디코딩 단계에서 검증합니다.
	if contentType == "" || !strings.Contains(strings.ToLower(contentType), "json") {
		logger.WithFields(applog.Fields{
			"url":            url,
			"status_code":    resp.StatusCode,
			"content_type":   contentType,
			"content_length": resp.ContentLength,
		}).Warn("[JSON 파싱 진행]: 비표준 Content-Type 헤더가 감지되었지만 데이터 유효성 확인을 위해 파싱을 계속합니다")
	}

	return nil
}

// decodeJSONResponse HTTP 응답 본문을 JSON으로 디코딩하여 지정된 타입으로 변환합니다.
//
// 매개변수:
//   - ctx: 요청의 생명주기를 제어하는 컨텍스트 (취소, 타임아웃 등)
//   - result: HTTP 요청 실행후 수신된 결과 데이터 (상태 코드, 헤더 및 메모리에 버퍼링된 본문 바이트 포함)
//   - v: JSON 응답을 디코딩할 대상 구조체의 포인터 (반드시 nil이 아닌 포인터여야 함)
//   - url: 요청을 보낸 대상 URL (에러 발생 시 어느 엔드포인트에서 문제가 생겼는지 추적하기 위한 용도)
//   - logger: 파싱 과정의 진행 상황 및 에러 상세 정보를 기록할 로거 객체
//
// 반환값:
//   - error: JSON 파싱 오류, 응답 크기 초과, 또는 데이터 무결성 오류 시 에러 반환
func (s *scraper) decodeJSONResponse(ctx context.Context, result fetchResult, v any, url string, logger *applog.Entry) error {
	// ============================================================
	// 1. 204 No Content 응답 처리
	// ============================================================
	// HTTP 204 상태 코드는 "요청은 성공했지만 반환할 본문이 없음"을 의미합니다.
	// 본문이 비어있는 것이 정상이므로 JSON 디코딩을 시도하면 EOF 에러가 발생합니다.
	// 따라서 디코딩을 건너뛰고 즉시 성공을 반환합니다.
	if result.Response.StatusCode == http.StatusNoContent {
		logger.WithField("status_code", http.StatusNoContent).
			Debug("[성공]: 204 No Content 수신으로 인한 JSON 파싱 생략")

		return nil
	}

	// ============================================================
	// 2. Content-Type 추출
	// ============================================================
	contentType := result.Response.Header.Get("Content-Type")

	// ============================================================
	// 3. 응답 본문 크기 제한 검증
	// ============================================================
	// executeRequest 함수는 메모리 보호를 위해 응답 본문을 maxResponseBodySize까지만 읽습니다.
	// 만약 실제 응답이 이 크기를 초과하면 IsTruncated 플래그가 true로 설정됩니다.
	//
	// JSON은 구조적 무결성이 필수입니다:
	//   - 데이터가 중간에 잘리면 닫는 괄호(}, ])가 누락되어 유효하지 않은 JSON이 됩니다.
	//   - HTML과 달리 부분 파싱을 지원하지 않으므로, 잘린 데이터는 전혀 사용할 수 없습니다.
	if result.IsTruncated {
		logger.WithFields(applog.Fields{
			"status_code":    result.Response.StatusCode,
			"content_type":   contentType,
			"content_length": result.Response.ContentLength,
			"body_size":      len(result.Body),
			"limit_bytes":    s.maxResponseBodySize,
			"truncated":      true,
		}).Error("[실패]: JSON 파싱 중단, 응답 본문 크기 초과(Truncated)")

		return newErrResponseBodySizeLimitExceeded(s.maxResponseBodySize, url, "application/json")
	}

	logger.Debug("[진행]: JSON 파싱 시작")

	// ============================================================
	// 4. 문자 인코딩 감지 및 UTF-8 변환
	// ============================================================
	utf8Reader, err := charset.NewReader(result.Response.Body, contentType)
	if err != nil {
		logger.WithError(err).
			WithFields(applog.Fields{
				"content_type": contentType,
				"body_preview": s.previewBody(result.Body, contentType),
			}).Warn("[경고]: 문자 인코딩 변환 실패, 인코딩 변환 없이 JSON 파싱을 계속합니다")

		// 인코딩 감지 실패 시, 이미 일부 읽혀진 result.Response.Body 대신
		// 원본 바이트(result.Body)를 사용하여 새 Reader를 생성합니다.
		// 이를 통해 데이터 소실 없이 파싱을 시도할 수 있습니다.
		utf8Reader = bytes.NewReader(result.Body)
	}

	// ============================================================
	// 5. JSON 디코딩 (스트림 방식)
	// ============================================================
	reader := &contextAwareReader{ctx: ctx, r: utf8Reader}
	decoder := json.NewDecoder(reader)
	if err = decoder.Decode(v); err != nil {
		// 컨텍스트 취소/타임아웃 에러는 래핑하지 않고 그대로 반환
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}

		// 디코딩 실패 시 디버깅을 위한 정보 수집
		// 에러 메시지와 함께 응답 본문의 일부를 로그에 포함합니다.
		logger := logger.WithError(err).
			WithFields(applog.Fields{
				"status_code":  result.Response.StatusCode,
				"content_type": contentType,
				"body_size":    len(result.Body),
				"body_preview": s.previewBody(result.Body, contentType),
				"error_type":   fmt.Sprintf("%T", err),
				"target_type":  fmt.Sprintf("%T", v),
			})

		// --------------------------------------------------------
		// JSON Syntax 에러 특별 처리
		// --------------------------------------------------------
		// [문법 오류 처리] json.SyntaxError는 에러가 발생한 정확한 바이트 위치(Offset)를 제공합니다.
		// 해당 위치 주변의 텍스트를 로그에 포함하여, 어떤 데이터 때문에 파싱이 실패했는지 즉시 식별할 수 있게 합니다.
		var offset int
		var snippet string
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			// 에러 발생 위치 주변의 문맥 데이터 추출 (전후 50바이트)
			//
			// [중요] syntaxErr.Offset은 "디코딩된 UTF-8 스트림" 기준의 바이트 위치입니다.
			// 만약 원본 데이터가 EUC-KR 등 다른 인코딩이었다면, result.Body의 바이트 위치와 다를 수 있습니다.
			// 따라서 정확한 위치를 찾기 위해 원본을 동일한 방식으로 다시 변환하여 위치를 찾습니다.
			const contextBytes = 50
			offset = int(syntaxErr.Offset)
			contextStart := max(int64(offset-contextBytes), 0)
			snippetLen := int64(contextBytes * 2)

			// [스니펫 추출] 올바른 인코딩(UTF-8)으로 변환된 데이터에서 문맥 데이터를 추출합니다.
			if r, err := charset.NewReader(bytes.NewReader(result.Body), contentType); err == nil {
				// [위치 이동] 스니펫 추출 시작 지점까지 데이터를 읽어서 건너뜁니다.
				// (charset.Reader는 io.Seeker를 구현하지 않을 수 있으므로 io.CopyN으로 대체)
				if contextStart > 0 {
					_, _ = io.CopyN(io.Discard, r, contextStart)
				}

				// [데이터 추출] 설정된 길이(snippetLen)만큼 에러 주변 문맥 데이터를 읽어 들입니다.
				snippetBuf := make([]byte, snippetLen)
				n, _ := io.ReadFull(r, snippetBuf)
				snippet = string(snippetBuf[:n])
			} else {
				// [차선책] 인코딩 변환 실패 시, 원본 바이트에서 에러 주변 데이터를 직접 추출합니다.
				// 인코딩 차이로 인해 정확한 문자의 위치는 다를 수 있으나, 대략적인 문맥 파악에는 충분합니다.
				fallbackStart := min(max(offset-contextBytes, 0), len(result.Body))
				fallbackEnd := min(max(offset+contextBytes, 0), len(result.Body))

				// 바이트 슬라이스를 문자열로 변환
				snippet = string(result.Body[fallbackStart:fallbackEnd])
			}

			logger = logger.WithFields(applog.Fields{
				"syntax_error_offset":  offset,
				"syntax_error_context": snippet,
			})
		}

		logger.Error("[실패]: JSON 데이터 변환 실패, 유효하지 않은 형식")

		return newErrJSONParseFailed(err, url, offset, snippet)
	}

	// ============================================================
	// 6. Strict Mode: JSON 데이터 외에 불필요한 데이터가 더 존재하는지 확인
	// ============================================================
	// JSON 파싱이 성공적으로 완료된 후, 스트림에 추가 데이터가 남아있는지 확인합니다.
	//
	// 왜 이 검증이 필요한가?
	//   - 표준 json.Decoder는 첫 번째 완전한 JSON 객체를 파싱하면 성공으로 간주합니다.
	//   - 하지만 서버 버그로 JSON 뒤에 불필요한 데이터가 붙어있을 수 있습니다.
	//
	// 실제 발생 가능한 문제 사례:
	//   1. JSON 뒤에 HTML 푸터가 붙는 경우:
	//      {"status": "ok"}<!-- Powered by XYZ -->
	//
	//   2. 디버그 메시지가 JSON 뒤에 출력되는 경우:
	//      {"data": [...]}DEBUG: Query took 123ms
	//
	//   3. 여러 JSON 객체가 연속으로 전송되는 경우 (JSON Lines 형식이 아닌데):
	//      {"id": 1}{"id": 2}
	//
	// 이러한 응답은 데이터 무결성 문제를 나타내므로 명시적으로 에러 처리합니다.
	if token, err := decoder.Token(); err != io.EOF {
		// 컨텍스트가 취소되었거나 타임아웃된 경우, 이를 "불필요한 데이터"로 간주하지 않고 올바른 에러를 반환해야 합니다.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}

		// 에러 발생 위치 주변의 문맥 데이터 추출 (전후 30바이트)
		offset := decoder.InputOffset()
		contextStart := min(max(int(offset)-30, 0), len(result.Body))
		contextEnd := min(max(int(offset)+30, 0), len(result.Body))

		// 바이트 슬라이스를 문자열로 변환 (멀티바이트 문자가 깨질 수 있으나 디버깅용으로 충분함)
		snippet := string(result.Body[contextStart:contextEnd])

		logger.WithFields(applog.Fields{
			"offset":           offset,
			"unexpected_token": token,
			"token_type":       fmt.Sprintf("%T", token),
			"context_snippet":  snippet,
		}).Error("[실패]: JSON 데이터 뒤에 불필요한 잔여 데이터가 감지되었습니다")

		return newErrJSONUnexpectedToken(url)
	}

	logger.WithFields(applog.Fields{
		"status_code":  result.Response.StatusCode,
		"content_type": contentType,
		"body_size":    len(result.Body),
	}).Debug("[성공]: JSON 파싱 완료")

	return nil
}
