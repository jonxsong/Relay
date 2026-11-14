package domain

import (
	"testing"
	"time"
)

func TestRetryDelayIsExponentialBoundedAndDeterministic(t *testing.T) {
	t.Parallel()
	policy := RetryPolicy{MaxRetries: 6, BaseDelay: time.Second, MaxDelay: 10 * time.Second, Jitter: 0.2}
	tests := []struct {
		retry int
		rand  float64
		want  time.Duration
	}{
		{retry: 1, rand: 0, want: 800 * time.Millisecond},
		{retry: 2, rand: 0.5, want: 2 * time.Second},
		{retry: 3, rand: 1, want: 4800 * time.Millisecond},
		{retry: 6, rand: 1, want: 10 * time.Second},
	}
	for _, test := range tests {
		got, err := policy.Delay(test.retry, test.rand)
		if err != nil {
			t.Fatalf("Delay(%d, %v) error = %v", test.retry, test.rand, err)
		}
		if got != test.want {
			t.Errorf("Delay(%d, %v) = %v, want %v", test.retry, test.rand, got, test.want)
		}
	}
}

func TestRetryDelayRejectsOutOfPolicyAttempt(t *testing.T) {
	t.Parallel()
	policy := RetryPolicy{MaxRetries: 2, BaseDelay: time.Second, MaxDelay: time.Minute}
	if _, err := policy.Delay(3, 0.5); err == nil {
		t.Fatal("Delay() error = nil, want error")
	}
}
