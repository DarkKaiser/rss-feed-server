package scraper

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"github.com/PuerkitoBio/goquery"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"golang.org/x/net/html/charset"
)

// FetchHTML 지정된 URL로 HTTP 요청을 보내 HTML 문서를 가져오고, 파싱된 goquery.Document를 반환합니다.
//
// 매개변수:
//   - ctx: 요청의 생명주기를 제어하는 컨텍스트 (취소, 타임아웃 등)
//   - method: HTTP 메서드 (예: "GET", "POST")
//   - rawURL: 요청할 URL
//   - body: 요청 본문 데이터 (nil 가능, GET 요청 시 일반적으로 nil)
//   - header: 추가 HTTP 헤더 (nil 가능, 예: User-Agent, Cookie 등)
//
// 반환값:
//   - *goquery.Document: 파싱된 HTML 문서 객체
//   - error: 네트워크 오류, 파싱 오류, 또는 응답 크기 초과 시 에러 반환
func (s *scraper) FetchHTML(ctx context.Context, method, rawURL string, body io.Reader, header http.Header) (*goquery.Document, error) {
	// 1단계: 요청 본문(Body) 처리
	// prepareBody는 전달받은 body를 메모리 버퍼로 읽어들여 재사용 가능한 리더로 변환합니다.
	// 이를 통해 네트워크 오류 발생 시 Fetcher가 동일한 본문으로 요청을 재시도할 수 있습니다.
	// 또한 maxRequestBodySize를 초과하는 본문은 이 단계에서 거부됩니다.
	reqBody, err := s.prepareBody(ctx, body)
	if err != nil {
		return nil, err
	}

	// 2단계: HTTP 헤더 구성
	// 요청 본문이 존재하는 경우, 호출자가 전달한 원본 헤더가 변경되지 않도록 복사본을 사용합니다.
	if reqBody != nil && header != nil {
		header = header.Clone()
	}

	// 3단계: HTTP 요청 실행을 위한 파라미터 구성
	// executeRequest 함수가 실제 네트워크 요청을 수행할 수 있도록 필요한 정보들을 requestParams 구조체에 담습니다.
	params := requestParams{
		Method:        method,
		URL:           rawURL,
		Body:          reqBody,
		Header:        header,
		DefaultAccept: "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		Validator: func(resp *http.Response, logger *applog.Entry) error {
			// 응답 검증: 상태 코드 확인(checkResponse) 외에 추가적으로
			// 응답 헤더의 Content-Type이 HTML 형식인지 확인합니다.
			// 비표준 Content-Type(예: text/plain 등)이라도 내용이 HTML일 수 있으므로,
			// 엄격하게 차단하지 않고 경고 로그만 남긴 후 파싱을 계속 진행합니다.
			return s.verifyHTMLContentType(resp, rawURL, logger)
		},
	}

	// 4단계: HTTP 요청 실행 및 응답 수신
	// executeRequest를 통해 실제 네트워크 요청을 수행하고, 응답 본문을 메모리 버퍼(result.Body)로 읽어들입니다.
	// 이때 result.Response.Body는 이미 NopCloser로 교체된 상태이므로,
	// 이후의 Close 호출은 실질적인 네트워크 리소스 해제가 아닌, API 규약을 준수하기 위한 관례적 명시입니다.
	// (실제 네트워크 연결 해제는 executeRequest 내부에서 이미 처리되었습니다)
	result, logger, err := s.executeRequest(ctx, params)
	if err != nil {
		return nil, err
	}
	defer result.Response.Body.Close()

	// 5단계: 응답 크기 확인
	// executeRequest는 maxResponseBodySize를 초과하는 응답을 자동으로 잘라냅니다.
	// HTML 파싱의 무결성을 보장하기 위해, 잘린(Truncated) 응답은 에러로 처리합니다.
	// (참고: 이미지나 비디오 파일 등은 Stream 처리가 가능하거나 메타데이터만 필요할 수 있어 Truncation을 허용하기도 하지만,
	//  HTML/JSON 구조 데이터는 완결성이 필수적이므로 엄격하게 차단합니다)
	if result.IsTruncated {
		logger.WithFields(applog.Fields{
			"truncated":   true,
			"status_code": result.Response.StatusCode,
			"body_size":   len(result.Body),
		}).Error("[실패]: HTTP 요청 완료 후 파싱 중단, 응답 본문 크기 초과(Truncated)")

		return nil, newErrResponseBodySizeLimitExceeded(s.maxResponseBodySize, rawURL, "text/html")
	}

	logger.WithFields(applog.Fields{
		"status_code": result.Response.StatusCode,
		"body_size":   len(result.Body),
	}).Debug("[성공]: HTML 요청 완료, 파싱 단계 진입")

	// 6단계: Content-Type 추출 (8단계의 HTML 파싱 시 Charset 변환을 위한 힌트)
	contentType := result.Response.Header.Get("Content-Type")

	// 7단계: 문서 URL 결정 (상대 경로 해석용)
	// HTML 내의 상대 경로(예: <a href="/path">)를 절대 경로로 변환하기 위한 기준 URL을 설정합니다.
	// 리다이렉션 후의 최종 URL(Response.Request.URL)을 우선 사용하며,
	// 만약 Request 객체가 없는 경우(Mocking 등)를 대비해 초기 요청 URL을 Fallback으로 사용합니다.
	var baseURL *url.URL
	if result.Response.Request != nil {
		baseURL = result.Response.Request.URL
	} else {
		if parsedURL, err := url.Parse(rawURL); err == nil {
			baseURL = parsedURL
		} else {
			logger.WithError(err).
				Warn("[주의]: Base URL 결정 실패, Fallback 파싱 에러")
		}
	}

	// 8단계: HTML 파싱 실행
	// parseHTML을 통해 메모리에 버퍼링된 응답 본문을 읽어 goquery.Document를 생성합니다.
	//  - result.Response.Body: executeRequest에서 이미 메모리로 읽어들인 응답 본문 (NopCloser로 래핑된 bytes.Reader)
	//  - contextAwareReader 래핑: 파싱 도중 Context가 취소되면 작업을 즉시 중단합니다.
	//  - baseURL: HTML 내의 상대 경로(href="/...")를 절대 경로로 변환하기 위한 기준 URL입니다.
	//  - contentType: 응답 헤더의 Charset 정보를 기반으로 인코딩을 자동 변환(예: EUC-KR → UTF-8)합니다.
	doc, err := s.parseHTML(ctx, &contextAwareReader{ctx: ctx, r: result.Response.Body}, baseURL, contentType, logger)
	if err != nil {
		// 컨텍스트 취소/타임아웃 에러는 래핑하지 않고 그대로 반환
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}

		// 파싱 실패 시 디버깅을 위해 응답 본문의 일부를 로그에 포함합니다.
		logger.WithError(err).
			WithFields(applog.Fields{
				"status_code":  result.Response.StatusCode,
				"content_type": contentType,
				"body_size":    len(result.Body),
				"body_preview": s.previewBody(result.Body, contentType),
			}).Error("[실패]: HTML 파싱 에러, goquery Document 생성 실패")

		return nil, newErrHTMLParseFailed(err, rawURL)
	}

	logger.WithFields(applog.Fields{
		"status_code":  result.Response.StatusCode,
		"content_type": contentType,
		"body_size":    len(result.Body),
	}).Debug("[성공]: HTML 요청 및 파싱 완료")

	return doc, nil
}

// FetchHTMLDocument 지정된 URL로 GET 요청을 보내 HTML 문서를 가져오는 헬퍼 함수입니다.
//
// 이 함수는 FetchHTML을 내부적으로 호출하며, HTTP 메서드를 "GET"으로 고정하고 요청 본문(Body)을 nil로 설정합니다.
// 단순히 웹 페이지를 읽어오는 가장 일반적인 사용 사례를 위한 간편한 인터페이스를 제공합니다.
//
// 매개변수:
//   - ctx: 요청의 생명주기를 제어하는 컨텍스트 (취소, 타임아웃 등)
//   - rawURL: 요청할 URL
//   - header: 추가 HTTP 헤더 (nil 가능, 예: User-Agent, Cookie 등)
//
// 반환값:
//   - *goquery.Document: 파싱된 HTML 문서 객체
//   - error: 네트워크 오류, 파싱 오류, 또는 응답 크기 초과 시 에러 반환
func (s *scraper) FetchHTMLDocument(ctx context.Context, rawURL string, header http.Header) (*goquery.Document, error) {
	return s.FetchHTML(ctx, http.MethodGet, rawURL, nil, header)
}

// ParseHTML io.Reader로부터 HTML 문서를 파싱하여 goquery.Document를 반환합니다.
//
// 이 함수는 이미 메모리에 로드된 HTML 데이터(문자열, 파일 등)를 처리할 때 사용됩니다.
// FetchHTML과 달리 HTTP 요청을 수행하지 않으며, 제공된 Reader에서 직접 데이터를 읽어 파싱합니다.
//
// 매개변수:
//   - ctx: 컨텍스트 (로깅 연동 및 취소 신호 감지)
//   - r: HTML 데이터를 읽을 io.Reader (nil 불가)
//   - rawURL: 문서의 기준 URL (상대 경로 링크를 절대 경로로 변환할 때 사용, 빈 문자열 가능)
//   - contentType: HTTP 응답의 Content-Type 헤더 (인코딩 감지를 위한 힌트로 사용됨, 빈 문자열 가능)
//
// 반환값:
//   - *goquery.Document: 파싱된 HTML 문서 객체
//   - error: 입출력 오류, 컨텍스트 취소 또는 파싱 실패 시 에러 반환
//
// 보안 고려사항:
//   - maxResponseBodySize를 초과하는 입력은 에러를 반환합니다. (DoS 방지 및 데이터 무결성 보장)
//   - 컨텍스트 취소 시 즉시 중단됩니다.
func (s *scraper) ParseHTML(ctx context.Context, r io.Reader, rawURL string, contentType string) (*goquery.Document, error) {
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// [1단계] 입력 검증
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	if r == nil {
		return nil, ErrInputReaderNil
	}

	rv := reflect.ValueOf(r)
	if rv.Kind() == reflect.Ptr && rv.IsNil() {
		return nil, ErrInputReaderTypedNil
	}

	// 컨텍스트가 이미 취소되었는지 확인합니다.
	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// [2단계] 로거 설정
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	logger := applog.WithComponent(component).
		WithContext(ctx).
		WithFields(applog.Fields{
			"url":          rawURL,
			"content_type": contentType,
			"reader_type":  fmt.Sprintf("%T", r),
		})

	logger.Debug("[진행]: 입력 데이터 검증 완료, 파싱 시작")

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// [3단계] Base URL 파싱 (상대 경로 링크 해석용)
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	var baseURL *url.URL
	if rawURL != "" {
		if parsedURL, err := url.Parse(rawURL); err == nil {
			baseURL = parsedURL
		} else {
			logger.WithError(err).
				Warn("[경고]: Base URL 설정 실패, URL 파싱 에러")
		}
	}

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// [4단계] 보안: 읽기 크기 제한 및 검증
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 서버 자원 보호 및 OOM 방지를 위해 입력 데이터 크기를 제한하고 초과 시 중단합니다.

	// maxResponseBodySize + 1만큼 읽는 이유:
	//   - maxResponseBodySize 이하인 경우: 전체 데이터를 읽음
	//   - maxResponseBodySize를 초과하는 경우: maxResponseBodySize+1 바이트를 읽어서 초과 여부를 감지
	//
	// 이를 통해 메모리 고갈 공격(DoS)을 방지하면서도 정확한 Truncation 감지가 가능합니다.
	limitReader := io.LimitReader(r, s.maxResponseBodySize+1)

	// 컨텍스트 취소 감지를 위한 Reader 래핑
	reader := &contextAwareReader{ctx: ctx, r: limitReader}

	// 제한된 범위 내에서 데이터를 메모리로 읽어들입니다.
	data, err := io.ReadAll(reader)
	if err != nil {
		// 컨텍스트 취소/타임아웃 에러는 래핑하지 않고 그대로 반환
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}

		logger.WithError(err).
			WithFields(applog.Fields{
				"limit_bytes": s.maxResponseBodySize,
			}).Error("[실패]: HTML 파싱 중단, 입력 데이터 읽기 실패")

		return nil, newErrReadHTMLInput(err)
	}

	// 입력 데이터의 크기 초과 여부 최종 검증
	// LimitReader가 maxResponseBodySize+1 만큼 읽었으므로, 실제로 maxResponseBodySize를 초과했는지 확인합니다.
	if int64(len(data)) > s.maxResponseBodySize {
		logger.WithFields(applog.Fields{
			"limit_bytes": s.maxResponseBodySize,
			"read_bytes":  len(data),
		}).Error("[실패]: HTML 파싱 중단, 입력 데이터 크기 초과")

		return nil, newErrInputDataSizeLimitExceeded(s.maxResponseBodySize, "HTML")
	}

	// 이미 메모리에 로드된 데이터를 파싱 로직에서 재사용하기 위해 bytes.Reader로 래핑합니다.
	parsingReader := bytes.NewReader(data)

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// [5단계] HTML 파싱
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// contextAwareReader로 래핑하여 파싱 중 컨텍스트 취소를 감지할 수 있도록 합니다.
	doc, err := s.parseHTML(ctx, &contextAwareReader{ctx: ctx, r: parsingReader}, baseURL, contentType, logger)
	if err != nil {
		// 컨텍스트 취소/타임아웃 에러는 래핑하지 않고 그대로 반환
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}

		logger.WithError(err).
			WithField("has_base_url", baseURL != nil).
			Error("[실패]: HTML 파싱 중단, DOM 객체 생성 실패")

		return nil, newErrHTMLParseFailed(err, rawURL)
	}

	logger.WithFields(applog.Fields{
		"title":        strings.TrimSpace(doc.Find("title").Text()),
		"node_count":   doc.Find("*").Length(),
		"has_base_url": baseURL != nil,
	}).Debug("[성공]: HTML 파싱 완료, DOM 트리 구성됨")

	return doc, nil
}

// verifyHTMLContentType HTML 응답의 Content-Type 헤더를 검증합니다.
//
// 이 함수는 FetchHTML 요청 시 응답 검증 단계에서 호출되며, 다음과 같은 역할을 수행합니다:
//  1. 예상치 못한 응답 타입 감지: JSON, 이미지, PDF 등 HTML이 아닌 응답을 조기에 발견
//  2. 비표준 Content-Type 경고: 실제로는 HTML이지만 잘못된 헤더를 사용하는 서버 감지
//
// 검증 전략 (JSON 검증과의 차이점):
//   - JSON 검증: HTML 응답을 발견하면 즉시 에러 반환 (파싱 불가능)
//   - HTML 검증: 비표준 Content-Type이어도 경고만 남기고 파싱 시도 (실용적 접근)
//
// 관대한 처리를 하는 이유:
//
//	웹 스크래핑 환경에서는 다음과 같은 상황이 매우 흔하게 발생합니다:
//	  - Content-Type 헤더가 아예 누락된 HTML 페이지
//	  - "text/plain"으로 설정되었지만 실제로는 HTML인 응답
//	  - 오래된 웹 서버가 "application/octet-stream"으로 HTML을 반환
//	  - 동적 페이지 생성 시 헤더 설정이 누락된 경우
//
//	이러한 경우 실제 내용은 유효한 HTML이므로, 엄격한 검증으로 차단하면 정상적으로 스크래핑 가능한 페이지를 놓치게 됩니다.
//	따라서 경고 로그만 남기고 파싱을 시도하여, 실제 HTML 유효성은 파싱 단계에서 검증합니다.
//
// 매개변수:
//   - resp: 검증할 HTTP 응답 객체
//   - url: 요청을 보낸 대상 URL (에러 발생 시 어느 엔드포인트에서 문제가 생겼는지 추적하기 위한 용도)
//   - logger: 검증 과정의 특이 사항이나 비표준 헤더 등을 기록할 로거 객체
//
// 반환값:
//   - error: 현재는 항상 nil을 반환 (비표준 Content-Type은 경고 로그로만 처리)
//
// 특수 케이스:
//   - 204 No Content: 본문이 없는 정상 응답이므로 검증 생략
func (s *scraper) verifyHTMLContentType(resp *http.Response, url string, logger *applog.Entry) error {
	// 204 No Content 응답은 본문이 없는 정상적인 응답입니다.
	// 이 경우 Content-Type 헤더가 없거나 의미가 없으므로 검증을 건너뜁니다.
	// (예: DELETE 요청 성공 시 서버가 204를 반환하는 경우)
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	contentType := resp.Header.Get("Content-Type")

	// 비표준 Content-Type 감지 (관대한 처리)
	//
	// HTML 스크래핑에서는 Content-Type이 정확하지 않아도 실제 내용이 HTML이면 파싱 가능합니다.
	//
	// 웹에는 다음과 같은 비표준 케이스가 매우 많습니다:
	//   - 헤더 누락: Content-Type이 아예 설정되지 않은 HTML 페이지
	//   - 잘못된 설정: "text/plain", "application/octet-stream" 등으로 설정된 HTML
	//   - 오래된 서버: 표준을 따르지 않는 레거시 웹 서버
	//
	// 이러한 경우 실제 데이터는 유효한 HTML이므로, 엄격한 검증(Strict Mode)으로 에러를 발생시키면
	// 정상적으로 스크래핑 가능한 많은 웹 페이지를 놓치게 됩니다.
	//
	// 따라서 경고 로그만 남기고 HTML 파싱을 계속 진행하여, 실제 HTML 유효성은 goquery 파싱 단계에서 검증합니다.
	if !isHTMLContentType(contentType) {
		logger.WithFields(applog.Fields{
			"url":            url,
			"status_code":    resp.StatusCode,
			"content_type":   contentType,
			"content_length": resp.ContentLength,
		}).Warn("[HTML 파싱 진행]: 비표준 Content-Type 헤더가 감지되었지만 데이터 유효성 확인을 위해 파싱을 계속합니다")
	}

	return nil
}

// parseHTML HTML 데이터를 파싱하여 goquery.Document 객체를 생성하는 내부 공통 메서드입니다.
//
// 이 함수는 다양한 문자 인코딩(EUC-KR, UTF-8 등)으로 작성된 HTML 문서를 안전하게 처리하기 위해
// 인코딩 자동 감지 및 UTF-8 변환 로직을 포함하고 있습니다.
//
// 매개변수:
//   - _: 컨텍스트 (현재 사용되지 않음)
//   - r: HTML 데이터를 읽을 io.Reader
//   - baseURL: 파싱할 HTML 페이지의 원본 URL (상대 경로 링크를 절대 경로로 변환할 때 사용, nil일 경우 변환 건너뜀)
//   - contentType: HTTP 응답의 Content-Type 헤더 (인코딩 감지를 위한 힌트로 사용됨, 예: "text/html; charset=euc-kr")
//   - logger: 파싱 및 인코딩 변환 과정의 진행 상황을 기록할 로거 객체
//
// 반환값:
//   - *goquery.Document: 파싱된 HTML 문서 객체
//   - error: 입출력 오류 또는 파싱 실패 시 에러 반환
func (s *scraper) parseHTML(_ context.Context, r io.Reader, baseURL *url.URL, contentType string, logger *applog.Entry) (*goquery.Document, error) {
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// [1단계] bufio.Reader로 래핑하여 Peek 기능 확보
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// bufio.Reader는 내부적으로 4KB 버퍼를 유지하며, Peek 메서드를 통해 버퍼의 데이터를
	// 소비하지 않고 미리 읽을 수 있습니다.
	// 이를 통해 인코딩 감지를 위한 데이터 샘플링과 실제 파싱을 위한 전체 데이터 읽기를
	// 동일한 Reader에서 순차적으로 수행할 수 있습니다.
	br := bufio.NewReader(r)

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// [2단계] 인코딩 감지를 위한 데이터 샘플링
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// HTML 문서의 인코딩 정보는 보통 문서 앞부분에 위치하므로(Content-Type 헤더 또는 <meta> 태그),
	// 1KB 정도만 읽어도 충분히 감지할 수 있습니다.
	// Peek은 에러가 발생해도(예: EOF로 1KB 미만만 읽힌 경우) 읽은 만큼의 데이터를 반환하므로
	// 작은 HTML 문서도 안전하게 처리할 수 있습니다.
	peekBytes, _ := br.Peek(1024)

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// [3단계] 문자 인코딩 감지
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// charset.DetermineEncoding은 다음 순서로 인코딩을 감지합니다:
	//   1. 바이트 순서 표시(BOM) 감지
	//   2. Content-Type 헤더의 charset 파라미터
	//   3. HTML 문서 내 <meta> 태그 또는 XML 선언의 encoding 속성
	//
	// 반환값:
	//   - enc: 감지된 인코딩의 encoding.Encoding 객체 (디코더 생성에 사용)
	//   - name: 인코딩 이름 (예: "euc-kr", "utf-8")
	//   - 에러는 무시 (감지 실패 시에도 기본 인코딩을 반환하므로 항상 처리 가능)
	enc, name, _ := charset.DetermineEncoding(peekBytes, contentType)

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// [4단계] UTF-8 변환 리더 생성
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// goquery는 UTF-8 인코딩만 지원하므로, 감지된 인코딩을 UTF-8로 변환하는 리더(transform.Reader)를 생성합니다.
	var utf8Reader io.Reader
	if enc != nil && name != "" {
		// 인코딩 감지 성공: 디코더를 통해 UTF-8로 변환
		// enc.NewDecoder().Reader(br)는 br에서 읽은 바이트를 감지된 인코딩으로 해석하여 UTF-8로 변환한 스트림을 제공합니다.
		utf8Reader = enc.NewDecoder().Reader(br)
	} else {
		// DetermineEncoding은 감지에 실패하더라도 보통 기본 인코딩(windows-1252 등)을 반환합니다.
		// 그러므로, enc가 nil이 아니라면 해당 인코딩을 사용하는 것이 안전합니다.
		if enc != nil {
			// 기본 인코딩 사용 (보통 windows-1252)
			utf8Reader = enc.NewDecoder().Reader(br)
		} else {
			// 정말 인코딩을 알 수 없는 경우라면, UTF-8로 가정하고 원본 데이터를 그대로 사용합니다.
			utf8Reader = br

			// 만약 실제로 다른 인코딩이었다면 파싱은 성공하지만 텍스트가 깨질 수 있으므로, 경고 로그를 남겨 운영자가 문제를 인지할 수 있도록 합니다.
			logger.WithFields(applog.Fields{
				"content_type":      contentType,
				"detected_encoding": name,
			}).Warn("[경고]: 인코딩 감지 실패, UTF-8로 가정하고 진행합니다.")
		}
	}

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// [5단계] HTML 파싱
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// UTF-8로 변환된 스트림을 goquery에 전달하여 DOM 트리를 구성합니다.
	doc, err := goquery.NewDocumentFromReader(utf8Reader)
	if err != nil {
		return nil, err
	}

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// [6단계] 상대 경로 링크 해석을 위한 URL 정보 주입
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// HTML 문서 내의 상대 경로 링크(예: <a href="/path/to/page">)를 절대 경로로 변환하려면 원본 페이지의 URL이 필요합니다.
	// goquery.Document의 Url 필드에 원본 URL을 설정하면,
	// Selection.Attr("href") 등으로 링크를 추출할 때 자동으로 절대 경로로 변환됩니다.
	if baseURL != nil {
		doc.Url = baseURL
	}

	return doc, nil
}
