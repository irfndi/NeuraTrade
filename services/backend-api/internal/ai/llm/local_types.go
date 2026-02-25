package llm

// ModelInfo holds model metadata (local copy to break import cycle)
type ModelInfo struct {
	ID            string
	ProviderID    string
	ContextWindow int
}

// Registry manages model registry (local copy to break import cycle)
type Registry struct{}

// NewRegistry creates a new registry
func NewRegistry() *Registry {
	return &Registry{}
}
