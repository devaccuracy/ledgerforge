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

package reconciliation

import (
	"context"
	"math/big"
	"time"

	"github.com/devaccuracy/ledgerforge/model"
)

// Repository contains only the persistence operations reconciliation consumes.
type Repository interface {
	GetTransactionsPaginated(ctx context.Context, id string, batchSize int, offset int64) ([]*model.Transaction, error)
	GroupTransactions(ctx context.Context, groupCriteria string, batchSize int, offset int64) (map[string][]*model.Transaction, error)
	GetTransactionsByCriteria(ctx context.Context, minAmount, maxAmount *float64, currency *string, minDate, maxDate *time.Time, limit int, offset int64) ([]*model.Transaction, error)
	RecordReconciliation(ctx context.Context, rec *model.Reconciliation) error
	GetReconciliation(ctx context.Context, id string) (*model.Reconciliation, error)
	UpdateReconciliationStatus(ctx context.Context, id string, status string, matchedCount, unmatchedCount int) error
	GetExternalTransactionsPaginated(ctx context.Context, uploadID string, batchSize int, offset int64) ([]*model.ExternalTransaction, error)
	RecordExternalTransaction(ctx context.Context, tx *model.ExternalTransaction, reconciliationID string) error
	RecordMatchingRule(ctx context.Context, rule *model.MatchingRule) error
	GetMatchingRules(ctx context.Context) ([]*model.MatchingRule, error)
	GetMatchingRule(ctx context.Context, id string) (*model.MatchingRule, error)
	UpdateMatchingRule(ctx context.Context, rule *model.MatchingRule) error
	DeleteMatchingRule(ctx context.Context, id string) error
	SaveReconciliationProgress(ctx context.Context, reconciliationID string, progress model.ReconciliationProgress) error
	LoadReconciliationProgress(ctx context.Context, reconciliationID string) (model.ReconciliationProgress, error)
	RecordMatches(ctx context.Context, reconciliationID string, matches []model.Match) error
	RecordUnmatched(ctx context.Context, reconciliationID string, results []string) error
	FetchAndGroupExternalTransactions(ctx context.Context, uploadID string, groupCriteria string, batchSize int, offset int64) (map[string][]*model.Transaction, error)
}

// Config controls reconciliation workers and checkpoint frequency.
type Config struct {
	MaxWorkers       int
	BatchSize        int
	ProgressInterval int
}

type FetchTransactions func(context.Context, string, int, int64) ([]*model.Transaction, error)
type ProcessTransaction func(context.Context, *model.Transaction) error

// RunBatches adapts the existing transaction batch runner without owning it.
// It must wait for every worker before returning.
type RunBatches func(context.Context, string, *big.Int, int, FetchTransactions, ProcessTransaction) error

type Service struct {
	datasource          Repository
	configuration       func() Config
	runBatches          RunBatches
	updateMetadata      func(context.Context, string, map[string]interface{}) error
	indexReconciliation func(model.Reconciliation)
}

func NewService(repository Repository, configuration func() Config, runBatches RunBatches,
	updateMetadata func(context.Context, string, map[string]interface{}) error,
	indexReconciliation func(model.Reconciliation),
) *Service {
	return &Service{datasource: repository, configuration: configuration, runBatches: runBatches,
		updateMetadata: updateMetadata, indexReconciliation: indexReconciliation}
}

func (s *Service) config() Config {
	var cfg Config
	if s.configuration != nil {
		cfg = s.configuration()
	}
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 10
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 1000
	}
	if cfg.ProgressInterval <= 0 {
		cfg.ProgressInterval = 100
	}
	return cfg
}
