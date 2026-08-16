package ledgerforge_test

import (
	"testing"

	"github.com/hibiken/asynq"

	"github.com/devaccuracy/ledgerforge"
	"github.com/devaccuracy/ledgerforge/config"
	"github.com/devaccuracy/ledgerforge/database"
	"github.com/devaccuracy/ledgerforge/model"
)

// These assignments make the established root import path part of the test
// contract while the implementation remains private under internal/core.
var (
	_ func(database.IDataSource) (*ledgerforge.LedgerForge, error)                   = ledgerforge.NewLedgerForge
	_ func() *model.BalanceTracker                                                   = ledgerforge.NewBalanceTracker
	_ func(*config.Configuration, *asynq.Client) *ledgerforge.Queue                  = ledgerforge.NewQueue
	_ func(*model.Transaction) bool                                                  = ledgerforge.IsInflightTransaction
	_ func(*ledgerforge.LedgerForge) *ledgerforge.LineageOutboxProcessor             = ledgerforge.NewLineageOutboxProcessor
	_ func(*ledgerforge.LedgerForge) *ledgerforge.QueuedTransactionRecoveryProcessor = ledgerforge.NewQueuedTransactionRecoveryProcessor
)

func TestPublicAPIFacadePreservesLegacyImportPath(t *testing.T) {
	t.Parallel()

	if ledgerforge.GeneralLedgerID != "general_ledger_id" {
		t.Fatalf("GeneralLedgerID changed: %q", ledgerforge.GeneralLedgerID)
	}

	if _, err := ledgerforge.SQLFiles.Open("sql/1708676327.sql"); err != nil {
		t.Fatalf("migration files are not available through the public facade: %v", err)
	}
}
