package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

var ErrRetryBodyMissing = errors.New("request body cannot be retried without GetBody")

// RetryableError marks a retryable outcome from a response classifier.
type RetryableError struct {
	Status     int
	RetryAfter time.Duration
	Cause      error
}

func (e RetryableError) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "teams request retryable error"
}

// RequestMeta carries request identifiers for executor logging.
type RequestMeta struct {
	ThreadID        string
	ClientMessageID string
}

type requestMetaKey struct{}

func WithRequestMeta(ctx context.Context, meta RequestMeta) context.Context {
	return context.WithValue(ctx, requestMetaKey{}, meta)
}

func requestMetaFromContext(ctx context.Context) (RequestMeta, bool) {
	meta, ok := ctx.Value(requestMetaKey{}).(RequestMeta)
	return meta, ok
}

// TeamsRequestExecutor wraps HTTP calls with retry/backoff behavior.
type TeamsRequestExecutor struct {
	HTTP        *http.Client
	Log         zerolog.Logger
	MaxRetries  int
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	sleep       func(ctx context.Context, d time.Duration) error
	jitter      func(d time.Duration) time.Duration
	mu          sync.Mutex
}

func (e *TeamsRequestExecutor) Do(ctx context.Context, req *http.Request, classify func(*http.Response) error) (*http.Response, error) {
	if e == nil || e.HTTP == nil {
		return nil, ErrMissingHTTPClient
	}
	if req == nil {
		return nil, errors.New("missing request")
	}
	if classify == nil {
		return nil, errors.New("missing response classifier")
	}
	maxRetries := e.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	baseBackoff := e.BaseBackoff
	if baseBackoff <= 0 {
		baseBackoff = 500 * time.Millisecond
	}
	maxBackoff := e.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = 10 * time.Second
	}

	var retries int
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		attempt := retries + 1
		attemptReq, err := requestForAttempt(ctx, req, attempt)
		if err != nil {
			return nil, err
		}
		resp, err := e.HTTP.Do(attemptReq)
		if err != nil {
			if isRetryableNetworkError(ctx, err) && retries < maxRetries {
				retries++
				backoff := e.computeBackoff(baseBackoff, maxBackoff, retries)
				logRetry(e.logWithMeta(ctx), attempt+1, 0, 0)
				logBackoff(e.logWithMeta(ctx), attempt+1, backoff)
				if err := e.sleepWithContext(ctx, backoff); err != nil {
					return nil, err
				}
				continue
			}
			logFailure(e.logWithMeta(ctx), attempt, err)
			return nil, err
		}

		classifyErr := classify(resp)
		if classifyErr == nil {
			if retries > 0 {
				logSuccess(e.logWithMeta(ctx), retries+1)
			}
			return resp, nil
		}

		retryable := RetryableError{}
		if errors.As(classifyErr, &retryable) && retries < maxRetries {
			retries++
			backoff := retryable.RetryAfter
			if backoff <= 0 {
				backoff = e.computeBackoff(baseBackoff, maxBackoff, retries)
			}
			logRetry(e.logWithMeta(ctx), attempt+1, retryable.Status, backoff)
			logBackoff(e.logWithMeta(ctx), attempt+1, backoff)
			drainAndClose(resp)
			if err := e.sleepWithContext(ctx, backoff); err != nil {
				return nil, err
			}
			continue
		}

		logFailure(e.logWithMeta(ctx), retries+1, classifyErr)
		return resp, classifyErr
	}
}

func requestForAttempt(ctx context.Context, req *http.Request, attempt int) (*http.Request, error) {
	if attempt == 1 && req.GetBody == nil {
		return req, nil
	}
	if req.GetBody == nil && req.Body != nil {
		return nil, ErrRetryBodyMissing
	}
	cloned := req.Clone(ctx)
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		cloned.Body = body
	}
	return cloned, nil
}

func (e *TeamsRequestExecutor) computeBackoff(base time.Duration, max time.Duration, retry int) time.Duration {
	backoff := base
	if retry > 1 {
		backoff = base * (1 << (retry - 1))
	}
	if backoff > max {
		backoff = max
	}
	return e.applyJitter(backoff)
}

func (e *TeamsRequestExecutor) applyJitter(backoff time.Duration) time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.jitter != nil {
		return e.jitter(backoff)
	}
	return time.Duration(float64(backoff) * (0.5 + rand.Float64()))
}

func (e *TeamsRequestExecutor) sleepWithContext(ctx context.Context, d time.Duration) error {
	if e.sleep != nil {
		return e.sleep(ctx, d)
	}
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func isRetryableNetworkError(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ctx.Err() == nil
	}
	if isTLSError(err) || isDNSError(err) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}
	return false
}

func isDNSError(err error) bool {
	unwrapped := unwrapURLError(err)
	var dnsErr *net.DNSError
	return errors.As(unwrapped, &dnsErr)
}

func isTLSError(err error) bool {
	unwrapped := unwrapURLError(err)
	var recordErr tls.RecordHeaderError
	if errors.As(unwrapped, &recordErr) {
		return true
	}
	var certInvalid x509.CertificateInvalidError
	if errors.As(unwrapped, &certInvalid) {
		return true
	}
	var unknownAuth x509.UnknownAuthorityError
	if errors.As(unwrapped, &unknownAuth) {
		return true
	}
	var hostErr x509.HostnameError
	if errors.As(unwrapped, &hostErr) {
		return true
	}
	return false
}

func unwrapURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

func (e *TeamsRequestExecutor) logWithMeta(ctx context.Context) zerolog.Logger {
	logger := e.Log
	if meta, ok := requestMetaFromContext(ctx); ok {
		if meta.ThreadID != "" {
			logger = logger.With().Str("thread_id", meta.ThreadID).Logger()
		}
		if meta.ClientMessageID != "" {
			logger = logger.With().Str("client_message_id", meta.ClientMessageID).Logger()
		}
	}
	return logger
}

func logRetry(logger zerolog.Logger, attempt int, status int, retryAfter time.Duration) {
	event := logger.Warn().Int("attempt", attempt)
	if status != 0 {
		event = event.Int("status", status)
	}
	if retryAfter > 0 {
		event = event.Dur("retry_after", retryAfter)
	}
	event.Msg("teams send retry")
}

func logBackoff(logger zerolog.Logger, attempt int, backoff time.Duration) {
	logger.Info().Int("attempt", attempt).Dur("duration", backoff).Msg("teams send backoff")
}

func logSuccess(logger zerolog.Logger, attempts int) {
	logger.Info().Int("attempts", attempts).Msg("teams send succeeded")
}

func logFailure(logger zerolog.Logger, attempts int, err error) {
	logger.Warn().Int("attempts", attempts).Err(err).Msg("teams send failed")
}
