package middleware

import (
	"net/http"
	"sync"
	"time"

	"e5-renewal/backend/respond"

	"github.com/gin-gonic/gin"
)

type rateLimiter struct {
	mu      sync.Mutex
	records map[string]*rateLimitRecord
	done    chan struct{}
}

type rateLimitRecord struct {
	count       int
	windowStart time.Time
}

func newRateLimiter() *rateLimiter {
	rl := &rateLimiter{
		records: make(map[string]*rateLimitRecord),
		done:    make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

func (r *rateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-r.done:
			return
		case <-ticker.C:
			r.mu.Lock()
			now := time.Now()
			for ip, rec := range r.records {
				if now.Sub(rec.windowStart) > time.Minute {
					delete(r.records, ip)
				}
			}
			r.mu.Unlock()
		}
	}
}

// Stop은 정리(cleanup) 고루틴을 종료합니다.
// Stop terminates the cleanup goroutine.
func (r *rateLimiter) Stop() {
	close(r.done)
}

func (r *rateLimiter) allow(ip string, maxAttempts int, window time.Duration) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	rec, ok := r.records[ip]
	if !ok || now.Sub(rec.windowStart) > window {
		r.records[ip] = &rateLimitRecord{count: 1, windowStart: now}
		return true
	}
	rec.count++
	return rec.count <= maxAttempts
}

// LoginRateLimit은 로그인 엔드포인트를 제한합니다: IP당 1분에 최대 5회까지 허용합니다.
// LoginRateLimit limits the login endpoint: at most 5 attempts per IP per minute.
func LoginRateLimit() gin.HandlerFunc {
	limiter := newRateLimiter()
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !limiter.allow(ip, 5, time.Minute) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, respond.Error(
				"로그인 시도가 너무 많습니다. 잠시 후 다시 시도해주세요",
				"Too many login attempts, please try again later",
			))
			return
		}
		c.Next()
	}
}
