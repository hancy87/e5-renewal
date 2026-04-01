package handlers

import (
	"net/http"

	"e5-renewal/backend/config"
	"e5-renewal/backend/middleware"
	"e5-renewal/backend/respond"
	"e5-renewal/backend/services/login"
	"e5-renewal/backend/services/security"

	"github.com/gin-gonic/gin"
)

type loginRequest struct {
	Key string `json:"key"`
}

type loginResponse struct {
	Token string `json:"token"`
}

func RegisterAuthRoutes(r *gin.Engine) {
	prefix := config.Get().Server.PathPrefix
	r.POST(prefix+"/api/login", middleware.LoginRateLimit(), loginHandler())
}

func loginHandler() gin.HandlerFunc {
	jwtSecret := []byte(config.Get().Security.JWTSecret)
	loginKey := login.Key()
	return func(c *gin.Context) {
		var req loginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, respond.Error("잘못된 요청입니다", "Invalid request"))
			return
		}

		if req.Key == "" {
			c.JSON(http.StatusBadRequest, respond.Error("로그인 키를 입력해주세요", "Key is required"))
			return
		}

		if !security.VerifyPassword(loginKey, req.Key) {
			c.JSON(http.StatusUnauthorized, respond.Error("로그인 키가 올바르지 않습니다", "Invalid credentials"))
			return
		}

		token, err := security.SignJWT(jwtSecret)
		if err != nil {
			c.JSON(http.StatusInternalServerError, respond.Error("토큰을 발급하지 못했습니다", "Failed to sign token"))
			return
		}

		c.JSON(http.StatusOK, respond.Merge(
			gin.H{"token": token},
			respond.Message("로그인에 성공했습니다", "Login successful"),
		))
	}
}
