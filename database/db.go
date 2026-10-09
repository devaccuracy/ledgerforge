/*
Copyright 2024 Blnk Finance Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package database

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"

	"github.com/devaccuracy/ledgerforge/config"
	"github.com/devaccuracy/ledgerforge/internal/cache"
	pgconn "github.com/devaccuracy/ledgerforge/internal/pgconn"
	"github.com/sirupsen/logrus"
)

// Declare a package-level variable to hold the singleton instance.
//
// sync.Once is not usable here: a Once latches on its first run whether that
// run succeeded or not, so one failed connection attempt would leave instance
// nil forever and hand that nil to every later caller. Connecting deserves a
// second attempt, so a failure must leave the singleton unset and retryable.
//
// The retry has to be added without giving up the concurrency a completed
// Once provides. GetDBConnection sits on request paths -- the health endpoint
// and two transaction handlers call it per request -- so serializing every
// caller on a mutex just to read a pointer that never changes again would be
// a regression. instance is therefore read through an atomic pointer on the
// fast path, and instanceMu is taken only to build the singleton, with a
// second check under the lock so concurrent first callers connect once.
var (
	instance   atomic.Pointer[Datasource]
	instanceMu sync.Mutex
	connectDB  = ConnectDB
	newCache   = cache.NewCache
)

type Datasource struct {
	Conn                 *sql.DB
	Cache                cache.Cache
	disableLegacyAPIKeys bool
	legacyAPIKeyWarning  *sync.Once
}

// Close closes the underlying database connection pool.
func (d *Datasource) Close() error {
	if d.Conn != nil {
		return d.Conn.Close()
	}
	return nil
}

// NewDataSource initializes a new database connection.
func NewDataSource(configuration *config.Configuration) (IDataSource, error) {
	con, err := GetDBConnection(configuration)
	if err != nil {
		return nil, err
	}

	// Set the default schema for this connection.
	if _, err := con.Conn.ExecContext(context.Background(), "SET search_path TO ledgerforge"); err != nil {
		return nil, err
	}
	con.legacyAPIKeyWarning.Do(func() { con.warnLegacyAPIKeys(context.Background()) })
	return con, nil
}

// GetDBConnection ensures a single database connection instance.
//
// It never returns a nil *Datasource alongside a nil error: either the
// singleton is returned, or the connection error that prevented building it.
func GetDBConnection(configuration *config.Configuration) (*Datasource, error) {
	// Fast path: once built, the singleton is read without locking.
	if ds := instance.Load(); ds != nil {
		return ds, nil
	}

	instanceMu.Lock()
	defer instanceMu.Unlock()

	// Another caller may have built it while we waited for the lock.
	if ds := instance.Load(); ds != nil {
		return ds, nil
	}

	con, err := connectDB(configuration.DataSource)
	if err != nil {
		// Leave the singleton unset so a later call can retry rather than
		// being permanently poisoned by one unreachable-database moment.
		return nil, err
	}

	cacheInstance, errCache := newCache()
	if errCache != nil {
		logrus.Errorf("Error creating cache: %v", errCache)
		// Continue without cache instead of failing completely.
	}

	ds := &Datasource{Conn: con, Cache: cacheInstance,
		disableLegacyAPIKeys: configuration.Server.AllowLegacyAPIKeys != nil && !*configuration.Server.AllowLegacyAPIKeys,
		legacyAPIKeyWarning:  &sync.Once{},
	}
	instance.Store(ds)
	return ds, nil
}

// ConnectDB establishes a database connection with pooling.
func ConnectDB(dsConfig config.DataSourceConfig) (*sql.DB, error) {
	return pgconn.ConnectDB(dsConfig)
}
