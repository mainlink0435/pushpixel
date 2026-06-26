package sync

import (
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/mainLink0435/pushpixel/internal/config"
)

type Backoff struct {
	cfg     config.RetryConfig
	attempt int
	mu      sync.Mutex
}

func NewBackoff(cfg config.RetryConfig) *Backoff {
	return &Backoff{cfg: cfg}
}

func (b *Backoff) Next() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.attempt++

	delay := time.Duration(math.Pow(2, float64(b.attempt-1))) * b.cfg.BaseDelay
	if b.cfg.MaxDelay > 0 && delay > b.cfg.MaxDelay {
		delay = b.cfg.MaxDelay
	}
	if b.cfg.Jitter && delay > 0 {
		jitter := time.Duration(rand.Int63n(int64(delay / 2)))
		delay += jitter
	}

	return delay
}

func (b *Backoff) Attempt() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.attempt
}

func (b *Backoff) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.attempt = 0
}

func (b *Backoff) IsMaxed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.attempt >= b.cfg.MaxAttempts
}
