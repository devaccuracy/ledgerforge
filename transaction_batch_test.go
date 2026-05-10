package ledgerforge

import (
	"context"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/config"
	"github.com/devaccuracy/ledgerforge/database/mocks"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestTryRecordQueuedTransactionBatchSkipsWithoutSiblingTransactions(t *testing.T) {
	config.MockConfig(&config.Configuration{
		Queue: config.QueueConfig{NumberOfQueues: 1},
	})

	ds := &mocks.MockDataSource{}
	createdAt := time.Now().UTC()
	ds.On(
		"GetQueuedTransactionsForCoalescing",
		mock.Anything,
		"bln_income",
		"bln_fee",
		"NGN",
		"txn_parent",
		createdAt,
		9,
	).Return([]*model.Transaction{}, nil).Once()
	ds.On(
		"GetQueuedTransactionsForSourceCoalescing",
		mock.Anything,
		"bln_income",
		"NGN",
		"txn_parent",
		createdAt,
		9,
	).Return([]*model.Transaction{}, nil).Once()
	ds.On(
		"GetQueuedTransactionsForDestinationCoalescing",
		mock.Anything,
		"bln_fee",
		"NGN",
		"txn_parent",
		createdAt,
		9,
	).Return([]*model.Transaction{}, nil).Once()

	ledgerforgeInstance := &LedgerForge{
		datasource: ds,
		config: &config.Configuration{
			Transaction: config.TransactionConfig{EnableCoalescing: true, BatchSize: 10},
		},
	}

	handled, err := ledgerforgeInstance.TryRecordQueuedTransactionBatch(context.Background(), &model.Transaction{
		TransactionID:     "txn_current_q",
		ParentTransaction: "txn_parent",
		Source:            "bln_income",
		Destination:       "bln_fee",
		Currency:          "NGN",
		Status:            StatusQueued,
		CreatedAt:         createdAt,
	})

	assert.NoError(t, err)
	assert.False(t, handled)
	ds.AssertExpectations(t)
}

func TestTryRecordQueuedTransactionBatchFailsOpenOnDiscoveryError(t *testing.T) {
	config.MockConfig(&config.Configuration{
		Queue: config.QueueConfig{NumberOfQueues: 1},
	})

	ds := &mocks.MockDataSource{}
	createdAt := time.Now().UTC()
	ds.On(
		"GetQueuedTransactionsForCoalescing",
		mock.Anything,
		"bln_income",
		"bln_fee",
		"NGN",
		"txn_parent",
		createdAt,
		9,
	).Return(nil, assert.AnError).Once()

	ledgerforgeInstance := &LedgerForge{
		datasource: ds,
		config: &config.Configuration{
			Transaction: config.TransactionConfig{EnableCoalescing: true, BatchSize: 10},
		},
	}

	handled, err := ledgerforgeInstance.TryRecordQueuedTransactionBatch(context.Background(), &model.Transaction{
		TransactionID:     "txn_current_q",
		ParentTransaction: "txn_parent",
		Source:            "bln_income",
		Destination:       "bln_fee",
		Currency:          "NGN",
		Status:            StatusQueued,
		CreatedAt:         createdAt,
	})

	assert.NoError(t, err)
	assert.False(t, handled)
	ds.AssertExpectations(t)
}

func TestBuildQueuedCoalescingBatchFallsBackToSourceScope(t *testing.T) {
	ds := &mocks.MockDataSource{}
	createdAt := time.Now().UTC()
	leader := &model.Transaction{
		TransactionID:     "txn_current_q",
		ParentTransaction: "txn_parent",
		Source:            "bln_income",
		Destination:       "bln_fee",
		Currency:          "NGN",
		Status:            StatusQueued,
		CreatedAt:         createdAt,
		Reference:         "ref_current_q",
	}

	ds.On(
		"GetQueuedTransactionsForCoalescing",
		mock.Anything,
		"bln_income",
		"bln_fee",
		"NGN",
		"txn_parent",
		createdAt,
		9,
	).Return([]*model.Transaction{}, nil).Once()
	ds.On(
		"GetQueuedTransactionsForSourceCoalescing",
		mock.Anything,
		"bln_income",
		"NGN",
		"txn_parent",
		createdAt,
		9,
	).Return([]*model.Transaction{
		{
			TransactionID:     "txn_sibling",
			ParentTransaction: "txn_sibling_parent",
			Source:            "bln_income",
			Destination:       "bln_tax",
			Currency:          "NGN",
			Status:            StatusQueued,
			Reference:         "ref_sibling",
		},
	}, nil).Once()

	ledgerforgeInstance := &LedgerForge{
		datasource: ds,
		config: &config.Configuration{
			Transaction: config.TransactionConfig{EnableCoalescing: true, BatchSize: 10},
		},
	}

	batch, scope, err := ledgerforgeInstance.buildQueuedCoalescingBatch(context.Background(), leader, 10)
	assert.NoError(t, err)
	assert.Equal(t, queuedCoalescingScopeSource, scope)
	assert.Len(t, batch, 2)
	assert.Equal(t, "ref_sibling_q", batch[1].Reference)
	ds.AssertExpectations(t)
}

func TestBuildQueuedCoalescingBatchFallsBackToDestinationScope(t *testing.T) {
	ds := &mocks.MockDataSource{}
	createdAt := time.Now().UTC()
	leader := &model.Transaction{
		TransactionID:     "txn_current_q",
		ParentTransaction: "txn_parent",
		Source:            "bln_income",
		Destination:       "bln_fee",
		Currency:          "NGN",
		Status:            StatusQueued,
		CreatedAt:         createdAt,
		Reference:         "ref_current_q",
	}

	ds.On(
		"GetQueuedTransactionsForCoalescing",
		mock.Anything,
		"bln_income",
		"bln_fee",
		"NGN",
		"txn_parent",
		createdAt,
		9,
	).Return([]*model.Transaction{}, nil).Once()
	ds.On(
		"GetQueuedTransactionsForSourceCoalescing",
		mock.Anything,
		"bln_income",
		"NGN",
		"txn_parent",
		createdAt,
		9,
	).Return([]*model.Transaction{}, nil).Once()
	ds.On(
		"GetQueuedTransactionsForDestinationCoalescing",
		mock.Anything,
		"bln_fee",
		"NGN",
		"txn_parent",
		createdAt,
		9,
	).Return([]*model.Transaction{
		{
			TransactionID:     "txn_sibling",
			ParentTransaction: "txn_sibling_parent",
			Source:            "bln_vat",
			Destination:       "bln_fee",
			Currency:          "NGN",
			Status:            StatusQueued,
			Reference:         "ref_sibling",
		},
	}, nil).Once()

	ledgerforgeInstance := &LedgerForge{
		datasource: ds,
		config: &config.Configuration{
			Transaction: config.TransactionConfig{EnableCoalescing: true, BatchSize: 10},
		},
	}

	batch, scope, err := ledgerforgeInstance.buildQueuedCoalescingBatch(context.Background(), leader, 10)
	assert.NoError(t, err)
	assert.Equal(t, queuedCoalescingScopeDestination, scope)
	assert.Len(t, batch, 2)
	assert.Equal(t, "ref_sibling_q", batch[1].Reference)
	ds.AssertExpectations(t)
}

func TestRestoreTransactionFlagsFromMetadata(t *testing.T) {
	txn := &model.Transaction{
		MetaData: map[string]interface{}{
			"inflight":        true,
			"atomic":          true,
			"allow_overdraft": true,
		},
	}

	restoreTransactionFlagsFromMetadata(txn)

	assert.True(t, txn.Inflight)
	assert.True(t, txn.Atomic)
	assert.True(t, txn.AllowOverdraft)
}

func TestValidateQueuedBatchTransactionReferenceUsesPrefetchedSet(t *testing.T) {
	ledgerforgeInstance := &LedgerForge{}
	prefetched := map[string]struct{}{
		"ref_1_q": {},
	}
	existing := map[string]struct{}{}
	batch := make(map[string]struct{})

	err := ledgerforgeInstance.validateQueuedBatchTransactionReference(context.Background(), &model.Transaction{
		Reference: "ref_1_q",
	}, prefetched, existing, batch)
	assert.NoError(t, err)
	assert.Contains(t, batch, "ref_1_q")

	err = ledgerforgeInstance.validateQueuedBatchTransactionReference(context.Background(), &model.Transaction{
		Reference: "ref_1_q",
	}, prefetched, existing, batch)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already been used")
}

func TestBatchReferenceCheckEnabled(t *testing.T) {
	ledgerforgeInstance := &LedgerForge{
		config: &config.Configuration{
			Transaction: config.TransactionConfig{
				DisableBatchReferenceCheck: false,
			},
		},
	}
	assert.True(t, ledgerforgeInstance.batchReferenceCheckEnabled())

	ledgerforgeInstance.config.Transaction.DisableBatchReferenceCheck = true
	assert.False(t, ledgerforgeInstance.batchReferenceCheckEnabled())
}
