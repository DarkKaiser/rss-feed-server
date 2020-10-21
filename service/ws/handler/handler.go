package handler

import (
	"fmt"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/service/ws/model"
	"github.com/labstack/echo"
	"net/http"
)

//
// WebServiceHandlers
//
type WebServiceHandlers struct {
	// @@@@@
	//allowedApplications []*model.AllowedApplication
}

func NewWebServiceHandlers(config *g.AppConfig) *WebServiceHandlers {
	// @@@@@
	// 허용된 Application 목록을 구한다.
	//var applications []*model.AllowedApplication
	//for _, application := range config.NotifyAPI.Applications {
	//	applications = append(applications, &model.AllowedApplication{
	//		ID:                application.ID,
	//		Title:             application.Title,
	//		Description:       application.Description,
	//		DefaultNotifierID: application.DefaultNotifierID,
	//	})
	//}

	return &WebServiceHandlers{
		//allowedApplications: applications,
	}
}

func (h *WebServiceHandlers) RequestRSSFeedHandler(c echo.Context) error {
	// @@@@@
	m := new(model.NotifyMessage)
	if err := c.Bind(m); err != nil {
		return err
	}

	//for _, application := range h.allowedApplications {
	//	if application.ID == m.ApplicationID {
	//		h.notificationSender.Notify(application.DefaultNotifierID, application.Title, m.Message, m.ErrorOccured)
	//
	//		return c.JSON(http.StatusOK, map[string]int{
	//			"result_code": 0,
	//		})
	//	}
	//}

	return echo.NewHTTPError(http.StatusUnauthorized, fmt.Sprintf("접근이 허용되지 않은 Application입니다(ID:%s)", m.ApplicationID))
}
