package api

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// exemptPaths are never rate-limited regardless of the bucket state.
var exemptPaths = map[string]bool{
	"/healthz": true,
	"/metrics": true,
}

// RateLimitMiddleware returns per-IP token bucket middleware.
// rps is the sustained rate (requests per second); burst is the bucket capacity.
// Each unique client IP gets its own limiter. Visitors not seen for 3 minutes
// are purged from the in-memory map by a background goroutine.
func RateLimitMiddleware(rps float64, burst int) func(http.Handler) http.Handler {
	type visitor struct {
		limiter  *rate.Limiter
		lastSeen time.Time
	}

	var (
		mu       sync.Mutex
		visitors = make(map[string]*visitor)
	)

	// Background goroutine purges stale visitors every minute to prevent
	// unbounded memory growth in long-running servers.
	go func() {
		for {
			time.Sleep(time.Minute)
			mu.Lock()
			for ip, v := range visitors {
				if time.Since(v.lastSeen) > 3*time.Minute {
					delete(visitors, ip)
				}
			}
			mu.Unlock()
		}
	}()

	getVisitor := func(ip string) *rate.Limiter {
		mu.Lock()
		defer mu.Unlock()
		v, ok := visitors[ip]
		if !ok {
			v = &visitor{limiter: rate.NewLimiter(rate.Limit(rps), burst)}
			visitors[ip] = v
		}
		v.lastSeen = time.Now()
		return v.limiter
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if exemptPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			// Extract host portion of RemoteAddr ("1.2.3.4:port" → "1.2.3.4").
			ip := r.RemoteAddr
			if i := strings.LastIndex(ip, ":"); i != -1 {
				ip = ip[:i]
			}

			if !getVisitor(ip).Allow() {
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
