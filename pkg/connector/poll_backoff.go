package connector

import (
	"errors"
	"net/http"
	"time"

	consumerclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

const (
	pollBaseDelay  = 2 * time.Second
	pollIdleCap    = 30 * time.Second
	pollFailureCap = 60 * time.Second
)

// PollBackoff holds per-thread backoff state.
type PollBackoff struct {
	Failures int
	Delay    time.Duration
}

func (b *PollBackoff) ensureBaseDelay() time.Duration {
	if b.Delay <= 0 {
		b.Delay = pollBaseDelay
	}
	return b.Delay
}

// OnSuccess resets failures and returns to fast polling.
func (b *PollBackoff) OnSuccess() {
	b.Failures = 0
	b.Delay = pollBaseDelay
}

// OnIdle gently increases delay up to the idle cap.
func (b *PollBackoff) OnIdle() {
	current := b.ensureBaseDelay()
	b.Failures = 0
	next := current + pollBaseDelay
	if next > pollIdleCap {
		next = pollIdleCap
	}
	b.Delay = next
}

// OnRetryAfter sets the delay to the server-provided value when present.
func (b *PollBackoff) OnRetryAfter(d time.Duration) {
	if d <= 0 {
		b.OnFailure()
		return
	}
	b.Failures++
	b.Delay = d
}

// OnFailure performs exponential backoff up to the failure cap.
func (b *PollBackoff) OnFailure() {
	b.Failures++
	delay := pollBaseDelay
	if b.Failures > 1 {
		delay = pollBaseDelay * time.Duration(1<<(b.Failures-1))
	}
	if delay > pollFailureCap {
		delay = pollFailureCap
	}
	b.Delay = delay
}

type PollBackoffReason string

const (
	PollBackoffSuccess    PollBackoffReason = "success"
	PollBackoffIdle       PollBackoffReason = "idle"
	PollBackoffRetryAfter PollBackoffReason = "retry_after"
	PollBackoffFailure    PollBackoffReason = "failure"
	PollBackoffClient4xx  PollBackoffReason = "client_4xx"
)

func isClient4xxNon429(status int) bool {
	return status >= http.StatusBadRequest && status < http.StatusInternalServerError && status != http.StatusTooManyRequests
}

// ApplyPollBackoff classifies a poll outcome and updates the backoff.
func ApplyPollBackoff(b *PollBackoff, messagesIngested int, err error) (time.Duration, PollBackoffReason) {
	if b == nil {
		return pollBaseDelay, PollBackoffFailure
	}

	if err == nil {
		if messagesIngested > 0 {
			b.OnSuccess()
			return b.Delay, PollBackoffSuccess
		}
		b.OnIdle()
		return b.Delay, PollBackoffIdle
	}

	var retryable consumerclient.RetryableError
	if errors.As(err, &retryable) {
		if retryable.Status == http.StatusTooManyRequests && retryable.RetryAfter > 0 {
			b.OnRetryAfter(retryable.RetryAfter)
			return b.Delay, PollBackoffRetryAfter
		}
		b.OnFailure()
		return b.Delay, PollBackoffFailure
	}

	var msgErr consumerclient.MessagesError
	if errors.As(err, &msgErr) && isClient4xxNon429(msgErr.Status) {
		b.Failures = 0
		b.Delay = pollIdleCap
		return b.Delay, PollBackoffClient4xx
	}

	b.OnFailure()
	return b.Delay, PollBackoffFailure
}

