package fetcher

import (
	"context"
	"io"
	"net/http"
)

// component 크롤링 서비스의 Fetcher 로깅용 컴포넌트 이름
const component = "crawl.fetcher"

// Fetcher HTTP 요청을 수행하는 핵심 인터페이스입니다.
//
// 이 인터페이스는 다양한 HTTP 클라이언트 구현체들이 공통으로 따르는 규약을 정의합니다.
// 재시도, 로깅, User-Agent 설정 등의 기능을 데코레이터 패턴으로 조합할 수 있도록 설계되었습니다.
//
// 구현 시 주의사항:
//   - 반환된 응답 객체의 Body는 반드시 호출자가 닫아야 합니다.
//   - 에러가 발생해도 응답 객체가 nil이 아닐 수 있습니다 (예: 상태 코드 에러, 리다이렉트 에러).
//   - Context 취소 시 즉시 요청을 중단하고 적절한 에러를 반환해야 합니다.
type Fetcher interface {
	io.Closer

	Do(req *http.Request) (*http.Response, error)
}

// Get 지정된 URL로 HTTP GET 요청을 전송하는 헬퍼 함수입니다.
//
// 이 함수는 Fetcher 인터페이스의 모든 구현체에서 공통으로 사용할 수 있으며,
// http.Request 객체를 직접 생성하는 번거로움을 줄여줍니다.
//
// 매개변수:
//   - ctx: 요청의 생명주기를 제어하는 Context (타임아웃, 취소 등)
//   - f: HTTP 요청을 실제로 수행할 Fetcher 구현체
//   - url: GET 요청을 보낼 URL (유효한 HTTP/HTTPS URL이어야 함)
//
// 반환값:
//   - *http.Response: 성공 시 HTTP 응답 객체 (Body는 호출자가 반드시 닫아야 함)
//   - error: URL 파싱 실패, 네트워크 오류, HTTP 에러 등
//
// 에러 처리:
//   - URL이 잘못된 경우 즉시 에러를 반환합니다.
//   - 요청 실패 시 커넥션 재사용을 위해 응답 객체의 Body를 자동으로 읽어서 버리고 닫습니다.
func Get(ctx context.Context, f Fetcher, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := f.Do(req)
	if err != nil {
		if resp != nil {
			// 커넥션 재사용을 위해 응답 객체의 Body를 안전하게 비우고 닫음
			drainAndCloseBody(resp.Body)
		}

		return nil, err
	}

	return resp, nil
}
