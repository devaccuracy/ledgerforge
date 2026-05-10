package database

import (
	"database/sql"
	"sync"
	"testing"

	"github.com/devaccuracy/ledgerforge/config"
	"github.com/devaccuracy/ledgerforge/internal/cache"
	"github.com/stretchr/testify/assert"
)

func TestGetDBConnection_Singleton(t *testing.T) {
	// Reset the instance and once for testing purposes
	instance = nil
	once = sync.Once{}

	prevConnectDB := connectDB
	prevNewCache := newCache
	t.Cleanup(func() {
		connectDB = prevConnectDB
		newCache = prevNewCache
		instance = nil
		once = sync.Once{}
	})

	// Create a mock configuration with a valid DNS string
	mockConfig := &config.Configuration{
		DataSource: config.DataSourceConfig{
			Dns: "postgres://postgres:password@localhost/ledgerforge?sslmode=disable",
		},
	}

	config.ConfigStore.Store(mockConfig)

	connectDB = func(config.DataSourceConfig) (*sql.DB, error) {
		return &sql.DB{}, nil
	}
	newCache = func() (cache.Cache, error) {
		return nil, nil
	}

	// First call to GetDBConnection should initialize the instance
	ds1, err := GetDBConnection(mockConfig)
	assert.NoError(t, err)
	assert.NotNil(t, ds1)

	// Second call should return the same instance
	ds2, err := GetDBConnection(mockConfig)
	assert.NoError(t, err)
	assert.Equal(t, ds1, ds2)
}

func TestGetDBConnection_Failure(t *testing.T) {
	// Reset the instance and once for testing purposes
	instance = nil
	once = sync.Once{}

	prevConnectDB := connectDB
	prevNewCache := newCache
	t.Cleanup(func() {
		connectDB = prevConnectDB
		newCache = prevNewCache
		instance = nil
		once = sync.Once{}
	})

	// Create a mock configuration with invalid DNS
	mockConfig := &config.Configuration{
		DataSource: config.DataSourceConfig{
			Dns: "invalid-dns",
		},
	}

	connectDB = func(config.DataSourceConfig) (*sql.DB, error) {
		return nil, assert.AnError
	}

	// Expect error when connecting to DB with invalid DNS
	_, err := GetDBConnection(mockConfig)
	assert.Error(t, err)
}

func TestConnectDB_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("requires a running PostgreSQL instance")
	}

	t.Skip("set up a reachable PostgreSQL instance before enabling this integration test")
}

func TestConnectDB_Failure(t *testing.T) {
	// Provide an invalid DNS string to simulate a failure
	invalidDNS := "invalid-dns"

	db, err := ConnectDB(config.DataSourceConfig{Dns: invalidDNS})
	assert.Error(t, err)
	assert.Nil(t, db)
}
