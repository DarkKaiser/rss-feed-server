package scraper

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	applog "github.com/darkkaiser/notify-server/pkg/log"
)

// requestParams HTTP 요청의 전체 처리 과정(생성, 전송, 검증, 본문 읽기)에 필요한 파라미터들을 묶은 구조체입니다.
type requestParams struct {
	// Method HTTP 요청 메서드입니다. (예: "GET", "POST")
	Method string

	// URL 요청을 보낼 대상 URL입니다.
	URL string

	// Body 요청 본문 데이터입니다.
	// prepareBody를 통해 이미 전송 가능한 형태(io.Reader)로 변환된 상태여야 합니다.
	// GET 요청 등 본문이 없는 경우 nil일 수 있습니다.
	Body io.Reader

	// Header HTTP 요청 헤더입니다.
	// 호출자가 제공한 헤더를 Clone하여 사용하며, 필요시 Content-Type이나 Accept 등의 헤더가 추가될 수 있습니다.
	Header http.Header

	// DefaultAccept Accept 헤더가 설정되지 않았을 때 사용할 기본값입니다.
	// 예: JSON 요청의 경우 "application/json", HTML 요청의 경우 브라우저 호환 값
	DefaultAccept string

	// Validator 응답을 검증하는 함수입니다.
	// 요청 유형(HTML/JSON)에 특화된 추가 검증을 수행할 때 사용됩니다.
	Validator func(*http.Response, *applog.Entry) error
}

// fetchResult HTTP 요청 실행 후의 결과(응답 객체, 본문 데이터, Truncation 플래그)를 담는 내부 구조체입니다.
type fetchResult struct {
	// Response HTTP 응답 객체입니다.
	//
	// 응답 본문(Body)은 이미 메모리로 읽힌 상태이며, bytes.NewReader로 교체되어 있습니다.
	// 따라서 호출자는 이 Body를 여러 번 읽을 수 있으며, 네트워크 연결은 이미 해제된 상태입니다.
	// (주의) Response.Body 자체는 Seek을 지원하지 않으므로, 탐색이 필요한 경우 fetchResult.Body를 사용하십시오.
	Response *http.Response

	// Body 응답 본문 데이터의 메모리 복사본입니다.
	//
	// maxResponseBodySize를 초과하는 경우 자동으로 잘린 상태일 수 있으며, 이 경우 IsTruncated 플래그가 true로 설정됩니다.
	// 호출자는 이 바이트 슬라이스를 직접 사용하여 파싱(HTML/JSON)을 수행합니다.
	Body []byte

	// IsTruncated 응답 본문이 크기 제한으로 인해 잘렸는지 여부를 나타냅니다.
	//
	// true인 경우:
	//   - 원본 응답 본문의 크기가 maxResponseBodySize를 초과했음
	//   - Body 필드에는 maxResponseBodySize만큼만 저장됨
	//   - 호출자는 이를 에러로 처리하거나 경고 로그를 남길 수 있음
	// false인 경우:
	//   - Body 필드에 전체 응답 본문이 저장됨
	IsTruncated bool
}

// executeRequest HTTP 요청의 전체 수명주기를 관리하는 핵심 함수입니다.
//
// 이 함수는 FetchHTML과 FetchJSON의 공통 로직으로, HTTP 요청 생성부터 응답 본문 읽기까지 전체 과정을 3단계로 나누어 처리합니다:
//  1. 요청 생성 및 전송 (createAndSendRequest)
//  2. 응답 검증 (validateResponse)
//  3. 응답 본문 읽기 및 버퍼링 (readResponseBodyWithLimit)
//
// 매개변수:
//   - ctx: 요청의 생명주기를 제어하는 컨텍스트 (취소, 타임아웃 등)
//   - params: HTTP 요청의 전체 처리 과정에 필요한 파라미터 (Method, URL, Body, Header, DefaultAccept, Validator)
//
// 반환값:
//   - fetchResult: HTTP 요청 실행 후의 결과 (응답 객체, 본문 데이터, Truncation 플래그)
//   - *applog.Entry: 로그 객체 (호출자가 추가 로깅에 사용)
//   - error: 요청 생성, 전송, 검증, 본문 읽기 중 발생한 에러
func (s *scraper) executeRequest(ctx context.Context, params requestParams) (result fetchResult, logger *applog.Entry, err error) {
	// 요청 전체의 수행 시간을 측정하기 위해 시작 시간을 기록합니다.
	start := time.Now()

	// 로거 설정
	logger = applog.WithComponent(component).
		WithContext(ctx).
		WithFields(applog.Fields{
			"url":    params.URL,
			"method": params.Method,
		})

	// 함수 종료 시 (성공/실패 무관하게) 수행 시간을 로그에 추가합니다.
	defer func() {
		logger = logger.WithField("duration_ms", time.Since(start).Milliseconds())
	}()

	// [단계 1] HTTP 요청 생성 및 전송
	httpResp, err := s.createAndSendRequest(ctx, params)
	if err != nil {
		logger.WithError(err).
			WithField("duration_ms", time.Since(start).Milliseconds()).
			Error("[실패]: HTTP 요청 전송 실패")

		return fetchResult{}, logger, err
	}
	// 응답 본문 닫기 예약: validateResponse 등 이후 로직에서 패닉이 발생하더라도 리소스가 해제되도록 보장합니다.
	//
	// defer로 Body를 닫는 이유:
	//   - 정상 흐름과 에러 흐름 모두에서 리소스가 해제됨을 보장
	//   - 이후 단계에서 본문을 메모리로 읽어들일 것이므로, 함수 종료 시 네트워크 연결 해제를 보장
	//
	// [중요] defer 실행 시점의 Body 객체(원본 네트워크 스트림)를 명시적으로 캡처하여 닫습니다.
	// 이후 로직에서 httpResp.Body가 메모리 버퍼(NopCloser)로 교체되더라도,
	// 여기서 캡처된 원본 Body가 닫히므로 리소스 누수가 발생하지 않습니다.
	originalBody := httpResp.Body
	defer originalBody.Close()

	// [단계 2] HTTP 응답 검증
	if err := s.validateResponse(httpResp, params, logger); err != nil {
		// 참고: validateResponse 함수 내부에서 에러 발생 시 연결 재사용을 위해 응답 본문을 일부 드레인(Drain)합니다.
		// 하지만, 실제 리소스 해제(Close)는 상단에 예약된 defer를 통해 함수 종료 시점에 실행됩니다.
		// 따라서 호출자인 이 시점에서는 별도의 드레인이나 Close 처리가 필요하지 않습니다.

		logger.WithError(err).
			WithField("duration_ms", time.Since(start).Milliseconds()).
			Error("[실패]: HTTP 응답 유효성 검증 실패")

		return fetchResult{}, logger, err
	}

	// [단계 3] 응답 본문 읽기 및 버퍼링
	//
	// readResponseBodyWithLimit를 호출하여:
	//   - 응답 본문을 메모리로 읽어들임
	//   - maxResponseBodySize를 초과하는 경우 자동으로 잘라냄 (Truncation)
	bodyBytes, isTruncated, err := s.readResponseBodyWithLimit(ctx, httpResp)
	if err != nil {
		logger.WithError(err).
			WithField("duration_ms", time.Since(start).Milliseconds()).
			Error("[실패]: 응답 본문 읽기 실패")

		return fetchResult{}, logger, newErrReadResponseBody(err)
	}

	// 응답 본문이 크기 제한을 초과하여 잘렸을 경우 경고 로그를 남깁니다.
	// 이는 에러가 아니므로 호출자(FetchHTML, FetchJSON)가 isTruncated 플래그를 확인하여 처리합니다.
	if isTruncated {
		logger.WithField("max_body_size", s.maxResponseBodySize).
			Warn("[경고]: 응답 본문 크기 제한 초과")
	}

	// 메모리 버퍼로 Body 교체
	//
	// 원본 Body(네트워크 스트림)를 메모리 버퍼(bytes.Reader)로 교체합니다.
	// 이를 통해:
	//   - 응답 데이터를 메모리에 확보하여 네트워크 리소스(연결)를 즉시 해제
	//   - 이후 파싱 단계에서 네트워크 상태와 무관하게 안정적인 데이터 접근 보장
	//   - (참고) io.NopCloser로 감싸여 있어 Response.Body를 통한 직접 Seek은 불가능하지만,
	//     필요 시 fetchResult.Body 바이트 슬라이스를 통해 언제든 재읽기가 가능함
	//
	// io.NopCloser로 래핑하는 이유:
	//   - http.Response.Body 필드는 io.ReadCloser 인터페이스를 요구함
	//   - bytes.Reader는 Close 메서드가 없으므로 NopCloser로 감싸서 규격 준수
	httpResp.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// 최종 결과 객체 생성
	result = fetchResult{
		Response:    httpResp,
		Body:        bodyBytes,
		IsTruncated: isTruncated,
	}

	return result, logger, nil
}
