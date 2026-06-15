package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"maritime-test-lab/clients/onboard-sync/domain"
)

// Backoff is full-jitter exponential capped at the policy cap. Seeded, so the
// draws replay; the invariant is that every draw stays within [0, cap].
func TestBackoffStaysWithinCap(t *testing.T) {
	const cap = time.Second
	p := domain.NewRetryPolicy(100*time.Millisecond, cap, 42)

	for attempt := 1; attempt <= 12; attempt++ {
		b := p.Backoff(attempt)
		assert.GreaterOrEqual(t, b, time.Duration(0), "attempt %d", attempt)
		assert.LessOrEqual(t, b, cap, "attempt %d must not exceed the cap", attempt)
	}
}

func TestBackoffEarlyAttemptsAreSmall(t *testing.T) {
	p := domain.NewRetryPolicy(100*time.Millisecond, time.Minute, 7)

	// attempt 1 ceiling is the base (100ms); the jittered draw cannot exceed it.
	for i := 0; i < 50; i++ {
		assert.LessOrEqual(t, p.Backoff(1), 100*time.Millisecond)
	}
}

// Syncer.RetryBackoff passes Backoff(retryCount), and retryCount is 0 before the
// first failure — so a non-positive attempt must clamp to attempt 1 (ceiling = the
// base), not fall through to base * 2^-1 (half the base). Observing the ceiling
// needs the max over many seeded draws: half-base would cap every draw at 50ms.
func TestBackoffClampsNonPositiveAttempt(t *testing.T) {
	const base = 100 * time.Millisecond
	p := domain.NewRetryPolicy(base, time.Minute, 7)

	var maxDraw time.Duration
	for i := 0; i < 200; i++ {
		b := p.Backoff(0)
		assert.GreaterOrEqual(t, b, time.Duration(0))
		assert.LessOrEqual(t, b, base, "attempt 0 ceiling is the base, never larger")
		if b > maxDraw {
			maxDraw = b
		}
	}

	assert.Greater(t, maxDraw, base/2, "attempt 0 must clamp to attempt 1 (base ceiling), not half of it")
}
