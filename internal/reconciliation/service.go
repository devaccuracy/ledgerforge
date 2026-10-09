/*
Copyright 2024 Blnk Finance Authors.
Copyright 2025 LedgerForge Contributors.

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
	"fmt"
	"io"
	"time"

	"github.com/devaccuracy/ledgerforge/internal/files"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/trace"
)

// UploadExternalData handles the process of uploading external data by detecting file type, parsing, and storing it.
// Parameters:
// - ctx: The context for controlling execution.
// - source: The source of the external data.
// - reader: An io.Reader for reading the data.
// - filename: The name of the file being uploaded.
// Returns:
// - string: The ID of the upload.
// - int: The total number of records processed.
// - error: If any step of the process fails.
func (s *Service) UploadExternalData(ctx context.Context, source string, reader io.Reader, filename string) (string, int, error) {
	return files.UploadExternalData(ctx, source, reader, filename, func(ctx context.Context, uploadID string, txn model.ExternalTransaction) error {
		return s.storeExternalTransaction(ctx, uploadID, txn)
	})
}

// storeExternalTransaction stores an external transaction in the datasource.
// Parameters:
// - ctx: The context for controlling execution.
// - uploadID: The unique ID of the current upload.
// - txn: The external transaction to store.
// Returns:
// - error: If storing the transaction fails.
func (s *Service) storeExternalTransaction(ctx context.Context, uploadID string, txn model.ExternalTransaction) error {
	return s.datasource.RecordExternalTransaction(ctx, &txn, uploadID)
}

// postReconciliationActions queues the indexing of reconciliation data in the background.
// This allows the reconciliation to be indexed without blocking the main process.
// Parameters:
// - ctx: The context for controlling execution.
// - reconciliation: The reconciliation object to be indexed.
func (s *Service) postReconciliationActions(_ context.Context, reconciliation model.Reconciliation) {
	go s.indexReconciliation(reconciliation)
}

// StartReconciliation initiates the reconciliation process by creating a new reconciliation entry and starting the
// process asynchronously. The process is detached to run in the background.
// Parameters:
// - ctx: The context controlling the reconciliation process.
// - uploadID: The ID of the uploaded transaction file to reconcile.
// - strategy: The reconciliation strategy to be used (e.g., "one_to_one").
// - groupCriteria: Criteria to group transactions (optional).
// - matchingRuleIDs: The IDs of the rules used for matching transactions.
// - isDryRun: If true, the reconciliation will not commit changes (useful for testing).
// Returns:
// - string: The ID of the reconciliation process.
// - error: If the reconciliation fails to start.
func (s *Service) StartReconciliation(ctx context.Context, uploadID string, strategy string, groupCriteria string, matchingRuleIDs []string, isDryRun bool) (string, error) {
	// Generate a unique ID for the reconciliation.
	reconciliationID := model.GenerateUUIDWithSuffix("recon")
	// Initialize a new reconciliation object with the provided parameters.
	reconciliation := model.Reconciliation{
		ReconciliationID: reconciliationID,
		UploadID:         uploadID,
		Status:           StatusStarted,
		StartedAt:        time.Now(),
		IsDryRun:         isDryRun,
	}

	// Record the reconciliation in the data source (e.g., database).
	if err := s.datasource.RecordReconciliation(ctx, &reconciliation); err != nil {
		return "", err
	}

	// Detach the context to allow the reconciliation process to run in the background.
	detachedCtx := context.Background()
	ctxWithTrace := trace.ContextWithSpan(detachedCtx, trace.SpanFromContext(ctx))

	// Start the reconciliation process asynchronously.
	go func() {
		err := s.processReconciliation(ctxWithTrace, reconciliation, strategy, groupCriteria, matchingRuleIDs)
		if err != nil {
			// If an error occurs during the reconciliation, log it and update the reconciliation status to "failed".
			logrus.Errorf("Error in reconciliation process: %v", err)
			err := s.datasource.UpdateReconciliationStatus(ctxWithTrace, reconciliationID, StatusFailed, 0, 0)
			if err != nil {
				logrus.Errorf("Error updating reconciliation status: %v", err)
			}
		}
	}()

	return reconciliationID, nil
}

// StartInstantReconciliation initiates a reconciliation process directly with provided transactions
// instead of loading them from an uploaded file.
// Parameters:
// - ctx: The context controlling the reconciliation process.
// - externalTransactions: The array of external transactions to reconcile.
// - strategy: The reconciliation strategy to be used (e.g., "one_to_one").
// - groupCriteria: Criteria to group transactions (optional).
// - matchingRuleIDs: The IDs of the rules used for matching transactions.
// - isDryRun: If true, the reconciliation will not commit changes (useful for testing).
// Returns:
// - string: The ID of the reconciliation process.
// - error: If the reconciliation fails to start.
func (s *Service) StartInstantReconciliation(ctx context.Context, externalTransactions []model.ExternalTransaction,
	strategy string, groupCriteria string, matchingRuleIDs []string, isDryRun bool,
) (string, error) {
	// Generate a unique ID for the reconciliation
	reconciliationID := model.GenerateUUIDWithSuffix("recon")

	// Use a temporary ID for the transactions
	tempID := model.GenerateUUIDWithSuffix("instant")

	// Initialize a new reconciliation object with the provided parameters
	reconciliation := model.Reconciliation{
		ReconciliationID: reconciliationID,
		UploadID:         tempID,
		Status:           StatusStarted,
		StartedAt:        time.Now(),
		IsDryRun:         isDryRun,
	}

	// Record the reconciliation in the data source
	if err := s.datasource.RecordReconciliation(ctx, &reconciliation); err != nil {
		return "", err
	}

	// Store the provided transactions in the database with the temporary ID
	for _, txn := range externalTransactions {
		if err := s.storeExternalTransaction(ctx, tempID, txn); err != nil {
			// Log error and update reconciliation status
			logrus.Errorf("Error storing transaction: %v", err)
			err := s.datasource.UpdateReconciliationStatus(ctx, reconciliationID, StatusFailed, 0, 0)
			if err != nil {
				logrus.Errorf("Error updating reconciliation status: %v", err)
				return "", fmt.Errorf("failed to store external transaction: %w", err)
			}
			return "", fmt.Errorf("failed to store external transaction: %w", err)
		}
	}

	// Detach the context to allow the reconciliation process to run in the background
	detachedCtx := context.Background()
	ctxWithTrace := trace.ContextWithSpan(detachedCtx, trace.SpanFromContext(ctx))

	// Start the reconciliation process asynchronously
	go func() {
		err := s.processReconciliation(ctxWithTrace, reconciliation, strategy, groupCriteria, matchingRuleIDs)
		if err != nil {
			// If an error occurs during the reconciliation, log it and update the reconciliation status to "failed"
			logrus.Errorf("Error in instant reconciliation process: %v", err)
			err := s.datasource.UpdateReconciliationStatus(ctxWithTrace, reconciliationID, StatusFailed, 0, 0)
			if err != nil {
				logrus.Errorf("Error updating reconciliation status: %v", err)
			}
		}
	}()

	return reconciliationID, nil
}

// GetReconciliation retrieves a reconciliation by its ID.
// Parameters:
// - ctx: The context controlling the request.
// - reconciliationID: The ID of the reconciliation to retrieve.
// Returns:
// - *model.Reconciliation: The retrieved reconciliation object.
// - error: If the reconciliation cannot be found or if retrieval fails.
func (s *Service) GetReconciliation(ctx context.Context, reconciliationID string) (*model.Reconciliation, error) {
	reconciliation, err := s.datasource.GetReconciliation(ctx, reconciliationID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve reconciliation: %w", err)
	}

	return reconciliation, nil
}
