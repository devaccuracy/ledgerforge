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
	"fmt"

	"github.com/devaccuracy/ledgerforge/internal/integrity"
	"go.opentelemetry.io/otel"
)

// ReadIntegritySnapshot never writes, locks economic rows, or calls the legacy
// balance reconciliation triggers. All evidence is from one consistent snapshot.
func (d Datasource) ReadIntegritySnapshot(ctx context.Context) (integrity.Snapshot, error) {
	ctx, span := otel.Tracer("transaction.database").Start(ctx, "ReadIntegritySnapshot")
	defer span.End()
	var snapshot integrity.Snapshot
	tx, err := d.Conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return snapshot, fmt.Errorf("begin integrity snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT balance_id, currency, balance::text, credit_balance::text,
 debit_balance::text, inflight_balance::text, inflight_credit_balance::text, inflight_debit_balance::text
 FROM ledgerforge.balances ORDER BY id`)
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var b integrity.Balance
		if err := rows.Scan(&b.ID, &b.Currency, &b.Balance, &b.Credit, &b.Debit, &b.InflightBalance, &b.InflightCredit, &b.InflightDebit); err != nil {
			_ = rows.Close()
			return snapshot, err
		}
		snapshot.Balances = append(snapshot.Balances, b)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return snapshot, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT `+chainRowColumns+` FROM ledgerforge.transactions ORDER BY id`)
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		r, err := scanChainRow(rows)
		if err != nil {
			_ = rows.Close()
			return snapshot, err
		}
		snapshot.Transactions = append(snapshot.Transactions, r)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return snapshot, err
	}
	if err := tx.Commit(); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}
