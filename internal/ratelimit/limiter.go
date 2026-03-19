package ratelimit

import (
	"sync"
	"time"
)

type Limiter struct {
	tokens     int
	maxTokens  int
	refillRate time.Duration
	mu         sync.Mutex
	lastRefill time.Time
}

func NewLimiter(maxTokens int, refillRate time.Duration) *Limiter {
	return &Limiter{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

func (l *Limiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.refill()

	if l.tokens > 0 {
		l.tokens--
		return true
	}

	return false
}

func (l *Limiter) Wait() {
	for {
		if l.Allow() {
			return
		}
		time.Sleep(l.refillRate / 2)
	}
}

func (l *Limiter) refill() {
	now := time.Now()
	elapsed := now.Sub(l.lastRefill)

	tokensToAdd := int(elapsed / l.refillRate)
	if tokensToAdd > 0 {
		l.tokens = min(l.tokens+tokensToAdd, l.maxTokens)
		l.lastRefill = now
	}
}

func (l *Limiter) SetMaxTokens(maxTokens int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.maxTokens = maxTokens
	if l.tokens > maxTokens {
		l.tokens = maxTokens
	}
}

func (l *Limiter) SetRefillRate(rate time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refillRate = rate
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
