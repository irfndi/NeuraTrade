package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	VendingBench2URL   = "https://andonlabs.com/evals/vending-bench-2"
	capabilityCacheTTL = 7 * 24 * time.Hour
)

type ModelRanking struct {
	ModelID      string  `json:"model_id"`
	ProviderID   string  `json:"provider_id"`
	Score        float64 `json:"score"`
	BenchVersion string  `json:"bench_version"`
	Rank         int     `json:"rank"`
}

type CapabilityRanking struct {
	BenchVersion string         `json:"bench_version"`
	SourceURL    string         `json:"source_url"`
	FetchedAt    time.Time      `json:"fetched_at"`
	Rankings     []ModelRanking `json:"rankings"`
}

type RankingService struct {
	client    *http.Client
	logger    *zap.Logger
	sourceURL string
	cacheTTL  time.Duration

	mu       sync.RWMutex
	rankings *CapabilityRanking
}

type RankingServiceOption func(*RankingService)

func NewRankingService(opts ...RankingServiceOption) *RankingService {
	rs := &RankingService{
		client:    &http.Client{Timeout: 30 * time.Second},
		logger:    zap.NewNop(),
		sourceURL: VendingBench2URL,
		cacheTTL:  capabilityCacheTTL,
	}

	for _, opt := range opts {
		opt(rs)
	}

	return rs
}

func WithRankingLogger(logger *zap.Logger) RankingServiceOption {
	return func(rs *RankingService) {
		rs.logger = logger
	}
}

func WithRankingSourceURL(url string) RankingServiceOption {
	return func(rs *RankingService) {
		rs.sourceURL = url
	}
}

func (rs *RankingService) FetchRankings(ctx context.Context) (*CapabilityRanking, error) {
	rs.logger.Info("Fetching capability rankings", zap.String("source", rs.sourceURL))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rs.sourceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := rs.client.Do(req)
	if err != nil {
		rs.logger.Warn("Failed to fetch live rankings, using seed data", zap.Error(err))
		return rs.cacheSeedFallback()
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		rs.logger.Warn("Ranking source returned non-200, using seed data",
			zap.Int("status", resp.StatusCode),
		)
		return rs.cacheSeedFallback()
	}

	var liveRanking CapabilityRanking
	if err := json.NewDecoder(resp.Body).Decode(&liveRanking); err != nil {
		rs.logger.Warn("Failed to parse live rankings, using seed data", zap.Error(err))
		return rs.cacheSeedFallback()
	}

	if len(liveRanking.Rankings) == 0 {
		return rs.cacheSeedFallback()
	}

	liveRanking.FetchedAt = time.Now().UTC()
	liveRanking.SourceURL = rs.sourceURL

	rs.mu.Lock()
	rs.rankings = &liveRanking
	rs.mu.Unlock()

	return &liveRanking, nil
}

func (rs *RankingService) GetRankings(ctx context.Context) (*CapabilityRanking, error) {
	rs.mu.RLock()
	if rs.rankings != nil && time.Since(rs.rankings.FetchedAt) < rs.cacheTTL {
		r := rs.rankings
		rs.mu.RUnlock()
		return r, nil
	}
	rs.mu.RUnlock()

	return rs.FetchRankings(ctx)
}

func (rs *RankingService) cacheSeedFallback() (*CapabilityRanking, error) {
	seed := SeedCapabilityRanking()
	rs.mu.Lock()
	rs.rankings = seed
	rs.mu.Unlock()
	return seed, nil
}

func (rs *RankingService) GetRankingForModel(ctx context.Context, modelID string) (float64, error) {
	rankings, err := rs.GetRankings(ctx)
	if err != nil {
		return 0.0, err
	}

	canonicalID := resolveCanonicalModel(modelID)
	for _, r := range rankings.Rankings {
		if r.ModelID == modelID || r.ModelID == canonicalID {
			return r.Score, nil
		}
		for _, alias := range resolveModelAliases(canonicalID) {
			if r.ModelID == alias {
				return r.Score, nil
			}
		}
	}

	rs.logger.Debug("No ranking found for model, returning default score",
		zap.String("model", modelID),
	)
	return DefaultRankingScore(modelID), nil
}

func (rs *RankingService) RankModels(ctx context.Context, models []ConfiguredModel) ([]ConfiguredModel, error) {
	rankings, err := rs.GetRankings(ctx)
	if err != nil {
		rs.logger.Warn("Failed to get rankings, using default ordering", zap.Error(err))
		return models, nil
	}

	if len(rankings.Rankings) == 0 {
		for i := range models {
			models[i].CapabilityScore = DefaultRankingScore(models[i].ModelID)
		}
		return models, nil
	}

	sorted := make([]ConfiguredModel, len(models))
	copy(sorted, models)

	for i := range sorted {
		score, err := rs.GetRankingForModel(ctx, sorted[i].ModelID)
		if err != nil {
			sorted[i].CapabilityScore = DefaultRankingScore(sorted[i].ModelID)
		} else {
			sorted[i].CapabilityScore = score
		}
	}

	sortByCapability(sorted)

	return sorted, nil
}

func sortByCapability(models []ConfiguredModel) {
	sort.Slice(models, func(i, j int) bool {
		return models[i].CapabilityScore > models[j].CapabilityScore
	})
}

func resolveModelAliases(modelID string) []string {
	knownAliases := map[string][]string{
		"gpt-4o":            {"gpt-4o-2024-11-20", "gpt-4o-2024-08-06"},
		"gpt-4o-mini":       {"gpt-4o-mini-2024-07-18"},
		"claude-sonnet-4":   {"claude-sonnet-4-20250514"},
		"claude-3.5-sonnet": {"claude-3-5-sonnet-20241022", "claude-3-5-sonnet-20240620"},
		"claude-3-opus":     {"claude-3-opus-20240229"},
		"gemini-2.5-pro":    {"gemini-2.5-pro-preview-05-06"},
	}
	if aliases, ok := knownAliases[modelID]; ok {
		return aliases
	}
	return nil
}

func resolveCanonicalModel(modelID string) string {
	aliasToCanonical := map[string]string{
		"gpt-4o-2024-11-20":            "gpt-4o",
		"gpt-4o-2024-08-06":            "gpt-4o",
		"gpt-4o-mini-2024-07-18":       "gpt-4o-mini",
		"claude-sonnet-4-20250514":     "claude-sonnet-4",
		"claude-3-5-sonnet-20241022":   "claude-3.5-sonnet",
		"claude-3-5-sonnet-20240620":   "claude-3.5-sonnet",
		"claude-3-opus-20240229":       "claude-3-opus",
		"gemini-2.5-pro-preview-05-06": "gemini-2.5-pro",
	}
	if canonical, ok := aliasToCanonical[modelID]; ok {
		return canonical
	}
	return modelID
}

func DefaultRankingScore(modelID string) float64 {
	canonicalID := resolveCanonicalModel(modelID)
	knownScores := map[string]float64{
		"claude-sonnet-4-20250514": 95.0,
		"claude-sonnet-4":          95.0,
		"gemini-2.5-pro":           93.0,
		"o3":                       92.0,
		"gpt-4.1":                  91.0,
		"claude-3.5-sonnet":        88.0,
		"claude-3-5-sonnet-latest": 88.0,
		"gpt-4o":                   86.0,
		"o4-mini":                  85.0,
		"gemini-2.5-flash":         84.0,
		"claude-3-opus":            82.0,
		"deepseek-r1":              81.0,
		"gpt-4.1-mini":             79.0,
		"gpt-4o-mini":              78.0,
		"deepseek-v3":              77.0,
		"llama-4-maverick":         75.0,
	}
	if score, ok := knownScores[canonicalID]; ok {
		return score
	}
	if score, ok := knownScores[modelID]; ok {
		return score
	}
	return 50.0
}

func SeedCapabilityRanking() *CapabilityRanking {
	return &CapabilityRanking{
		BenchVersion: "vending-bench-2-seed",
		SourceURL:    VendingBench2URL,
		FetchedAt:    time.Now().UTC(),
		Rankings: []ModelRanking{
			{ModelID: "claude-sonnet-4-20250514", ProviderID: "anthropic", Score: 95.0, Rank: 1, BenchVersion: "vending-bench-2"},
			{ModelID: "claude-sonnet-4", ProviderID: "anthropic", Score: 95.0, Rank: 2, BenchVersion: "vending-bench-2"},
			{ModelID: "gemini-2.5-pro", ProviderID: "google", Score: 93.0, Rank: 3, BenchVersion: "vending-bench-2"},
			{ModelID: "o3", ProviderID: "openai", Score: 92.0, Rank: 4, BenchVersion: "vending-bench-2"},
			{ModelID: "gpt-4.1", ProviderID: "openai", Score: 91.0, Rank: 5, BenchVersion: "vending-bench-2"},
			{ModelID: "claude-3-5-sonnet-latest", ProviderID: "anthropic", Score: 88.0, Rank: 6, BenchVersion: "vending-bench-2"},
			{ModelID: "claude-3.5-sonnet", ProviderID: "anthropic", Score: 88.0, Rank: 7, BenchVersion: "vending-bench-2"},
			{ModelID: "gpt-4o", ProviderID: "openai", Score: 86.0, Rank: 8, BenchVersion: "vending-bench-2"},
			{ModelID: "o4-mini", ProviderID: "openai", Score: 85.0, Rank: 9, BenchVersion: "vending-bench-2"},
			{ModelID: "gemini-2.5-flash", ProviderID: "google", Score: 84.0, Rank: 10, BenchVersion: "vending-bench-2"},
			{ModelID: "claude-3-opus", ProviderID: "anthropic", Score: 82.0, Rank: 11, BenchVersion: "vending-bench-2"},
			{ModelID: "deepseek-r1", ProviderID: "deepseek", Score: 81.0, Rank: 12, BenchVersion: "vending-bench-2"},
			{ModelID: "gpt-4.1-mini", ProviderID: "openai", Score: 79.0, Rank: 13, BenchVersion: "vending-bench-2"},
			{ModelID: "gpt-4o-mini", ProviderID: "openai", Score: 78.0, Rank: 14, BenchVersion: "vending-bench-2"},
			{ModelID: "deepseek-v3", ProviderID: "deepseek", Score: 77.0, Rank: 15, BenchVersion: "vending-bench-2"},
			{ModelID: "llama-4-maverick", ProviderID: "meta", Score: 75.0, Rank: 16, BenchVersion: "vending-bench-2"},
		},
	}
}
