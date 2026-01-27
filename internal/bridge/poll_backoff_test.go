package teamsbridge

import (
	"errors"
	"net/http"
	"testing"
	"time"

	consumerclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

func TestPollBackoffSuccessResetsDelay(t *testing.T) {
	b := &PollBackoff{Failures: 3, Delay: 25 * time.Second}
	b.OnSuccess()
	if b.Failures != 0 {
		t.Fatalf("expected failures reset, got %d", b.Failures)
	}
	if b.Delay != pollBaseDelay {
		t.Fatalf("expected base delay %s, got %s", pollBaseDelay, b.Delay)
	}
}

func TestPollBackoffIdleIncreasesToCap(t *testing.T) {
	b := &PollBackoff{}
	var last time.Duration
	for i := 0; i < 50; i++ {
		b.OnIdle()
		if b.Delay < last {
			t.Fatalf("delay decreased: %s -> %s", last, b.Delay)
		}
		last = b.Delay
	}
	if b.Delay != pollIdleCap {
		t.Fatalf("expected idle cap %s, got %s", pollIdleCap, b.Delay)
	}
}

func TestPollBackoffRetryAfterOverrides(t *testing.T) {
	b := &PollBackoff{}
	b.OnFailure()
	b.OnFailure()
	b.OnRetryAfter(10 * time.Second)
	if b.Delay != 10*time.Second {
		t.Fatalf("expected retry-after delay 10s, got %s", b.Delay)
	}
}

func TestPollBackoffFailureExponentialCapped(t *testing.T) {
	b := &PollBackoff{}
	expected := []time.Duration{
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
		60 * time.Second,
		60 * time.Second,
	}
	for i, want := range expected {
		b.OnFailure()
		if b.Delay != want {
			t.Fatalf("attempt %d: expected %s, got %s", i+1, want, b.Delay)
		}
	}
}

func TestApplyPollBackoffClassification(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		b := &PollBackoff{Failures: 2, Delay: 20 * time.Second}
		delay, reason := ApplyPollBackoff(b, SyncResult{MessagesIngested: 1, Advanced: true}, nil)
		if reason != PollBackoffSuccess {
			t.Fatalf("unexpected reason: %s", reason)
		}
		if delay != pollBaseDelay || b.Delay != pollBaseDelay || b.Failures != 0 {
			t.Fatalf("unexpected backoff state: delay=%s failures=%d", b.Delay, b.Failures)
		}
	})

	t.Run("idle", func(t *testing.T) {
		b := &PollBackoff{}
		delay, reason := ApplyPollBackoff(b, SyncResult{}, nil)
		if reason != PollBackoffIdle {
			t.Fatalf("unexpected reason: %s", reason)
		}
		if delay <= pollBaseDelay {
			t.Fatalf("expected delay to increase from base, got %s", delay)
		}
	})

	t.Run("retry_after", func(t *testing.T) {
		b := &PollBackoff{}
		retryErr := consumerclient.RetryableError{Status: http.StatusTooManyRequests, RetryAfter: 7 * time.Second}
		delay, reason := ApplyPollBackoff(b, SyncResult{}, retryErr)
		if reason != PollBackoffRetryAfter {
			t.Fatalf("unexpected reason: %s", reason)
		}
		if delay != 7*time.Second {
			t.Fatalf("expected retry-after delay 7s, got %s", delay)
		}
	})

	t.Run("retryable_failure", func(t *testing.T) {
		b := &PollBackoff{}
		retryErr := consumerclient.RetryableError{Status: http.StatusServiceUnavailable}
		delay, reason := ApplyPollBackoff(b, SyncResult{}, retryErr)
		if reason != PollBackoffFailure {
			t.Fatalf("unexpected reason: %s", reason)
		}
		if delay != pollBaseDelay {
			t.Fatalf("expected base delay on first failure, got %s", delay)
		}
	})

	t.Run("client_4xx", func(t *testing.T) {
		b := &PollBackoff{Failures: 3, Delay: 10 * time.Second}
		msgErr := consumerclient.MessagesError{Status: http.StatusForbidden}
		delay, reason := ApplyPollBackoff(b, SyncResult{}, msgErr)
		if reason != PollBackoffClient4xx {
			t.Fatalf("unexpected reason: %s", reason)
		}
		if delay != pollIdleCap || b.Delay != pollIdleCap || b.Failures != 0 {
			t.Fatalf("unexpected backoff state: delay=%s failures=%d", b.Delay, b.Failures)
		}
	})

	t.Run("unknown_error", func(t *testing.T) {
		b := &PollBackoff{}
		delay, reason := ApplyPollBackoff(b, SyncResult{}, errors.New("boom"))
		if reason != PollBackoffFailure {
			t.Fatalf("unexpected reason: %s", reason)
		}
		if delay != pollBaseDelay {
			t.Fatalf("expected base delay on first failure, got %s", delay)
		}
	})
}
