package response

// SuccessResponse API 성공 응답
type SuccessResponse struct {
	// ResultCode 처리 결과 코드 (0: 성공)
	ResultCode int `json:"result_code" example:"0"`

	// Message 성공 메시지
	Message string `json:"message" example:"성공"`
}
