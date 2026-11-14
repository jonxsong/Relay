package domain

import (
	"errors"
	"math"
	"time"
)

type RetryPolicy struct {
	MaxRetries int           `json:"max_retries"`
	BaseDelay  time.Duration `json:"base_delay"`
	MaxDelay   time.Duration `json:"max_delay"`
	Jitter     float64       `json:"jitter"`
}

func (p RetryPolicy) Validate() error {
	if p.MaxRetries < 0 || p.MaxRetries > 100 {
		return errors.New("max retries must be between 0 and 100")
	}
	if p.BaseDelay <= 0 {
		return errors.New("base delay must be positive")
	}
	if p.MaxDelay < p.BaseDelay {
		return errors.New("max delay must be at least the base delay")
	}
	if p.Jitter < 0 || p.Jitter > 1 {
		return errors.New("jitter must be between 0 and 1")
	}
	return nil
}

// Delay returns the bounded exponential delay for a one-based retry number.
// randomUnit must be in [0, 1] and allows callers to inject deterministic tests.
func (p RetryPolicy) Delay(retryNumber int, randomUnit float64) (time.Duration, error) {
	if err := p.Validate(); err != nil {
		return 0, err
	}
	if retryNumber < 1 || retryNumber > p.MaxRetries {
		return 0, errors.New("retry number is outside policy")
	}
	if randomUnit < 0 || randomUnit > 1 || math.IsNaN(randomUnit) {
		return 0, errors.New("random unit must be between 0 and 1")
	}

	delay := p.BaseDelay
	for i := 1; i < retryNumber && delay < p.MaxDelay; i++ {
		if delay > p.MaxDelay/2 {
			delay = p.MaxDelay
			break
		}
		delay *= 2
	}
	if delay > p.MaxDelay {
		delay = p.MaxDelay
	}

	if p.Jitter == 0 {
		return delay, nil
	}
	factor := 1 - p.Jitter + (2 * p.Jitter * randomUnit)
	jittered := time.Duration(float64(delay) * factor)
	if jittered > p.MaxDelay {
		return p.MaxDelay, nil
	}
	if jittered < 0 {
		return 0, nil
	}
	return jittered, nil
}
