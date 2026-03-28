package fetcher

import (
	"net/http"
)

// StatusCodeFetcher HTTP 응답의 상태 코드를 검증하는 미들웨어입니다.
//
// 주요 목적:
//   - 허용된 HTTP 응답 상태 코드만 성공으로 처리
//   - 허용되지 않은 응답 상태 코드 조기 감지 및 에러 반환
//   - 실패한 응답의 리소스를 안전하게 정리하여 커넥션 재사용 보장
type StatusCodeFetcher struct {
	delegate Fetcher

	// allowedStatusCodes 허용할 HTTP 응답 상태 코드 목록입니다.
	// nil 또는 빈 슬라이스인 경우 200 OK만 허용합니다.
	allowedStatusCodes []int
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ Fetcher = (*StatusCodeFetcher)(nil)

// NewStatusCodeFetcher 200 OK만 허용하는 StatusCodeFetcher 인스턴스를 생성합니다.
func NewStatusCodeFetcher(delegate Fetcher) *StatusCodeFetcher {
	return &StatusCodeFetcher{
		delegate: delegate,
	}
}

// NewStatusCodeFetcherWithOptions 특정 HTTP 응답 상태 코드들을 허용하는 StatusCodeFetcher 인스턴스를 생성합니다.
func NewStatusCodeFetcherWithOptions(delegate Fetcher, allowedStatusCodes ...int) *StatusCodeFetcher {
	return &StatusCodeFetcher{
		delegate:           delegate,
		allowedStatusCodes: allowedStatusCodes,
	}
}

// Do HTTP 요청을 수행하고 응답 상태 코드를 검증합니다.
//
// 매개변수:
//   - req: 처리할 HTTP 요청
//
// 반환값:
//   - HTTP 응답 객체 (성공 시)
//   - 에러 (요청 처리 중 발생한 에러)
//
// 주의사항:
//   - 응답 상태 코드 검증 실패 시 nil Response를 반환하므로, 호출자는 에러 체크 후 Response를 사용해야 합니다.
//   - 에러 발생 시(Delegate 에러 또는 응답 상태 코드 검증 실패) 응답 객체의 Body는 이 함수 내부에서 자동으로 정리되므로,
//     호출자가 별도로 닫을 필요가 없습니다.
//   - 성공 시에는 호출자가 반드시 응답 객체의 Body를 닫아야 합니다.
func (f *StatusCodeFetcher) Do(req *http.Request) (*http.Response, error) {
	resp, err := f.delegate.Do(req)
	if err != nil {
		// 에러가 발생했더라도 응답 객체가 있을 수 있음 (예: 상태 코드 에러, 리다이렉트 에러)
		if resp != nil {
			// 커넥션 재사용을 위해 응답 객체의 Body를 안전하게 비우고 닫음
			drainAndCloseBody(resp.Body)
		}

		return nil, err
	}

	// 응답 상태 코드 검증
	// CheckResponseStatusWithoutReconstruct는 상태 코드가 허용 목록에 없으면 에러를 반환합니다.
	// 에러 객체에는 상태 코드, URL, 응답 객체의 Body 일부(BodySnippet) 등이 포함되어 있습니다.
	if statusErr := CheckResponseStatusWithoutReconstruct(resp, f.allowedStatusCodes...); statusErr != nil {
		// 커넥션 재사용을 위해 응답 객체의 Body를 안전하게 비우고 닫음
		drainAndCloseBody(resp.Body)

		return nil, statusErr
	}

	return resp, nil
}

func (f *StatusCodeFetcher) Close() error {
	return f.delegate.Close()
}
