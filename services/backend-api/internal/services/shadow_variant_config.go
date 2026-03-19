package services

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
)

const (
	ShadowPolicyMinConfidence   = "min_confidence"
	ShadowPolicyMaxCapitalPct   = "max_capital_pct"
	ShadowPolicyMaxSpreadPct    = "max_bid_ask_spread_pct"
	ShadowPolicyLossStreakLimit = "loss_streak_budget"
)

type ShadowVariantConfig struct {
	VariantID       string                 `json:"variant_id"`
	Name            string                 `json:"name"`
	PolicyOverrides map[string]interface{} `json:"policy_overrides"`
	Description     string                 `json:"description"`
}

type ShadowVariantPolicy struct {
	MinConfidence   *float64
	MaxCapitalPct   *float64
	MaxSpreadPct    *float64
	LossStreakLimit *int
}

func NewDefaultShadowVariant() ShadowVariantConfig {
	return ShadowVariantConfig{
		VariantID:       "baseline",
		Name:            "Baseline Mirror",
		PolicyOverrides: map[string]interface{}{},
		Description:     "Mirrors live scalping policy thresholds without overrides",
	}
}

func (c ShadowVariantConfig) Normalize() (ShadowVariantConfig, error) {
	variantID := strings.TrimSpace(strings.ToLower(c.VariantID))
	if variantID == "" {
		return ShadowVariantConfig{}, fmt.Errorf("variant_id is required")
	}
	name := strings.TrimSpace(c.Name)
	if name == "" {
		name = variantID
	}
	description := strings.TrimSpace(c.Description)
	overrides := c.PolicyOverrides
	if overrides == nil {
		overrides = map[string]interface{}{}
	} else {
		overrides = copyStringInterfaceMap(overrides)
	}
	return ShadowVariantConfig{
		VariantID:       variantID,
		Name:            name,
		PolicyOverrides: overrides,
		Description:     description,
	}, nil
}

func (c ShadowVariantConfig) ParsedPolicy() ShadowVariantPolicy {
	policy := ShadowVariantPolicy{}
	if value, ok := readShadowFloat(c.PolicyOverrides, ShadowPolicyMinConfidence); ok {
		v := clampShadowFloat(value, 0.05, 0.99)
		policy.MinConfidence = &v
	}
	if value, ok := readShadowFloat(c.PolicyOverrides, ShadowPolicyMaxCapitalPct); ok {
		v := clampShadowFloat(value, 0.10, 100.0)
		policy.MaxCapitalPct = &v
	}
	if value, ok := readShadowFloat(c.PolicyOverrides, ShadowPolicyMaxSpreadPct); ok {
		v := appautonomy.NormalizeScalpingMaxBidAskSpreadPct(value)
		policy.MaxSpreadPct = &v
	}
	if value, ok := readShadowInt(c.PolicyOverrides, ShadowPolicyLossStreakLimit); ok {
		v := clampShadowInt(value, 1, 20)
		policy.LossStreakLimit = &v
	}
	return policy
}

func (c ShadowVariantConfig) ApplyToPolicy(base appautonomy.ScalpingCyclePolicy) appautonomy.ScalpingCyclePolicy {
	overrides := c.ParsedPolicy()
	result := base
	if overrides.MinConfidence != nil {
		result.EffectiveMinConfidence = *overrides.MinConfidence
	}
	if overrides.MaxCapitalPct != nil {
		result.EffectiveMaxCapitalPct = *overrides.MaxCapitalPct
	}
	if overrides.MaxSpreadPct != nil {
		result.MaxBidAskSpreadPct = *overrides.MaxSpreadPct
	}
	if overrides.LossStreakLimit != nil {
		result.LossStreakBudget = *overrides.LossStreakLimit
	}
	return result
}

type ShadowVariantStore struct {
	mu       sync.RWMutex
	variants map[string]ShadowVariantConfig
}

func NewShadowVariantStore(seed []ShadowVariantConfig) *ShadowVariantStore {
	store := &ShadowVariantStore{variants: make(map[string]ShadowVariantConfig)}
	if len(seed) == 0 {
		seed = []ShadowVariantConfig{NewDefaultShadowVariant()}
	}
	for _, item := range seed {
		normalized, err := item.Normalize()
		if err != nil {
			continue
		}
		store.variants[normalized.VariantID] = normalized
	}
	if len(store.variants) == 0 {
		defaultVariant, _ := NewDefaultShadowVariant().Normalize()
		store.variants[defaultVariant.VariantID] = defaultVariant
	}
	if _, hasBaseline := store.variants["baseline"]; !hasBaseline {
		defaultVariant, _ := NewDefaultShadowVariant().Normalize()
		store.variants[defaultVariant.VariantID] = defaultVariant
	}
	return store
}

func (s *ShadowVariantStore) List() []ShadowVariantConfig {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]ShadowVariantConfig, 0, len(s.variants))
	for _, variant := range s.variants {
		clone := variant
		clone.PolicyOverrides = copyStringInterfaceMap(variant.PolicyOverrides)
		result = append(result, clone)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].VariantID < result[j].VariantID
	})
	return result
}

func (s *ShadowVariantStore) Get(variantID string) (ShadowVariantConfig, bool) {
	if s == nil {
		return ShadowVariantConfig{}, false
	}
	key := strings.TrimSpace(strings.ToLower(variantID))
	if key == "" {
		return ShadowVariantConfig{}, false
	}
	s.mu.RLock()
	variant, ok := s.variants[key]
	s.mu.RUnlock()
	if !ok {
		return ShadowVariantConfig{}, false
	}
	variant.PolicyOverrides = copyStringInterfaceMap(variant.PolicyOverrides)
	return variant, true
}

func (s *ShadowVariantStore) Upsert(config ShadowVariantConfig) (ShadowVariantConfig, error) {
	if s == nil {
		return ShadowVariantConfig{}, fmt.Errorf("shadow variant store is nil")
	}
	normalized, err := config.Normalize()
	if err != nil {
		return ShadowVariantConfig{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.variants[normalized.VariantID] = normalized
	return normalized, nil
}

func (s *ShadowVariantStore) Delete(variantID string) bool {
	if s == nil {
		return false
	}
	key := strings.TrimSpace(strings.ToLower(variantID))
	if key == "" || key == "baseline" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.variants[key]; !ok {
		return false
	}
	delete(s.variants, key)
	return true
}

func readShadowFloat(overrides map[string]interface{}, key string) (float64, bool) {
	if len(overrides) == 0 {
		return 0, false
	}
	raw, ok := overrides[key]
	if !ok {
		return 0, false
	}
	value := readFloatMetric(raw)
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	if value == 0 {
		return 0, false
	}
	return value, true
}

func readShadowInt(overrides map[string]interface{}, key string) (int, bool) {
	if len(overrides) == 0 {
		return 0, false
	}
	raw, ok := overrides[key]
	if !ok {
		return 0, false
	}
	value := readIntMetric(raw)
	if value <= 0 {
		return 0, false
	}
	return value, true
}

func clampShadowFloat(value, minValue, maxValue float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, -1) {
		return minValue
	}
	if math.IsInf(value, 1) {
		return maxValue
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func clampShadowInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func copyStringInterfaceMap(source map[string]interface{}) map[string]interface{} {
	if len(source) == 0 {
		return map[string]interface{}{}
	}
	result := make(map[string]interface{}, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
