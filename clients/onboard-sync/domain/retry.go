package domain

import (
	"math"
	"math/rand"
	"time"
)

// RetryPolicy computes the wait between sync attempts: exponential backoff with
// full jitter, capped. Retries are infinite while an operation is queued — a
// vessel can be offline for days (D-018). The rng is seeded so a test replays
// the exact same jitter.
type RetryPolicy struct {
	base time.Duration
	cap  time.Duration
	rng  *rand.Rand
}

// NewRetryPolicy builds a policy. seed makes the jitter deterministic.
func NewRetryPolicy(base, cap time.Duration, seed int64) RetryPolicy {
	return RetryPolicy{base: base, cap: cap, rng: rand.New(rand.NewSource(seed))}
}

// Backoff returns the wait before the given attempt (1-indexed): a full-jitter
// draw from [0, min(base * 2^(attempt-1), cap)].
func (p RetryPolicy) Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	exp := float64(p.base) * math.Pow(2, float64(attempt-1))
	ceiling := time.Duration(math.Min(exp, float64(p.cap)))
	if ceiling <= 0 {
		return 0
	}
	return time.Duration(p.rng.Int63n(int64(ceiling) + 1))
}
