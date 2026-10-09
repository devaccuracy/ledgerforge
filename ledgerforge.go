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

// Package ledgerforge provides the stable public API for LedgerForge Core.
//
// The implementation lives in an internal core package. This facade intentionally
// preserves the original github.com/devaccuracy/ledgerforge import path for
// existing applications while keeping the repository root focused on module
// metadata and public compatibility.
package ledgerforge

import (
	"embed"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"github.com/devaccuracy/ledgerforge/config"
	"github.com/devaccuracy/ledgerforge/database"
	"github.com/devaccuracy/ledgerforge/internal/core"
	"github.com/devaccuracy/ledgerforge/model"
)

const (
	InflightActionCommit  = core.InflightActionCommit
	InflightActionVoid    = core.InflightActionVoid
	IndexModeRefresh      = core.IndexModeRefresh
	BulkInflightCommit    = core.BulkInflightCommit
	BulkInflightVoid      = core.BulkInflightVoid
	GeneralLedgerID       = core.GeneralLedgerID
	LineageProviderKey    = core.LineageProviderKey
	LineageFundAllocation = core.LineageFundAllocation
	AllocationFIFO        = core.AllocationFIFO
	AllocationLIFO        = core.AllocationLIFO
	AllocationProp        = core.AllocationProp
	StatusQueued          = core.StatusQueued
	StatusApplied         = core.StatusApplied
	StatusScheduled       = core.StatusScheduled
	StatusRejected        = core.StatusRejected
	StatusInflight        = core.StatusInflight
	StatusVoid            = core.StatusVoid
	StatusCommit          = core.StatusCommit
	StatusStarted         = core.StatusStarted
	StatusInProgress      = core.StatusInProgress
	StatusCompleted       = core.StatusCompleted
	StatusFailed          = core.StatusFailed
)

type (
	RefundOptions                      = core.RefundOptions
	BulkTransactionError               = core.BulkTransactionError
	InflightActionPayload              = core.InflightActionPayload
	ChainProcessor                     = core.ChainProcessor
	ChainVerifyResult                  = core.ChainVerifyResult
	IndexTask                          = core.IndexTask
	IndexUpserter                      = core.IndexUpserter
	BulkInflightAction                 = core.BulkInflightAction
	BulkInflightItem                   = core.BulkInflightItem
	BulkInflightOutcome                = core.BulkInflightOutcome
	LedgerForge                        = core.LedgerForge
	Queue                              = core.Queue
	TransactionTypePayload             = core.TransactionTypePayload
	QueuedTransactionRecoveryProcessor = core.QueuedTransactionRecoveryProcessor
	LineageOutboxProcessor             = core.LineageOutboxProcessor
	LineageSource                      = core.LineageSource
	Allocation                         = core.Allocation
	LineageOutboxPayload               = core.LineageOutboxPayload
	ProviderBreakdown                  = core.ProviderBreakdown
	BalanceLineage                     = core.BalanceLineage
	TransactionLineage                 = core.TransactionLineage
	BatchJobResult                     = core.BatchJobResult
	NewWebhook                         = core.NewWebhook
)

// SQLFiles contains the embedded migration files used by the LedgerForge CLI.
var SQLFiles embed.FS = core.SQLFiles

// NewLedgerForge initializes a LedgerForge instance using the provided datasource.
func NewLedgerForge(db database.IDataSource) (*LedgerForge, error) {
	return core.NewLedgerForge(db)
}

// NewBalanceTracker creates a balance tracker.
func NewBalanceTracker() *model.BalanceTracker {
	return core.NewBalanceTracker()
}

// NewQueue creates the transaction queue service.
func NewQueue(conf *config.Configuration, client *asynq.Client) *Queue {
	return core.NewQueue(conf, client)
}

// IsInflightTransaction reports whether a transaction is an inflight transaction.
func IsInflightTransaction(transaction *model.Transaction) bool {
	return core.IsInflightTransaction(transaction)
}

// NewLineageOutboxProcessor creates the lineage outbox worker.
func NewLineageOutboxProcessor(service *LedgerForge) *LineageOutboxProcessor {
	return core.NewLineageOutboxProcessor(service)
}

// NewQueuedTransactionRecoveryProcessor creates the queued-transaction recovery worker.
func NewQueuedTransactionRecoveryProcessor(service *LedgerForge) *QueuedTransactionRecoveryProcessor {
	return core.NewQueuedTransactionRecoveryProcessor(service)
}

// ErrQueueBackpressure indicates that the queue cannot safely accept more work.
var ErrQueueBackpressure = core.ErrQueueBackpressure

// NewChainProcessor creates the transaction hash-chain worker.
func NewChainProcessor(service *LedgerForge) *ChainProcessor { return core.NewChainProcessor(service) }

// DocumentToIndexMap converts a search document into its index representation.
func DocumentToIndexMap(document interface{}) (map[string]interface{}, error) {
	return core.DocumentToIndexMap(document)
}

// ClassifyInflightError returns the public error code for an inflight operation.
func ClassifyInflightError(err error) string { return core.ClassifyInflightError(err) }

// ErrEntityNotFound indicates that a metadata target does not exist.
var ErrEntityNotFound = core.ErrEntityNotFound

// ErrInflightActionQueued indicates a pending commit or void for a transaction.
var ErrInflightActionQueued = core.ErrInflightActionQueued

// IsDuplicateReferenceError identifies a transaction reference conflict.
func IsDuplicateReferenceError(err error) bool { return core.IsDuplicateReferenceError(err) }

// NewQueueWithRedisClient creates a queue with Redis memory backpressure checks.
func NewQueueWithRedisClient(conf *config.Configuration, client *asynq.Client, redisClient redis.UniversalClient) *Queue {
	return core.NewQueue(conf, client, redisClient)
}
