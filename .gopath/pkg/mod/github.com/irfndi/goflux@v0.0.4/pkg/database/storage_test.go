package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStorageInterface(t *testing.T) {
	t.Run("InfluxDBStorage creation", func(t *testing.T) {
		storage, err := NewInfluxDBStorage("http://localhost:8086", "testdb")
		assert.NoError(t, err)
		assert.NotNil(t, storage)
	})

	t.Run("TimescaleDBStorage creation", func(t *testing.T) {
		storage, err := NewTimescaleDBStorage("postgres://user:pass@localhost/testdb")
		assert.NoError(t, err)
		assert.NotNil(t, storage)
	})

	t.Run("not implemented methods return errors", func(t *testing.T) {
		storage, _ := NewInfluxDBStorage("http://localhost:8086", "testdb")

		err := storage.StoreCandle("BTC", &Candle{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})
}
