package fetcher

import (
	"errors"
	"io"
	"net/http"
)

const (
	// NoLimit 응답 본문의 크기 제한을 적용하지 않음을 나타내는 값입니다.
	NoLimit = -1

	// defaultMaxBytes 응답 본문의 최대 허용 크기입니다. (10MB)
	defaultMaxBytes int64 = 10 * 1024 * 1024
)

// maxBytesReader http.MaxBytesReader를 래핑하여 apperrors 형식의 에러 메시지를 제공하는 내부 헬퍼 구조체입니다.
type maxBytesReader struct {
	rc io.ReadCloser

	// 응답 본문의 최대 허용 크기입니다. (에러 메시지 출력용)
	limit int64
}

// Read 데이터를 읽으며, 크기 제한 초과 시 도메인 전용 에러(apperrors.AppError)로 변환하여 반환합니다.
func (r *maxBytesReader) Read(p []byte) (n int, err error) {
	n, err = r.rc.Read(p)
	if err != nil {
		// http.MaxBytesReader는 크기 제한 초과 시 *http.MaxBytesError를 반환합니다.
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return n, newErrResponseBodyTooLarge(r.limit)
		}
	}

	return n, err
}

// Close 래핑된 ReadCloser를 닫습니다.
func (r *maxBytesReader) Close() error {
	return r.rc.Close()
}

// MaxBytesFetcher HTTP 응답 본문의 크기를 제한하는 미들웨어입니다.
//
// 주요 기능:
//   - Content-Length 헤더 기반 조기 차단 (네트워크 대역폭 절약)
//   - 실제 읽기 시점의 바이트 수 제한 (악의적인 Content-Length 조작 방어)
type MaxBytesFetcher struct {
	delegate Fetcher

	// limit HTTP 응답 본문의 최대 허용 크기입니다. (단위: 바이트)
	//
	// 이 값은 normalizeByteLimit 함수를 통해 정규화되며, 항상 양수값만 저장됩니다.
	// (NoLimit(-1)인 경우 NewMaxBytesFetcher가 미들웨어를 우회하므로 이 필드에 저장되지 않음)
	limit int64
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ Fetcher = (*MaxBytesFetcher)(nil)

// NewMaxBytesFetcher 새로운 MaxBytesFetcher 인스턴스를 생성합니다.
func NewMaxBytesFetcher(delegate Fetcher, limit int64) Fetcher {
	if limit == NoLimit {
		return delegate
	}

	// 응답 본문의 최대 허용 크기 정규화
	limit = normalizeByteLimit(limit)

	return &MaxBytesFetcher{
		delegate: delegate,
		limit:    limit,
	}
}

// Do HTTP 요청을 수행하고, 응답 본문의 크기를 제한합니다.
//
// 매개변수:
//   - req: 처리할 HTTP 요청
//
// 반환값:
//   - HTTP 응답 객체 (성공 시)
//   - 에러 (요청 처리 중 발생한 에러)
//
// 주의사항:
//   - 응답 객체의 Body는 반드시 호출자가 닫아야 합니다.
//   - Body를 읽는 도중 제한 초과 시 에러가 발생할 수 있습니다.
//   - Content-Length 헤더가 없는 응답도 2차 방어로 보호됩니다.
func (f *MaxBytesFetcher) Do(req *http.Request) (*http.Response, error) {
	resp, err := f.delegate.Do(req)
	if err != nil {
		// 에러가 발생했더라도 응답 객체가 있을 수 있음 (예: 상태 코드 에러, 리다이렉트 에러)
		if resp != nil {
			// 커넥션 재사용을 위해 응답 객체의 Body를 안전하게 비우고 닫음
			drainAndCloseBody(resp.Body)
		}

		return nil, err
	}

	// 1차 방어: Content-Length 헤더 기반 조기 차단
	// 장점: 실제 데이터를 다운로드하기 전에 차단하여 네트워크 대역폭 절약
	if resp.ContentLength > f.limit {
		if resp.Body != nil {
			// 커넥션 재사용을 위해 응답 객체의 Body를 안전하게 비우고 닫음
			drainAndCloseBody(resp.Body)
		}

		return nil, newErrResponseBodyTooLargeByContentLength(resp.ContentLength, f.limit)
	}

	// 2차 방어: 실제 읽기 시점의 바이트 수 제한
	//
	// http.MaxBytesReader는 Content-Length 헤더를 신뢰하지 않고,
	// 실제 Read() 호출 시 읽은 바이트 수를 기준으로 제한합니다.
	// 따라서 다음과 같은 경우를 방어할 수 있습니다:
	//   - Content-Length 헤더가 없는 응답
	//   - Content-Length가 실제 크기보다 작게 조작된 악의적인 응답
	//
	// 주의사항:
	//   - MaxBytesReader는 제한 초과 시 에러를 반환하지만 Body를 자동으로 닫지 않습니다.
	//   - 호출자는 반드시 defer resp.Body.Close()를 사용해야 합니다.
	//   - 이는 일반적인 HTTP 응답 객체의 Body 처리 규칙과 동일합니다.
	resp.Body = &maxBytesReader{
		rc:    http.MaxBytesReader(nil, resp.Body, f.limit),
		limit: f.limit,
	}

	return resp, nil
}

func (f *MaxBytesFetcher) Close() error {
	return f.delegate.Close()
}

// normalizeByteLimit HTTP 응답 본문의 최대 허용 크기를 정규화합니다.
//
// 정규화 규칙:
//   - NoLimit(-1): 그대로 유지
//   - 0 이하: 기본값(defaultMaxBytes)으로 보정
//   - 양수: 그대로 유지
//
// 동작 방식:
//   - NoLimit(-1): 크기 제한 없음
//   - 양수: 지정된 크기만큼 응답 본문 크기 제한
func normalizeByteLimit(limit int64) int64 {
	if limit == NoLimit {
		return NoLimit
	}

	if limit <= 0 {
		return defaultMaxBytes
	}

	return limit
}
