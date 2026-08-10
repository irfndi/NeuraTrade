package risk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
)

func TestMemoryKillSwitchStore_Empty(t *testing.T) {
	store := NewMemoryKillSwitchStore()
	got, found, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatalf("expected not found on empty store, got found=true")
	}
	if got.Engaged || got.CancelOrders {
		t.Fatalf("expected zero state, got %+v", got)
	}
}

func TestMemoryKillSwitchStore_RoundTrip(t *testing.T) {
	store := NewMemoryKillSwitchStore()
	want := PersistedKillSwitchState{
		Engaged:      true,
		EngagedAt:    time.Unix(1700000000, 0),
		EngagedBy:    "tester",
		Reason:       "manual",
		CancelOrders: true,
		UpdatedAt:    time.Unix(1700000001, 0),
	}
	if err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, found, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !found {
		t.Fatalf("expected found=true after save")
	}
	if got != want {
		t.Fatalf("round-trip mismatch: want %+v got %+v", want, got)
	}
}

func TestKillSwitchImpl_ReconcileNoStore(t *testing.T) {
	ks := NewKillSwitch()
	if err := ks.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile with no store: %v", err)
	}
	if ks.IsEngaged() {
		t.Fatalf("expected disengaged, got engaged")
	}
}

func TestKillSwitchImpl_ReconcileFromStore(t *testing.T) {
	store := NewMemoryKillSwitchStore()
	if err := store.Save(context.Background(), PersistedKillSwitchState{
		Engaged:      true,
		EngagedAt:    time.Unix(1700000000, 0),
		EngagedBy:    "operator",
		Reason:       "live risk event",
		CancelOrders: true,
		UpdatedAt:    time.Unix(1700000001, 0),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	ks := NewKillSwitch()
	ks.SetStore(store)
	if err := ks.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !ks.IsEngaged() {
		t.Fatalf("expected engaged after reconcile, got disengaged")
	}
	state := ks.State()
	if state.EngagedBy != "operator" {
		t.Fatalf("EngagedBy mismatch: %s", state.EngagedBy)
	}
	if state.Reason != "live risk event" {
		t.Fatalf("Reason mismatch: %s", state.Reason)
	}
}

func TestKillSwitchImpl_EngagePersists(t *testing.T) {
	store := NewMemoryKillSwitchStore()
	ks := NewKillSwitch()
	ks.SetStore(store)
	if err := ks.Engage(context.Background(), "drawdown breach"); err != nil {
		t.Fatalf("engage: %v", err)
	}

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		_, found, _ := store.Load(context.Background())
		if found {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, found, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !found {
		t.Fatalf("expected persisted state")
	}
	if !got.Engaged {
		t.Fatalf("expected engaged=true, got %+v", got)
	}
	if got.Reason != "drawdown breach" {
		t.Fatalf("expected reason 'drawdown breach', got %q", got.Reason)
	}
}

func TestKillSwitchImpl_DisengagePersists(t *testing.T) {
	store := NewMemoryKillSwitchStore()
	ks := NewKillSwitch()
	ks.SetStore(store)
	_ = ks.Engage(context.Background(), "test")

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		got, found, _ := store.Load(context.Background())
		if found && got.Engaged {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := ks.Disengage(context.Background()); err != nil {
		t.Fatalf("disengage: %v", err)
	}
	deadline = time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		got, _, _ := store.Load(context.Background())
		if !got.Engaged {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected disengaged state to be persisted")
}

type mockDB struct {
	queryRowFn func(ctx context.Context, query string, args ...any) database.Row
	execFn     func(ctx context.Context, query string, args ...any) (database.Result, error)
}

func (m *mockDB) QueryRow(ctx context.Context, query string, args ...any) database.Row {
	return m.queryRowFn(ctx, query, args...)
}

func (m *mockDB) Exec(ctx context.Context, query string, args ...any) (database.Result, error) {
	return m.execFn(ctx, query, args...)
}

type mockRow struct {
	scanFn func(dest ...any) error
}

func (m *mockRow) Scan(dest ...any) error { return m.scanFn(dest...) }

type mockResult struct{}

func (m *mockResult) RowsAffected() (int64, error) { return 1, nil }

func TestSQLKillSwitchStore_LoadNoRows(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...any) database.Row {
			return &mockRow{scanFn: func(_ ...any) error {
				return errors.New("sql: no rows in result set")
			}}
		},
	}
	store := NewSQLKillSwitchStore(db)
	_, found, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if found {
		t.Fatalf("expected not found")
	}
}

func TestSQLKillSwitchStore_LoadTableMissing(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...any) database.Row {
			return &mockRow{scanFn: func(_ ...any) error {
				return errors.New(`pq: relation "risk_kill_switch_state" does not exist`)
			}}
		},
	}
	store := NewSQLKillSwitchStore(db)
	_, _, err := store.Load(context.Background())
	if !errors.Is(err, ErrKillSwitchSchemaMissing) {
		t.Fatalf("expected ErrKillSwitchSchemaMissing, got %v", err)
	}
}

func TestSQLKillSwitchStore_LoadSQLiteMissing(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...any) database.Row {
			return &mockRow{scanFn: func(_ ...any) error {
				return errors.New("SQL logic error: no such table: risk_kill_switch_state (1)")
			}}
		},
	}
	store := NewSQLKillSwitchStore(db)
	_, _, err := store.Load(context.Background())
	if !errors.Is(err, ErrKillSwitchSchemaMissing) {
		t.Fatalf("expected ErrKillSwitchSchemaMissing, got %v", err)
	}
}

func TestSQLKillSwitchStore_LoadSuccess(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...any) database.Row {
			return &mockRow{scanFn: func(dest ...any) error {
				engaged := dest[0].(*int)
				engagedAt := dest[1].(**int64)
				engagedBy := dest[2].(*string)
				reason := dest[3].(*string)
				cancelOrders := dest[4].(*int)
				updatedAt := dest[5].(*int64)

				v := 1
				ts := int64(1700000000)
				*engaged = v
				*engagedAt = &ts
				*engagedBy = "operator"
				*reason = "drawdown"
				*cancelOrders = 1
				*updatedAt = 1700000001
				return nil
			}}
		},
	}
	store := NewSQLKillSwitchStore(db)
	got, found, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !found {
		t.Fatalf("expected found")
	}
	if !got.Engaged {
		t.Fatalf("expected engaged=true, got %+v", got)
	}
	if got.EngagedBy != "operator" {
		t.Fatalf("EngagedBy mismatch: %q", got.EngagedBy)
	}
	if got.Reason != "drawdown" {
		t.Fatalf("Reason mismatch: %q", got.Reason)
	}
	if !got.CancelOrders {
		t.Fatalf("CancelOrders mismatch: %+v", got)
	}
	if got.EngagedAt.Unix() != 1700000000 {
		t.Fatalf("EngagedAt mismatch: %v", got.EngagedAt)
	}
}

func TestSQLKillSwitchStore_LoadEngagedAtNull(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...any) database.Row {
			return &mockRow{scanFn: func(dest ...any) error {
				engaged := dest[0].(*int)
				engagedAt := dest[1].(**int64)
				engagedBy := dest[2].(*string)
				reason := dest[3].(*string)
				cancelOrders := dest[4].(*int)
				updatedAt := dest[5].(*int64)

				*engaged = 0
				*engagedAt = nil
				*engagedBy = ""
				*reason = ""
				*cancelOrders = 0
				*updatedAt = 1700000001
				return nil
			}}
		},
	}
	store := NewSQLKillSwitchStore(db)
	got, found, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !found {
		t.Fatalf("expected found")
	}
	if got.Engaged {
		t.Fatalf("expected engaged=false")
	}
	if !got.EngagedAt.IsZero() {
		t.Fatalf("expected zero EngagedAt, got %v", got.EngagedAt)
	}
}

func TestSQLKillSwitchStore_SaveSuccess(t *testing.T) {
	called := false
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...any) (database.Result, error) {
			called = true
			return &mockResult{}, nil
		},
	}
	store := NewSQLKillSwitchStore(db)
	err := store.Save(context.Background(), PersistedKillSwitchState{
		Engaged:      true,
		EngagedAt:    time.Unix(1700000000, 0),
		EngagedBy:    "operator",
		Reason:       "drawdown",
		CancelOrders: true,
		UpdatedAt:    time.Unix(1700000001, 0),
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !called {
		t.Fatalf("expected Exec to be called")
	}
}

func TestSQLKillSwitchStore_SaveTableMissing(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...any) (database.Result, error) {
			return nil, errors.New("no such table: risk_kill_switch_state")
		},
	}
	store := NewSQLKillSwitchStore(db)
	err := store.Save(context.Background(), PersistedKillSwitchState{Engaged: true})
	if !errors.Is(err, ErrKillSwitchSchemaMissing) {
		t.Fatalf("expected ErrKillSwitchSchemaMissing, got %v", err)
	}
}

// failingSaveStore is a KillSwitchStore whose Save always fails, used to
// verify fail-closed behavior when persistence is broken.
type failingSaveStore struct {
	state *PersistedKillSwitchState
}

func (m *failingSaveStore) Load(_ context.Context) (PersistedKillSwitchState, bool, error) {
	if m.state == nil {
		return PersistedKillSwitchState{}, false, nil
	}
	return *m.state, true, nil
}

func (m *failingSaveStore) Save(_ context.Context, _ PersistedKillSwitchState) error {
	return errors.New("simulated persistence failure")
}

// TestKillSwitchImpl_ReconcileFailsClosedOnLoadError verifies that a load
// error during reconcile leaves the kill switch ENGAGED (trading blocked)
// and returns the error instead of silently starting disengaged.
func TestKillSwitchImpl_ReconcileFailsClosedOnLoadError(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...any) database.Row {
			return &mockRow{scanFn: func(_ ...any) error {
				return errors.New("connection refused")
			}}
		},
	}
	store := NewSQLKillSwitchStore(db)
	ks := NewKillSwitch()
	ks.SetStore(store)

	err := ks.Reconcile(context.Background())
	if err == nil {
		t.Fatalf("expected reconcile error, got nil")
	}
	if !ks.IsEngaged() {
		t.Fatalf("expected kill switch to be ENGAGED (fail closed) after reconcile load error")
	}
}

// TestKillSwitchImpl_EngagePersistErrorKeepsEngaged verifies that when the
// synchronous persistence fails, Engage surfaces the error AND keeps the
// kill switch engaged in memory (fail closed, never silently succeed).
func TestKillSwitchImpl_EngagePersistErrorKeepsEngaged(t *testing.T) {
	ks := NewKillSwitch()
	ks.SetStore(&failingSaveStore{})

	err := ks.Engage(context.Background(), "test")
	if err == nil {
		t.Fatalf("expected engage error when persistence fails")
	}
	if !ks.IsEngaged() {
		t.Fatalf("expected kill switch to remain ENGAGED when persistence fails (fail closed)")
	}
}

// TestKillSwitchImpl_DisengagePersistErrorRestoresEngaged verifies that a
// failed disengage persistence rolls the state back to engaged (fail closed)
// instead of leaving the kill switch silently disengaged.
func TestKillSwitchImpl_DisengagePersistErrorRestoresEngaged(t *testing.T) {
	ks := NewKillSwitch()
	ks.SetStore(&failingSaveStore{})

	if err := ks.Engage(context.Background(), "test"); err == nil {
		t.Fatalf("expected engage error when persistence fails")
	}
	if !ks.IsEngaged() {
		t.Fatalf("expected engaged after failed persist")
	}

	err := ks.Disengage(context.Background())
	if err == nil {
		t.Fatalf("expected disengage error when persistence fails")
	}
	if !ks.IsEngaged() {
		t.Fatalf("expected kill switch to remain ENGAGED when disengage persistence fails (fail closed)")
	}
}

// TestKillSwitchImpl_EngagePersistsSynchronously verifies that after Engage
// returns, the persisted state is already available (no background goroutine,
// so state ordering is deterministic).
func TestKillSwitchImpl_EngagePersistsSynchronously(t *testing.T) {
	store := NewMemoryKillSwitchStore()
	ks := NewKillSwitch()
	ks.SetStore(store)

	if err := ks.Engage(context.Background(), "sync"); err != nil {
		t.Fatalf("engage: %v", err)
	}
	got, found, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !found {
		t.Fatalf("expected state to be persisted immediately after Engage returned")
	}
	if !got.Engaged || got.Reason != "sync" {
		t.Fatalf("unexpected persisted state: %+v", got)
	}
}
