package mcpserver

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/devaccuracy/ledgerforge"
	"github.com/devaccuracy/ledgerforge/model"
)

func TestReadOnlyServerExposesOnlyReadTools(t *testing.T) {
	session := connect(t, New(&fakeService{}, Options{}))
	tools, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)

	names := toolNames(tools.Tools)
	require.Len(t, names, 12)
	require.Contains(t, names, "ledgerforge_get_ledger")
	require.Contains(t, names, "ledgerforge_get_transaction_context")
	require.Contains(t, names, "ledgerforge_get_transaction_lineage")
	require.NotContains(t, names, "ledgerforge_queue_transaction")
	require.NotContains(t, names, "ledgerforge_transfer_funds")
}

func TestWriteEnabledServerExecutesCreateLedger(t *testing.T) {
	service := &fakeService{}
	session := connect(t, New(service, Options{AllowWrites: true}))
	tools, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, tools.Tools, 20)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ledgerforge_create_ledger",
		Arguments: map[string]any{"name": "operating", "confirm": true},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, "operating", service.createdLedger.Name)
}

func TestTransferFundsUsesExactMinorUnits(t *testing.T) {
	service := &fakeService{}
	session := connect(t, New(service, Options{AllowWrites: true}))

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ledgerforge_transfer_funds",
		Arguments: map[string]any{
			"source_balance_id":      "bal_source",
			"destination_balance_id": "bal_destination",
			"amount_minor":           "1234",
			"currency":               "USD",
			"currency_multiplier":    100,
			"reference":              "payment-2026-001",
			"confirm":                true,
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.NotNil(t, service.queuedTransaction)
	require.Equal(t, "1234", service.queuedTransaction.PreciseAmount.String())
	require.Equal(t, 100.0, service.queuedTransaction.Precision)
	require.Equal(t, "payment-2026-001", service.queuedTransaction.Reference)
}

func TestBulkTransfersPreservesBatchControls(t *testing.T) {
	service := &fakeService{}
	session := connect(t, New(service, Options{AllowWrites: true}))

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ledgerforge_create_bulk_transfers",
		Arguments: map[string]any{
			"transfers": []map[string]any{{
				"source_balance_id":      "bal_source",
				"destination_balance_id": "bal_destination",
				"amount_minor":           "100",
				"currency":               "USD",
				"currency_multiplier":    100,
				"reference":              "batch-payment-001",
			}},
			"atomic":    true,
			"run_async": true,
			"confirm":   true,
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.NotNil(t, service.bulkRequest)
	require.True(t, service.bulkRequest.Atomic)
	require.True(t, service.bulkRequest.RunAsync)
	require.Len(t, service.bulkRequest.Transactions, 1)
	require.Equal(t, "100", service.bulkRequest.Transactions[0].PreciseAmount.String())
}

func TestWriteToolRequiresExplicitConfirmation(t *testing.T) {
	service := &fakeService{}
	session := connect(t, New(service, Options{AllowWrites: true}))

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ledgerforge_create_ledger",
		Arguments: map[string]any{"name": "operating"},
	})
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Empty(t, service.createdLedger.Name)
}

func TestResourcesAndPromptsAreUsable(t *testing.T) {
	service := &fakeService{
		balance: &model.Balance{BalanceID: "bal_123", Currency: "USD", CurrencyMultiplier: 100, Balance: big.NewInt(1234)},
	}
	session := connect(t, New(service, Options{}))

	capabilities, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "ledgerforge://capabilities"})
	require.NoError(t, err)
	require.Len(t, capabilities.Contents, 1)
	require.Contains(t, capabilities.Contents[0].Text, "read_only")

	balance, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "ledgerforge://balances/bal_123"})
	require.NoError(t, err)
	require.Contains(t, balance.Contents[0].Text, `"balance_minor": "1234"`)

	prompt, err := session.GetPrompt(context.Background(), &mcp.GetPromptParams{
		Name:      "ledgerforge_investigate_balance",
		Arguments: map[string]string{"balance_id": "bal_123"},
	})
	require.NoError(t, err)
	require.Len(t, prompt.Messages, 1)
	require.Contains(t, prompt.Messages[0].Content.(*mcp.TextContent).Text, "ledgerforge_get_balance")
}

func TestReadToolReturnsServiceData(t *testing.T) {
	service := &fakeService{ledger: &model.Ledger{LedgerID: "ldg_123", Name: "operating"}}
	session := connect(t, New(service, Options{}))

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ledgerforge_get_ledger",
		Arguments: map[string]any{"ledger_id": "ldg_123"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, "ldg_123", service.requestedLedgerID)
}

func TestNormalizePagination(t *testing.T) {
	limit, offset := normalizePagination(paginationInput{Limit: 999, Offset: -1})
	require.Equal(t, maxPageSize, limit)
	require.Zero(t, offset)

	limit, offset = normalizePagination(paginationInput{})
	require.Equal(t, 20, limit)
	require.Zero(t, offset)
}

func TestParsePreciseAmount(t *testing.T) {
	amount, err := parsePreciseAmount("12345")
	require.NoError(t, err)
	require.Equal(t, "12345", amount.String())

	_, err = parsePreciseAmount("12.34")
	require.Error(t, err)
}

func connect(t *testing.T, server *Server) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	runErr := make(chan error, 1)
	go func() { runErr <- server.Protocol().Run(ctx, serverTransport) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "ledgerforge-mcp-test-client", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = session.Close()
		cancel()
		err := <-runErr
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("MCP server stopped unexpectedly: %v", err)
		}
	})
	return session
}

func toolNames(tools []*mcp.Tool) map[string]struct{} {
	names := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		names[tool.Name] = struct{}{}
	}
	return names
}

type fakeService struct {
	ledger            *model.Ledger
	balance           *model.Balance
	transaction       *model.Transaction
	requestedLedgerID string
	createdLedger     model.Ledger
	queuedTransaction *model.Transaction
	bulkRequest       *model.BulkTransactionRequest
}

func (s *fakeService) GetLedgerByID(id string) (*model.Ledger, error) {
	s.requestedLedgerID = id
	if s.ledger == nil {
		return &model.Ledger{LedgerID: id}, nil
	}
	return s.ledger, nil
}

func (s *fakeService) GetAllLedgers(int, int) ([]model.Ledger, error) {
	return nil, nil
}

func (s *fakeService) GetBalanceByID(_ context.Context, id string, _ []string, _ bool) (*model.Balance, error) {
	if s.balance == nil {
		return &model.Balance{BalanceID: id, Balance: big.NewInt(0)}, nil
	}
	return s.balance, nil
}

func (s *fakeService) GetAllBalances(context.Context, int, int) ([]model.Balance, error) {
	return nil, nil
}

func (s *fakeService) GetBalanceAtTime(context.Context, string, time.Time, bool) (*model.Balance, error) {
	return s.balance, nil
}

func (s *fakeService) GetBalanceByIndicator(context.Context, string, string) (*model.Balance, error) {
	return s.balance, nil
}

func (s *fakeService) GetTransaction(_ context.Context, id string) (*model.Transaction, error) {
	if s.transaction == nil {
		return &model.Transaction{TransactionID: id}, nil
	}
	return s.transaction, nil
}

func (s *fakeService) GetTransactionByRef(context.Context, string) (model.Transaction, error) {
	return model.Transaction{}, nil
}

func (s *fakeService) GetAllTransactions(int, int) ([]model.Transaction, error) {
	return nil, nil
}

func (s *fakeService) GetBalanceLineage(context.Context, string) (*ledgerforge.BalanceLineage, error) {
	return nil, nil
}

func (s *fakeService) GetTransactionLineage(context.Context, string) (*ledgerforge.TransactionLineage, error) {
	return nil, nil
}

func (s *fakeService) CreateLedger(ledger model.Ledger) (model.Ledger, error) {
	s.createdLedger = ledger
	return ledger, nil
}

func (s *fakeService) CreateBalance(context.Context, model.Balance) (model.Balance, error) {
	return model.Balance{}, nil
}

func (s *fakeService) QueueTransaction(_ context.Context, transaction *model.Transaction) (*model.Transaction, error) {
	s.queuedTransaction = transaction
	return transaction, nil
}

func (s *fakeService) CommitInflightTransaction(context.Context, string, *big.Int) (*model.Transaction, error) {
	return &model.Transaction{}, nil
}

func (s *fakeService) VoidInflightTransaction(context.Context, string) (*model.Transaction, error) {
	return &model.Transaction{}, nil
}

func (s *fakeService) RefundTransaction(context.Context, string, bool) (*model.Transaction, error) {
	return &model.Transaction{}, nil
}

func (s *fakeService) CreateBulkTransactions(_ context.Context, request *model.BulkTransactionRequest) (*model.BulkTransactionResult, error) {
	s.bulkRequest = request
	return &model.BulkTransactionResult{BatchID: "bulk_123", Status: "processing"}, nil
}
