package subscription_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"e5-renewal/backend/database"
	"e5-renewal/backend/models"
	"e5-renewal/backend/services/graph"
	"e5-renewal/backend/services/oauth"
	"e5-renewal/backend/services/subscription"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rewriteTransport struct{ target string }

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	target := strings.TrimPrefix(t.target, "http://")
	req.URL.Scheme, req.URL.Host = "http", target
	return http.DefaultTransport.RoundTrip(req)
}

func setupService(t *testing.T, graphStatus int, graphBody string) (*subscription.Service, models.Account, context.Context) {
	t.Helper()
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-token","expires_in":3600}`))
	}))
	t.Cleanup(tokenServer.Close)
	graphServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(graphStatus)
		_, _ = w.Write([]byte(graphBody))
	}))
	t.Cleanup(graphServer.Close)

	dbPath := fmt.Sprintf("%s/test.db", t.TempDir())
	require.NoError(t, database.Init(dbPath))
	database.MustInitEncryption("subscription-test-encryption-key")
	ctx := context.Background()
	account := models.Account{Name: "sync-account", AuthType: models.AuthTypeClientCredentials, AuthInfo: `{"client_id":"cid","client_secret":"secret","tenant_id":"tenant"}`, SubscriptionSyncStatus: models.SubscriptionSyncNever}
	require.NoError(t, database.Accounts.Create(ctx, &account))
	client := &http.Client{Transport: &rewriteTransport{target: tokenServer.URL}}
	graphClient := &http.Client{Transport: &rewriteTransport{target: graphServer.URL}}
	svc := subscription.New(oauth.NewService(client), &graph.Caller{HTTPClient: graphClient})
	svc.Now = func() time.Time { return time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC) }
	return svc, account, ctx
}

func TestSyncStoresDeveloperE5Expiry(t *testing.T) {
	svc, account, ctx := setupService(t, http.StatusOK, `{"value":[{"skuId":"c42b9cae-ea4f-4ab7-9717-81576235ccac","skuPartNumber":"DEVELOPERPACK_E5","status":"Enabled","nextLifecycleDateTime":"2026-11-10T18:30:00Z"}]}`)
	updated, err := svc.Sync(ctx, account)
	require.NoError(t, err)
	require.NotNil(t, updated.SubscriptionExpiresAt)
	assert.Equal(t, "2026-11-10", updated.SubscriptionExpiresAt.Format("2006-01-02"))
	assert.Equal(t, models.SubscriptionSourceGraph, updated.SubscriptionExpirySource)
	assert.Equal(t, models.SubscriptionSyncSuccess, updated.SubscriptionSyncStatus)
	assert.Empty(t, updated.SubscriptionSyncError)
}

func TestSyncFailurePreservesManualExpiry(t *testing.T) {
	svc, account, ctx := setupService(t, http.StatusForbidden, `{"error":{"code":"Authorization_RequestDenied"}}`)
	manual := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, database.Accounts.UpdateSubscriptionSync(ctx, account.ID, map[string]any{"subscription_expires_at": manual, "subscription_expiry_source": models.SubscriptionSourceManual}))

	_, err := svc.Sync(ctx, account)
	require.Error(t, err)
	stored, getErr := database.Accounts.GetByID(ctx, account.ID)
	require.NoError(t, getErr)
	require.NotNil(t, stored.SubscriptionExpiresAt)
	assert.Equal(t, "2026-12-01", stored.SubscriptionExpiresAt.Format("2006-01-02"))
	assert.Equal(t, models.SubscriptionSourceManual, stored.SubscriptionExpirySource)
	assert.Equal(t, models.SubscriptionSyncError, stored.SubscriptionSyncStatus)
	assert.Equal(t, "permission_denied", stored.SubscriptionSyncErrorCode)
}
