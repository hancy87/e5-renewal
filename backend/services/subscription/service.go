package subscription

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"e5-renewal/backend/database"
	"e5-renewal/backend/models"
	"e5-renewal/backend/services/graph"
	"e5-renewal/backend/services/oauth"
)

const (
	developerE5Part = "DEVELOPERPACK_E5"
	developerE5ID   = "c42b9cae-ea4f-4ab7-9717-81576235ccac"
)

type SyncError struct {
	Code, Message string
	HTTPStatus    int
}

func (e *SyncError) Error() string { return e.Message }

type Service struct {
	OAuth *oauth.Service
	Graph *graph.Caller
	Now   func() time.Time
}

func New(o *oauth.Service, g *graph.Caller) *Service {
	return &Service{OAuth: o, Graph: g, Now: time.Now}
}

func (s *Service) Sync(ctx context.Context, account models.Account) (*models.Account, error) {
	now := s.Now().UTC()
	_ = database.Accounts.UpdateSubscriptionSync(ctx, account.ID, map[string]any{
		"subscription_sync_status":       models.SubscriptionSyncPending,
		"subscription_sync_attempted_at": now,
	})

	token, err := s.acquireToken(ctx, account)
	if err != nil {
		return nil, s.fail(ctx, account.ID, now, "token_error", err.Error(), http.StatusUnprocessableEntity)
	}
	subs, err := s.Graph.ListSubscriptions(ctx, token)
	if err != nil {
		code, status := "graph_error", http.StatusBadGateway
		var he *graph.HTTPError
		if errors.As(err, &he) {
			switch he.Status {
			case 401:
				code, status = "token_error", http.StatusUnprocessableEntity
			case 403:
				code, status = "permission_denied", http.StatusUnprocessableEntity
			case 429:
				code = "rate_limited"
			}
		}
		return nil, s.fail(ctx, account.ID, now, code, err.Error(), status)
	}

	matches := make([]graph.CompanySubscription, 0, 1)
	for _, sub := range subs {
		if sub.Status == "Enabled" && (strings.EqualFold(sub.SKUPartNumber, developerE5Part) || strings.EqualFold(sub.SKUId, developerE5ID)) {
			matches = append(matches, sub)
		}
	}
	if len(matches) == 0 {
		return nil, s.fail(ctx, account.ID, now, "e5_not_found", "활성 Microsoft 365 E5 Developer 구독을 찾지 못했습니다", http.StatusUnprocessableEntity)
	}
	if len(matches) > 1 {
		return nil, s.fail(ctx, account.ID, now, "e5_ambiguous", "Microsoft 365 E5 Developer 구독이 여러 개여서 대상을 결정할 수 없습니다", http.StatusUnprocessableEntity)
	}
	if matches[0].NextLifecycleDateTime.IsZero() {
		return nil, s.fail(ctx, account.ID, now, "lifecycle_date_missing", "구독 응답에 다음 수명 주기 날짜가 없습니다", http.StatusUnprocessableEntity)
	}

	expiry := matches[0].NextLifecycleDateTime.UTC()
	expiry = time.Date(expiry.Year(), expiry.Month(), expiry.Day(), 0, 0, 0, 0, time.UTC)
	if err := database.Accounts.UpdateSubscriptionSync(ctx, account.ID, map[string]any{
		"subscription_expires_at":        expiry,
		"subscription_expiry_source":     models.SubscriptionSourceGraph,
		"subscription_sync_status":       models.SubscriptionSyncSuccess,
		"subscription_sync_attempted_at": now,
		"subscription_synced_at":         now,
		"subscription_sync_error_code":   "",
		"subscription_sync_error":        "",
	}); err != nil {
		return nil, fmt.Errorf("save subscription expiry: %w", err)
	}
	return database.Accounts.GetByID(ctx, account.ID)
}

func (s *Service) acquireToken(ctx context.Context, account models.Account) (string, error) {
	var auth models.AuthInfoData
	if err := json.Unmarshal([]byte(account.AuthInfo), &auth); err != nil {
		return "", err
	}
	if account.AuthType == models.AuthTypeClientCredentials {
		resp, err := s.OAuth.AcquireClientToken(ctx, auth.TenantID, auth.ClientID, auth.ClientSecret)
		if err != nil {
			return "", err
		}
		return resp.AccessToken, nil
	}
	if account.AuthType != models.AuthTypeAuthCode {
		return "", fmt.Errorf("unknown auth type %q", account.AuthType)
	}
	scope := strings.Join(graph.DelegatedScopeURLs(), " ") + " offline_access"
	resp, err := s.OAuth.RefreshToken(ctx, auth.TenantID, auth.ClientID, auth.ClientSecret, auth.RefreshToken, scope)
	if err != nil {
		return "", err
	}
	if resp.RefreshToken != "" {
		auth.RefreshToken = resp.RefreshToken
		updated, _ := json.Marshal(auth)
		_ = database.Accounts.UpdateAuthInfo(ctx, account.ID, string(updated), account.AuthExpiresAt)
	}
	return resp.AccessToken, nil
}

func (s *Service) fail(ctx context.Context, id uint, attempted time.Time, code, message string, status int) error {
	message = sanitize(message)
	_ = database.Accounts.UpdateSubscriptionSync(ctx, id, map[string]any{
		"subscription_sync_status":       models.SubscriptionSyncError,
		"subscription_sync_attempted_at": attempted,
		"subscription_sync_error_code":   code,
		"subscription_sync_error":        message,
	})
	return &SyncError{Code: code, Message: message, HTTPStatus: status}
}

func sanitize(message string) string {
	message = strings.ReplaceAll(message, "\n", " ")
	if len(message) > 500 {
		message = message[:500]
	}
	return message
}
