package agentcontrol

import (
	"context"
	"strings"
	"testing"
)

func TestAgentRuntimeStartValidatesRequiredDependencies(t *testing.T) {
	t.Parallel()

	runtime := NewAgentRuntime(AgentRuntimeConfig{})
	err := runtime.Start(context.Background())
	if err == nil {
		t.Fatal("expected validation error")
	}

	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing dependency error, got: %v", err)
	}
}
