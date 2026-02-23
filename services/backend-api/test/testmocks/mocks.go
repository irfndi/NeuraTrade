package testmocks

import (
	"context"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
)

// MockCCXTService implements ccxt.CCXTService for testing
type MockCCXTService struct {
	mock.Mock
}

// MockCCXTClient implements ccxt.CCXTClient for testing
type MockCCXTClient struct {
	mock.Mock
}

// MockCollectorService implements services.CollectorService for testing
type MockCollectorService struct {
	mock.Mock
}

// MockCacheAnalyticsService implements services.CacheAnalyticsService for testing
type MockCacheAnalyticsService struct {
	mock.Mock
}

// MockSpotArbitrageCalculator implements services.ArbitrageCalculator for testing
type MockSpotArbitrageCalculator struct {
	mock.Mock
}

// CalculateArbitrageOpportunities mocks arbitrage opportunity calculations
func (m *MockSpotArbitrageCalculator) CalculateArbitrageOpportunities(ctx context.Context, marketData map[string][]models.MarketData) ([]models.ArbitrageOpportunity, error) {
	args := m.Called(ctx, marketData)
	var opportunities []models.ArbitrageOpportunity
	if result := args.Get(0); result != nil {
		opportunities = result.([]models.ArbitrageOpportunity)
	}
	return opportunities, args.Error(1)
}

// MockRedisClient implements RedisInterface for testing
type MockRedisClient struct {
	mock.Mock
}

// MockPostgresDB implements database.PostgresDB for testing
type MockPostgresDB struct {
	mock.Mock
	PoolFunc  func() interface{}
	QueryFunc func(ctx context.Context, query string, args ...interface{}) (pgx.Rows, error)
}

func (m *MockPostgresDB) Pool() interface{} {
	if m.PoolFunc != nil {
		return m.PoolFunc()
	}
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0)
}

func (m *MockPostgresDB) Close() {
	m.Called()
}

func (m *MockPostgresDB) HealthCheck(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockPostgresDB) Query(ctx context.Context, query string, args ...interface{}) (pgx.Rows, error) {
	if m.QueryFunc != nil {
		return m.QueryFunc(ctx, query, args...)
	}
	return m.Called(ctx, query, args).Get(0).(pgx.Rows), m.Called(ctx, query, args).Error(1)
}

// MockRows implements pgx.Rows for testing
type MockRows struct {
	mock.Mock
	CloseFunc             func()
	NextFunc              func() bool
	ScanFunc              func(dest ...interface{}) error
	ErrFunc               func() error
	CommandTagFunc        func() pgconn.CommandTag
	FieldDescriptionsFunc func() []pgconn.FieldDescription
	ValuesFunc            func() ([]interface{}, error)
	RawValuesFunc         func() [][]byte
	ConnFunc              func() *pgx.Conn
}

func (m *MockRows) Close() {
	if m.CloseFunc != nil {
		m.CloseFunc()
	}
	m.Called()
}

func (m *MockRows) Next() bool {
	if m.NextFunc != nil {
		return m.NextFunc()
	}
	args := m.Called()
	return args.Bool(0)
}

func (m *MockRows) Scan(dest ...interface{}) error {
	if m.ScanFunc != nil {
		return m.ScanFunc(dest...)
	}
	args := m.Called(dest)
	return args.Error(0)
}

func (m *MockRows) Err() error {
	if m.ErrFunc != nil {
		return m.ErrFunc()
	}
	args := m.Called()
	return args.Error(0)
}

func (m *MockRows) CommandTag() pgconn.CommandTag {
	if m.CommandTagFunc != nil {
		return m.CommandTagFunc()
	}
	args := m.Called()
	return args.Get(0).(pgconn.CommandTag)
}

func (m *MockRows) FieldDescriptions() []pgconn.FieldDescription {
	if m.FieldDescriptionsFunc != nil {
		return m.FieldDescriptionsFunc()
	}
	args := m.Called()
	return args.Get(0).([]pgconn.FieldDescription)
}

func (m *MockRows) Values() ([]interface{}, error) {
	if m.ValuesFunc != nil {
		return m.ValuesFunc()
	}
	args := m.Called()
	return args.Get(0).([]interface{}), args.Error(1)
}

func (m *MockRows) RawValues() [][]byte {
	if m.RawValuesFunc != nil {
		return m.RawValuesFunc()
	}
	args := m.Called()
	return args.Get(0).([][]byte)
}

func (m *MockRows) Conn() *pgx.Conn {
	if m.ConnFunc != nil {
		return m.ConnFunc()
	}
	args := m.Called()
	return args.Get(0).(*pgx.Conn)
}

// MockPool implements pgxpool.Pool for testing
type MockPool struct {
	mock.Mock
	QueryFunc func(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
	ExecFunc  func(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error)
	BeginFunc func(ctx context.Context) (pgx.Tx, error)
}

func (m *MockPool) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	if m.QueryFunc != nil {
		return m.QueryFunc(ctx, sql, args...)
	}
	mockArgs := m.Called(ctx, sql, args)
	if mockArgs.Get(0) == nil {
		return &MockRows{}, nil
	}
	return mockArgs.Get(0).(pgx.Rows), mockArgs.Error(1)
}

func (m *MockPool) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	if m.ExecFunc != nil {
		return m.ExecFunc(ctx, sql, args...)
	}
	mockArgs := m.Called(ctx, sql, args)
	if mockArgs.Get(0) == nil {
		return pgconn.NewCommandTag("MOCK"), nil
	}
	return mockArgs.Get(0).(pgconn.CommandTag), mockArgs.Error(1)
}

func (m *MockPool) Begin(ctx context.Context) (pgx.Tx, error) {
	if m.BeginFunc != nil {
		return m.BeginFunc(ctx)
	}
	mockArgs := m.Called(ctx)
	if mockArgs.Get(0) == nil {
		return &MockTx{}, nil
	}
	return mockArgs.Get(0).(pgx.Tx), mockArgs.Error(1)
}

// MockTx implements pgx.Tx for testing
type MockTx struct {
	mock.Mock
	ExecFunc     func(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error)
	RollbackFunc func(ctx context.Context) error
	CommitFunc   func(ctx context.Context) error
	BeginFunc    func(ctx context.Context) (pgx.Tx, error)
	ConnFunc     func() *pgx.Conn
}

func (m *MockTx) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	if m.ExecFunc != nil {
		return m.ExecFunc(ctx, sql, args...)
	}
	mockArgs := m.Called(ctx, sql, args)
	if mockArgs.Get(0) == nil {
		return pgconn.NewCommandTag("MOCK"), nil
	}
	return mockArgs.Get(0).(pgconn.CommandTag), mockArgs.Error(1)
}

func (m *MockTx) Rollback(ctx context.Context) error {
	if m.RollbackFunc != nil {
		return m.RollbackFunc(ctx)
	}
	return m.Called(ctx).Error(0)
}

func (m *MockTx) Commit(ctx context.Context) error {
	if m.CommitFunc != nil {
		return m.CommitFunc(ctx)
	}
	return m.Called(ctx).Error(0)
}

func (m *MockTx) Begin(ctx context.Context) (pgx.Tx, error) {
	if m.BeginFunc != nil {
		return m.BeginFunc(ctx)
	}
	return m.Called(ctx).Get(0).(pgx.Tx), m.Called(ctx).Error(1)
}

func (m *MockTx) Conn() *pgx.Conn {
	if m.ConnFunc != nil {
		return m.ConnFunc()
	}
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(*pgx.Conn)
}

func (m *MockTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	args := m.Called(ctx, tableName, columnNames, rowSrc)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockTx) LargeObjects() pgx.LargeObjects {
	args := m.Called()
	if args.Get(0) == nil {
		return pgx.LargeObjects{}
	}
	return args.Get(0).(pgx.LargeObjects)
}

func (m *MockTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	args := m.Called(ctx, name, sql)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pgconn.StatementDescription), args.Error(1)
}

func (m *MockTx) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return m.Called(ctx, sql, args).Get(0).(pgx.Rows), m.Called(ctx, sql, args).Error(1)
}

func (m *MockTx) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	return m.Called(ctx, sql, args).Get(0).(pgx.Row)
}

func (m *MockTx) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	args := m.Called(ctx, batch)
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(pgx.BatchResults)
}

// MockCommandTag implements pgconn.CommandTag for testing
type MockCommandTag struct {
	rowsAffected int64
}

func (m *MockCommandTag) RowsAffected() int64 {
	return m.rowsAffected
}

// Additional methods required for pgconn.CommandTag interface
func (m *MockCommandTag) Insert() bool   { return false }
func (m *MockCommandTag) Update() bool   { return false }
func (m *MockCommandTag) Delete() bool   { return false }
func (m *MockCommandTag) Select() bool   { return false }
func (m *MockCommandTag) String() string { return "MOCK" }
func (m *MockCommandTag) Oid() uint32    { return 0 }

// MockResult implements pgconn.CommandTag for testing
type MockResult struct {
	commandTag       MockCommandTag
	RowsAffectedFunc func() int64
}

func (m *MockResult) RowsAffected() int64 {
	if m.RowsAffectedFunc != nil {
		return m.RowsAffectedFunc()
	}
	return m.commandTag.rowsAffected
}

// Additional methods required for pgconn.CommandTag interface
func (m *MockResult) Insert() bool   { return false }
func (m *MockResult) Update() bool   { return false }
func (m *MockResult) Delete() bool   { return false }
func (m *MockResult) Select() bool   { return false }
func (m *MockResult) String() string { return "MOCK" }
func (m *MockResult) Oid() uint32    { return 0 }

// MockRow implements pgx.Row for testing
type MockRow struct {
	mock.Mock
	ScanFunc func(dest ...interface{}) error
}

func (m *MockRow) Scan(dest ...interface{}) error {
	if m.ScanFunc != nil {
		return m.ScanFunc(dest...)
	}
	return m.Called(dest).Error(0)
}

func (m *MockRedisClient) Get(ctx context.Context, key string) *redis.StringCmd {
	args := m.Called(ctx, key)
	return args.Get(0).(*redis.StringCmd)
}

func (m *MockRedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	args := m.Called(ctx, key, value, expiration)
	return args.Get(0).(*redis.StatusCmd)
}

// Mock implementations for CCXTService interface methods
func (m *MockCCXTService) Initialize(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockCCXTService) IsHealthy(ctx context.Context) bool {
	args := m.Called(ctx)
	return args.Bool(0)
}

func (m *MockCCXTService) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockCCXTService) GetServiceURL() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockCCXTService) GetSupportedExchanges() []string {
	args := m.Called()
	return args.Get(0).([]string)
}

func (m *MockCCXTService) GetExchangeInfo(exchangeID string) (ccxt.ExchangeInfo, bool) {
	args := m.Called(exchangeID)
	return args.Get(0).(ccxt.ExchangeInfo), args.Bool(1)
}

func (m *MockCCXTService) GetExchangeConfig(ctx context.Context) (*ccxt.ExchangeConfigResponse, error) {
	args := m.Called(ctx)
	return args.Get(0).(*ccxt.ExchangeConfigResponse), args.Error(1)
}

func (m *MockCCXTService) AddExchangeToBlacklist(ctx context.Context, exchange string) (*ccxt.ExchangeManagementResponse, error) {
	args := m.Called(ctx, exchange)
	return args.Get(0).(*ccxt.ExchangeManagementResponse), args.Error(1)
}

func (m *MockCCXTService) RemoveExchangeFromBlacklist(ctx context.Context, exchange string) (*ccxt.ExchangeManagementResponse, error) {
	args := m.Called(ctx, exchange)
	return args.Get(0).(*ccxt.ExchangeManagementResponse), args.Error(1)
}

func (m *MockCCXTService) RefreshExchanges(ctx context.Context) (*ccxt.ExchangeManagementResponse, error) {
	args := m.Called(ctx)
	return args.Get(0).(*ccxt.ExchangeManagementResponse), args.Error(1)
}

func (m *MockCCXTService) AddExchange(ctx context.Context, exchange string) (*ccxt.ExchangeManagementResponse, error) {
	args := m.Called(ctx, exchange)
	return args.Get(0).(*ccxt.ExchangeManagementResponse), args.Error(1)
}

func (m *MockCCXTService) FetchMarketData(ctx context.Context, exchanges []string, symbols []string) ([]ccxt.MarketPriceInterface, error) {
	args := m.Called(ctx, exchanges, symbols)
	return args.Get(0).([]ccxt.MarketPriceInterface), args.Error(1)
}

func (m *MockCCXTService) FetchSingleTicker(ctx context.Context, exchange, symbol string) (ccxt.MarketPriceInterface, error) {
	args := m.Called(ctx, exchange, symbol)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(ccxt.MarketPriceInterface), args.Error(1)
}

func (m *MockCCXTService) FetchOrderBook(ctx context.Context, exchange, symbol string, limit int) (*ccxt.OrderBookResponse, error) {
	args := m.Called(ctx, exchange, symbol, limit)
	return args.Get(0).(*ccxt.OrderBookResponse), args.Error(1)
}

func (m *MockCCXTService) CalculateOrderBookMetrics(ctx context.Context, exchange, symbol string, limit int) (*ccxt.OrderBookMetrics, error) {
	args := m.Called(ctx, exchange, symbol, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ccxt.OrderBookMetrics), args.Error(1)
}

func (m *MockCCXTService) FetchOHLCV(ctx context.Context, exchange, symbol, timeframe string, limit int) (*ccxt.OHLCVResponse, error) {
	args := m.Called(ctx, exchange, symbol, timeframe, limit)
	return args.Get(0).(*ccxt.OHLCVResponse), args.Error(1)
}

func (m *MockCCXTService) FetchTrades(ctx context.Context, exchange, symbol string, limit int) (*ccxt.TradesResponse, error) {
	args := m.Called(ctx, exchange, symbol, limit)
	return args.Get(0).(*ccxt.TradesResponse), args.Error(1)
}

func (m *MockCCXTService) FetchMarkets(ctx context.Context, exchange string) (*ccxt.MarketsResponse, error) {
	args := m.Called(ctx, exchange)
	return args.Get(0).(*ccxt.MarketsResponse), args.Error(1)
}

func (m *MockCCXTService) FetchFundingRate(ctx context.Context, exchange, symbol string) (*ccxt.FundingRate, error) {
	args := m.Called(ctx, exchange, symbol)
	return args.Get(0).(*ccxt.FundingRate), args.Error(1)
}

func (m *MockCCXTService) FetchFundingRates(ctx context.Context, exchange string, symbols []string) ([]ccxt.FundingRate, error) {
	args := m.Called(ctx, exchange, symbols)
	return args.Get(0).([]ccxt.FundingRate), args.Error(1)
}

func (m *MockCCXTService) FetchAllFundingRates(ctx context.Context, exchange string) ([]ccxt.FundingRate, error) {
	args := m.Called(ctx, exchange)
	return args.Get(0).([]ccxt.FundingRate), args.Error(1)
}

func (m *MockCCXTService) CalculateArbitrageOpportunities(ctx context.Context, exchanges []string, symbols []string, minProfitPercent decimal.Decimal) ([]models.ArbitrageOpportunityResponse, error) {
	args := m.Called(ctx, exchanges, symbols, minProfitPercent)
	return args.Get(0).([]models.ArbitrageOpportunityResponse), args.Error(1)
}

func (m *MockCCXTService) CalculateFundingRateArbitrage(ctx context.Context, symbols []string, exchanges []string, minProfit float64) ([]ccxt.FundingArbitrageOpportunity, error) {
	args := m.Called(ctx, symbols, exchanges, minProfit)
	return args.Get(0).([]ccxt.FundingArbitrageOpportunity), args.Error(1)
}

func (m *MockCCXTService) FetchBalance(ctx context.Context, exchange string) (*ccxt.BalanceResponse, error) {
	args := m.Called(ctx, exchange)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ccxt.BalanceResponse), args.Error(1)
}

// Mock implementations for CollectorService
func (m *MockCollectorService) Start() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockCollectorService) Stop() {
	m.Called()
}

func (m *MockCollectorService) RestartWorker(exchange string) error {
	args := m.Called(exchange)
	return args.Error(0)
}

// Mock implementations for CacheAnalyticsService
func (m *MockCacheAnalyticsService) GetCacheStats() map[string]interface{} {
	args := m.Called()
	return args.Get(0).(map[string]interface{})
}

func (m *MockCacheAnalyticsService) GetCacheStatsByCategory(category string) map[string]interface{} {
	args := m.Called(category)
	return args.Get(0).(map[string]interface{})
}

func (m *MockCacheAnalyticsService) GetCacheMetrics() map[string]interface{} {
	args := m.Called()
	return args.Get(0).(map[string]interface{})
}

func (m *MockCacheAnalyticsService) ResetCacheStats() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockCacheAnalyticsService) RecordCacheHit(category string) {
	m.Called(category)
}

func (m *MockCacheAnalyticsService) RecordCacheMiss(category string) {
	m.Called(category)
}

func (m *MockSpotArbitrageCalculator) GetCalculatorStats() map[string]interface{} {
	args := m.Called()
	return args.Get(0).(map[string]interface{})
}
