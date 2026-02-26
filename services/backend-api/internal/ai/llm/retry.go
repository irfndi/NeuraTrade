package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAIRetryBaseDelay = 400 * time.Millisecond
	defaultAIRetryMaxDelay  = 3 * time.Second
)

type transportRetryPolicy struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

type httpAttemptResult struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

func resolveTransportRetryPolicy(maxRetries int) transportRetryPolicy {
	if maxRetries < 0 {
		maxRetries = 0
	}

	baseDelay := defaultAIRetryBaseDelay
	if raw := strings.TrimSpace(os.Getenv("NEURATRADE_AI_RETRY_BASE_MS")); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
			baseDelay = time.Duration(ms) * time.Millisecond
		}
	}

	maxDelay := defaultAIRetryMaxDelay
	if raw := strings.TrimSpace(os.Getenv("NEURATRADE_AI_RETRY_MAX_MS")); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
			maxDelay = time.Duration(ms) * time.Millisecond
		}
	}

	if maxDelay < baseDelay {
		maxDelay = baseDelay
	}

	return transportRetryPolicy{
		MaxRetries: maxRetries,
		BaseDelay:  baseDelay,
		MaxDelay:   maxDelay,
	}
}

func (p transportRetryPolicy) backoffForRetry(retryCount int, retryAfter time.Duration) time.Duration {
	delay := p.BaseDelay
	if retryCount > 1 {
		for i := 1; i < retryCount; i++ {
			delay *= 2
			if delay >= p.MaxDelay {
				delay = p.MaxDelay
				break
			}
		}
	}

	if retryAfter > delay {
		delay = retryAfter
	}
	if delay > p.MaxDelay {
		delay = p.MaxDelay
	}
	if delay < 0 {
		delay = p.BaseDelay
	}
	return delay
}

func executeWithTransportRetry(ctx context.Context, policy transportRetryPolicy, send func(context.Context) (*http.Response, error)) (*httpAttemptResult, error) {
	var lastErr error

	for attempt := 0; ; attempt++ {
		resp, err := send(ctx)
		if err != nil {
			lastErr = err
			if attempt >= policy.MaxRetries || !isRetryableTransportError(err) {
				return nil, err
			}

			delay := policy.backoffForRetry(attempt+1, 0)
			if waitErr := waitForRetry(ctx, delay); waitErr != nil {
				return nil, waitErr
			}
			continue
		}

		result, readErr := buildHTTPAttemptResult(resp)
		if readErr != nil {
			lastErr = readErr
			if attempt >= policy.MaxRetries || !isRetryableTransportError(readErr) {
				return nil, readErr
			}

			delay := policy.backoffForRetry(attempt+1, 0)
			if waitErr := waitForRetry(ctx, delay); waitErr != nil {
				return nil, waitErr
			}
			continue
		}

		if !isRetryableHTTPStatus(result.StatusCode) || attempt >= policy.MaxRetries {
			return result, nil
		}

		retryAfter := parseRetryAfterHeader(result.Headers.Get("Retry-After"))
		delay := policy.backoffForRetry(attempt+1, retryAfter)
		if waitErr := waitForRetry(ctx, delay); waitErr != nil {
			return nil, waitErr
		}
	}

	// Should never be reached.
	return nil, fmt.Errorf("transport retry exhausted: %w", lastErr)
}

func buildHTTPAttemptResult(resp *http.Response) (*httpAttemptResult, error) {
	if resp == nil {
		return nil, fmt.Errorf("nil HTTP response")
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	headers := make(http.Header, len(resp.Header))
	for key, values := range resp.Header {
		copied := make([]string, len(values))
		copy(copied, values)
		headers[key] = copied
	}

	return &httpAttemptResult{
		StatusCode: resp.StatusCode,
		Headers:    headers,
		Body:       body,
	}, nil
}

func isRetryableHTTPStatus(statusCode int) bool {
	return statusCode == http.StatusRequestTimeout ||
		statusCode == http.StatusTooManyRequests ||
		(statusCode >= http.StatusInternalServerError && statusCode <= 599)
}

func isRetryableTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return true
		}
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "temporarily unavailable") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "server closed idle connection") ||
		strings.Contains(msg, "tls handshake timeout")
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func parseRetryAfterHeader(raw string) time.Duration {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}

	if retryAt, err := http.ParseTime(value); err == nil {
		delay := time.Until(retryAt)
		if delay > 0 {
			return delay
		}
	}

	return 0
}
