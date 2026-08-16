package mcpserver

import (
	"context"
	"errors"
	"math/big"
	"testing"

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
	require.Len(t, names, 9)
	require.Contains(t, names, "ledgerforge_get_ledger")
	require.Contains(t, names, "ledgerforge_get_transaction_lineage")
	require.NotContains(t, names, "ledgerforge_queue_transaction")
}

func TestWriteEnabledServerExecutesCreateLedger(t *testing.T) {
	service := &fakeService{}
	session := connect(t, New(service, Options{AllowWrites: true}))
	tools, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, tools.Tools, 14)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ledgerforge_create_ledger",
		Arguments: map[string]any{"name": "operating"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, "operating", service.createdLedger.Name)
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
	requestedLedgerID string
	createdLedger     model.Ledger
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

func (s *fakeService) GetBalanceByID(context.Context, string, []string, bool) (*model.Balance, error) {
	return nil, nil
}

func (s *fakeService) GetAllBalances(context.Context, int, int) ([]model.Balance, error) {
	return nil, nil
}

func (s *fakeService) GetTransaction(context.Context, string) (*model.Transaction, error) {
	return nil, nil
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

func (s *fakeService) QueueTransaction(context.Context, *model.Transaction) (*model.Transaction, error) {
	return &model.Transaction{}, nil
}

func (s *fakeService) CommitInflightTransaction(context.Context, string, *big.Int) (*model.Transaction, error) {
	return &model.Transaction{}, nil
}

func (s *fakeService) VoidInflightTransaction(context.Context, string) (*model.Transaction, error) {
	return &model.Transaction{}, nil
}
