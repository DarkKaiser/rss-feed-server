package model

// @@@@@ 전체
type NotifyMessage struct {
	ApplicationID string `json:"application_id" form:"application_id" query:"application_id"`

	Message      string `json:"message" form:"message" query:"message"`
	ErrorOccured bool   `json:"error_occured" form:"error_occured" query:"error_occured"`
}
