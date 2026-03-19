package api

import (
	"sync"
	"time"
)

// RateLimiter implements per-token rate limiting.
// See Architecture Section 24.3 (Rate Limiting).
type RateLimiter struct {
	maxRPM  int
	mu      sync.Mutex
	buckets map[string]*tokenBucket
}

type tokenBucket struct {
	count     int
	resetAt   time.Time
}

// NewRateLimiter creates a rate limiter with the given requests-per-minute limit.
func NewRateLimiter(maxRPM int) *RateLimiter {
	if maxRPM == 0 {
		maxRPM = 120
	}
	return &RateLimiter{
		maxRPM:  maxRPM,
		buckets: make(map[string]*tokenBucket),
	}
}

// Allow checks if a request from the given token should be allowed.
// Returns false if the rate limit is exceeded.
func (rl *RateLimiter) Allow(token string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	bucket, exists := rl.buckets[token]

	if !exists || now.After(bucket.resetAt) {
		rl.buckets[token] = &tokenBucket{
			count:   1,
			resetAt: now.Add(1 * time.Minute),
		}
		return true
	}

	if bucket.count >= rl.maxRPM {
		return false
	}

	bucket.count++
	return true
}

// RequestsRemaining returns how many requests the token has left in the current window.
func (rl *RateLimiter) RequestsRemaining(token string) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, exists := rl.buckets[token]
	if !exists || time.Now().After(bucket.resetAt) {
		return rl.maxRPM
	}
	remaining := rl.maxRPM - bucket.count
	if remaining < 0 {
		return 0
	}
	return remaining
}

// SecondsUntilReset returns the number of seconds until the rate limit window
// resets for the given token. Returns 60 as a fallback if no bucket exists.
func (rl *RateLimiter) SecondsUntilReset(token string) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, exists := rl.buckets[token]
	if !exists {
		return 60
	}
	secs := int(time.Until(bucket.resetAt).Seconds())
	if secs < 1 {
		return 1
	}
	return secs
}
