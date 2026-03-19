package response

// ErrorResponse API 오류 응답
type ErrorResponse struct {
	// ResultCode HTTP 상태 코드 (예: 400, 401, 500)
	ResultCode int `json:"result_code" example:"400"`

	// Message 에러 메시지
	Message string `json:"message" example:"요청한 RSS 피드(ID:naver-cafe)를 찾을 수 없습니다."`
}
