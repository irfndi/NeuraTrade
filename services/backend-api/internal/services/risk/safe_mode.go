package risk

import (
	"context"
	"sync"
)

type SafeModeConfig struct {
	BlockNewTrades     bool
	BlockModifications bool
}

type SafeMode struct {
	mu      sync.RWMutex
	enabled bool
	config  SafeModeConfig
}

func NewSafeMode(cfg SafeModeConfig) *SafeMode {
	return &SafeMode{config: cfg}
}

func (s *SafeMode) Enable(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = true
	return nil
}

func (s *SafeMode) Disable(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = false
	return nil
}

func (s *SafeMode) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}
