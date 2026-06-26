package sync

import (
	"math"
	"testing"
	"time"

	"github.com/mainLink0435/pushpixel/internal/config"
)

func TestBackoff_InitialState(t *testing.T) {
	b := NewBackoff(config.RetryConfig{
		MaxAttempts: 5,
		BaseDelay:   time.Second,
		MaxDelay:    30 * time.Second,
		Jitter:      false,
	})
	if b.Attempt() != 0 {
		t.Errorf("expected 0 attempts, got %d", b.Attempt())
	}
	if b.IsMaxed() {
		t.Fatal("should not be maxed initially")
	}
}

func TestBackoff_Exponential(t *testing.T) {
	b := NewBackoff(config.RetryConfig{
		MaxAttempts: 10,
		BaseDelay:   time.Second,
		MaxDelay:    30 * time.Second,
		Jitter:      false,
	})

	expected := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second, // capped at max
		30 * time.Second,
		30 * time.Second,
		30 * time.Second,
		30 * time.Second,
	}

	for i, exp := range expected {
		got := b.Next()
		if got != exp {
			t.Errorf("attempt %d: expected %v, got %v", i+1, exp, got)
		}
	}
}

func TestBackoff_IsMaxed(t *testing.T) {
	b := NewBackoff(config.RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   time.Second,
		MaxDelay:    10 * time.Second,
		Jitter:      false,
	})

	for i := 0; i < 3; i++ {
		if b.IsMaxed() {
			t.Errorf("should not be maxed at attempt %d", i+1)
		}
		b.Next()
	}

	if !b.IsMaxed() {
		t.Fatal("should be maxed after 3 attempts")
	}
}

func TestBackoff_Reset(t *testing.T) {
	b := NewBackoff(config.RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   time.Second,
		MaxDelay:    10 * time.Second,
		Jitter:      false,
	})

	b.Next()
	b.Next()
	b.Next()

	if !b.IsMaxed() {
		t.Fatal("should be maxed")
	}

	b.Reset()
	if b.IsMaxed() {
		t.Fatal("should not be maxed after reset")
	}
	if b.Attempt() != 0 {
		t.Errorf("expected 0 after reset, got %d", b.Attempt())
	}
}

func TestBackoff_JitterRange(t *testing.T) {
	for i := 0; i < 50; i++ {
		b := NewBackoff(config.RetryConfig{
			MaxAttempts: 5,
			BaseDelay:   10 * time.Second,
			MaxDelay:    30 * time.Second,
			Jitter:      true,
		})

		got := b.Next()
		if got < 10*time.Second {
			t.Errorf("jitter should not reduce below base: got %v", got)
		}
		if got > 30*time.Second+30*time.Second/2 {
			t.Errorf("delay too high: got %v", got)
		}
	}
}

func TestBackoff_ZeroAttempts(t *testing.T) {
	b := NewBackoff(config.RetryConfig{
		MaxAttempts: 0,
		BaseDelay:   time.Second,
		MaxDelay:    30 * time.Second,
		Jitter:      false,
	})

	if !b.IsMaxed() {
		t.Fatal("should be maxed with 0 attempts")
	}
}

func TestBackoff_NoMaxDelay(t *testing.T) {
	b := NewBackoff(config.RetryConfig{
		MaxAttempts: 10,
		BaseDelay:   time.Second,
		MaxDelay:    0,
		Jitter:      false,
	})

	for i := 0; i < 10; i++ {
		got := b.Next()
		expected := time.Duration(math.Pow(2, float64(i))) * time.Second
		if got != expected {
			t.Errorf("attempt %d: expected %v, got %v", i+1, expected, got)
		}

		// Verify not capped
		if got > 0 && got < time.Duration(math.Pow(2, float64(i)))*time.Second {
			t.Errorf("attempt %d: unexpectedly capped at %v", i+1, got)
		}
	}
}
