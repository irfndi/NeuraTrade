package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

const (
	// LiveReadinessManifestEnv is the path to the manifest that records which
	// strategies have verified evidence before global live mode can be enabled.
	LiveReadinessManifestEnv = "NEURATRADE_LIVE_READINESS_MANIFEST"
)

// LiveModeGuard blocks or permits a transition into real-money live mode.
type LiveModeGuard func(ctx context.Context, chatID string, changedBy string) error

// StrategyLiveReadiness records one strategy's live-readiness evidence.
type StrategyLiveReadiness struct {
	Ready      bool   `json:"ready"`
	Evidence   string `json:"evidence,omitempty"`
	Reason     string `json:"reason,omitempty"`
	VerifiedAt string `json:"verified_at,omitempty"`
}

// LiveReadinessManifest is the file format consumed by ManifestLiveModeGuard.
type LiveReadinessManifest struct {
	UpdatedAt  string                           `json:"updated_at,omitempty"`
	Strategies map[string]StrategyLiveReadiness `json:"strategies"`
}

// DefaultLiveReadinessStrategies returns every strategy/operator surface that
// must have proof before the global live mode can be used for real money.
func DefaultLiveReadinessStrategies() []string {
	return []string{"paper_trading", "scalping", "daily_trading", "swing_trading", "arbitrage"}
}

// ManifestLiveModeGuard requires a JSON manifest with ready=true and non-empty
// evidence for each required strategy.
func ManifestLiveModeGuard(manifestPath string, requiredStrategies []string) LiveModeGuard {
	required := normalizeReadinessStrategies(requiredStrategies)
	return func(_ context.Context, _ string, _ string) error {
		if len(required) == 0 {
			return nil
		}
		path := strings.TrimSpace(manifestPath)
		if path == "" {
			return fmt.Errorf(
				"live mode blocked: %s is required with verified evidence for %s",
				LiveReadinessManifestEnv,
				strings.Join(required, ", "),
			)
		}

		// #nosec G304 -- path is operator-configured via NEURATRADE_LIVE_READINESS_MANIFEST.
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("live mode blocked: read readiness manifest %q: %w", path, err)
		}

		var manifest LiveReadinessManifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return fmt.Errorf("live mode blocked: parse readiness manifest %q: %w", path, err)
		}

		strategies := normalizeReadinessManifestStrategies(manifest.Strategies)
		var blockers []string
		for _, strategy := range required {
			status, ok := strategies[strategy]
			switch {
			case !ok:
				blockers = append(blockers, fmt.Sprintf("%s=missing", strategy))
			case !status.Ready:
				reason := strings.TrimSpace(status.Reason)
				if reason == "" {
					reason = "not_ready"
				}
				blockers = append(blockers, fmt.Sprintf("%s=%s", strategy, reason))
			case strings.TrimSpace(status.Evidence) == "":
				blockers = append(blockers, fmt.Sprintf("%s=missing_evidence", strategy))
			}
		}
		if len(blockers) > 0 {
			return fmt.Errorf("live mode blocked: readiness proof incomplete (%s)", strings.Join(blockers, "; "))
		}

		return nil
	}
}

func normalizeReadinessStrategies(strategies []string) []string {
	seen := make(map[string]struct{}, len(strategies))
	normalized := make([]string, 0, len(strategies))
	for _, strategy := range strategies {
		strategy = strings.ToLower(strings.TrimSpace(strategy))
		if strategy == "" {
			continue
		}
		if _, ok := seen[strategy]; ok {
			continue
		}
		seen[strategy] = struct{}{}
		normalized = append(normalized, strategy)
	}
	sort.Strings(normalized)
	return normalized
}

func normalizeReadinessManifestStrategies(strategies map[string]StrategyLiveReadiness) map[string]StrategyLiveReadiness {
	normalized := make(map[string]StrategyLiveReadiness, len(strategies))
	for strategy, status := range strategies {
		key := strings.ToLower(strings.TrimSpace(strategy))
		if key == "" {
			continue
		}
		normalized[key] = status
	}
	return normalized
}
