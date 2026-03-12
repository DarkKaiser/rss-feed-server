package errors

//go:generate stringer -type=ErrorType

// ErrorType 에러의 종류를 나타내는 타입입니다.
type ErrorType int

// 에러 타입 상수
const (
	// Unknown 알 수 없는 에러
	Unknown ErrorType = iota

	// Internal 내부 로직 오류 (버그 등)
	Internal

	// System 시스템 또는 인프라 오류 (디스크, 네트워크 등)
	System

	// Unauthorized 인증 실패 (로그인 필요, 토큰 만료 등)
	Unauthorized

	// Forbidden 권한 없음 (접근 권한 부족)
	Forbidden

	// InvalidInput 잘못된 입력값 (유효성 검사 실패)
	InvalidInput

	// Conflict 리소스 충돌 (중복 생성 등)
	Conflict

	// NotFound 리소스를 찾을 수 없음
	NotFound

	// ExecutionFailed 비즈니스 로직 수행 실패 (외부 프로세스 오류 등)
	ExecutionFailed

	// ParsingFailed 데이터 파싱 또는 형식 변환 실패
	ParsingFailed

	// Timeout 작업 시간 초과
	Timeout

	// Unavailable 서비스 일시적 사용 불가
	Unavailable
)
