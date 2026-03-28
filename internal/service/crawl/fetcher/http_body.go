package fetcher

import (
	"io"
	"sync"
)

const (
	// maxDrainBytes 커넥션 재사용을 위해 응답 객체의 Body를 비울 때 읽을 최대 바이트 수입니다. (64KB)
	// HTTP 커넥션 풀링을 위해 응답 객체의 Body를 완전히 읽어야 하지만,
	// 너무 큰 응답은 성능 저하를 유발하므로 64KB로 제한합니다.
	maxDrainBytes = 64 * 1024
)

var (
	// drainBufPool drainAndCloseBody에서 사용할 바이트 버퍼 풀입니다.
	//
	// HTTP 커넥션 풀링을 위해 응답 객체의 Body를 읽어야 하는데, 매번 새로운 버퍼를 할당하면 GC 부담이 증가합니다.
	// sync.Pool을 사용하여 버퍼를 재사용함으로써 메모리 할당을 최적화 합니다.
	drainBufPool = sync.Pool{
		New: func() any {
			b := make([]byte, 32*1024) // 32KB 버퍼 (대부분의 응답 처리에 충분)
			return &b
		},
	}
)

// drainAndCloseBody HTTP 커넥션 재사용을 위해 응답 객체의 Body를 안전하게 비우고 닫습니다.
//
// HTTP Keep-Alive 커넥션 풀링을 위해서는 응답 객체의 Body를 완전히 읽어야 합니다.
// Body를 읽지 않고 닫으면 커넥션이 재사용되지 않아 매번 새 TCP 연결이 필요하므로,
// 일정량(maxDrainBytes)을 읽어서 버린 후 닫아 커넥션 풀에 반환합니다.
//
// 매개변수:
//   - body: 비울 응답 객체의 Body (nil이면 아무 작업도 하지 않음)
//
// 주의사항:
//   - 64KB를 초과하는 응답은 완전히 읽히지 않으므로 해당 커넥션은 재사용되지 않음
//   - 이는 거대한 응답으로 인한 메모리 고갈을 방지하기 위한 트레이드오프
func drainAndCloseBody(body io.ReadCloser) {
	if body == nil {
		return
	}
	defer body.Close()

	// sync.Pool에서 버퍼를 빌려와서 사용
	bufPtr := drainBufPool.Get().(*[]byte)
	defer drainBufPool.Put(bufPtr)

	// maxDrainBytes(64KB)만큼만 읽고 나머지는 버림!
	// 이 범위를 초과하는 바디를 가진 커넥션은 재사용되지 않고 닫힘!
	_, _ = io.CopyBuffer(io.Discard, io.LimitReader(body, maxDrainBytes), *bufPtr)
}
