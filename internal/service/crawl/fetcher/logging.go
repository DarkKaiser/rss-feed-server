package fetcher

import (
	"net/http"
	"time"

	applog "github.com/darkkaiser/notify-server/pkg/log"
)

// LoggingFetcher HTTP 요청의 상세 정보를 로그로 남기는 미들웨어입니다.
//
// 로깅되는 정보:
//   - 요청 메서드 (GET, POST 등)
//   - 요청 URL (민감 정보 마스킹 처리됨)
//   - 응답 상태 코드 (200, 404, 500 등)
//   - 요청 처리 소요 시간
//   - 에러 메시지 (에러 발생 시)
type LoggingFetcher struct {
	delegate Fetcher
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ Fetcher = (*LoggingFetcher)(nil)

// NewLoggingFetcher 새로운 LoggingFetcher 인스턴스를 생성합니다.
func NewLoggingFetcher(delegate Fetcher) *LoggingFetcher {
	return &LoggingFetcher{
		delegate: delegate,
	}
}

// Do HTTP 요청을 수행하고 상세 로그를 기록합니다.
//
// 매개변수:
//   - req: 처리할 HTTP 요청
//
// 반환값:
//   - HTTP 응답 객체 (성공 시)
//   - 에러 (요청 처리 중 발생한 에러)
//
// 주의사항:
//   - URL은 redactURL()을 통해 민감 정보(비밀번호, 쿼리 파라미터 등)가 마스킹됩니다.
//   - Context를 통해 요청별 로그 추적이 가능합니다.
//   - 에러가 발생했더라도 응답 객체가 있다면 상태 코드를 함께 로깅합니다.
func (f *LoggingFetcher) Do(req *http.Request) (*http.Response, error) {
	start := time.Now()

	resp, err := f.delegate.Do(req)

	// 소요 시간을 계산합니다.
	duration := time.Since(start)

	// 기본 로그 필드 준비
	fields := applog.Fields{
		"method":   req.Method,
		"url":      redactURL(req.URL), // 민감 정보 마스킹
		"duration": duration.String(),
	}

	if err != nil {
		fields["error"] = err.Error()

		// 에러가 발생했더라도 응답 객체가 있을 수 있음 (예: 상태 코드 에러, 리다이렉트 에러)
		if resp != nil {
			fields["status"] = resp.Status
			fields["status_code"] = resp.StatusCode
		}

		applog.WithComponent(component).
			WithContext(req.Context()).
			WithFields(fields).
			Error("HTTP 요청 실패: 요청 처리 중 에러 발생")

		return resp, err
	}

	// 성공 시 응답 상태 정보 추가
	if resp != nil {
		fields["status"] = resp.Status
		fields["status_code"] = resp.StatusCode
	}

	applog.WithComponent(component).
		WithContext(req.Context()).
		WithFields(fields).
		Debug("HTTP 요청 성공: 정상 처리 완료")

	return resp, nil
}

func (f *LoggingFetcher) Close() error {
	return f.delegate.Close()
}
