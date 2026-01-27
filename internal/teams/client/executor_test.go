package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestExecutorRetryAfter(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		if call == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sleepDurations := []time.Duration{}
	exec := &TeamsRequestExecutor{
		HTTP:        server.Client(),
		MaxRetries:  2,
		BaseBackoff: 100 * time.Millisecond,
		MaxBackoff:  time.Second,
		sleep: func(ctx context.Context, d time.Duration) error {
			sleepDurations = append(sleepDurations, d)
			return nil
		},
		jitter: func(d time.Duration) time.Duration { return d },
	}

	body := []byte(`{"hello":"world"}`)
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}

	_, err = exec.Do(ctx, req, func(resp *http.Response) error {
		if resp.StatusCode == http.StatusTooManyRequests {
			value := resp.Header.Get("Retry-After")
			if value != "" {
				seconds, _ := strconv.Atoi(value)
				return RetryableError{Status: resp.StatusCode, RetryAfter: time.Duration(seconds) * time.Second}
			}
			return RetryableError{Status: resp.StatusCode}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}
	if len(sleepDurations) != 1 {
		t.Fatalf("expected one backoff, got %d", len(sleepDurations))
	}
	if sleepDurations[0] != 2*time.Second {
		t.Fatalf("unexpected retry-after backoff: %s", sleepDurations[0])
	}
}

func TestExecutorExponentialBackoff(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		if call <= 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var sleeps []time.Duration
	exec := &TeamsRequestExecutor{
		HTTP:        server.Client(),
		MaxRetries:  3,
		BaseBackoff: 100 * time.Millisecond,
		MaxBackoff:  250 * time.Millisecond,
		sleep: func(ctx context.Context, d time.Duration) error {
			sleeps = append(sleeps, d)
			return nil
		},
		jitter: func(d time.Duration) time.Duration { return d },
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	_, err = exec.Do(context.Background(), req, func(resp *http.Response) error {
		if resp.StatusCode == http.StatusTooManyRequests {
			return RetryableError{Status: resp.StatusCode}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}
	if len(sleeps) != 3 {
		t.Fatalf("expected 3 backoffs, got %d", len(sleeps))
	}
	if sleeps[0] != 100*time.Millisecond || sleeps[1] != 200*time.Millisecond || sleeps[2] != 250*time.Millisecond {
		t.Fatalf("unexpected backoff sequence: %v", sleeps)
	}
}

func TestExecutorRetriesOn5xx(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	exec := &TeamsRequestExecutor{
		HTTP:       server.Client(),
		MaxRetries: 2,
		sleep:      func(ctx context.Context, d time.Duration) error { return nil },
		jitter:     func(d time.Duration) time.Duration { return d },
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	_, err = exec.Do(context.Background(), req, func(resp *http.Response) error {
		if resp.StatusCode >= http.StatusInternalServerError {
			return RetryableError{Status: resp.StatusCode}
		}
		return nil
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if calls != 3 {
		t.Fatalf("expected 3 attempts, got %d", calls)
	}
}

func TestExecutorNoRetryOn4xx(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	exec := &TeamsRequestExecutor{
		HTTP:       server.Client(),
		MaxRetries: 3,
		sleep:      func(ctx context.Context, d time.Duration) error { return nil },
		jitter:     func(d time.Duration) time.Duration { return d },
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	_, err = exec.Do(context.Background(), req, func(resp *http.Response) error {
		if resp.StatusCode == http.StatusBadRequest {
			return errors.New("permanent")
		}
		return nil
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if calls != 1 {
		t.Fatalf("expected 1 attempt, got %d", calls)
	}
}

func TestExecutorContextCancelStopsRetries(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	exec := &TeamsRequestExecutor{
		HTTP:       server.Client(),
		MaxRetries: 3,
		sleep: func(ctx context.Context, d time.Duration) error {
			cancel()
			return ctx.Err()
		},
		jitter: func(d time.Duration) time.Duration { return d },
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	_, err = exec.Do(ctx, req, func(resp *http.Response) error {
		return RetryableError{Status: resp.StatusCode}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 attempt, got %d", calls)
	}
}

func TestExecutorRetryableNetworkError(t *testing.T) {
	var calls int32
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return nil, timeoutErr{}
	})}

	exec := &TeamsRequestExecutor{
		HTTP:       client,
		MaxRetries: 1,
		sleep:      func(ctx context.Context, d time.Duration) error { return nil },
		jitter:     func(d time.Duration) time.Duration { return d },
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.invalid", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	_, err = exec.Do(context.Background(), req, func(resp *http.Response) error {
		return nil
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if calls != 2 {
		t.Fatalf("expected 2 attempts, got %d", calls)
	}
}

func TestExecutorNoRetryOnDNSError(t *testing.T) {
	var calls int32
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return nil, &net.DNSError{Err: "no such host", Name: "example.invalid"}
	})}

	exec := &TeamsRequestExecutor{
		HTTP:       client,
		MaxRetries: 2,
		sleep:      func(ctx context.Context, d time.Duration) error { return nil },
		jitter:     func(d time.Duration) time.Duration { return d },
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.invalid", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	_, err = exec.Do(context.Background(), req, func(resp *http.Response) error {
		return nil
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if calls != 1 {
		t.Fatalf("expected 1 attempt, got %d", calls)
	}
}
