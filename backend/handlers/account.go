package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"e5-renewal/backend/config"
	"e5-renewal/backend/database"
	"e5-renewal/backend/middleware"
	"e5-renewal/backend/models"
	"e5-renewal/backend/respond"
	"e5-renewal/backend/services/graph"
	"e5-renewal/backend/services/oauth"
	"e5-renewal/backend/services/scheduler"
	"e5-renewal/backend/services/subscription"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type accountRequest struct {
	Name                  string `json:"name"`
	AuthType              string `json:"auth_type"`
	ClientID              string `json:"client_id"`
	ClientSecret          string `json:"client_secret"`
	TenantID              string `json:"tenant_id"`
	RefreshToken          string `json:"refresh_token"`
	NotifyEnabled         bool   `json:"notify_enabled"`
	AuthExpiresAt         string `json:"auth_expires_at"`
	SubscriptionExpiresAt string `json:"subscription_expires_at"`
}

type scheduleResponse struct {
	Enabled        bool       `json:"enabled"`
	Paused         bool       `json:"paused"`
	PauseReason    string     `json:"pause_reason"`
	PauseThreshold int        `json:"pause_threshold"`
	NextRunAt      *time.Time `json:"next_run_at"`
	LastRunAt      *time.Time `json:"last_run_at"`
}

type accountResponse struct {
	ID                          uint              `json:"id"`
	Name                        string            `json:"name"`
	AuthType                    string            `json:"auth_type"`
	ClientID                    string            `json:"client_id"`
	ClientSecret                string            `json:"client_secret"`
	TenantID                    string            `json:"tenant_id"`
	RefreshToken                string            `json:"refresh_token"`
	NotifyEnabled               bool              `json:"notify_enabled"`
	AuthExpiresAt               string            `json:"auth_expires_at"`
	SubscriptionExpiresAt       string            `json:"subscription_expires_at"`
	SubscriptionExpirySource    string            `json:"subscription_expiry_source"`
	SubscriptionSyncStatus      string            `json:"subscription_sync_status"`
	SubscriptionSyncAttemptedAt *time.Time        `json:"subscription_sync_attempted_at"`
	SubscriptionSyncedAt        *time.Time        `json:"subscription_synced_at"`
	SubscriptionSyncErrorCode   string            `json:"subscription_sync_error_code"`
	SubscriptionSyncError       string            `json:"subscription_sync_error"`
	Health                      *float64          `json:"health"`
	TotalRuns                   int               `json:"total_runs"`
	SuccessRuns                 int               `json:"success_runs"`
	LastRun                     *time.Time        `json:"last_run"`
	Schedule                    *scheduleResponse `json:"schedule"`
	CreatedAt                   time.Time         `json:"created_at"`
	UpdatedAt                   time.Time         `json:"updated_at"`
}

type scheduleRequest struct {
	Enabled        *bool `json:"enabled"`
	PauseThreshold *int  `json:"pause_threshold"`
	Paused         *bool `json:"paused"`
}

func RegisterAccountRoutes(r *gin.Engine, sched *scheduler.Scheduler) {
	prefix := config.Get().Server.PathPrefix
	group := r.Group(prefix + "/api")
	group.Use(middleware.RequireAuth())
	group.GET("/accounts", listAccountsHandler())
	group.GET("/accounts/:id", getAccountHandler())
	group.POST("/accounts", createAccountHandler(sched))
	group.PUT("/accounts/:id", updateAccountHandler(sched))
	group.DELETE("/accounts/:id", deleteAccountHandler(sched))
	group.POST("/accounts/verify", verifyAccountHandler())
	group.POST("/accounts/:id/trigger", triggerAccountHandler(sched))
	group.POST("/accounts/:id/subscription-expiry/sync", syncSubscriptionExpiryHandler(sched))
	group.GET("/accounts/:id/schedule", getScheduleHandler())
	group.PUT("/accounts/:id/schedule", updateScheduleHandler(sched))
}

func getAccountHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		id, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("계정 ID가 올바르지 않습니다", "Invalid account ID"))
			return
		}

		account, err := database.Accounts.GetByID(ctx, uint(id))
		if err != nil {
			c.JSON(http.StatusNotFound, respond.Error("계정을 찾을 수 없습니다", "Account not found"))
			return
		}

		resp := buildAccountResponseUnmasked(ctx, *account)
		if s, err := database.Schedules.GetByAccountID(ctx, account.ID); err == nil {
			resp.Schedule = &scheduleResponse{
				Enabled:        s.Enabled,
				Paused:         s.Paused,
				PauseReason:    s.PauseReason,
				PauseThreshold: s.PauseThreshold,
				NextRunAt:      s.NextRunAt,
				LastRunAt:      s.LastRunAt,
			}
		}
		c.JSON(http.StatusOK, resp)
	}
}

func listAccountsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		accounts, err := database.Accounts.List(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, respond.Error("계정 목록을 조회하지 못했습니다", "Failed to query accounts"))
			return
		}

		result := make([]accountResponse, 0, len(accounts))
		for _, acc := range accounts {
			resp := buildAccountResponse(ctx, acc)
			if s, err := database.Schedules.GetByAccountID(ctx, acc.ID); err == nil {
				resp.Schedule = &scheduleResponse{
					Enabled:        s.Enabled,
					Paused:         s.Paused,
					PauseReason:    s.PauseReason,
					PauseThreshold: s.PauseThreshold,
					NextRunAt:      s.NextRunAt,
					LastRunAt:      s.LastRunAt,
				}
			}
			result = append(result, resp)
		}
		c.JSON(http.StatusOK, result)
	}
}

func createAccountHandler(sched *scheduler.Scheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var req accountRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("잘못된 요청입니다", "Invalid request"))
			return
		}
		if req.AuthType != models.AuthTypeAuthCode && req.AuthType != models.AuthTypeClientCredentials {
			c.JSON(http.StatusBadRequest, respond.Error("auth_type 값이 올바르지 않습니다", "Invalid auth_type"))
			return
		}

		authExpiresAt, err := parseOptionalDate(req.AuthExpiresAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("클라이언트 시크릿 만료일 형식이 올바르지 않습니다", "Invalid auth expiry date"))
			return
		}
		subscriptionExpiresAt, err := parseOptionalDate(req.SubscriptionExpiresAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("구독 만료일 형식이 올바르지 않습니다", "Invalid subscription expiry date"))
			return
		}

		authInfoJSON, _ := json.Marshal(models.AuthInfoData{
			ClientID:     req.ClientID,
			ClientSecret: req.ClientSecret,
			TenantID:     req.TenantID,
			RefreshToken: req.RefreshToken,
		})

		account := models.Account{
			Name:                   req.Name,
			AuthType:               req.AuthType,
			AuthInfo:               string(authInfoJSON),
			NotifyEnabled:          req.NotifyEnabled,
			AuthExpiresAt:          authExpiresAt,
			SubscriptionExpiresAt:  subscriptionExpiresAt,
			SubscriptionSyncStatus: models.SubscriptionSyncNever,
		}
		if subscriptionExpiresAt != nil {
			account.SubscriptionExpirySource = models.SubscriptionSourceManual
		}

		if err := database.Accounts.Create(ctx, &account); err != nil {
			c.JSON(http.StatusInternalServerError, respond.Error("계정을 생성하지 못했습니다", "Failed to create account"))
			return
		}

		_ = database.Schedules.Create(ctx, &models.Schedule{
			AccountID:      account.ID,
			Enabled:        false,
			PauseThreshold: 30,
		})
		sched.QueueSubscriptionSync(account.ID)

		c.JSON(http.StatusCreated, respond.Merge(
			gin.H{"id": account.ID},
			respond.Status("생성되었습니다", "Created"),
		))
	}
}

func updateAccountHandler(sched *scheduler.Scheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		id, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("계정 ID가 올바르지 않습니다", "Invalid account ID"))
			return
		}

		account, err := database.Accounts.GetByID(ctx, uint(id))
		if err != nil {
			c.JSON(http.StatusNotFound, respond.Error("계정을 찾을 수 없습니다", "Account not found"))
			return
		}

		var req accountRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("잘못된 요청입니다", "Invalid request"))
			return
		}
		if req.AuthType != models.AuthTypeAuthCode && req.AuthType != models.AuthTypeClientCredentials {
			c.JSON(http.StatusBadRequest, respond.Error("auth_type 값이 올바르지 않습니다", "Invalid auth_type"))
			return
		}
		authExpiresAt, err := parseOptionalDate(req.AuthExpiresAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("클라이언트 시크릿 만료일 형식이 올바르지 않습니다", "Invalid auth expiry date"))
			return
		}
		subscriptionExpiresAt, err := parseOptionalDate(req.SubscriptionExpiresAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("구독 만료일 형식이 올바르지 않습니다", "Invalid subscription expiry date"))
			return
		}

		// 제출된 시크릿이 마스킹 패턴이면 기존 값을 유지합니다.
		// If the submitted secret contains the mask pattern, preserve the existing value.
		var existingAuth models.AuthInfoData
		_ = json.Unmarshal([]byte(account.AuthInfo), &existingAuth)

		if req.ClientSecret == maskSecret(existingAuth.ClientSecret) {
			req.ClientSecret = existingAuth.ClientSecret
		}
		if req.RefreshToken == maskSecret(existingAuth.RefreshToken) {
			req.RefreshToken = existingAuth.RefreshToken
		}
		credentialsChanged := req.AuthType != account.AuthType || req.ClientID != existingAuth.ClientID || req.ClientSecret != existingAuth.ClientSecret || req.TenantID != existingAuth.TenantID || req.RefreshToken != existingAuth.RefreshToken

		authInfoJSON, _ := json.Marshal(models.AuthInfoData{
			ClientID:     req.ClientID,
			ClientSecret: req.ClientSecret,
			TenantID:     req.TenantID,
			RefreshToken: req.RefreshToken,
		})

		account.Name = req.Name
		account.AuthType = req.AuthType
		account.AuthInfo = string(authInfoJSON)
		account.NotifyEnabled = req.NotifyEnabled
		account.AuthExpiresAt = authExpiresAt
		existingSubscriptionExpiry := account.SubscriptionExpiresAt
		account.SubscriptionExpiresAt = subscriptionExpiresAt
		if !sameOptionalDate(existingSubscriptionExpiry, subscriptionExpiresAt) {
			account.SubscriptionExpirySource = ""
			if subscriptionExpiresAt != nil {
				account.SubscriptionExpirySource = models.SubscriptionSourceManual
			}
		}

		if err := database.Accounts.UpdateDetails(ctx, account); err != nil {
			c.JSON(http.StatusInternalServerError, respond.Error("계정을 수정하지 못했습니다", "Failed to update account"))
			return
		}
		if credentialsChanged {
			sched.QueueSubscriptionSync(account.ID)
		}
		c.JSON(http.StatusOK, respond.Status("수정되었습니다", "Updated"))
	}
}

func sameOptionalDate(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Format("2006-01-02") == b.Format("2006-01-02")
}

func syncSubscriptionExpiryHandler(sched *scheduler.Scheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("계정 ID가 올바르지 않습니다", "Invalid account ID"))
			return
		}
		account, err := sched.SyncSubscriptionNow(c.Request.Context(), uint(id))
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, respond.Error("계정을 찾을 수 없습니다", "Account not found"))
				return
			}
			var syncErr *subscription.SyncError
			if errors.As(err, &syncErr) {
				c.JSON(syncErr.HTTPStatus, respond.Error(syncErr.Message, syncErr.Code))
				return
			}
			c.JSON(http.StatusBadGateway, respond.Error("구독 만료일 동기화에 실패했습니다", "Subscription expiry sync failed"))
			return
		}
		c.JSON(http.StatusOK, buildAccountResponse(c.Request.Context(), *account))
	}
}

func deleteAccountHandler(sched *scheduler.Scheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		id, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("계정 ID가 올바르지 않습니다", "Invalid account ID"))
			return
		}

		if _, err := database.Accounts.GetByID(ctx, uint(id)); err != nil {
			c.JSON(http.StatusNotFound, respond.Error("계정을 찾을 수 없습니다", "Account not found"))
			return
		}

		if err := database.Accounts.DeleteCascade(ctx, uint(id)); err != nil {
			c.JSON(http.StatusInternalServerError, respond.Error("계정을 삭제하지 못했습니다", "Failed to delete account"))
			return
		}
		sched.UnregisterAccount(uint(id))
		c.JSON(http.StatusOK, respond.Status("삭제되었습니다", "Deleted"))
	}
}

type verifyRequest struct {
	AuthType     string `json:"auth_type"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	TenantID     string `json:"tenant_id"`
	RefreshToken string `json:"refresh_token"`
}

func verifyAccountHandler() gin.HandlerFunc {
	oauthSvc := oauth.NewService(nil)
	return func(c *gin.Context) {
		var req verifyRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("잘못된 요청입니다", "Invalid request"))
			return
		}

		ctx := c.Request.Context()
		switch req.AuthType {
		case models.AuthTypeAuthCode:
			if req.RefreshToken == "" {
				c.JSON(http.StatusBadRequest, respond.Error("auth_code 방식에는 refresh_token이 필요합니다", "refresh_token is required for auth_code"))
				return
			}
			scope := strings.Join(graph.DelegatedScopeURLs(), " ") + " offline_access"
			_, err := oauthSvc.RefreshToken(ctx, req.TenantID, req.ClientID, req.ClientSecret, req.RefreshToken, scope)
			if err != nil {
				c.JSON(http.StatusUnprocessableEntity, respond.Error("토큰 검증에 실패했습니다: "+err.Error(), "Token verification failed: "+err.Error()))
				return
			}
		case models.AuthTypeClientCredentials:
			_, err := oauthSvc.AcquireClientToken(ctx, req.TenantID, req.ClientID, req.ClientSecret)
			if err != nil {
				c.JSON(http.StatusUnprocessableEntity, respond.Error("클라이언트 토큰 검증에 실패했습니다: "+err.Error(), "Client token verification failed: "+err.Error()))
				return
			}
		default:
			c.JSON(http.StatusBadRequest, respond.Error("auth_type 값이 올바르지 않습니다", "Invalid auth_type"))
			return
		}

		c.JSON(http.StatusOK, respond.Status("유효합니다", "Valid"))
	}
}

func triggerAccountHandler(sched *scheduler.Scheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("계정 ID가 올바르지 않습니다", "Invalid account ID"))
			return
		}
		result, err := sched.TriggerNow(c.Request.Context(), uint(id))
		if err != nil && result == nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, respond.Error("계정을 찾을 수 없습니다", "Account not found"))
				return
			}
			c.JSON(http.StatusUnprocessableEntity, respond.Error("수동 실행에 실패했습니다: "+err.Error(), "Manual trigger failed: "+err.Error()))
			return
		}
		// 토큰 실패 같은 오류가 있더라도 기록된 실행 결과는 그대로 반환합니다.
		// Even on error (for example, token failure), return the recorded result.
		c.JSON(http.StatusOK, result)
	}
}

func getScheduleHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		id, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("계정 ID가 올바르지 않습니다", "Invalid account ID"))
			return
		}

		s, err := database.Schedules.GetByAccountID(ctx, uint(id))
		if err != nil {
			c.JSON(http.StatusNotFound, respond.Error("스케줄을 찾을 수 없습니다", "Schedule not found"))
			return
		}

		c.JSON(http.StatusOK, scheduleResponse{
			Enabled:        s.Enabled,
			Paused:         s.Paused,
			PauseReason:    s.PauseReason,
			PauseThreshold: s.PauseThreshold,
			NextRunAt:      s.NextRunAt,
			LastRunAt:      s.LastRunAt,
		})
	}
}

func updateScheduleHandler(sched *scheduler.Scheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		id, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("계정 ID가 올바르지 않습니다", "Invalid account ID"))
			return
		}

		s, err := database.Schedules.GetByAccountID(ctx, uint(id))
		if err != nil {
			c.JSON(http.StatusNotFound, respond.Error("스케줄을 찾을 수 없습니다", "Schedule not found"))
			return
		}

		var req scheduleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("잘못된 요청입니다", "Invalid request"))
			return
		}

		if req.Enabled != nil {
			s.Enabled = *req.Enabled
		}
		if req.PauseThreshold != nil {
			s.PauseThreshold = *req.PauseThreshold
		}
		if req.Paused != nil {
			s.Paused = *req.Paused
			if !*req.Paused {
				s.PauseReason = ""
			}
		}

		if s.Enabled && !s.Paused && s.NextRunAt == nil {
			nextRun := sched.ComputeNextRun()
			s.NextRunAt = &nextRun
		}

		if err := database.Schedules.Save(ctx, s); err != nil {
			c.JSON(http.StatusInternalServerError, respond.Error("스케줄을 수정하지 못했습니다", "Failed to update schedule"))
			return
		}

		sched.RegisterAccount(ctx, uint(id))
		c.JSON(http.StatusOK, respond.Status("수정되었습니다", "Updated"))
	}
}

func buildAccountResponse(ctx context.Context, acc models.Account) accountResponse {
	var authInfo models.AuthInfoData
	_ = json.Unmarshal([]byte(acc.AuthInfo), &authInfo)

	resp := accountResponse{
		ID:                          acc.ID,
		Name:                        acc.Name,
		AuthType:                    acc.AuthType,
		ClientID:                    authInfo.ClientID,
		ClientSecret:                maskSecret(authInfo.ClientSecret),
		TenantID:                    authInfo.TenantID,
		RefreshToken:                maskSecret(authInfo.RefreshToken),
		NotifyEnabled:               acc.NotifyEnabled,
		SubscriptionExpirySource:    acc.SubscriptionExpirySource,
		SubscriptionSyncStatus:      acc.SubscriptionSyncStatus,
		SubscriptionSyncAttemptedAt: acc.SubscriptionSyncAttemptedAt,
		SubscriptionSyncedAt:        acc.SubscriptionSyncedAt,
		SubscriptionSyncErrorCode:   acc.SubscriptionSyncErrorCode,
		SubscriptionSyncError:       acc.SubscriptionSyncError,
		CreatedAt:                   acc.CreatedAt,
		UpdatedAt:                   acc.UpdatedAt,
	}
	if acc.AuthExpiresAt != nil {
		resp.AuthExpiresAt = acc.AuthExpiresAt.Format("2006-01-02")
	}
	if acc.SubscriptionExpiresAt != nil {
		resp.SubscriptionExpiresAt = acc.SubscriptionExpiresAt.Format("2006-01-02")
	}

	totalEp, successEp, _ := database.TaskLogs.EndpointCountsByAccount(ctx, acc.ID)
	resp.TotalRuns = int(totalEp)
	resp.SuccessRuns = int(successEp)

	if last, err := database.TaskLogs.LastByAccount(ctx, acc.ID); err == nil {
		resp.LastRun = &last.StartedAt
	}

	resp.Health = computeHealth(ctx, acc.ID)
	return resp
}

func buildAccountResponseUnmasked(ctx context.Context, acc models.Account) accountResponse {
	var authInfo models.AuthInfoData
	_ = json.Unmarshal([]byte(acc.AuthInfo), &authInfo)

	resp := accountResponse{
		ID:                          acc.ID,
		Name:                        acc.Name,
		AuthType:                    acc.AuthType,
		ClientID:                    authInfo.ClientID,
		ClientSecret:                authInfo.ClientSecret,
		TenantID:                    authInfo.TenantID,
		RefreshToken:                authInfo.RefreshToken,
		NotifyEnabled:               acc.NotifyEnabled,
		SubscriptionExpirySource:    acc.SubscriptionExpirySource,
		SubscriptionSyncStatus:      acc.SubscriptionSyncStatus,
		SubscriptionSyncAttemptedAt: acc.SubscriptionSyncAttemptedAt,
		SubscriptionSyncedAt:        acc.SubscriptionSyncedAt,
		SubscriptionSyncErrorCode:   acc.SubscriptionSyncErrorCode,
		SubscriptionSyncError:       acc.SubscriptionSyncError,
		CreatedAt:                   acc.CreatedAt,
		UpdatedAt:                   acc.UpdatedAt,
	}
	if acc.AuthExpiresAt != nil {
		resp.AuthExpiresAt = acc.AuthExpiresAt.Format("2006-01-02")
	}
	if acc.SubscriptionExpiresAt != nil {
		resp.SubscriptionExpiresAt = acc.SubscriptionExpiresAt.Format("2006-01-02")
	}

	totalEp, successEp, _ := database.TaskLogs.EndpointCountsByAccount(ctx, acc.ID)
	resp.TotalRuns = int(totalEp)
	resp.SuccessRuns = int(successEp)

	if last, err := database.TaskLogs.LastByAccount(ctx, acc.ID); err == nil {
		resp.LastRun = &last.StartedAt
	}

	resp.Health = computeHealth(ctx, acc.ID)
	return resp
}

// maskSecret은 API 응답에 안전하게 노출할 수 있도록 시크릿 문자열을 마스킹합니다.
// maskSecret returns a masked version of a secret string for safe API responses.
func maskSecret(s string) string {
	if len(s) <= 8 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + strings.Repeat("*", 8) + s[len(s)-4:]
}

func computeHealth(ctx context.Context, accountID uint) *float64 {
	logs, _ := database.TaskLogs.Last20ByAccount(ctx, accountID)
	if len(logs) == 0 {
		return nil
	}
	var totalEp, successEp int
	for i := range logs {
		totalEp += logs[i].TotalEndpoints
		successEp += logs[i].SuccessCount
	}
	if totalEp == 0 {
		return nil
	}
	h := float64(successEp) / float64(totalEp) * 100
	return &h
}

func parseOptionalDate(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
