/*
Copyright 2026 LedgerForge Contributors.

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
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/devaccuracy/ledgerforge/config"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/lib/pq"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
)

func keyLookupRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"api_key_id", "key", "name", "owner_id", "scopes", "expires_at", "created_at", "last_used_at", "is_revoked", "revoked_at", "key_prefix", "parent_api_key_id"})
}

func TestAPIKeyLegacyLookupOnlyAfterMissingPrefix(t *testing.T) {
	hash, err := hashAPIKey("0123456789abcdef-valid")
	require.NoError(t, err)
	for _, key := range []string{"0123456789abcdef-valid", "0123456789abcdef-wrong"} {
		t.Run(key, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			require.NoError(t, err)
			t.Cleanup(func() {
				mock.ExpectClose()
				require.NoError(t, db.Close())
			})
			mock.ExpectQuery(prefixedAPIKeyQuery).WithArgs(getKeyPrefix(key)).WillReturnRows(keyLookupRows().AddRow("id", hash, "name", "owner", pq.StringArray{"ledgers:read"}, time.Now().Add(time.Hour), time.Now(), time.Now(), false, nil, getKeyPrefix(key), ""))
			ds := Datasource{Conn: db}
			got, err := ds.GetAPIKey(context.Background(), key)
			if key == "0123456789abcdef-valid" {
				require.NoError(t, err)
				require.Equal(t, "id", got.APIKeyID)
			} else {
				require.ErrorIs(t, err, ErrAPIKeyNotFound)
			}
			require.NoError(t, mock.ExpectationsWereMet(), "matching prefixes must not trigger a legacy scan")
		})
	}
}

func TestAPIKeyLegacyDisabledSkipsFallback(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
	})
	ds := Datasource{Conn: db, disableLegacyAPIKeys: true}
	mock.ExpectQuery(prefixedAPIKeyQuery).WithArgs("missing").WillReturnRows(keyLookupRows())
	_, err = ds.GetAPIKey(context.Background(), "missing")
	require.ErrorIs(t, err, ErrAPIKeyNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAPIKeyLegacyStartupWarningAndConfiguredFlag(t *testing.T) {
	mock := mockSingletonConnection(t)
	resetDBSingleton()
	t.Cleanup(resetDBSingleton)
	hooks := logrus.StandardLogger().Hooks
	hook := logtest.NewGlobal()
	t.Cleanup(func() { logrus.StandardLogger().ReplaceHooks(hooks) })
	allow := false
	cfg := &config.Configuration{Server: config.ServerConfig{AllowLegacyAPIKeys: &allow}, DataSource: config.DataSourceConfig{Dns: "postgres://postgres:password@localhost/ledgerforge?sslmode=disable"}}
	mock.ExpectExec("SET search_path TO ledgerforge").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT count(*) FROM ledgerforge.api_keys WHERE key_prefix IS NULL OR key_prefix = ''")).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	got, err := NewDataSource(cfg)
	require.NoError(t, err)
	require.True(t, got.(*Datasource).disableLegacyAPIKeys)
	entry := hook.LastEntry()
	require.NotNil(t, entry)
	require.Equal(t, logrus.WarnLevel, entry.Level)
	require.Equal(t, int64(3), entry.Data["legacy_api_key_count"])
	require.Equal(t, false, entry.Data["allow_legacy_api_keys"])
	require.Contains(t, entry.Message, "rotate legacy API keys")
	mock.ExpectExec("SET search_path TO ledgerforge").WillReturnResult(sqlmock.NewResult(0, 0))
	_, err = NewDataSource(cfg)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet(), "startup warning should run once per datasource")
}

func legacyTestKey(t *testing.T, ds *Datasource, expiry time.Time, revoked bool) *model.APIKey {
	t.Helper()
	ctx := context.Background()
	key, err := ds.CreateAPIKey(ctx, "legacy regression", model.GenerateUUIDWithSuffix("owner"), []string{"ledgers:read"}, expiry)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = ds.Conn.ExecContext(ctx, "DELETE FROM ledgerforge.api_keys WHERE api_key_id = $1", key.APIKeyID)
	})
	_, err = ds.Conn.ExecContext(ctx, "UPDATE ledgerforge.api_keys SET key_prefix = NULL, is_revoked = $2 WHERE api_key_id = $1", key.APIKeyID, revoked)
	require.NoError(t, err)
	return key
}

func TestAPIKeyLegacyRejectsInactiveWithoutBackfill(t *testing.T) {
	ds := openRealTestDB(t)
	for _, tc := range []struct {
		name    string
		expiry  time.Time
		revoked bool
	}{
		{"expired", time.Now().Add(-time.Hour), false}, {"revoked", time.Now().Add(time.Hour), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := legacyTestKey(t, &ds, tc.expiry, tc.revoked)
			_, err := ds.GetAPIKey(context.Background(), key.Key)
			require.ErrorIs(t, err, ErrAPIKeyNotFound)
			var count int
			require.NoError(t, ds.Conn.QueryRowContext(context.Background(), "SELECT count(*) FROM ledgerforge.api_keys WHERE api_key_id = $1 AND key_prefix IS NULL", key.APIKeyID).Scan(&count))
			require.Equal(t, 1, count, "inactive keys must not be verified or backfilled")
			rows, err := ds.Conn.QueryContext(context.Background(), legacyAPIKeyQuery)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, rows.Close()) })
			for rows.Next() {
				var id, hash, name, owner string
				var scopes pq.StringArray
				var expiry, created, used time.Time
				var revoked bool
				var revokedAt interface{}
				var prefix interface{}
				var parent string
				require.NoError(t, rows.Scan(&id, &hash, &name, &owner, &scopes, &expiry, &created, &used, &revoked, &revokedAt, &prefix, &parent))
				require.NotEqual(t, key.APIKeyID, id, "SQL must exclude inactive candidates before bcrypt")
			}
			require.NoError(t, rows.Err())
		})
	}
}

func TestAPIKeyLegacyVerificationGlobalLimitRealDB(t *testing.T) {
	ds := openRealTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	t.Cleanup(func() { cancel(); wg.Wait() })
	for i := 0; i < 12; i++ {
		legacyTestKey(t, &ds, time.Now().Add(time.Hour), false)
	}
	indexed, err := ds.CreateAPIKey(ctx, "indexed regression", model.GenerateUUIDWithSuffix("owner"), []string{"ledgers:read"}, time.Now().Add(time.Hour))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = ds.Conn.ExecContext(context.Background(), "DELETE FROM ledgerforge.api_keys WHERE api_key_id = $1", indexed.APIKeyID)
	})
	results := make(chan error, maxLegacyAPIKeyVerifications)
	for i := 0; i < maxLegacyAPIKeyVerifications; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := ds.GetAPIKey(ctx, "unknown-legacy-credential"); results <- err }()
	}
	// Register this last so cleanup always cancels workers before deleting keys.
	t.Cleanup(func() { cancel(); wg.Wait() })
	require.Eventually(t, func() bool { return len(legacyAPIKeyVerificationSlots) == maxLegacyAPIKeyVerifications }, 5*time.Second, time.Millisecond)
	otherDatasource := Datasource{Conn: ds.Conn}
	_, err = otherDatasource.GetAPIKey(ctx, "another-unknown-credential")
	require.ErrorIs(t, err, ErrLegacyAPIKeyVerificationBusy, "limit must apply across datasources")
	got, err := otherDatasource.GetAPIKey(ctx, indexed.Key)
	require.NoError(t, err, "legacy saturation must not block indexed credentials")
	require.Equal(t, indexed.APIKeyID, got.APIKeyID)
	cancel()
	wg.Wait()
	close(results)
	for err := range results {
		require.True(t, errors.Is(err, context.Canceled) || errors.Is(err, ErrAPIKeyNotFound), err)
	}
	require.Empty(t, legacyAPIKeyVerificationSlots, "cancellation must release verification capacity")
}
