package backtest

import (
	"github.com/irfndi/goflux/pkg/metrics"
)

// Analyzer is an interface for analyzing backtest results.
type Analyzer interface {
	Name() string
	Analyze(trades []metrics.Trade, equityCurve []metrics.EquityPoint) interface{}
}

// AnalysisResult represents the collected results from all analyzers.
type AnalysisResult map[string]interface{}

// AnalyzerRegistry maintains a list of available analyzers.
type AnalyzerRegistry struct {
	analyzers []Analyzer
}

// NewAnalyzerRegistry returns a new AnalyzerRegistry.
func NewAnalyzerRegistry() *AnalyzerRegistry {
	return &AnalyzerRegistry{
		analyzers: make([]Analyzer, 0),
	}
}

// Add adds an analyzer to the registry.
func (ar *AnalyzerRegistry) Add(analyzer Analyzer) {
	ar.analyzers = append(ar.analyzers, analyzer)
}

// Run executes all registered analyzers and returns the combined results.
func (ar *AnalyzerRegistry) Run(trades []metrics.Trade, equityCurve []metrics.EquityPoint) AnalysisResult {
	results := make(AnalysisResult)
	for _, analyzer := range ar.analyzers {
		results[analyzer.Name()] = analyzer.Analyze(trades, equityCurve)
	}
	return results
}
