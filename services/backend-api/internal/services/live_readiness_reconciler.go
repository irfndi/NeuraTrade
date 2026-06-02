package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// LiveReadinessReconcilerConfig tunes how often the reconciler evaluates paper-trading
// readiness and how far back it looks for evidence.
type LiveReadinessReconcilerConfig struct {
	Interval       time.Duration
	LookbackWindow time.Duration
	Strategies     []string
	Capital        decimal.Decimal
}

// DefaultLiveReadinessReconcilerConfig returns sensible production defaults.
func DefaultLiveReadinessReconcilerConfig() LiveReadinessReconcilerConfig {
	return LiveReadinessReconcilerConfig{
		Interval:       1 * time.Hour,
		LookbackWindow: 7 * 24 * time.Hour,
		Strategies:     []string{"scalping", "daily_trading", "swing_trading", "arbitrage"},
	}
}

// LiveReadinessReconciler periodically regenerates the paper-trading readiness
// manifest and persists it to the database when acceptance criteria pass.
type LiveReadinessReconciler struct {
	db     DBPool
	store  *LiveReadinessManifestStore
	logger Logger
	config LiveReadinessReconcilerConfig

	mu        sync.Mutex
	running   bool
	stopCh    chan struct{}
	stopDone  chan struct{}
	lastRun   time.Time
	lastError error
}

// NewLiveReadinessReconciler creates a reconciler backed by the database.
func NewLiveReadinessReconciler(db DBPool, store *LiveReadinessManifestStore, logger Logger, config LiveReadinessReconcilerConfig) *LiveReadinessReconciler {
	if config.Interval <= 0 {
		config.Interval = DefaultLiveReadinessReconcilerConfig().Interval
	}
	if config.LookbackWindow <= 0 {
		config.LookbackWindow = DefaultLiveReadinessReconcilerConfig().LookbackWindow
	}
	if len(config.Strategies) == 0 {
		config.Strategies = DefaultLiveReadinessReconcilerConfig().Strategies
	}
	return &LiveReadinessReconciler{
		db:     db,
		store:  store,
		logger: logger,
		config: config,
	}
}

// Start begins the background reconciliation loop.  It is safe to call only
// once; subsequent calls return an error until Stop is called.
func (r *LiveReadinessReconciler) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return fmt.Errorf("reconciler already running")
	}

	r.stopCh = make(chan struct{})
	r.stopDone = make(chan struct{})
	r.running = true

	go r.loop(ctx)
	return nil
}

// Stop signals the background loop to exit and waits for it to finish.
func (r *LiveReadinessReconciler) Stop() {
	r.mu.Lock()
	shouldWait := r.running
	if r.running {
		close(r.stopCh)
		r.running = false
	}
	r.mu.Unlock()

	if shouldWait {
		<-r.stopDone
	}
}

// LastRun returns the timestamp of the most recent successful reconciliation.
func (r *LiveReadinessReconciler) LastRun() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastRun
}

// LastError returns the most recent reconciliation error, if any.
func (r *LiveReadinessReconciler) LastError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastError
}

func (r *LiveReadinessReconciler) loop(ctx context.Context) {
	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
		close(r.stopDone)
	}()

	ticker := time.NewTicker(r.config.Interval)
	defer ticker.Stop()

	// Run immediately on start.
	r.reconcile(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.reconcile(ctx)
		}
	}
}

func (r *LiveReadinessReconciler) reconcile(ctx context.Context) {
	endTime := time.Now()
	startTime := endTime.Add(-r.config.LookbackWindow)

	generator := NewReadinessManifestGenerator(r.db, r.logger)
	manifest, genErr := generator.GenerateManifest(ctx, startTime, endTime, r.config.Strategies)

	var lastErr error
	if genErr != nil {
		lastErr = fmt.Errorf("generate manifest: %w", genErr)
		if r.logger != nil {
			r.logger.Error(fmt.Sprintf("Live readiness reconciler: %v", lastErr))
		}
	} else if manifest.Acceptance.Ready {
		if r.store == nil {
			if r.logger != nil {
				r.logger.Warn("Live readiness reconciler: manifest ready but no store configured, skipping DB persistence")
			}
		} else if _, saveErr := generator.SaveManifestToDB(ctx, r.store, manifest); saveErr != nil {
			lastErr = fmt.Errorf("save ready manifest: %w", saveErr)
			if r.logger != nil {
				r.logger.Error(fmt.Sprintf("Live readiness reconciler: %v", lastErr))
			}
		} else if r.logger != nil {
			r.logger.Info("Live readiness reconciler: manifest accepted and persisted to database")
		}
	} else {
		if r.logger != nil {
			r.logger.Warn(fmt.Sprintf("Live readiness reconciler: manifest not ready (%s)", manifest.Acceptance.Failures))
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastRun = time.Now()
	r.lastError = lastErr
}
