package middleware

import (
	"net/http"
	"strings"

	"e5-renewal/backend/config"
	"e5-renewal/backend/respond"
	"e5-renewal/backend/services/security"

	"github.com/gin-gonic/gin"
)

// RequireAuth는 전역 jwtSecret을 사용해 Authorization 헤더의 JWT를 검증합니다.
// RequireAuth validates the JWT from the Authorization header using the global jwtSecret.
func RequireAuth() gin.HandlerFunc {
	secret := []byte(config.Get().Security.JWTSecret)
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, respond.Error("Bearer 토큰이 필요합니다", "Missing bearer token"))
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if _, err := security.ParseJWT(secret, token); err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, respond.Error("토큰이 올바르지 않습니다", "Invalid token"))
			return
		}

		c.Next()
	}
}
