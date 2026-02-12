package connector

import (
	"net/http"
	"testing"
	"time"

	consumerclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

func TestPollBackoffSuccessResetsDelay(t *testing.T) {
	b := &PollBackoff{Failures: 3, Delay: 30 * time.Second}
	b.OnSuccess()
	if b.Failures != 0 {
		t.Fatalf("expected failures reset, got %d", b.Failures)
	}
	if b.Delay != pollBaseDelay {
		t.Fatalf("expected delay %v, got %v", pollBaseDelay, b.Delay)
	}
}

func TestPollBackoffIdleIncreasesToCap(t *testing.T) {
	b := &PollBackoff{}
	for i := 0; i < 100; i++ {
		b.OnIdle()
		if b.Delay > pollIdleCap {
			t.Fatalf("delay exceeded cap: %v", b.Delay)
		}
	}
	if b.Delay != pollIdleCap {
		t.Fatalf("expected delay capped at %v, got %v", pollIdleCap, b.Delay)
	}
}

func TestPollBackoffRetryAfterOverrides(t *testing.T) {
	b := &PollBackoff{Failures: 2, Delay: 30 * time.Second}
	b.OnRetryAfter(10 * time.Second)
	if b.Delay != 10*time.Second {
		t.Fatalf("expected delay 10s, got %v", b.Delay)
	}
	if b.Failures != 3 {
		t.Fatalf("expected failures incremented, got %d", b.Failures)
	}
}

func TestPollBackoffFailureExponentialCapped(t *testing.T) {
	b := &PollBackoff{}
	for i := 0; i < 20; i++ {
		b.OnFailure()
		if b.Delay > pollFailureCap {
			t.Fatalf("delay exceeded cap: %v", b.Delay)
		}
	}
	if b.Delay != pollFailureCap {
		t.Fatalf("expected delay capped at %v, got %v", pollFailureCap, b.Delay)
	}
}

func TestApplyPollBackoffClassification(t *testing.T) {
	b := &PollBackoff{}

	delay, reason := ApplyPollBackoff(b, 1, nil)
	if reason != PollBackoffSuccess || delay != pollBaseDelay {
		t.Fatalf("success: expected (%v,%q), got (%v,%q)", pollBaseDelay, PollBackoffSuccess, delay, reason)
	}

	delay, reason = ApplyPollBackoff(b, 0, nil)
	if reason != PollBackoffIdle || delay <= pollBaseDelay {
		t.Fatalf("idle: expected reason=%q and delay>%v, got (%v,%q)", PollBackoffIdle, pollBaseDelay, delay, reason)
	}

	delay, reason = ApplyPollBackoff(b, 0, consumerclient.MessagesError{Status: http.StatusForbidden})
	if reason != PollBackoffClient4xx || delay != pollIdleCap {
		t.Fatalf("client4xx: expected (%v,%q), got (%v,%q)", pollIdleCap, PollBackoffClient4xx, delay, reason)
	}

	delay, reason = ApplyPollBackoff(b, 0, consumerclient.RetryableError{Status: http.StatusTooManyRequests, RetryAfter: 11 * time.Second})
	if reason != PollBackoffRetryAfter || delay != 11*time.Second {
		t.Fatalf("retry-after: expected (11s,%q), got (%v,%q)", PollBackoffRetryAfter, delay, reason)
	}

	delay, reason = ApplyPollBackoff(b, 0, consumerclient.RetryableError{Status: http.StatusBadGateway})
	if reason != PollBackoffFailure || delay <= 0 {
		t.Fatalf("failure: expected reason=%q and delay>0, got (%v,%q)", PollBackoffFailure, delay, reason)
	}
}

