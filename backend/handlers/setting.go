package handlers

import (
	"encoding/json"
	"net/http"

	"e5-renewal/backend/config"
	"e5-renewal/backend/database"
	"e5-renewal/backend/middleware"
	"e5-renewal/backend/models"
	"e5-renewal/backend/respond"
	"e5-renewal/backend/services/notifier"

	"github.com/gin-gonic/gin"
)

func RegisterSettingRoutes(r *gin.Engine) {
	prefix := config.Get().Server.PathPrefix
	group := r.Group(prefix + "/api")
	group.Use(middleware.RequireAuth())
	group.PUT("/settings/notification", updateNotificationSettingsHandler())
	group.GET("/settings/notification", getNotificationSettingsHandler())
	group.POST("/settings/notification/test", testNotificationSettingsHandler())
}

func updateNotificationSettingsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req models.NotificationConfig
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("잘못된 요청입니다", "Invalid request"))
			return
		}

		valueBytes, err := json.Marshal(req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, respond.Error("설정 값을 인코딩하지 못했습니다", "Failed to encode setting value"))
			return
		}

		if err := database.Settings.Upsert(c.Request.Context(), models.SettingKeyNotification, string(valueBytes)); err != nil {
			c.JSON(http.StatusInternalServerError, respond.Error("설정을 저장하지 못했습니다", "Failed to save setting"))
			return
		}

		c.JSON(http.StatusOK, respond.Status("수정되었습니다", "Updated"))
	}
}

func getNotificationSettingsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := database.Settings.Get(c.Request.Context(), models.SettingKeyNotification)
		if err != nil {
			c.JSON(http.StatusInternalServerError, respond.Error("설정을 조회하지 못했습니다", "Failed to query setting"))
			return
		}
		if raw == "" {
			c.JSON(http.StatusOK, models.NotificationConfig{
				Language:         "ko",
				ExpiryDaysBefore: 7,
				HealthThreshold:  50,
			})
			return
		}

		var value models.NotificationConfig
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			c.JSON(http.StatusInternalServerError, respond.Error("설정 값을 파싱하지 못했습니다", "Failed to parse setting value"))
			return
		}

		c.JSON(http.StatusOK, value)
	}
}

func testNotificationSettingsHandler() gin.HandlerFunc {
	svc := notifier.NewService()
	return func(c *gin.Context) {
		raw, err := database.Settings.Get(c.Request.Context(), models.SettingKeyNotification)
		if err != nil || raw == "" {
			c.JSON(http.StatusBadRequest, respond.Error("알림 설정을 찾을 수 없습니다", "Notification setting not found"))
			return
		}

		var value models.NotificationConfig
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			c.JSON(http.StatusInternalServerError, respond.Error("설정 값을 파싱하지 못했습니다", "Failed to parse setting value"))
			return
		}
		if value.URL == "" {
			c.JSON(http.StatusBadRequest, respond.Error("알림 URL이 비어 있습니다", "Notification URL is empty"))
			return
		}
		lang := value.Language
		if lang == "" {
			lang = "ko"
		}
		title, msg := notifier.FormatTest(lang)
		if err := svc.Send(value.URL, title, msg); err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("테스트 알림 전송에 실패했습니다: "+err.Error(), "Failed to send test notification: "+err.Error()))
			return
		}
		c.JSON(http.StatusOK, respond.Status("전송되었습니다", "Sent"))
	}
}
