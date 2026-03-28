package scraper

import (
	"context"
	"io"
)

// contextAwareReader Context 취소를 감지하는 io.Reader 래퍼입니다.
//
// 이 구조체는 io.Reader 인터페이스를 구현하며, 매 Read 호출 시마다 Context의 상태를 확인하여
// 취소(Cancel) 또는 타임아웃(Timeout)이 발생한 경우 즉시 읽기 작업을 중단합니다.
//
// 사용 목적:
//   - 장시간 블로킹될 수 있는 읽기 작업(예: 네트워크 스트림, 대용량 파일)에서
//     Context 취소 시그널을 감지하여 리소스 낭비를 방지합니다.
//   - io.ReadAll과 같이 반복적으로 Read를 호출하는 함수에서 사용하면,
//     매 Read마다 Context 상태를 확인하여 조기 종료가 가능합니다.
//
// 주의사항:
//   - 기본 Reader(r)가 블로킹되면 Context 취소를 즉시 감지할 수 없습니다.
//     (Context 확인은 Read 호출 전에만 수행되므로, Read 내부에서 블로킹되면 다음 Read까지 대기)
//   - 따라서 네트워크 타임아웃이나 Deadline이 설정된 Reader와 함께 사용하는 것이 권장됩니다.
type contextAwareReader struct {
	ctx context.Context // 취소 감지를 위한 Context
	r   io.Reader       // 실제 데이터를 읽어올 기본 Reader
}

// Read io.Reader 인터페이스를 구현하며, Context 상태를 확인한 후 기본 Reader에서 데이터를 읽습니다.
//
// 반환값:
//   - n: 읽은 바이트 수
//   - err: Context 취소 에러 또는 기본 Reader의 에러
func (r *contextAwareReader) Read(p []byte) (n int, err error) {
	// Context 취소 여부 확인
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}

	return r.r.Read(p)
}
