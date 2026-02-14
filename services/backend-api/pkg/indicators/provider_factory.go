package indicators

// ProviderType represents the type of indicator provider.
type ProviderType string

const (
	// ProviderTypeTalib uses the traditional talib wrapper.
	ProviderTypeTalib ProviderType = "talib"
	// ProviderTypeGoFlux uses the GoFlux library (future).
	ProviderTypeGoFlux ProviderType = "goflux"
)

// ProviderConfig holds configuration for creating indicator providers.
type ProviderConfig struct {
	Type ProviderType
	// Add configuration options here as needed
}

// NewProvider creates an IndicatorProvider based on the configuration.
func NewProvider(config *ProviderConfig) (IndicatorProvider, error) {
	if config == nil {
		config = &ProviderConfig{Type: ProviderTypeTalib}
	}

	switch config.Type {
	case ProviderTypeTalib:
		return NewTalibAdapter(), nil
	case ProviderTypeGoFlux:
		// GoFlux adapter will be implemented in neura-dvl
		return nil, NewIndicatorError("GoFlux", "provider not yet implemented", nil)
	default:
		return nil, NewIndicatorError(string(config.Type), "unknown provider type", nil)
	}
}

// NewDefaultProvider creates a provider with default settings (Talib).
func NewDefaultProvider() IndicatorProvider {
	return NewTalibAdapter()
}
