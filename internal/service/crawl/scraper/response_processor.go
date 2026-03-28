package scraper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher"
	applog "github.com/darkkaiser/notify-server/pkg/log"
	"golang.org/x/net/html/charset"
)

// validateResponse HTTP 응답의 유효성을 검증하고, 검증 실패 시 에러를 반환합니다.
//
// 이 함수는 HTTP 응답이 성공적인지 확인하고, 필요한 경우 사용자 정의 검증 로직(Validator)을 실행합니다.
// 검증 실패 시 연결 재사용을 위해 응답 본문을 드레인하고 닫습니다.
//
// 매개변수:
//   - resp: HTTP 응답 객체
//   - params: 요청 정보 (사용자 정의 Validator 함수, URL 등 포함)
//   - logger: 로그 객체 (검증 과정에서 추가 필드 기록용)
//
// 반환값:
//   - error: 검증 실패 시 에러 반환
func (s *scraper) validateResponse(resp *http.Response, params requestParams, logger *applog.Entry) error {
	// 등록된 responseCallback이 있다면 호출합니다.
	// 안전성을 위해 Body를 http.NoBody로 교체한 복사본을 전달합니다.
	// 이를 통해 콜백 함수가 Body를 읽어서 이후 처리에 영향을 주는 것을 방지합니다.
	if s.responseCallback != nil {
		safeResp := *resp
		safeResp.Body = http.NoBody

		// 헤더는 맵이므로 얕은 복사 시 원본이 수정될 수 있습니다.
		// 콜백에서의 사이드 이펙트를 방지하기 위해 깊은 복사를 수행합니다.
		safeResp.Header = resp.Header.Clone()

		// Trailer도 맵이므로 깊은 복사를 수행합니다.
		// HTTP Trailer는 청크 전송 인코딩에서 사용되며, 대부분의 경우 nil이거나 비어있습니다.
		if resp.Trailer != nil {
			safeResp.Trailer = resp.Trailer.Clone()
		}

		// Request 포인터가 원본을 공유하므로, 콜백에서 실수로 원본 요청을 수정하지 못하도록 nil로 설정합니다.
		safeResp.Request = nil

		s.responseCallback(&safeResp)
	}

	// HTTP 204 상태 코드는 본문이 없는 성공 응답입니다. 추가 검증 없이 즉시 성공으로 간주합니다.
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	// HTTP 상태 코드가 2xx 범위의 성공 응답인지 확인
	// 201 Created, 202 Accepted 등 API에서 자주 사용되는 성공 코드를 명시적으로 허용합니다.
	allowedStatusCodes := []int{http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent}
	if err := fetcher.CheckResponseStatus(resp, allowedStatusCodes...); err != nil {
		// 디버깅을 돕기 위해 에러 메시지에 응답 본문의 일부를 포함하여 반환합니다. (읽기 실패 시 무시)
		bodySnippet, _ := s.readErrorResponseBody(resp)

		// 연결 재사용을 위해 응답 본문의 나머지를 드레인합니다.
		// readErrorResponseBody에서 이미 1KB를 읽었으므로, 추가로 3KB를 읽어 총 4KB까지 드레인합니다.
		// 대용량 에러 응답으로 인한 메모리 낭비를 방지하기 위해 제한을 둡니다.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 3072))

		return newErrHTTPRequestFailed(err, params.URL, resp.StatusCode, bodySnippet)
	}

	// 사용자 정의 Validator 실행
	if params.Validator != nil {
		if err := params.Validator(resp, logger); err != nil {
			// 검증 실패 시 디버깅을 위해 응답 본문의 일부를 읽어서 에러 메시지에 포함합니다.
			data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			preview := s.previewBody(data, resp.Header.Get("Content-Type"))

			// 연결 재사용을 위해 응답 본문의 나머지를 드레인합니다.
			// 이미 1KB를 읽었으므로, 추가로 3KB를 읽어 총 4KB까지 드레인합니다.
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 3072))

			if preview != "" {
				// 디버깅을 돕기 위해 에러 메시지에 응답 본문의 일부를 포함하여 반환합니다.
				return newErrValidationFailed(err, params.URL, preview)
			}

			// 응답 본문의 일부를 읽지 못한 경우 원본 에러만 반환합니다.
			return err
		}
	}

	return nil
}

// readResponseBodyWithLimit HTTP 응답 본문을 메모리로 읽어들이며, 크기 제한을 적용합니다.
//
// 이 함수는 executeRequest의 3단계 중 마지막 단계로, 응답 본문을 안전하게 읽어들입니다.
// maxResponseBodySize를 초과하는 경우 자동으로 잘라내며(Truncation), 호출자에게 플래그로 알립니다.
//
// 매개변수:
//   - ctx: 요청의 생명주기를 제어하는 컨텍스트 (취소 감지용)
//   - resp: HTTP 응답 객체 (Body는 아직 읽지 않은 상태)
//
// 반환값:
//   - []byte: 응답 본문 데이터 (최대 maxResponseBodySize 바이트)
//   - bool: Truncation 발생 여부 (true: 잘림, false: 전체 데이터)
//   - error: I/O 에러 또는 컨텍스트 취소 에러 발생 시 반환
func (s *scraper) readResponseBodyWithLimit(ctx context.Context, resp *http.Response) ([]byte, bool, error) {
	// HTTP 204 상태 코드는 본문이 없는 성공 응답입니다.
	// 불필요한 메모리 할당을 방지하기 위해 즉시 nil을 반환합니다.
	if resp.StatusCode == http.StatusNoContent {
		return nil, false, nil
	}

	// maxResponseBodySize + 1만큼 읽는 이유:
	//   - maxResponseBodySize 이하인 경우: 전체 데이터를 읽음
	//   - maxResponseBodySize를 초과하는 경우: maxResponseBodySize+1 바이트를 읽어서 초과 여부를 감지
	//
	// 이를 통해 메모리 고갈 공격(DoS)을 방지하면서도 정확한 Truncation 감지가 가능합니다.
	limit := s.maxResponseBodySize + 1
	limitReader := io.LimitReader(resp.Body, limit)

	// 컨텍스트 취소 감지를 위한 Reader 래핑
	reader := &contextAwareReader{ctx: ctx, r: limitReader}

	// 전체 본문 데이터를 메모리로 읽어들입니다.
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, false, err
	}

	// Truncation 감지 및 처리
	truncated := false
	if int64(len(data)) > s.maxResponseBodySize {
		truncated = true

		// 크기 초과 시 데이터 잘라내기
		data = data[:s.maxResponseBodySize]
	}

	return data, truncated, nil
}

// readErrorResponseBody HTTP 오류 응답의 본문 일부를 읽어 디버깅용 문자열로 반환합니다.
//
// 매개변수:
//   - resp: HTTP 오류 응답 객체 (Body는 아직 읽지 않은 상태)
//
// 반환값:
//   - string: UTF-8로 변환되고 정제된 오류 본문 문자열 (최대 1024바이트)
//   - error: 본문 읽기 실패 시 발생하는 I/O 에러
func (s *scraper) readErrorResponseBody(resp *http.Response) (string, error) {
	// 일정 크기(1024바이트)의 데이터를 메모리에 읽어둔 후, 복사본으로 인코딩 변환을 시도합니다.
	buf := new(bytes.Buffer)
	_, err := io.CopyN(buf, resp.Body, 1024)
	if err != nil && err != io.EOF {
		return "", err
	}

	rawBytes := buf.Bytes()

	// Content-Type 헤더를 기반으로 charset.NewReader를 사용하여 UTF-8로 변환합니다.
	utf8Reader, err := charset.NewReader(bytes.NewReader(rawBytes), resp.Header.Get("Content-Type"))
	if err != nil {
		// 인코딩 감지에 실패하더라도 원본 데이터를 사용합니다.
		// strings.ToValidUTF8이 잘못된 문자를 정제하므로 안전합니다.
		cleanBody := strings.ToValidUTF8(string(rawBytes), "")

		return cleanBody, nil
	}

	// 오류 응답의 본문 데이터 읽기
	utf8Data, err := io.ReadAll(utf8Reader)
	if err != nil {
		// 읽기 실패 시 원본 데이터를 사용합니다.
		// strings.ToValidUTF8이 잘못된 문자를 정제하므로 안전합니다.
		cleanBody := strings.ToValidUTF8(string(rawBytes), "")

		return cleanBody, nil
	}

	// UTF-8 유효성 정제
	// 크기 제한으로 인해 멀티바이트 문자가 중간에 잘릴 수 있습니다.
	// 예: UTF-8 한글 "가"는 3바이트(0xEA 0xB0 0x80)인데, 1024바이트 경계에서 2바이트만 포함된 경우
	//
	// strings.ToValidUTF8은 잘못된 UTF-8 시퀀스를 처리합니다:
	//   - 두 번째 인자가 ""이면: 잘못된 시퀀스 제거 (깔끔한 로그 유지)
	//   - 두 번째 인자가 "�"이면: 잘못된 시퀀스를 U+FFFD(�)로 대체
	cleanBody := strings.ToValidUTF8(string(utf8Data), "")

	return cleanBody, nil
}

// previewBody HTTP 응답 본문의 앞부분을 로그에 출력하기 적합한 형태로 변환하여 반환합니다.
//
// 이 함수는 디버깅 및 모니터링을 위해 응답 본문의 일부를 로그에 안전하게 출력할 수 있도록
// 인코딩 변환, 크기 제한, UTF-8 유효성 검증, 바이너리 감지 등의 처리를 수행합니다.
//
// 매개변수:
//   - body: 미리보기할 응답 본문 데이터
//   - contentType: HTTP 응답의 Content-Type 헤더 값
//
// 반환값:
//   - string: 로그에 출력하기 적합하게 처리된 본문 미리보기 문자열
//     -최대 1024바이트로 제한됨
//     -UTF-8로 변환되어 깨진 문자 없이 출력 가능
//     -바이너리 데이터의 경우 "[바이너리 데이터] (N bytes)" 형식으로 반환
//     -잘린 경우 "...(생략됨)" 접미사 추가
func (s *scraper) previewBody(body []byte, contentType string) string {
	if len(body) == 0 {
		return ""
	}

	// 최대 미리보기 크기 (바이트)
	const maxPreviewSize = 1024

	// [단계 1] 초기 크기 제한
	// 최대 1024바이트까지만 처리하여 메모리 사용량을 제한합니다.
	limit := min(len(body), maxPreviewSize)
	limitedBody := body[:limit]

	// [단계 2] 인코딩 변환 (UTF-8이 아닌 경우)
	// Content-Type에 UTF-8이 명시되지 않은 경우, charset.NewReader를 사용하여 UTF-8로 변환합니다.
	//
	// charset.NewReader는 다음 순서로 인코딩을 감지합니다:
	//   1. 바이트 순서 표시(BOM) 감지
	//   2. Content-Type 헤더의 charset 파라미터
	//   3. HTML 문서 내 <meta> 태그 또는 XML 선언의 encoding 속성
	if !isUTF8ContentType(contentType) {
		utf8Reader, err := charset.NewReader(bytes.NewReader(limitedBody), contentType)
		if err == nil {
			decoded, err := io.ReadAll(utf8Reader)
			if err == nil {
				// 변환 성공: UTF-8로 디코딩된 데이터 사용
				limitedBody = decoded
			} else if len(decoded) > 0 {
				// 부분 변환 성공: 에러가 발생했더라도 디코딩된 부분까지는 사용
				// (잘린 멀티바이트 문자로 인한 에러 허용)
				limitedBody = decoded
			} else {
				// 변환 실패 시: 원본 데이터를 그대로 사용 (단계 4에서 UTF-8 유효성 검증 수행)
			}
		}
	}

	// [단계 3] 변환 후 크기 다시 제한
	// 인코딩 변환 과정에서 데이터 크기가 증가할 수 있으므로 (예: EUC-KR 2바이트 → UTF-8 3바이트)
	// 다시 한번 최대 크기로 제한합니다.
	if len(limitedBody) > maxPreviewSize {
		limitedBody = limitedBody[:maxPreviewSize]
	}

	// [단계 4] UTF-8 유효성 검증 및 정제
	// 크기 제한으로 인해 멀티바이트 문자가 중간에 잘릴 수 있습니다.
	// 예: UTF-8 한글 "가"는 3바이트(0xEA 0xB0 0x80)인데, 1024바이트 경계에서 2바이트만 포함된 경우
	//
	// strings.ToValidUTF8은 잘못된 UTF-8 시퀀스를 처리합니다:
	//   - 두 번째 인자가 ""이면: 잘못된 시퀀스 제거 (깔끔한 로그 유지)
	//   - 두 번째 인자가 "�"이면: 잘못된 시퀀스를 U+FFFD(�)로 대체
	preview := strings.ToValidUTF8(string(limitedBody), "")

	// [단계 5] 바이너리 데이터 감지
	// 이미지, 동영상, 압축 파일 등의 바이너리 데이터를 로그에 출력하면 터미널이 깨질 수 있으므로
	// 제어 문자 존재 여부를 확인하여 바이너리 데이터를 필터링합니다.
	//
	// 허용되는 제어 문자 (ASCII 0-31 범위):
	//   - \t (0x09, Tab): 탭 문자
	//   - \n (0x0A, Line Feed): 줄바꿈
	//   - \r (0x0D, Carriage Return): 캐리지 리턴
	//
	// 위 3가지를 제외한 제어 문자가 발견되면 바이너리로 간주합니다.
	isBinary := false
	for _, r := range preview {
		if r < 32 && r != '\t' && r != '\n' && r != '\r' {
			isBinary = true
			break
		}
	}

	if isBinary {
		return fmt.Sprintf("[바이너리 데이터] (%d 바이트)", len(body))
	}

	// [최종] 잘림 표시
	// 원본 본문이 미리보기보다 긴 경우 "...(생략됨)" 접미사를 추가하여
	// 로그를 보는 사람에게 데이터가 잘렸음을 명시적으로 알립니다.
	if len(body) > len(preview) {
		return preview + "...(생략됨)"
	}

	return preview
}

// isUTF8ContentType HTTP 응답의 Content-Type 헤더에서 UTF-8 인코딩이 명시되어 있는지 확인합니다.
//
// 매개변수:
//   - contentType: 검증할 Content-Type 헤더 값
//
// 반환값:
//   - true: Content-Type에 UTF-8 인코딩이 명시되어 있는 경우
//     → 데이터가 이미 UTF-8로 인코딩되어 있으므로 변환 불필요
//   - false: UTF-8 인코딩이 명시되지 않았거나 다른 인코딩(euc-kr, shift-jis 등)이 지정된 경우
//     → charset.NewReader를 통한 자동 감지 및 UTF-8 변환 필요
func isUTF8ContentType(contentType string) bool {
	lowerType := strings.ToLower(contentType)
	return strings.Contains(lowerType, "utf-8")
}

// isHTMLContentType HTTP 응답의 Content-Type 헤더가 HTML 형식인지 판단합니다.
//
// 매개변수:
//   - contentType: 검증할 Content-Type 헤더 값
//
// 반환값:
//   - bool: HTML 타입이면 true, 그렇지 않으면 false
//
// 인식하는 HTML 타입:
//   - text/html: 표준 HTML 문서
//   - application/xhtml+xml: XHTML 문서
func isHTMLContentType(contentType string) bool {
	// 1. 표준 파싱 시도
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil {
		return mediaType == "text/html" || mediaType == "application/xhtml+xml"
	}

	// 2. 파싱 실패 시, Fallback으로 문자열 접두사 확인
	lowerType := strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(lowerType, "text/html") || strings.HasPrefix(lowerType, "application/xhtml+xml")
}
