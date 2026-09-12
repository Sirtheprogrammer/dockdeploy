package api

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type ipRateLimiter struct {
	mu       sync.Mutex
	limits   map[string][]time.Time
	maxReqs  int
	window   time.Duration
}

func newIPRateLimiter(maxReqs int, window time.Duration) *ipRateLimiter {
	limiter := &ipRateLimiter{
		limits:  make(map[string][]time.Time),
		maxReqs: maxReqs,
		window:  window,
	}

	// Housekeeping goroutine to clean old IPs
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		for range ticker.C {
			limiter.cleanup()
		}
	}()

	return limiter
}

func (l *ipRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)

	// Filter timestamps within the current window
	times := l.limits[ip]
	valid := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= l.maxReqs {
		l.limits[ip] = valid
		return false
	}

	l.limits[ip] = append(valid, now)
	return true
}

func (l *ipRateLimiter) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := time.Now().Add(-l.window)
	for ip, times := range l.limits {
		valid := times[:0]
		for _, t := range times {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		if len(valid) == 0 {
			delete(l.limits, ip)
		} else {
			l.limits[ip] = valid
		}
	}
}

// rateLimitAuth limits attempts to sensitive auth endpoints (login, setup) per IP.
func (s *Server) rateLimitAuth(next http.Handler) http.Handler {
	limiter := newIPRateLimiter(15, time.Minute)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}

		if !limiter.allow(ip) {
			w.Header().Set("Retry-After", "60")
			Wrap(s.Log, func(http.ResponseWriter, *http.Request) error {
				return RateLimited("Too many requests. Please try again later.")
			})(w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}
