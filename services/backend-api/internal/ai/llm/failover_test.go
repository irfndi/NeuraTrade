package llm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type recordingFailoverClient struct {
	provider Provider
	model    string
}

func (c *recordingFailoverClient) Complete(_ context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	c.model = req.Model
	return &CompletionResponse{Provider: c.provider, Model: req.Model}, nil
}

func (c *recordingFailoverClient) Stream(_ context.Context, req *CompletionRequest) (<-chan StreamEvent, error) {
	c.model = req.Model
	ch := make(chan StreamEvent)
	close(ch)
	return ch, nil
}

func (c *recordingFailoverClient) Provider() Provider {
	return c.provider
}

func (c *recordingFailoverClient) Close() error {
	return nil
}

func TestFailoverClientPreservesRequestModelWhenDefaultModelConfigured(t *testing.T) {
	recorder := &recordingFailoverClient{provider: ProviderOpenAI}
	client := NewFailoverClient([]FailoverNode{{
		Client:       recorder,
		Provider:     ProviderOpenAI,
		DefaultModel: "provider-default",
	}}, 0)

	_, err := client.Complete(context.Background(), &CompletionRequest{Model: "operator-model"})

	require.NoError(t, err)
	require.Equal(t, "operator-model", recorder.model)
}

func TestFailoverClientUsesDefaultModelOnlyWhenRequestModelEmpty(t *testing.T) {
	recorder := &recordingFailoverClient{provider: ProviderOpenAI}
	client := NewFailoverClient([]FailoverNode{{
		Client:       recorder,
		Provider:     ProviderOpenAI,
		DefaultModel: "provider-default",
	}}, 0)

	_, err := client.Complete(context.Background(), &CompletionRequest{})

	require.NoError(t, err)
	require.Equal(t, "provider-default", recorder.model)
}

func TestFailoverClientModelOverrideWinsOverRequestAndDefault(t *testing.T) {
	recorder := &recordingFailoverClient{provider: ProviderOpenAI}
	client := NewFailoverClient([]FailoverNode{{
		Client:        recorder,
		Provider:      ProviderOpenAI,
		ModelOverride: "env-override",
		DefaultModel:  "provider-default",
	}}, 0)

	_, err := client.Complete(context.Background(), &CompletionRequest{Model: "operator-model"})

	require.NoError(t, err)
	require.Equal(t, "env-override", recorder.model)
}
