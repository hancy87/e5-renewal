package handlers

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"e5-renewal/backend/config"
	"e5-renewal/backend/middleware"
	"e5-renewal/backend/respond"
	"e5-renewal/backend/services/graph"
	"e5-renewal/backend/services/oauth"

	"github.com/gin-gonic/gin"
)

// delegatedScope는 auth_code 토큰 요청에 사용할 scope 문자열을 구성합니다.
// delegatedScope builds the scope string for auth_code token requests.
func delegatedScope() string {
	return strings.Join(graph.DelegatedScopeURLs(), " ") + " offline_access"
}

func RegisterOAuthRoutes(r *gin.Engine) {
	registerOAuthRoutes(r, oauth.NewService(nil))
}

func registerOAuthRoutes(r *gin.Engine, svc *oauth.Service) {
	prefix := config.Get().Server.PathPrefix

	// Microsoft OAuth 콜백은 인증 없이 호출됩니다.
	// OAuth callback from Microsoft — no auth required.
	r.GET(prefix+"/api/oauth/callback", func(c *gin.Context) {
		code := c.Query("code")
		state := c.Query("state")
		if code == "" || state == "" {
			c.Data(http.StatusBadRequest, "text/html", oauthResultHTML("error", "코드 또는 state 파라미터가 없습니다. Missing code or state parameter.", requestOrigin(c)))
			return
		}

		stateData, ok := oauth.GlobalStateStore.Consume(state)
		if !ok {
			c.Data(http.StatusBadRequest, "text/html", oauthResultHTML("error", "state가 올바르지 않거나 만료되었습니다. 다시 인증해주세요. State is invalid or expired, please re-authorize.", requestOrigin(c)))
			return
		}

		origin := originFromRedirectURI(stateData.RedirectURI, c)

		tokenResp, err := svc.ExchangeAuthCode(c.Request.Context(),
			stateData.TenantID, stateData.ClientID, stateData.ClientSecret,
			code, stateData.RedirectURI, delegatedScope())
		if err != nil {
			c.Data(http.StatusBadRequest, "text/html", oauthResultHTML("error", "인가 코드 교환에 실패했습니다. Authorization code exchange failed. "+err.Error(), origin))
			return
		}

		var tokenJSON []byte
		tokenJSON, err = json.Marshal(map[string]string{
			"refresh_token": tokenResp.RefreshToken,
			"access_token":  tokenResp.AccessToken,
		})
		if err != nil {
			c.Data(http.StatusInternalServerError, "text/html", oauthResultHTML("error", "내부 오류가 발생했습니다. Internal error.", origin))
			return
		}
		c.Data(http.StatusOK, "text/html", oauthResultHTML("success", string(tokenJSON), origin))
	})

	// 아래 OAuth 라우트는 인증이 필요합니다.
	// Auth-protected OAuth routes.
	authGroup := r.Group(prefix + "/api/oauth")
	authGroup.Use(middleware.RequireAuth())

	// 1단계: 프론트엔드가 authorize URL을 요청합니다(쿼리 파라미터에 시크릿이 노출되지 않도록 POST 사용).
	// Step 1: frontend requests authorize URL (POST to avoid leaking secret in query params).
	authGroup.POST("/authorize", func(c *gin.Context) {
		var req struct {
			ClientID     string `json:"client_id" binding:"required"`
			ClientSecret string `json:"client_secret"`
			TenantID     string `json:"tenant_id" binding:"required"`
			RedirectURI  string `json:"redirect_uri" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("client_id, tenant_id, redirect_uri는 필수입니다", "client_id, tenant_id, and redirect_uri are required"))
			return
		}

		parsed, err := url.Parse(req.RedirectURI)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			c.JSON(http.StatusBadRequest, respond.Error("redirect_uri가 올바르지 않습니다", "Invalid redirect_uri"))
			return
		}

		redirectURI := req.RedirectURI
		ttl := 5 * time.Minute

		state := oauth.GlobalStateStore.NewState(oauth.OAuthState{
			ClientID:     req.ClientID,
			ClientSecret: req.ClientSecret,
			TenantID:     req.TenantID,
			RedirectURI:  redirectURI,
			TTL:          ttl,
		})

		scopes := append([]string{"offline_access"}, graph.DelegatedScopeURLs()...)
		authorizeURL := svc.BuildAuthorizeURL(req.TenantID, req.ClientID, redirectURI, state, scopes)

		c.JSON(http.StatusOK, respond.Merge(
			gin.H{"authorize_url": authorizeURL},
			respond.Message("인가 URL을 생성했습니다", "Authorization URL generated"),
		))
	})

	// 교환 엔드포인트는 콜백 URL을 받아 코드와 토큰을 교환합니다.
	// Exchange endpoint: accepts a callback URL and exchanges the code for tokens.
	authGroup.POST("/exchange", func(c *gin.Context) {
		var req struct {
			CallbackURL string `json:"callback_url" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("콜백 URL이 올바르지 않습니다", "Invalid callback URL"))
			return
		}

		parsed, err := url.Parse(req.CallbackURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			c.JSON(http.StatusBadRequest, respond.Error("콜백 URL이 올바르지 않습니다", "Invalid callback URL"))
			return
		}

		query := parsed.Query()
		code := query.Get("code")
		state := query.Get("state")
		if code == "" || state == "" {
			c.JSON(http.StatusBadRequest, respond.Error("콜백 URL에 code 또는 state가 없습니다", "Missing code or state in callback URL"))
			return
		}

		stateData, ok := oauth.GlobalStateStore.Consume(state)
		if !ok {
			c.JSON(http.StatusBadRequest, respond.Error("state가 올바르지 않거나 만료되었습니다. 다시 인증해주세요", "State is invalid or expired, please re-authorize"))
			return
		}

		tokenResp, err := svc.ExchangeAuthCode(c.Request.Context(),
			stateData.TenantID, stateData.ClientID, stateData.ClientSecret,
			code, stateData.RedirectURI, delegatedScope())
		if err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("인가 코드 교환에 실패했습니다: "+err.Error(), "Authorization code exchange failed: "+err.Error()))
			return
		}

		c.JSON(http.StatusOK, respond.Merge(
			gin.H{
				"refresh_token": tokenResp.RefreshToken,
				"access_token":  tokenResp.AccessToken,
			},
			respond.Message("토큰 교환에 성공했습니다", "Token exchange successful"),
		))
	})
}

// originFromRedirectURI는 저장된 redirect_uri에서 origin(scheme://host)을 추출합니다.
// reverse proxy 뒤에 있더라도 실제 사용자 스킴을 우선 반영하며, 파싱에 실패하면 requestOrigin으로 대체합니다.
// originFromRedirectURI extracts the origin (scheme://host) from the stored
// redirect_uri, which reflects the real user-facing scheme even behind a
// reverse proxy. Falls back to requestOrigin if parsing fails.
func originFromRedirectURI(redirectURI string, c *gin.Context) string {
	if parsed, err := url.Parse(redirectURI); err == nil && parsed.Host != "" {
		return parsed.Scheme + "://" + parsed.Host
	}
	return requestOrigin(c)
}

// requestOrigin은 들어온 요청에서 origin(scheme://host)을 계산합니다.
// requestOrigin derives the origin (scheme://host) from the incoming request.
func requestOrigin(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, c.Request.Host)
}

// oauthResultHTML은 OAuth 결과를 opener 창으로 postMessage로 전달한 뒤 스스로 닫는 HTML을 반환합니다.
// oauthResultHTML returns an HTML page that posts the OAuth result back to the
// opener window via postMessage (using a specific origin), then closes itself.
func oauthResultHTML(status, payload, origin string) []byte {
	safeStatus, _ := json.Marshal(status)
	safePayload, _ := json.Marshal(payload)
	safeOrigin, _ := json.Marshal(origin)

	displayText := map[string]string{
		"success": "인가에 성공했습니다. Authorization successful.",
		"error":   "인가에 실패했습니다. Authorization failed: " + html.EscapeString(payload),
	}[status]

	htmlDoc := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>OAuth</title></head>
<body>
<script>
(function() {
  var result = { status: %s, payload: %s };
  if (window.opener) {
    window.opener.postMessage({ type: 'e5-oauth-result', data: result }, %s);
  }
  setTimeout(function() { window.close(); }, 500);
})();
</script>
<p style="font-family:sans-serif;text-align:center;margin-top:40px">
  %s
</p>
</body>
</html>`, safeStatus, safePayload, safeOrigin, displayText)
	return []byte(htmlDoc)
}
