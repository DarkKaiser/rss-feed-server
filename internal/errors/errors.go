// Package errors 애플리케이션 전용 에러 처리 시스템을 제공합니다.
//
// 이 패키지는 표준 errors 패키지를 확장하여 타입 기반 에러 분류와
// 에러 체이닝을 지원합니다. 모든 에러는 ErrorType으로 분류되며,
// Wrap 함수를 통해 컨텍스트를 누적할 수 있습니다.
//
// # 기본 사용법
//
// 새 에러 생성:
//
//	err := errors.New(errors.NotFound, "사용자를 찾을 수 없습니다")
//
// 에러 래핑 (컨텍스트 추가):
//
//	if err != nil {
//	    return errors.Wrap(err, errors.Internal, "데이터베이스 조회 실패")
//	}
//
// 에러 타입 검사:
//
//	if errors.Is(err, errors.NotFound) {
//	    // NotFound 타입 에러 처리
//	}
//
// 에러 체인 탐색:
//
//	rootErr := errors.RootCause(err)  // 최상위 원인 에러 반환
//
// # ErrorType 선택 가이드
//
// 각 ErrorType은 에러의 성격과 원인에 따라 구분됩니다.
// 적절한 타입을 선택하면 에러 처리 로직을 명확하게 구성할 수 있습니다.
//
// Unknown:
//   - 분류할 수 없는 에러 (기본값, 사용 지양)
//   - 외부 라이브러리 에러를 AppError로 변환할 수 없을 때
//
// Internal:
//   - 애플리케이션 내부 로직 오류 (버그로 간주)
//   - nil 포인터 참조, 예상하지 못한 상태, 로직 오류 등
//   - 예: "예상치 못한 nil 값", "잘못된 상태 전이"
//
// System:
//   - 시스템 또는 인프라 수준의 장애
//   - 디스크 I/O, 네트워크, 데이터베이스 연결 등
//   - 예: "파일 읽기 실패", "DB 연결 실패"
//
// Unauthorized:
//   - 인증 실패 (사용자 신원 확인 실패)
//   - 로그인 필요, 토큰 만료, 잘못된 자격증명 등
//   - 예: "로그인이 필요합니다", "토큰이 만료되었습니다"
//
// Forbidden:
//   - 권한 부족 (인증은 성공했지만 접근 권한 없음)
//   - 예: "관리자 권한이 필요합니다", "이 리소스에 접근할 수 없습니다"
//
// InvalidInput:
//   - 사용자 입력값 검증 실패
//   - 유효성 검사 실패, 잘못된 형식, 필수 값 누락 등
//   - 예: "이메일 형식이 올바르지 않습니다", "필수 항목이 누락되었습니다"
//
// Conflict:
//   - 리소스 충돌 또는 상태 불일치
//   - 중복 생성, 동시성 문제, 버전 충돌 등
//   - 예: "이미 존재하는 사용자입니다", "리소스가 이미 수정되었습니다"
//
// NotFound:
//   - 요청한 리소스를 찾을 수 없음
//   - 예: "사용자를 찾을 수 없습니다", "페이지가 존재하지 않습니다"
//
// ExecutionFailed:
//   - 비즈니스 로직 또는 외부 프로세스 실행 실패
//   - 웹 스크래핑 실패, 외부 API 호출 실패, 작업 실행 오류 등
//   - 예: "페이지 파싱 실패", "외부 API 호출 실패"
//
// ParsingFailed:
//   - 데이터 파싱, 변환, 디코딩 실패
//   - HTML/JSON 파싱 오류, 잘못된 데이터 포맷 등
//   - 예: "HTML 구조 분석 실패", "JSON 디코딩 오류", "날짜 형식 변환 실패"
//
// Timeout:
//   - 작업 시간 초과
//   - HTTP 요청 타임아웃, 작업 처리 시간 초과 등
//   - 예: "요청 시간이 초과되었습니다"
//
// Unavailable:
//   - 서비스 일시적 사용 불가
//   - 서비스 점검, 과부하, 일시적 장애 등
//   - 예: "서비스가 일시적으로 사용 불가능합니다"
//
// # Wrap 시 타입 선택 원칙
//
// 1. 원인 에러가 AppError인 경우:
//   - 컨텍스트만 추가하고 동일한 타입 유지 (일반적)
//   - 또는 더 상위 추상화 레벨의 타입으로 변경
//   - 예: NotFound를 Wrap하여 Internal로 변경 (드물게 사용)
//
// 2. 원인 에러가 외부 라이브러리 에러인 경우:
//   - 에러의 성격에 맞는 적절한 타입 선택
//   - 예: sql.ErrNoRows → NotFound
//   - 예: context.DeadlineExceeded → Timeout
//   - 예: net.Error → System
//   - 예: json.UnmarshalError → InvalidInput
//
// 3. 타입 선택이 애매한 경우:
//   - 에러가 발생한 계층(layer)을 고려
//   - 사용자 입력 계층: InvalidInput
//   - 비즈니스 로직 계층: ExecutionFailed, Conflict 등
//   - 인프라 계층: System, Timeout, Unavailable
package errors

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// AppError 애플리케이션에서 발생하는 모든 에러를 표준화하여 표현하는 구조체입니다.
type AppError struct {
	errType ErrorType    // 에러의 종류
	message string       // 사용자에게 보여줄 메시지
	cause   error        // 이 에러가 발생하게 된 근본 원인 (에러 체이닝)
	stack   []StackFrame // 에러 발생 시점의 함수 호출 스택 정보
}

// Type 에러의 타입을 반환합니다.
func (e *AppError) Type() ErrorType {
	return e.errType
}

// Message 에러 메시지를 반환합니다.
func (e *AppError) Message() string {
	return e.message
}

// Stack 스택 트레이스를 반환합니다.
func (e *AppError) Stack() []StackFrame {
	if e.stack == nil {
		return nil
	}
	return e.stack
}

// Error 표준 errors.Error 인터페이스를 구현합니다.
func (e *AppError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.errType, e.message, e.cause)
	}
	return fmt.Sprintf("[%s] %s", e.errType, e.message)
}

// Unwrap 표준 errors.Unwrap 인터페이스를 구현합니다.
func (e *AppError) Unwrap() error {
	return e.cause
}

// Format fmt.Formatter 인터페이스를 구현합니다.
// %+v 사용 시 에러 체인과 스택 트레이스를 상세히 출력합니다.
func (e *AppError) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v':
		if s.Flag('+') {
			// 에러 타입과 메시지
			fmt.Fprintf(s, "[%s] %s", e.errType, e.message)

			// 스택 트레이스 출력 정책:
			// 스택 중복 출력을 방지하기 위해 다음 조건에서만 스택을 출력합니다.
			//
			// 1. Root 에러인 경우 (cause가 nil)
			// 2. 외부 에러(표준 error 등)를 감싼 경우 (cause가 AppError가 아님)
			//
			// 즉, AppError가 다른 AppError를 감싸고 있는 체인의 중간 단계에서는
			// 스택을 출력하지 않고, 체인의 가장 끝(Root) 또는 외부 에러와의 경계에서만 출력합니다.
			var target *AppError
			if e.cause == nil || !errors.As(e.cause, &target) {
				if len(e.stack) > 0 {
					fmt.Fprint(s, "\nStack trace:")
					for _, frame := range e.stack {
						// 함수명에서 패키지 경로 간소화
						funcName := frame.Function
						if idx := strings.LastIndex(funcName, "/"); idx != -1 {
							funcName = funcName[idx+1:]
						}
						fmt.Fprintf(s, "\n\t%s:%d %s", frame.File, frame.Line, funcName)
					}
				}
			}

			// Cause 출력
			if e.cause != nil {
				fmt.Fprint(s, "\nCaused by:\n")
				if formatter, ok := e.cause.(fmt.Formatter); ok {
					formatter.Format(s, verb)
				} else {
					fmt.Fprintf(s, "\t%v", e.cause)
				}
			}
			return
		}
		fallthrough
	case 's':
		io.WriteString(s, e.Error())
	case 'q':
		fmt.Fprintf(s, "%q", e.Error())
	}
}

// New 새로운 에러를 생성합니다.
func New(errType ErrorType, message string) error {
	return &AppError{
		errType: errType,
		message: message,
		stack:   captureStack(defaultCallerSkip),
	}
}

// Newf 포맷 문자열을 사용하여 새로운 에러를 생성합니다.
func Newf(errType ErrorType, format string, args ...any) error {
	return &AppError{
		errType: errType,
		message: fmt.Sprintf(format, args...),
		stack:   captureStack(defaultCallerSkip),
	}
}

// Wrap 기존 에러를 감싸서 새로운 에러를 생성합니다.
func Wrap(err error, errType ErrorType, message string) error {
	if err == nil {
		return nil
	}
	return &AppError{
		errType: errType,
		message: message,
		cause:   err,
		stack:   captureStack(defaultCallerSkip),
	}
}

// Wrapf 포맷 문자열을 사용하여 기존 에러를 감쌉니다.
func Wrapf(err error, errType ErrorType, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return &AppError{
		errType: errType,
		message: fmt.Sprintf(format, args...),
		cause:   err,
		stack:   captureStack(defaultCallerSkip),
	}
}

// Is 에러 체인에 특정 ErrorType이 포함되어 있는지 확인합니다.
func Is(err error, errType ErrorType) bool {
	for err != nil {
		if appErr, ok := err.(*AppError); ok {
			if appErr.errType == errType {
				return true
			}
		}
		err = errors.Unwrap(err)
	}
	return false
}

// As 에러 체인에서 특정 타입의 에러를 찾아 대상 변수에 할당합니다.
func As(err error, target any) bool {
	return errors.As(err, target)
}

// RootCause 에러가 발생한 가장 근본적인 원인 에러를 찾습니다.
func RootCause(err error) error {
	if err == nil {
		return nil
	}

	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			return err
		}
		err = unwrapped
	}
}

// UnderlyingType 에러 체인에서 가장 안쪽에 있는 AppError의 ErrorType을 반환합니다.
//
// 이 함수는 여러 겹으로 래핑된 에러의 근본적인(underlying) 타입을 찾습니다.
// 에러 체인 전체를 순회하면서 가장 안쪽(Root에 가까운)에 위치한 AppError를 찾아
// 그 타입을 반환합니다. 외부 라이브러리 에러(sql.ErrNoRows, context.DeadlineExceeded 등)를
// AppError로 래핑한 경우에도 의도한 ErrorType을 올바르게 반환합니다.
//
// 주요 사용 사례:
//   - HTTP 응답 코드 결정 시 에러의 근본 성격 파악
//   - 로깅 레벨 결정 시 에러의 본질적 타입 확인
//   - 여러 단계로 래핑된 에러의 원래 분류 확인
//
// 반환값:
//   - 체인에 AppError가 하나라도 존재하는 경우: 가장 안쪽 AppError의 ErrorType
//   - 체인에 AppError가 없거나 err이 nil인 경우: Unknown
//
// 사용 예시:
//
//	// 예시 1: AppError 체인
//	err := Wrap(New(NotFound, "user not found"), Internal, "query failed")
//	underlyingType := UnderlyingType(err)  // NotFound 반환
//
//	// 예시 2: 외부 에러 래핑
//	err := Wrap(sql.ErrNoRows, NotFound, "user not found")
//	underlyingType := UnderlyingType(err)  // NotFound 반환 (외부 에러도 올바르게 분류)
func UnderlyingType(err error) ErrorType {
	var lastAppErrorType ErrorType = Unknown

	for err != nil {
		if appErr, ok := err.(*AppError); ok {
			lastAppErrorType = appErr.errType
		}
		err = errors.Unwrap(err)
	}

	return lastAppErrorType
}
