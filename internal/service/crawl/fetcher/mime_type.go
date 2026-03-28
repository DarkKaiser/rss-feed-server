package fetcher

import (
	"mime"
	"net/http"
	"strings"

	applog "github.com/darkkaiser/notify-server/pkg/log"
)

// MimeTypeFetcher HTTP 응답의 MIME 타입을 검증하는 미들웨어입니다.
//
// 주요 목적:
//   - HTML/텍스트 수집 작업 중 의도치 않은 대용량 바이너리 파일(이미지, 동영상 등) 다운로드 방지
//   - 허용된 MIME 타입만 처리하여 메모리 및 네트워크 리소스 보호
//   - 잘못된 MIME 타입을 조기에 감지하여 불필요한 처리 방지
type MimeTypeFetcher struct {
	delegate Fetcher

	// allowedMimeTypes 응답으로 허용할 MIME 타입 목록입니다.
	// 대소문자를 구분하지 않으며, 빈 슬라이스인 경우 검증을 생략합니다.
	allowedMimeTypes []string

	// allowMissingContentType Content-Type 헤더가 없는 응답을 허용할지 여부입니다.
	allowMissingContentType bool
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ Fetcher = (*MimeTypeFetcher)(nil)

// NewMimeTypeFetcher 새로운 MimeTypeFetcher 인스턴스를 생성합니다.
func NewMimeTypeFetcher(delegate Fetcher, allowedMimeTypes []string, allowMissingContentType bool) Fetcher {
	if len(allowedMimeTypes) == 0 {
		return delegate
	}

	return &MimeTypeFetcher{
		delegate:                delegate,
		allowedMimeTypes:        allowedMimeTypes,
		allowMissingContentType: allowMissingContentType,
	}
}

// Do HTTP 요청을 수행하고 MIME 타입을 검증합니다.
//
// 매개변수:
//   - req: 처리할 HTTP 요청
//
// 반환값:
//   - HTTP 응답 객체 (성공 시)
//   - 에러 (요청 처리 중 발생한 에러)
func (f *MimeTypeFetcher) Do(req *http.Request) (*http.Response, error) {
	resp, err := f.delegate.Do(req)
	if err != nil {
		// 에러가 발생했더라도 응답 객체가 있을 수 있음 (예: 상태 코드 에러, 리다이렉트 에러)
		if resp != nil {
			// 커넥션 재사용을 위해 응답 객체의 Body를 안전하게 비우고 닫음
			drainAndCloseBody(resp.Body)
		}

		return nil, err
	}

	// 1. Content-Type 헤더 추출
	contentType := resp.Header.Get("Content-Type")

	// 2. Content-Type이 비어있는 경우 처리
	if contentType == "" {
		if f.allowMissingContentType {
			return resp, nil
		}

		if resp.Body != nil {
			// 커넥션 재사용을 위해 응답 객체의 Body를 안전하게 비우고 닫음
			drainAndCloseBody(resp.Body)
		}

		return nil, ErrMissingResponseContentType
	}

	// 3. MIME 타입 파싱: 파라미터를 제거하고 순수 미디어 타입만 추출
	// 예: "text/html; charset=utf-8" -> "text/html"
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		// 파싱 실패 시 폴백: 세미콜론 이전 부분을 소문자로 변환하여 사용
		// 일부 서버는 표준을 준수하지 않는 Content-Type을 반환할 수 있음
		// 예: "text/html ; charset=utf-8" (공백 포함) -> "text/html" 로 정상 처리되도록 TrimSpace 적용
		applog.WithComponent(component).
			WithContext(req.Context()).
			WithFields(applog.Fields{
				"content_type": contentType,
				"url":          redactURL(req.URL),
				"error":        err.Error(),
			}).
			Warn("Content-Type 파싱 경고: 표준 형식이 아니어서 폴백 처리함")

		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}

	// 4. 허용된 타입 목록과 비교 (대소문자 무시)
	isAllowed := false
	for _, t := range f.allowedMimeTypes {
		if strings.EqualFold(mediaType, t) {
			isAllowed = true
			break
		}
	}

	// 5. 허용되지 않은 타입인 경우 에러 반환
	if !isAllowed && len(f.allowedMimeTypes) > 0 {
		// 커넥션 재사용을 위해 응답 객체의 Body를 안전하게 비우고 닫음
		drainAndCloseBody(resp.Body)

		return nil, newErrUnsupportedMediaType(mediaType, f.allowedMimeTypes)
	}

	return resp, nil
}

func (f *MimeTypeFetcher) Close() error {
	return f.delegate.Close()
}
