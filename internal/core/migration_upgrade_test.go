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

package core

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/database"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/lib/pq"
	migrate "github.com/rubenv/sql-migrate"
	"github.com/stretchr/testify/require"
)

const mainMigrationCommit = "feda9b1fb45489cbcae46f5eb803f97d919c414d"
const mainMigrationRoot = "testdata/migrations/main_feda9b1"

// Pin the historical SQL so upgrades run in shallow CI checkouts and test containers.
//
//go:embed testdata/migrations/main_feda9b1
var mainMigrationFixtures embed.FS

func TestMigrationBaselineFixture(t *testing.T) {
	manifest, err := mainMigrationFixtures.ReadFile(path.Join(mainMigrationRoot, "provenance.json"))
	require.NoError(t, err)
	var provenance struct {
		Commit string
		Files  map[string]string
	}
	require.NoError(t, json.Unmarshal(manifest, &provenance))
	require.Equal(t, mainMigrationCommit, provenance.Commit)
	source := migrate.EmbedFileSystemMigrationSource{FileSystem: mainMigrationFixtures, Root: mainMigrationRoot}
	migrations, err := source.FindMigrations()
	require.NoError(t, err)
	require.Len(t, migrations, 33, "the complete migration history from the pinned main commit")
	require.Len(t, provenance.Files, len(migrations))
	for _, migration := range migrations {
		data, err := mainMigrationFixtures.ReadFile(path.Join(mainMigrationRoot, migration.Id))
		require.NoError(t, err)
		sum := sha256.Sum256(data)
		require.Equal(t, provenance.Files[migration.Id], hex.EncodeToString(sum[:]), migration.Id)
	}
}

func openMigrationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	if testing.Short() {
		t.Skip("requires isolated PostgreSQL")
	}
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:password@localhost:5432/ledgerforge?sslmode=disable"
	}
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, admin.Close()) })
	require.NoError(t, admin.Ping())
	name := "migration_" + strings.ReplaceAll(model.GenerateUUIDWithSuffix("db"), "-", "_")
	_, err = admin.Exec("CREATE DATABASE " + pq.QuoteIdentifier(name))
	require.NoError(t, err)
	var conn *sql.DB
	t.Cleanup(func() {
		if conn != nil {
			require.NoError(t, conn.Close())
		}
		_, err := admin.Exec("DROP DATABASE " + pq.QuoteIdentifier(name) + " WITH (FORCE)")
		require.NoError(t, err)
	})
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	parsed.Path = "/" + name
	conn, err = sql.Open("postgres", parsed.String())
	require.NoError(t, err)
	return conn
}

// Compare economic data and original INSERT evidence, allowing additive schema fields.
func migrationLedgerSnapshot(t *testing.T, conn *sql.DB) string {
	t.Helper()
	var snapshot string
	err := conn.QueryRow(`SELECT jsonb_build_object(
        'transactions', (SELECT jsonb_agg(jsonb_build_object(
            'id', id, 'transaction_id', transaction_id, 'parent_transaction', parent_transaction,
            'source', source, 'destination', destination, 'currency', currency, 'reference', reference,
            'amount', amount, 'precise_amount', precise_amount, 'precision', precision,
            'status', status, 'created_at', created_at, 'scheduled_for', scheduled_for,
            'effective_date', effective_date, 'hash', hash, 'meta_data', meta_data
        ) ORDER BY id) FROM ledgerforge.transactions),
        'balances', (SELECT jsonb_agg(jsonb_build_object(
            'id', id, 'balance_id', balance_id, 'ledger_id', ledger_id, 'currency', currency,
            'balance', balance, 'credit_balance', credit_balance, 'debit_balance', debit_balance,
            'inflight_balance', inflight_balance, 'inflight_credit_balance', inflight_credit_balance,
            'inflight_debit_balance', inflight_debit_balance, 'version', version, 'meta_data', meta_data
        ) ORDER BY id) FROM ledgerforge.balances),
        'journal', (SELECT jsonb_agg(to_jsonb(j) ORDER BY id) FROM ledgerforge.transaction_journal j WHERE action_type = 'INSERT')
    )::text`).Scan(&snapshot)
	require.NoError(t, err)
	return snapshot
}

func seedMainMigrationLedger(t *testing.T, conn *sql.DB) {
	t.Helper()
	_, err := conn.Exec(`INSERT INTO ledgerforge.balances
        (balance_id,ledger_id,currency,balance,credit_balance,debit_balance,currency_multiplier,
         inflight_balance,inflight_credit_balance,inflight_debit_balance,meta_data)
        VALUES ('main_src','general_ledger_id','USD',9900,10100,200,100,-100,0,100,'{"customer":"source"}'),
               ('main_dst','general_ledger_id','USD',100,200,100,100,100,100,0,'{"customer":"destination"}');
        INSERT INTO ledgerforge.transactions
        (transaction_id,parent_transaction,source,destination,reference,amount,precise_amount,precision,
         rate,currency,status,created_at,effective_date,hash,meta_data)
        VALUES
        ('main_applied','','main_src','main_dst','main_applied_ref',1,100,100,1.25,'USD','APPLIED',
            '2026-01-02 03:04:05','2026-01-01 03:04:05','legacy_applied','{"customer":"original"}'),
        ('main_refund','main_applied','main_dst','main_src','main_applied_refund',1,100,100,1.25,'USD','APPLIED',
            '2026-01-02 03:04:06','2026-01-01 03:04:05','legacy_refund','{"customer":"refund"}'),
        ('main_hold','','main_src','main_dst','main_hold_ref',2,200,100,1,'USD','INFLIGHT',
            '2026-01-02 03:04:07','2026-01-02 03:04:07','legacy_hold','{"inflight":true,"customer":"hold"}'),
        ('main_partial','main_hold','main_src','main_dst','main_partial_ref',1,100,100,1,'USD','APPLIED',
            '2026-01-02 03:04:08','2026-01-02 03:04:07','legacy_partial','{"customer":"commit"}'),
        ('main_queued','','main_src','main_dst','main_queued_ref',0.5,50,100,1,'USD','QUEUED',
            '2026-01-02 03:04:09','2026-01-02 03:04:09','legacy_queued','{"allow_overdraft":false,"customer":"queue"}')`)
	require.NoError(t, err)
}

func TestMigrationUpgradeFromSeededMain(t *testing.T) {
	conn := openMigrationTestDB(t)
	set := migrate.MigrationSet{SchemaName: "ledgerforge"}
	baseline := migrate.EmbedFileSystemMigrationSource{FileSystem: mainMigrationFixtures, Root: mainMigrationRoot}
	baselineMigrations, err := baseline.FindMigrations()
	require.NoError(t, err)
	count, err := set.Exec(conn, "postgres", baseline, migrate.Up)
	require.NoError(t, err)
	require.Equal(t, len(baselineMigrations), count)
	seedMainMigrationLedger(t, conn)
	before := migrationLedgerSnapshot(t, conn)

	current := migrate.EmbedFileSystemMigrationSource{FileSystem: SQLFiles, Root: "sql"}
	count, err = set.Exec(conn, "postgres", current, migrate.Up)
	require.NoError(t, err, "upgrade the populated main schema")
	require.Positive(t, count)
	require.JSONEq(t, before, migrationLedgerSnapshot(t, conn), "preserve economic rows and historical INSERT evidence")
	count, err = set.Exec(conn, "postgres", current, migrate.Up)
	require.NoError(t, err)
	require.Zero(t, count, "a second upgrade must be a no-op")
	require.JSONEq(t, before, migrationLedgerSnapshot(t, conn))

	var claim string
	require.NoError(t, conn.QueryRow(`SELECT reversal_transaction FROM ledgerforge.refund_claims WHERE original_transaction='main_applied'`).Scan(&claim))
	require.Equal(t, "main_refund", claim)
	// Effective-rate schema changes must preserve the original rate as evidence,
	// and a legitimate upgrade must not look like post-migration tampering.
	var historicalRate string
	require.NoError(t, conn.QueryRow(`SELECT new_data->>'rate' FROM ledgerforge.transaction_journal
        WHERE transaction_id='main_applied' AND action_type='INSERT'`).Scan(&historicalRate))
	require.Equal(t, "1.25", historicalRate)
	result, err := (database.Datasource{Conn: conn}).VerifyChainSnapshot(context.Background(), nil)
	require.NoError(t, err)
	require.True(t, result.Verified, "unchanged main data must verify after upgrade: %+v", result.Tampering)
}

func TestMigrationUpgradePreservesV1Commitment(t *testing.T) {
	conn := openMigrationTestDB(t)
	set := migrate.MigrationSet{SchemaName: "ledgerforge"}
	current := migrate.EmbedFileSystemMigrationSource{FileSystem: SQLFiles, Root: "sql"}
	// Build a pre-v2 installation and seal a row using the original v1 encoding.
	_, err := set.ExecVersion(conn, "postgres", current, migrate.Up, 1781162329)
	require.NoError(t, err)
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	row := model.ChainRow{TransactionID: "v1_existing", Source: "", Destination: "", Amount: "0", PreciseAmount: "0",
		Currency: "USD", Status: "REJECTED", Reference: "v1_existing_ref", CreatedAt: created}
	hash := model.ComputeChainHash(model.ChainGenesisHash, row)
	_, err = conn.Exec(`INSERT INTO ledgerforge.transactions
        (transaction_id,source,destination,amount,precise_amount,currency,status,reference,created_at,
         chain_seq,chain_prev_hash,chain_hash)
        VALUES ($1,NULL,NULL,0,0,$2,$3,$4,$5,1,$6,$7)`, row.TransactionID, row.Currency, row.Status, row.Reference, created, model.ChainGenesisHash, hash)
	require.NoError(t, err)
	_, err = conn.Exec(`UPDATE ledgerforge.chain_state SET last_seq=1,head_hash=$1 WHERE chain_key='global'`, hash)
	require.NoError(t, err)

	count, err := set.Exec(conn, "postgres", current, migrate.Up)
	require.NoError(t, err)
	require.Positive(t, count)
	var preserved string
	require.NoError(t, conn.QueryRow(`SELECT chain_hash FROM ledgerforge.transactions WHERE transaction_id=$1`, row.TransactionID).Scan(&preserved))
	require.Equal(t, hash, preserved, "an upgrade must not rewrite a retained v1 commitment")
	anchors := []model.ChainCheckpoint{{Sequence: 1, Count: 1, HeadHash: hash}}
	result, err := (database.Datasource{Conn: conn}).VerifyChainSnapshotWithAnchors(context.Background(), nil, anchors)
	require.NoError(t, err)
	require.True(t, result.Verified, "retained v1 anchor must verify: %+v", result.Tampering)
	count, err = set.Exec(conn, "postgres", current, migrate.Up)
	require.NoError(t, err)
	require.Zero(t, count)
	_, err = conn.Exec(`DELETE FROM ledgerforge.transactions WHERE transaction_id=$1`, row.TransactionID)
	require.Error(t, err, "upgraded commitments must retain deletion protection")
}
