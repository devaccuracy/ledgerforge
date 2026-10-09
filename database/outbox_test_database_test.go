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
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/lib/pq"
	migrate "github.com/rubenv/sql-migrate"
	"github.com/stretchr/testify/require"
)

// Outbox claims operate on every pending row, so each test needs its own database.
func openOutboxTestDB(t *testing.T) Datasource {
	t.Helper()
	admin := openRealTestDB(t)
	name := "outbox_" + strings.ReplaceAll(gofakeit.UUID(), "-", "_")
	_, err := admin.Conn.ExecContext(context.Background(), "CREATE DATABASE "+pq.QuoteIdentifier(name))
	require.NoError(t, err)
	var conn *sql.DB
	t.Cleanup(func() {
		if conn != nil {
			require.NoError(t, conn.Close())
		}
		_, err := admin.Conn.ExecContext(context.Background(), "DROP DATABASE "+pq.QuoteIdentifier(name)+" WITH (FORCE)")
		require.NoError(t, err, "remove the test database and all its fixtures")
	})
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = defaultRealTestDSN
	}
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	parsed.Path = "/" + name
	conn, err = sql.Open("postgres", parsed.String())
	require.NoError(t, err)
	migrations := migrate.FileMigrationSource{Dir: "../internal/core/sql"}
	_, err = (migrate.MigrationSet{SchemaName: "ledgerforge"}).Exec(conn, "postgres", migrations, migrate.Up)
	require.NoError(t, err)
	return Datasource{Conn: conn}
}

func TestOutboxDatabaseIsolation_RealDB(t *testing.T) {
	outside := openOutboxTestDB(t)
	external := newOutboxEntry("unrelated-")
	require.NoError(t, outside.InsertLineageOutbox(context.Background(), external))
	var externalName string
	require.NoError(t, outside.Conn.QueryRow("SELECT current_database()").Scan(&externalName))

	var ownName string
	t.Run("claims never park or consume another database's pending rows", func(t *testing.T) {
		inside := openOutboxTestDB(t)
		own := newOutboxEntry("isolated-")
		require.NoError(t, inside.InsertLineageOutbox(context.Background(), own))
		require.NoError(t, inside.Conn.QueryRow("SELECT current_database()").Scan(&ownName))
		require.NotEqual(t, externalName, ownName)
		claimed, err := inside.ClaimPendingOutboxEntries(context.Background(), 500, time.Minute)
		require.NoError(t, err)
		require.Len(t, claimed, 1)
		require.Equal(t, own.TransactionID, claimed[0].TransactionID)
		got, err := outside.GetOutboxByTransactionID(context.Background(), external.TransactionID)
		require.NoError(t, err)
		require.Equal(t, "pending", got.Status)
		require.Nil(t, got.LockedUntil, "unrelated pending work must remain available")
	})

	// Subtest cleanup has removed its database, including processing fixtures.
	var remaining int
	require.NoError(t, outside.Conn.QueryRow(`SELECT count(*) FROM pg_database WHERE datname = $1`, ownName).Scan(&remaining))
	require.Zero(t, remaining)
	claimed, err := outside.ClaimPendingOutboxEntries(context.Background(), 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, external.TransactionID, claimed[0].TransactionID)
}
