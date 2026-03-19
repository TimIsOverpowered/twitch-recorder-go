package ratelimit

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewLimiter(t *testing.T) {
	l := NewLimiter(10, 1*time.Second)

	assert.Equal(t, 10, l.maxTokens)
	assert.Equal(t, 10, l.tokens)
	assert.Equal(t, 1*time.Second, l.refillRate)
}

func TestAllow_Success(t *testing.T) {
	l := NewLimiter(5, 1*time.Second)

	for i := 0; i < 5; i++ {
		assert.True(t, l.Allow(), "Should allow request %d", i+1)
	}

	assert.False(t, l.Allow(), "Should deny after max tokens")
}

func TestAllow_Refill(t *testing.T) {
	l := NewLimiter(2, 100*time.Millisecond)

	for i := 0; i < 2; i++ {
		assert.True(t, l.Allow())
	}

	assert.False(t, l.Allow(), "Should be empty after using all tokens")

	time.Sleep(150 * time.Millisecond)

	assert.True(t, l.Allow(), "Should have refilled token")
}

func TestWait(t *testing.T) {
	l := NewLimiter(1, 50*time.Millisecond)

	assert.True(t, l.Allow())
	assert.False(t, l.Allow(), "Should be empty")

	start := time.Now()
	l.Wait()
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, elapsed, 25*time.Millisecond, "Should have waited for refill")
}

func TestSetMaxTokens(t *testing.T) {
	l := NewLimiter(10, 1*time.Second)

	l.SetMaxTokens(5)
	assert.Equal(t, 5, l.maxTokens)
	assert.Equal(t, 5, l.tokens, "Should cap current tokens to new max")

	l.SetMaxTokens(20)
	assert.Equal(t, 20, l.maxTokens)
}

func TestSetRefillRate(t *testing.T) {
	l := NewLimiter(10, 1*time.Second)

	l.SetRefillRate(500 * time.Millisecond)
	assert.Equal(t, 500*time.Millisecond, l.refillRate)
}

func TestConcurrentAccess(t *testing.T) {
	l := NewLimiter(100, 1*time.Millisecond)

	var wg sync.WaitGroup
	allowed := 0
	denied := 0
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if l.Allow() {
					mu.Lock()
					allowed++
					mu.Unlock()
				} else {
					mu.Lock()
					denied++
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()

	assert.Equal(t, 100, allowed, "Should allow exactly maxTokens requests")
	assert.Greater(t, denied, 0, "Should have some denials")
}

func TestRefillMultipleTokens(t *testing.T) {
	l := NewLimiter(5, 100*time.Millisecond)

	for i := 0; i < 5; i++ {
		l.Allow()
	}

	assert.False(t, l.Allow(), "Should be empty")

	time.Sleep(350 * time.Millisecond)

	assert.True(t, l.Allow())
	assert.True(t, l.Allow())
	assert.True(t, l.Allow())
	assert.False(t, l.Allow(), "Should have 3 tokens after 350ms")
}

func TestMinFunction(t *testing.T) {
	assert.Equal(t, 3, min(3, 5))
	assert.Equal(t, 7, min(10, 7))
	assert.Equal(t, 4, min(4, 4))
}
