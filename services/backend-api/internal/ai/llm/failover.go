package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// FailoverNode defines one provider client in the failover chain.
type FailoverNode struct {
	Client        Client
	Provider      Provider
	ModelOverride string
}

// FailoverAttemptInfo captures details for the last failover attempt.
type FailoverAttemptInfo struct {
	Timestamp          time.Time `json:"timestamp"`
	PrimaryProvider    string    `json:"primary_provider"`
	AttemptedProviders []string  `json:"attempted_providers"`
	FailedProviders    []string  `json:"failed_providers"`
	SuccessProvider    string    `json:"success_provider,omitempty"`
	FailoverAttempted  bool      `json:"failover_attempted"`
	FailoverSucceeded  bool      `json:"failover_succeeded"`
	LastError          string    `json:"last_error,omitempty"`
}

// FailoverStats summarizes failover runtime behavior.
type FailoverStats struct {
	TotalRequests     int64               `json:"total_requests"`
	TotalFailures     int64               `json:"total_failures"`
	FailoverAttempts  int64               `json:"failover_attempts"`
	FailoverSuccesses int64               `json:"failover_successes"`
	FailoverFailures  int64               `json:"failover_failures"`
	LastAttempt       FailoverAttemptInfo `json:"last_attempt"`
}

// FailoverClient wraps multiple LLM clients and fails over on retryable runtime errors.
type FailoverClient struct {
	mu      sync.RWMutex
	nodes   []FailoverNode
	maxHops int
	stats   FailoverStats
}

// NewFailoverClient creates a failover client from a provider chain.
func NewFailoverClient(nodes []FailoverNode, maxHops int) *FailoverClient {
	filtered := make([]FailoverNode, 0, len(nodes))
	for _, node := range nodes {
		if node.Client == nil {
			continue
		}
		provider := node.Provider
		if provider == "" {
			provider = node.Client.Provider()
		}
		filtered = append(filtered, FailoverNode{
			Client:        node.Client,
			Provider:      provider,
			ModelOverride: strings.TrimSpace(node.ModelOverride),
		})
	}

	if maxHops < 0 {
		maxHops = 0
	}

	return &FailoverClient{
		nodes:   filtered,
		maxHops: maxHops,
	}
}

func (c *FailoverClient) Provider() Provider {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.nodes) == 0 {
		return ProviderOpenAI
	}
	return c.nodes[0].Provider
}

func (c *FailoverClient) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	c.mu.RLock()
	nodes := make([]FailoverNode, len(c.nodes))
	copy(nodes, c.nodes)
	maxHops := c.maxHops
	c.mu.RUnlock()

	if len(nodes) == 0 {
		return nil, fmt.Errorf("failover client has no configured providers")
	}

	maxProviders := maxHops + 1
	if maxProviders <= 0 || maxProviders > len(nodes) {
		maxProviders = len(nodes)
	}
	nodes = nodes[:maxProviders]

	attemptInfo := FailoverAttemptInfo{
		Timestamp:          time.Now().UTC(),
		PrimaryProvider:    string(nodes[0].Provider),
		AttemptedProviders: make([]string, 0, len(nodes)),
		FailedProviders:    make([]string, 0, len(nodes)),
	}

	var lastErr error
	for idx, node := range nodes {
		attemptInfo.AttemptedProviders = append(attemptInfo.AttemptedProviders, string(node.Provider))
		attemptReq := cloneCompletionRequest(req)
		if strings.TrimSpace(node.ModelOverride) != "" {
			attemptReq.Model = strings.TrimSpace(node.ModelOverride)
		}

		resp, err := node.Client.Complete(ctx, attemptReq)
		if err == nil {
			successProvider := node.Provider
			if resp != nil && strings.TrimSpace(string(resp.Provider)) != "" {
				successProvider = resp.Provider
			}
			attemptInfo.SuccessProvider = string(successProvider)
			attemptInfo.FailoverAttempted = idx > 0
			attemptInfo.FailoverSucceeded = idx > 0
			c.recordFailoverResult(attemptInfo, true)
			return resp, nil
		}

		lastErr = err
		attemptInfo.LastError = err.Error()
		attemptInfo.FailedProviders = append(attemptInfo.FailedProviders, string(node.Provider))

		if idx == len(nodes)-1 || !isRetryableFailoverError(err) || ctx.Err() != nil {
			break
		}
	}

	attemptInfo.FailoverAttempted = len(attemptInfo.FailedProviders) > 1 || (len(attemptInfo.FailedProviders) == 1 && len(nodes) > 1)
	attemptInfo.FailoverSucceeded = false
	c.recordFailoverResult(attemptInfo, false)

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("failover exhausted without response")
}

func (c *FailoverClient) Stream(ctx context.Context, req *CompletionRequest) (<-chan StreamEvent, error) {
	c.mu.RLock()
	nodes := make([]FailoverNode, len(c.nodes))
	copy(nodes, c.nodes)
	maxHops := c.maxHops
	c.mu.RUnlock()

	if len(nodes) == 0 {
		return nil, fmt.Errorf("failover client has no configured providers")
	}

	maxProviders := maxHops + 1
	if maxProviders <= 0 || maxProviders > len(nodes) {
		maxProviders = len(nodes)
	}
	nodes = nodes[:maxProviders]

	var lastErr error
	for idx, node := range nodes {
		attemptReq := cloneCompletionRequest(req)
		if strings.TrimSpace(node.ModelOverride) != "" {
			attemptReq.Model = strings.TrimSpace(node.ModelOverride)
		}
		stream, err := node.Client.Stream(ctx, attemptReq)
		if err == nil {
			return stream, nil
		}
		lastErr = err
		if idx == len(nodes)-1 || !isRetryableFailoverError(err) || ctx.Err() != nil {
			break
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("failover exhausted without stream")
}

func (c *FailoverClient) Close() error {
	c.mu.RLock()
	nodes := make([]FailoverNode, len(c.nodes))
	copy(nodes, c.nodes)
	c.mu.RUnlock()

	var firstErr error
	for _, node := range nodes {
		if node.Client == nil {
			continue
		}
		if err := node.Client.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Stats returns a copy of failover stats for diagnostics.
func (c *FailoverClient) Stats() FailoverStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	stats := c.stats
	stats.LastAttempt.AttemptedProviders = append([]string(nil), stats.LastAttempt.AttemptedProviders...)
	stats.LastAttempt.FailedProviders = append([]string(nil), stats.LastAttempt.FailedProviders...)
	return stats
}

func (c *FailoverClient) recordFailoverResult(attempt FailoverAttemptInfo, success bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stats.TotalRequests++
	if !success {
		c.stats.TotalFailures++
	}
	if attempt.FailoverAttempted {
		c.stats.FailoverAttempts++
		if success {
			c.stats.FailoverSuccesses++
		} else {
			c.stats.FailoverFailures++
		}
	}

	c.stats.LastAttempt = attempt
}

func cloneCompletionRequest(req *CompletionRequest) *CompletionRequest {
	if req == nil {
		return &CompletionRequest{}
	}
	copied := *req

	if req.Messages != nil {
		copied.Messages = append([]Message(nil), req.Messages...)
	}
	if req.Tools != nil {
		copied.Tools = append([]ToolDefinition(nil), req.Tools...)
	}
	if req.StopSequences != nil {
		copied.StopSequences = append([]string(nil), req.StopSequences...)
	}
	if req.Metadata != nil {
		copied.Metadata = make(map[string]string, len(req.Metadata))
		for key, value := range req.Metadata {
			copied.Metadata[key] = value
		}
	}
	if req.ResponseFormat != nil {
		responseFormatCopy := *req.ResponseFormat
		if req.ResponseFormat.JSONSchema != nil {
			responseFormatCopy.JSONSchema = make(map[string]interface{}, len(req.ResponseFormat.JSONSchema))
			for key, value := range req.ResponseFormat.JSONSchema {
				responseFormatCopy.JSONSchema[key] = value
			}
		}
		copied.ResponseFormat = &responseFormatCopy
	}

	return &copied
}

func isRetryableFailoverError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var rateLimitErr RateLimitedError
	if errors.As(err, &rateLimitErr) {
		return true
	}
	var contextLengthErr ContextLengthExceededError
	if errors.As(err, &contextLengthErr) {
		return false
	}
	var contentFilteredErr ContentFilteredError
	if errors.As(err, &contentFilteredErr) {
		return false
	}
	var apiErr ProviderAPIError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable()
	}

	if isRetryableTransportError(err) {
		return true
	}

	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "context deadline exceeded") ||
		strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "status 408") ||
		strings.Contains(lower, "status 429") ||
		strings.Contains(lower, "status 5")
}
