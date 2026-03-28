package scraper

import (
	"net/http"
)

// Option Scraper 구성을 위한 옵션 함수 타입입니다.
type Option func(*scraper)

// WithMaxRequestBodySize HTTP 요청 본문의 최대 읽기 크기를 바이트 단위로 설정합니다.
//
// 이 옵션은 POST, PUT, PATCH 등의 메서드로 데이터를 전송할 때, 요청 본문의 크기를 제한하여
// 메모리 사용량을 제어하고 과도한 데이터 전송을 방지합니다. 설정된 크기를 초과하는 요청 본문은
// 전송되지 않으며 에러를 반환합니다.
//
// 매개변수:
//   - size: 최대 읽기 크기(바이트). 0보다 큰 값이어야 합니다.
func WithMaxRequestBodySize(size int64) Option {
	return func(s *scraper) {
		if size > 0 {
			s.maxRequestBodySize = size
		}
	}
}

// WithMaxResponseBodySize HTTP 응답 본문의 최대 읽기 크기를 바이트 단위로 설정합니다.
//
// 이 옵션은 메모리 사용량을 제어하고 악의적이거나 예상보다 큰 응답으로부터 시스템을 보호하기 위해 사용됩니다.
// 설정된 크기를 초과하는 응답 본문은 에러 처리되며, 부분 데이터도 반환하지 않습니다.
// 이는 불완전한 데이터로 인한 오동작을 방지하기 위함입니다.
//
// 매개변수:
//   - size: 최대 읽기 크기(바이트). 0보다 큰 값이어야 합니다.
func WithMaxResponseBodySize(size int64) Option {
	return func(s *scraper) {
		if size > 0 {
			s.maxResponseBodySize = size
		}
	}
}

// WithResponseCallback HTTP 응답을 수신한 직후, 응답 본문을 읽기 전에 실행될 콜백 함수를 설정합니다.
//
// 이 콜백은 응답 본문이 닫히기 전에 호출되므로, 응답 헤더, 상태 코드, 쿠키 등의 메타데이터를
// 검사하거나 로깅하는 용도로 사용할 수 있습니다. 본문 데이터를 읽거나 수정하는 작업은
// 스크래퍼의 내부 처리 로직과 충돌할 수 있으므로 권장하지 않습니다.
//
// 매개변수:
//   - callback: *http.Response를 인자로 받는 콜백 함수
//
// 주의사항:
//   - 콜백 함수 내에서 Response.Body를 읽거나 닫으면 안 됩니다.
//   - 콜백 함수는 빠르게 실행되어야 하며, 블로킹 작업은 피해야 합니다.
//   - 콜백 함수에서 발생한 패닉은 스크래핑 작업 전체를 중단시킬 수 있습니다.
func WithResponseCallback(callback func(*http.Response)) Option {
	return func(s *scraper) {
		s.responseCallback = callback
	}
}
