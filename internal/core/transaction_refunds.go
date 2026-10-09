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

package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	redlock "github.com/devaccuracy/ledgerforge/internal/lock"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/lib/pq"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func (l *LedgerForge) RefundWorker(ctx context.Context, jobs <-chan *model.Transaction, results chan<- BatchJobResult, wg *sync.WaitGroup, amount *big.Int) {
	ctx, span := tracer.Start(ctx, "RefundWorker")
	defer span.End()

	defer wg.Done()
	for originalTxn := range jobs {
		queuedRefundTxn, err := l.RefundTransaction(ctx, originalTxn.TransactionID, originalTxn.SkipQueue)
		if err != nil {
			results <- BatchJobResult{Error: err}
			span.RecordError(err)
			continue
		}
		results <- BatchJobResult{Txn: queuedRefundTxn}
		span.AddEvent("Refund processed", trace.WithAttributes(attribute.String("transaction.id", queuedRefundTxn.TransactionID)))
	}
}

// RefundWorkerWithOptions returns a transactionWorker that refunds each
// job with an explicit skipQueue decision, rather than inheriting it
// from the original transaction.
//
// The caller can choose synchronous processing for the reversal independently
// of the immutable routing decision on the original transaction.
//
// RefundWorker is left unchanged; this is purely additive.
// RefundWorkerWithRefundOptions returns a transactionWorker that refunds each
// job using the supplied options, so a caller can set the reversal's
// description and metadata as well as its queueing.
//
// RefundWorkerWithOptions is the same worker with only SkipQueue set.
func (l *LedgerForge) RefundWorkerWithRefundOptions(opts RefundOptions) transactionWorker {
	return func(ctx context.Context, jobs <-chan *model.Transaction, results chan<- BatchJobResult, wg *sync.WaitGroup, amount *big.Int) {
		ctx, span := tracer.Start(ctx, "RefundWorkerWithRefundOptions")
		defer span.End()

		defer wg.Done()
		for originalTxn := range jobs {
			queuedRefundTxn, err := l.RefundTransactionWithOptions(ctx, originalTxn.TransactionID, opts)
			if err != nil {
				results <- BatchJobResult{Error: err}
				span.RecordError(err)
				continue
			}
			results <- BatchJobResult{Txn: queuedRefundTxn}
			span.AddEvent("Refund processed", trace.WithAttributes(attribute.String("transaction.id", queuedRefundTxn.TransactionID)))
		}
	}
}

func (l *LedgerForge) RefundWorkerWithOptions(skipQueue bool) transactionWorker {
	return func(ctx context.Context, jobs <-chan *model.Transaction, results chan<- BatchJobResult, wg *sync.WaitGroup, amount *big.Int) {
		ctx, span := tracer.Start(ctx, "RefundWorkerWithOptions")
		defer span.End()
		span.SetAttributes(attribute.Bool("refund.skip_queue", skipQueue))

		defer wg.Done()
		for originalTxn := range jobs {
			queuedRefundTxn, err := l.RefundTransaction(ctx, originalTxn.TransactionID, skipQueue)
			if err != nil {
				results <- BatchJobResult{Error: err}
				span.RecordError(err)
				continue
			}
			results <- BatchJobResult{Txn: queuedRefundTxn}
			span.AddEvent("Refund processed", trace.WithAttributes(attribute.String("transaction.id", queuedRefundTxn.TransactionID)))
		}
	}
}

// ProcessQueuedTransaction preserves the existing queued-worker behavior while routing the
// decision through the shared internal transaction executor.

func (l *LedgerForge) getOriginalTransactionForRefund(ctx context.Context, transactionID string) (*model.Transaction, error) {
	ctx, span := tracer.Start(ctx, "getOriginalTransactionForRefund")
	defer span.End()

	originalTxn, err := l.datasource.GetTransaction(ctx, transactionID)
	if err != nil {
		// Check if the error is due to no row found
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), fmt.Sprintf("Transaction with ID '%s' not found", transactionID)) {
			// Check the queue for the transaction
			queuedTxn, queueErr := l.queue.GetTransactionFromQueue(transactionID)
			if queueErr != nil {
				span.RecordError(queueErr)
				// Return the original DB error if queue retrieval also fails
				return nil, fmt.Errorf("transaction %s not found: %w", transactionID, err)
			}
			if queuedTxn == nil {
				err := fmt.Errorf("transaction %s not found", transactionID)
				span.RecordError(err)
				return nil, err
			}
			span.AddEvent("Transaction found in queue", trace.WithAttributes(attribute.String("transaction.id", transactionID)))
			return queuedTxn, nil // Return the transaction found in the queue
		}
		// Return other database errors directly
		span.RecordError(err)
		return nil, fmt.Errorf("failed to get transaction %s from DB: %w", transactionID, err)
	}
	span.AddEvent("Transaction found in DB", trace.WithAttributes(attribute.String("transaction.id", transactionID)))
	return originalTxn, nil
}

// validateTransactionForRefund checks if the given transaction is eligible for a refund.
func (l *LedgerForge) validateTransactionForRefund(ctx context.Context, originalTxn *model.Transaction) error {
	ctx, span := tracer.Start(ctx, "validateTransactionForRefund")
	defer span.End()

	// A refund reverses a committed transfer.
	switch originalTxn.Status {
	case StatusApplied:
		// refundable
	default:
		err := fmt.Errorf("transaction %s cannot be refunded in status %s (only APPLIED)", originalTxn.TransactionID, originalTxn.Status)
		span.RecordError(err)
		return err
	}

	// Check if the transaction has already been refunded
	isRefunded, err := l.datasource.IsTransactionRefunded(ctx, originalTxn)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to check if transaction %s was already refunded: %w", originalTxn.TransactionID, err)
	}
	if isRefunded {
		err := &errTransactionAlreadyRefunded{transactionID: originalTxn.TransactionID}
		span.RecordError(err)
		return err
	}

	span.AddEvent("Transaction validated for refund", trace.WithAttributes(attribute.String("transaction.id", originalTxn.TransactionID)))
	return nil
}

// RefundOptions carries the caller-supplied settings for a refund.
//
// The zero value reproduces the historical behaviour: queued asynchronously,
// with the reversal inheriting the original transaction's description and
// metadata.
type RefundOptions struct {
	// SkipQueue processes the refund synchronously instead of placing it on
	// the transaction queue.
	SkipQueue bool

	// Description replaces the reversal's description, which otherwise copies
	// the original transaction's.
	Description string

	// MetaData is merged onto the metadata inherited from the original
	// transaction, with these keys winning on conflict. Merging rather than
	// replacing preserves caller metadata. Reservation flags are cleared
	// because a refund always reverses committed funds.
	MetaData map[string]interface{}
}

// prepareRefundTransaction creates and configures a new transaction object for the refund.
func prepareRefundTransaction(originalTxn *model.Transaction, opts RefundOptions) *model.Transaction {
	newTransaction := *originalTxn // Create a copy
	newTransaction.TransactionID = model.GenerateUUIDWithSuffix("txn")
	newTransaction.Reference = model.RefundReference(originalTxn.TransactionID)
	newTransaction.ParentTransaction = originalTxn.TransactionID
	newTransaction.ParentKind = "economic"
	newTransaction.Source = originalTxn.Destination // Swap source and destination
	newTransaction.Destination = originalTxn.Source
	newTransaction.AllowOverdraft = true
	newTransaction.SkipQueue = opts.SkipQueue

	if opts.Description != "" {
		newTransaction.Description = opts.Description
	}

	if len(opts.MetaData) > 0 {
		// Build a new map rather than writing into the inherited one: the copy
		// above shares the original transaction's metadata map, so mutating it
		// here would also rewrite the transaction being refunded.
		merged := make(map[string]interface{}, len(originalTxn.MetaData)+len(opts.MetaData))
		for k, v := range originalTxn.MetaData {
			merged[k] = v
		}
		for k, v := range opts.MetaData {
			merged[k] = v
		}
		newTransaction.MetaData = merged
	}

	newTransaction.Status = ""
	newTransaction.Inflight = false
	newTransaction.QueueAction = ""
	newTransaction.OverdraftLimit = 0
	newTransaction.ScheduledFor = time.Time{}
	newTransaction.InflightExpiryDate = time.Time{}
	newTransaction.InflightCommitDate = time.Time{}
	newTransaction.MetaData = cloneTransactionForAsync(&newTransaction).MetaData
	delete(newTransaction.MetaData, "inflight")

	return &newTransaction
}

// RefundTransaction processes a refund for a given transaction by its ID.
// It starts a tracing span, retrieves the original transaction, validates its status, creates a new refund transaction, and queues it.
//
// Parameters:
// - ctx context.Context: The context for the operation.
// - transactionID string: The ID of the transaction to be refunded.
//
// Returns:
// - *model.Transaction: A pointer to the refunded Transaction model.
// - error: An error if the transaction could not be refunded.
func (l *LedgerForge) RefundTransaction(ctx context.Context, transactionID string, skipQueue bool) (*model.Transaction, error) {
	return l.RefundTransactionWithOptions(ctx, transactionID, RefundOptions{SkipQueue: skipQueue})
}

// RefundTransactionWithOptions refunds a transaction, letting the caller set
// the reversal's description and metadata rather than inheriting the
// original's.
//
// RefundTransaction is the same call with only SkipQueue set.
func (l *LedgerForge) RefundTransactionWithOptions(ctx context.Context, transactionID string, opts RefundOptions) (*model.Transaction, error) {
	ctx, span := tracer.Start(ctx, "RefundTransaction")
	defer span.End()

	// Serialize refunds of the same transaction so the already-refunded check
	// can't be raced into a double refund.
	lockKey := fmt.Sprintf("refund:%s", transactionID)
	locker := redlock.NewLocker(l.redis, lockKey, model.GenerateUUIDWithSuffix("loc"))
	if err := locker.Lock(ctx, l.Config().Transaction.LockDuration); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to acquire lock for refund: %w", err)
	}
	defer l.releaseSingleLock(ctx, locker)

	// 1. Retrieve the original transaction (from DB or Queue)
	originalTxn, err := l.getOriginalTransactionForRefund(ctx, transactionID)
	if err != nil {
		span.RecordError(err)
		return nil, err // Error includes context from the helper function
	}

	// 2. Validate if the transaction can be refunded
	if err := l.validateTransactionForRefund(ctx, originalTxn); err != nil {
		span.RecordError(err)
		return nil, err // Error includes context from the helper function
	}

	// 3. Prepare the refund transaction object
	refundTxnObject := prepareRefundTransaction(originalTxn, opts)

	// 4. Queue the refund transaction for processing
	queuedRefundTxn, err := l.QueueTransaction(ctx, refundTxnObject)
	if err != nil {
		span.RecordError(err)
		var claimErr *pq.Error
		if errors.As(err, &claimErr) && claimErr.Code == "23505" && claimErr.Constraint == "refund_claims_pkey" {
			return nil, &errTransactionAlreadyRefunded{transactionID: originalTxn.TransactionID}
		}
		return nil, fmt.Errorf("failed to queue refund transaction %s: %w", refundTxnObject.TransactionID, err)
	}

	span.AddEvent("Refund transaction queued", trace.WithAttributes(attribute.String("refund.transaction.id", queuedRefundTxn.TransactionID)))
	return queuedRefundTxn, nil
}

// errTransactionAlreadyRefunded marks a leg that was already reversed. Fan-out
// refunds treat this as skippable so a parent refund can finish remaining legs.
type errTransactionAlreadyRefunded struct {
	transactionID string
}

func (e *errTransactionAlreadyRefunded) Error() string {
	return fmt.Sprintf("transaction %s has already been refunded", e.transactionID)
}

func isTransactionAlreadyRefundedError(err error) bool {
	var target *errTransactionAlreadyRefunded
	return errors.As(err, &target)
}

func unwrapRefundBatchErrors(err error) ([]error, bool) {
	if err == nil {
		return nil, false
	}
	type unwrapMultiple interface {
		Unwrap() []error
	}
	u, ok := err.(unwrapMultiple)
	if !ok {
		return []error{err}, false
	}
	errs := u.Unwrap()
	if len(errs) == 0 {
		return nil, true
	}
	return errs, true
}

func allRefundBatchErrorsAreAlreadyRefunded(err error) bool {
	errs, joined := unwrapRefundBatchErrors(err)
	if !joined {
		return isTransactionAlreadyRefundedError(err)
	}
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		if !isTransactionAlreadyRefundedError(e) {
			return false
		}
	}
	return true
}

// nonSkippableRefundBatchErrors drops already-refunded legs from a batch error
// so API classifiers do not treat a mixed failure as a safe idempotent conflict
// when a fatal leg error is also present.
func nonSkippableRefundBatchErrors(err error) error {
	errs, joined := unwrapRefundBatchErrors(err)
	if !joined {
		if isTransactionAlreadyRefundedError(err) {
			return nil
		}
		return err
	}
	var fatal []error
	for _, e := range errs {
		if !isTransactionAlreadyRefundedError(e) {
			fatal = append(fatal, e)
		}
	}
	if len(fatal) == 0 {
		return nil
	}
	if len(fatal) == 1 {
		return fatal[0]
	}
	return errors.Join(fatal...)
}

func reconcileRefundBatchResults(applied []*model.Transaction, err error) ([]*model.Transaction, error) {
	if err == nil {
		return applied, nil
	}
	// Some legs refunded, others were already done — success for the caller.
	if len(applied) > 0 && allRefundBatchErrorsAreAlreadyRefunded(err) {
		return applied, nil
	}
	if fatal := nonSkippableRefundBatchErrors(err); fatal != nil {
		return applied, fatal
	}
	// Only skippable errors remain and nothing new was applied.
	return applied, err
}

// ProcessRefundsInBatches fans out a refund across every refundable leg under
// parentTransactionID. Legs that were already reversed are skipped; the call
// succeeds when at least one leg is refunded and fails with conflict only when
// nothing was left to refund.
func (l *LedgerForge) ProcessRefundsInBatches(ctx context.Context, parentTransactionID string, opts RefundOptions) ([]*model.Transaction, error) {
	ctx, span := tracer.Start(ctx, "ProcessRefundsInBatches")
	defer span.End()

	ctx, err := l.admitRefundPolicy(ctx, parentTransactionID, opts)
	if err != nil {
		return nil, err
	}
	applied, err := l.ProcessTransactionInBatches(
		ctx,
		parentTransactionID,
		big.NewInt(0),
		1,
		false,
		l.GetRefundableTransactionsByParentID,
		l.RefundWorkerWithRefundOptions(opts),
	)
	result, reconcileErr := reconcileRefundBatchResults(applied, err)
	if reconcileErr != nil {
		l.failPolicyRefund(ctx)
	}
	if reconcileErr != nil {
		span.RecordError(reconcileErr)
	}
	return result, reconcileErr
}
