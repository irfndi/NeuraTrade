package cache

import (
	"context"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/stretchr/testify/mock"
)

// MockBlacklistRepository is a mock implementation of BlacklistRepository for testing
type MockBlacklistRepository struct {
	mock.Mock
}

func (m *MockBlacklistRepository) AddExchange(ctx context.Context, exchangeName, reason string, expiresAt *time.Time) (*database.ExchangeBlacklistEntry, error) {
	args := m.Called(ctx, exchangeName, reason, expiresAt)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*database.ExchangeBlacklistEntry), args.Error(1)
}

func (m *MockBlacklistRepository) RemoveExchange(ctx context.Context, exchangeName string) error {
	args := m.Called(ctx, exchangeName)
	return args.Error(0)
}

func (m *MockBlacklistRepository) IsBlacklisted(ctx context.Context, exchangeName string) (bool, string, error) {
	args := m.Called(ctx, exchangeName)
	return args.Bool(0), args.String(1), args.Error(2)
}

func (m *MockBlacklistRepository) GetAllBlacklisted(ctx context.Context) ([]database.ExchangeBlacklistEntry, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]database.ExchangeBlacklistEntry), args.Error(1)
}

func (m *MockBlacklistRepository) CleanupExpired(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockBlacklistRepository) GetBlacklistHistory(ctx context.Context, limit int) ([]database.ExchangeBlacklistEntry, error) {
	args := m.Called(ctx, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]database.ExchangeBlacklistEntry), args.Error(1)
}

func (m *MockBlacklistRepository) ClearAll(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}
