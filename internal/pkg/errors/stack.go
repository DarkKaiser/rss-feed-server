package errors

import (
	"path/filepath"
	"runtime"
)

// defaultCallerSkip 스택 트레이스 수집 시 건너뛸 호출 스택의 깊이입니다.
//
// runtime.Callers를 호출하면 현재 실행 중인 함수들부터 역순으로 스택이 쌓입니다.
// 사용자가 에러를 생성한 위치(New/Wrap 호출 지점)를 정확히 기록하기 위해
// 불필요한 내부 함수 호출 3단계를 건너뜁니다:
//
// 1. runtime.Callers      (스택 수집 함수)
// 2. captureStack         (내부 유틸리티 함수)
// 3. New/Wrap/Newf/Wrapf  (공개 에러 생성 함수)
//
// 결과적으로 이 값(3)을 사용해야 사용자의 코드 위치가 0번째 스택으로 기록됩니다.
const defaultCallerSkip = 3

// StackFrame 단일 함수 호출 스택의 실행 컨텍스트 정보를 캡슐화한 구조체입니다.
type StackFrame struct {
	File     string // 파일 이름
	Line     int    // 줄 번호
	Function string // 함수 이름
}

// captureStack 현재 실행 위치의 스택 정보를 수집하여 반환합니다. (최대 5단계)
func captureStack(skip int) []StackFrame {
	const maxFrames = 5
	pc := make([]uintptr, maxFrames)
	n := runtime.Callers(skip, pc)

	if n == 0 {
		return nil
	}

	callersFrames := runtime.CallersFrames(pc[:n])

	frames := make([]StackFrame, 0, n)
	for {
		frame, more := callersFrames.Next()
		frames = append(frames, StackFrame{
			File:     filepath.Base(frame.File),
			Line:     frame.Line,
			Function: frame.Function,
		})
		if !more {
			break
		}
	}

	return frames
}
