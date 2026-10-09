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
	"fmt"
	"time"

	"github.com/devaccuracy/ledgerforge/internal/apierror"
	"github.com/devaccuracy/ledgerforge/model"
	"go.opentelemetry.io/otel"
)

// chainKey is the partition key of the single global chain. The chain_state
// table is keyed so the chain can be partitioned later without a redesign.
const chainKey = "global"

// ChainPendingTransactions seals the next batch of unchained transactions into
// the global hash chain in a single DB transaction. It locks the one chain_state
// row FOR UPDATE (serializing every chainer instance — a second writer blocks,
// then re-reads the advanced head), scans unchained rows older than cutoff in id
// order, links each onto the head with canonical v3, writes their
// chain_* columns, and advances chain_state — all atomically, so a crash mid
// batch loses nothing. Returns the number of rows sealed.
func (d Datasource) ChainPendingTransactions(ctx context.Context, cutoff time.Time, batchSize int) (int, error) {
	if batchSize <= 0 {
		return 0, fmt.Errorf("chain batch size must be positive")
	}
	ctx, span := otel.Tracer("transaction.database").Start(ctx, "ChainPendingTransactions")
	defer span.End()

	tx, err := d.Conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return 0, apierror.NewAPIError(apierror.ErrInternalServer, "Failed to begin chain transaction", err)
	}
	defer func() { _ = tx.Rollback() }()

	var lastSeq int64
	var head string
	if err := tx.QueryRowContext(ctx,
		`SELECT last_seq, head_hash FROM ledgerforge.chain_state WHERE chain_key = $1 FOR UPDATE`,
		chainKey,
	).Scan(&lastSeq, &head); err != nil {
		return 0, apierror.NewAPIError(apierror.ErrInternalServer, "Failed to lock chain state", err)
	}

	rows, err := tx.QueryContext(ctx, `SELECT `+chainRowColumns+`
 FROM ledgerforge.transactions
 WHERE chain_seq IS NULL AND created_at < $1
 ORDER BY id ASC LIMIT $2 FOR UPDATE`, cutoff, batchSize)
	if err != nil {
		return 0, apierror.NewAPIError(apierror.ErrInternalServer, "Failed to scan unchained transactions", err)
	}

	type link struct {
		id       string
		seq      int64
		prevHash string
		hash     string
	}
	var batch []link
	for rows.Next() {
		r, err := scanChainRow(rows)
		if err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("read unchained transaction: %w", err)
		}
		prev := head
		head, err = model.ComputeChainHashVersion(3, prev, r)
		if err != nil {
			_ = rows.Close()
			return 0, err
		}
		lastSeq++
		batch = append(batch, link{id: r.ID, seq: lastSeq, prevHash: prev, hash: head})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, apierror.NewAPIError(apierror.ErrInternalServer, "Error iterating unchained transactions", err)
	}
	// The rows cursor must be closed before issuing further statements on this tx.
	_ = rows.Close()

	if len(batch) == 0 {
		// Commit to release the chain_state lock without advancing.
		if err := tx.Commit(); err != nil {
			return 0, apierror.NewAPIError(apierror.ErrInternalServer, "Failed to commit empty chain batch", err)
		}
		return 0, nil
	}

	stmt, err := tx.PrepareContext(ctx,
		`UPDATE ledgerforge.transactions SET chain_seq = $1, chain_prev_hash = $2, chain_hash = $3, chain_version = 3 WHERE id = $4 AND chain_seq IS NULL`)
	if err != nil {
		return 0, apierror.NewAPIError(apierror.ErrInternalServer, "Failed to prepare chain update", err)
	}
	defer func() { _ = stmt.Close() }()
	for _, l := range batch {
		if _, err := stmt.ExecContext(ctx, l.seq, l.prevHash, l.hash, l.id); err != nil {
			return 0, apierror.NewAPIError(apierror.ErrInternalServer, "Failed to write chain link", err)
		}

		if l.seq%1000 == 0 || l.seq == lastSeq {
			if _, err := tx.ExecContext(ctx, `INSERT INTO ledgerforge.chain_checkpoints (sequence, entry_count, head_hash) VALUES ($1, $1, $2)`, l.seq, l.hash); err != nil {
				return 0, fmt.Errorf("write chain checkpoint: %w", err)
			}
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE ledgerforge.chain_state SET last_seq = $1, head_hash = $2, updated_at = timezone('UTC', now()) WHERE chain_key = $3`,
		lastSeq, head, chainKey,
	); err != nil {
		return 0, apierror.NewAPIError(apierror.ErrInternalServer, "Failed to advance chain state", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, apierror.NewAPIError(apierror.ErrInternalServer, "Failed to commit chain batch", err)
	}

	span.AddEvent("Chained transaction batch")
	return len(batch), nil
}

// GetChainState returns the global chain bookmark row.
func (d Datasource) GetChainState(ctx context.Context) (*model.ChainState, error) {
	var s model.ChainState
	err := d.Conn.QueryRowContext(ctx,
		`SELECT chain_key, last_seq, head_hash, genesis_hash, genesis_at, updated_at
		 FROM ledgerforge.chain_state WHERE chain_key = $1`, chainKey,
	).Scan(&s.ChainKey, &s.LastSeq, &s.HeadHash, &s.GenesisHash, &s.GenesisAt, &s.UpdatedAt)
	if err != nil {
		return nil, apierror.NewAPIError(apierror.ErrInternalServer, "Failed to get chain state", err)
	}
	return &s, nil
}

// GetChainedTransactionsAfter returns chained transactions in chain order with
// chain_seq > afterSeq, for the verifier to page through from genesis.
func (d Datasource) GetChainedTransactionsAfter(ctx context.Context, afterSeq int64, limit int) ([]model.ChainedTransaction, error) {
	rows, err := d.Conn.QueryContext(ctx, `SELECT chain_seq, COALESCE(chain_prev_hash, ''), COALESCE(chain_hash, ''), COALESCE(chain_version, 1), `+chainRowColumns+`
 FROM ledgerforge.transactions WHERE chain_seq IS NOT NULL AND chain_seq > $1
 ORDER BY chain_seq ASC LIMIT $2`, afterSeq, limit)
	if err != nil {
		return nil, apierror.NewAPIError(apierror.ErrInternalServer, "Failed to query chained transactions", err)
	}
	defer func() { _ = rows.Close() }()

	var out []model.ChainedTransaction
	for rows.Next() {
		var ct model.ChainedTransaction
		if err := scanChainedTransaction(rows, &ct); err != nil {
			return nil, fmt.Errorf("scan chained transaction: %w", err)
		}
		out = append(out, ct)
	}
	if err := rows.Err(); err != nil {
		return nil, apierror.NewAPIError(apierror.ErrInternalServer, "Error iterating chained transactions", err)
	}
	return out, nil
}

// CountUnchainedTransactions returns how many transactions older than cutoff are
// still unchained — the chainer's backlog, for the lag metric.
func (d Datasource) CountUnchainedTransactions(ctx context.Context, cutoff time.Time) (int64, error) {
	var n int64
	if err := d.Conn.QueryRowContext(ctx,
		`SELECT count(*) FROM ledgerforge.transactions WHERE chain_seq IS NULL AND created_at < $1`, cutoff,
	).Scan(&n); err != nil {
		return 0, apierror.NewAPIError(apierror.ErrInternalServer, "Failed to count unchained transactions", err)
	}
	return n, nil
}

// chainRowColumns is shared by the chainer and snapshot verifier. PostgreSQL
// NUMERIC text and timestamps are read directly, without a float round trip.
const chainRowColumns = `id::text, transaction_id, COALESCE(source, ''), COALESCE(destination, ''),
 COALESCE(amount::text, ''), COALESCE(precise_amount::text, ''),
 COALESCE(currency, ''), COALESCE(status, ''), COALESCE(reference, ''), created_at,
 COALESCE(precision::text, ''), COALESCE(rate::text, ''), COALESCE(parent_transaction, ''), effective_date, COALESCE(overdraft_limit::text, ''), COALESCE(queue_action, ''),
 ledgerforge.transaction_execution_controls(transaction_id, execution_controls)::text, scheduled_for,
 COALESCE((SELECT j.new_data->>'rate' FROM ledgerforge.transaction_journal j
 JOIN ledgerforge.chain_schema_transitions transition ON transition.column_name = 'rate'
 WHERE j.transaction_id = transactions.transaction_id AND j.action_type = 'INSERT' AND j.succeeded
 AND j.id <= transition.last_legacy_journal_id AND transactions.execution_controls IS NULL
 ORDER BY j.id LIMIT 1), '')`

type chainScanner interface{ Scan(...interface{}) error }

func chainRowTargets(r *model.ChainRow) []interface{} {
	return []interface{}{&r.ID, &r.TransactionID, &r.Source, &r.Destination, &r.Amount,
		&r.PreciseAmount, &r.Currency, &r.Status, &r.Reference, &r.CreatedAt,
		&r.Precision, &r.Rate, &r.ParentTransaction, &r.EffectiveDate, &r.OverdraftLimit, &r.QueueAction, &r.ExecutionControls, &r.ScheduledFor, &r.HistoricalRate}
}
func scanChainRow(s chainScanner) (model.ChainRow, error) {
	var r model.ChainRow
	err := s.Scan(chainRowTargets(&r)...)
	return r, err
}
func scanChainedTransaction(s chainScanner, ct *model.ChainedTransaction) error {
	targets := []interface{}{&ct.ChainSeq, &ct.ChainPrevHash, &ct.ChainHash, &ct.CanonicalVersion}
	return s.Scan(append(targets, chainRowTargets(&ct.Row)...)...)
}

// VerifyChainSnapshot reads the head, links, checkpoints, and coverage in one
// read-only REPEATABLE READ snapshot. Sequence denotes audit observation order,
// not transaction ID or PostgreSQL commit order.
func (d Datasource) VerifyChainSnapshot(ctx context.Context, progress func(int64)) (model.ChainVerifyResult, error) {
	return d.VerifyChainSnapshotWithAnchors(ctx, progress, nil)
}

// VerifyChainSnapshotWithAnchors also compares independently retained roots.
// Database-only commitments cannot prove integrity against an administrator
// who rewrites both the evidence and its hashes; exported anchors supply that
// independent trust boundary.
func (d Datasource) VerifyChainSnapshotWithAnchors(ctx context.Context, progress func(int64), anchors []model.ChainCheckpoint) (model.ChainVerifyResult, error) {
	tx, err := d.Conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return model.ChainVerifyResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result := model.ChainVerifyResult{Tampering: []model.ChainIssue{}}
	var state model.ChainState
	if err := tx.QueryRowContext(ctx, `SELECT last_seq, head_hash, genesis_hash FROM ledgerforge.chain_state WHERE chain_key = $1`, chainKey).Scan(&state.LastSeq, &state.HeadHash, &state.GenesisHash); err != nil {
		return result, err
	}
	checkpoints, err := readChainCheckpoints(ctx, tx)
	if err != nil {
		return result, err
	}
	roots := make(map[int64]string, len(checkpoints))
	for _, cp := range checkpoints {
		roots[cp.Sequence] = cp.HeadHash
	}
	add := func(seq int64, id, reason string) {
		result.Tampering = append(result.Tampering, model.ChainIssue{Sequence: seq, TransactionID: id, Reason: reason})
		if result.Reason == "" {
			result.BrokenSeq, result.BrokenTxnID, result.Reason = seq, id, reason
		}
	}

	for _, anchor := range anchors {
		if anchor.Sequence <= 0 || anchor.Count != anchor.Sequence {
			add(anchor.Sequence, "", "invalid external checkpoint count or sequence")
		} else if root, ok := roots[anchor.Sequence]; !ok || root != anchor.HeadHash {
			add(anchor.Sequence, "", "external checkpoint root does not match stored checkpoint")
		}
	}
	if state.GenesisHash != model.ChainGenesisHash {
		add(0, "", "unexpected genesis hash")
	}
	head := state.GenesisHash
	rows, err := tx.QueryContext(ctx, `SELECT chain_seq, COALESCE(chain_prev_hash, ''), COALESCE(chain_hash, ''), COALESCE(chain_version, 1), `+chainRowColumns+` FROM ledgerforge.transactions WHERE chain_seq IS NOT NULL ORDER BY chain_seq`)
	if err != nil {
		return result, err
	}
	var count int64
	for rows.Next() {
		var ct model.ChainedTransaction
		if err := scanChainedTransaction(rows, &ct); err != nil {
			_ = rows.Close()
			return result, err
		}
		count++
		if ct.ChainSeq != count {
			add(ct.ChainSeq, ct.Row.TransactionID, fmt.Sprintf("non-contiguous chain_seq: expected %d, got %d", count, ct.ChainSeq))
		}
		if ct.ChainPrevHash != head {
			add(ct.ChainSeq, ct.Row.TransactionID, "chain_prev_hash does not match running head")
		}
		hash, hashErr := model.ComputeChainHashVersion(ct.CanonicalVersion, ct.ChainPrevHash, ct.Row)
		if hashErr != nil {
			add(ct.ChainSeq, ct.Row.TransactionID, hashErr.Error())
		} else if hash != ct.ChainHash {
			add(ct.ChainSeq, ct.Row.TransactionID, "live financial data does not match stored chain_hash")
		}
		if root, ok := roots[ct.ChainSeq]; ok {
			if root != ct.ChainHash {
				add(ct.ChainSeq, ct.Row.TransactionID, "checkpoint root does not match chain")
			}
			delete(roots, ct.ChainSeq)
		}
		head, result.LastSeq = ct.ChainHash, ct.ChainSeq
		if progress != nil {
			progress(ct.ChainSeq)
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return result, err
	}
	result.HeadHash = head
	if count != state.LastSeq || result.LastSeq != state.LastSeq || head != state.HeadHash {
		add(state.LastSeq, "", "replayed length or head does not match chain_state")
	}
	for _, cp := range checkpoints {
		if _, missing := roots[cp.Sequence]; missing {
			add(cp.Sequence, "", "checkpoint refers to a missing chain entry")
		}
		if cp.Count != cp.Sequence {
			add(cp.Sequence, "", "checkpoint count does not match sequence")
		}
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM ledgerforge.transactions WHERE chain_seq IS NULL`).Scan(&result.Pending); err != nil {
		return result, err
	}
	// The existing INSERT journal also witnesses rows deleted or changed before
	// they can be sealed. Metadata and chain updates are deliberately excluded.
	rows, err = tx.QueryContext(ctx, `SELECT DISTINCT j.transaction_id,
 CASE WHEN t.id IS NULL THEN 'committed transaction missing from live data (INSERT journal evidence)'
 ELSE 'live financial data differs from INSERT journal evidence' END
 FROM ledgerforge.transaction_journal j LEFT JOIN ledgerforge.transactions t ON t.transaction_id = j.transaction_id
 WHERE j.action_type = 'INSERT' AND j.succeeded AND (t.id IS NULL OR EXISTS (
 SELECT 1 FROM jsonb_each(j.new_data) e
 WHERE e.key NOT IN ('meta_data','chain_seq','chain_prev_hash','chain_hash','chain_version')
 AND e.value IS DISTINCT FROM to_jsonb(t)->e.key
 AND NOT (e.key = 'rate' AND t.execution_controls IS NULL AND EXISTS (
 SELECT 1 FROM ledgerforge.chain_schema_transitions transition
 WHERE transition.column_name = e.key AND j.id <= transition.last_legacy_journal_id
 AND to_jsonb(t)->e.key = transition.restored_default)))) ORDER BY j.transaction_id`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var id, reason string
		if err := rows.Scan(&id, &reason); err != nil {
			_ = rows.Close()
			return result, err
		}
		add(0, id, reason)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return result, err
	}
	result.Verified = len(result.Tampering) == 0
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

type chainQuerier interface {
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
}

func readChainCheckpoints(ctx context.Context, q chainQuerier) ([]model.ChainCheckpoint, error) {
	rows, err := q.QueryContext(ctx, `SELECT sequence, entry_count, head_hash, created_at FROM ledgerforge.chain_checkpoints ORDER BY sequence`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []model.ChainCheckpoint{}
	for rows.Next() {
		var cp model.ChainCheckpoint
		if err := rows.Scan(&cp.Sequence, &cp.Count, &cp.HeadHash, &cp.CreatedAt); err != nil {
			return nil, err
		}
		cp.CreatedAt = cp.CreatedAt.UTC()
		out = append(out, cp)
	}
	return out, rows.Err()
}

func (d Datasource) ExportChainCheckpoints(ctx context.Context) ([]model.ChainCheckpoint, error) {
	return readChainCheckpoints(ctx, d.Conn)
}
