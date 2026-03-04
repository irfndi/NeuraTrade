package agentcontrol

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentRuntimeStartValidatesRequiredDependencies(t *testing.T) {
	t.Helper()

	baseConfig := AgentRuntimeConfig{
		AuditLogger:      NewLogger(AuditConfig{Level: LevelInfo}),
		EventIngestor:    NewIngestor(IngestConfig{BackendEventURL: "http://localhost:8080/events"}),
		PolicyEngine:     NewEngine(PolicyConfig{}),
		PlaybookRegistry: NewRegistry(),
	}

	tests := []struct {
		name string
		cfg  AgentRuntimeConfig
		want string
	}{
		{
			name: "missing audit logger",
			cfg: AgentRuntimeConfig{
				EventIngestor:    baseConfig.EventIngestor,
				PolicyEngine:     baseConfig.PolicyEngine,
				PlaybookRegistry: baseConfig.PlaybookRegistry,
			},
			want: "audit logger",
		},
		{
			name: "missing event ingestor",
			cfg: AgentRuntimeConfig{
				AuditLogger:      baseConfig.AuditLogger,
				PolicyEngine:     baseConfig.PolicyEngine,
				PlaybookRegistry: baseConfig.PlaybookRegistry,
			},
			want: "event ingestor",
		},
		{
			name: "missing policy engine",
			cfg: AgentRuntimeConfig{
				AuditLogger:      baseConfig.AuditLogger,
				EventIngestor:    baseConfig.EventIngestor,
				PlaybookRegistry: baseConfig.PlaybookRegistry,
			},
			want: "policy engine",
		},
		{
			name: "missing playbook registry",
			cfg: AgentRuntimeConfig{
				AuditLogger:   baseConfig.AuditLogger,
				EventIngestor: baseConfig.EventIngestor,
				PolicyEngine:  baseConfig.PolicyEngine,
			},
			want: "playbook registry",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()
			runtime := NewAgentRuntime(tt.cfg)
			err := runtime.Start(context.Background())
			require.Error(t, err)
			require.ErrorContains(t, err, tt.want)
		})
	}
}
