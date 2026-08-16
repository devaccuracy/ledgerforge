// Package mcpserver exposes selected LedgerForge operations through the Model
// Context Protocol. It is intentionally transport-agnostic; cmd/ledgerforge-mcp
// runs it over standard input and output.
package mcpserver

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"

	"github.com/devaccuracy/ledgerforge"
	"github.com/devaccuracy/ledgerforge/model"
)

const maxPageSize = 100

// Service is the LedgerForge capability surface available to MCP tools.
// Keeping this narrow makes the transport independently testable and prevents
// tools from reaching persistence internals directly.
type Service interface {
	GetLedgerByID(id string) (*model.Ledger, error)
	GetAllLedgers(limit, offset int) ([]model.Ledger, error)
	GetBalanceByID(ctx context.Context, id string, include []string, withQueued bool) (*model.Balance, error)
	GetAllBalances(ctx context.Context, limit, offset int) ([]model.Balance, error)
	GetTransaction(ctx context.Context, id string) (*model.Transaction, error)
	GetTransactionByRef(ctx context.Context, reference string) (model.Transaction, error)
	GetAllTransactions(limit, offset int) ([]model.Transaction, error)
	GetBalanceLineage(ctx context.Context, balanceID string) (*ledgerforge.BalanceLineage, error)
	GetTransactionLineage(ctx context.Context, transactionID string) (*ledgerforge.TransactionLineage, error)
	CreateLedger(ledger model.Ledger) (model.Ledger, error)
	CreateBalance(ctx context.Context, balance model.Balance) (model.Balance, error)
	QueueTransaction(ctx context.Context, transaction *model.Transaction) (*model.Transaction, error)
	CommitInflightTransaction(ctx context.Context, transactionID string, amount *big.Int) (*model.Transaction, error)
	VoidInflightTransaction(ctx context.Context, transactionID string) (*model.Transaction, error)
}

// Options configures the MCP server.
type Options struct {
	Version     string
	AllowWrites bool
}

// Server owns the MCP protocol server and the LedgerForge service it exposes.
type Server struct {
	service     Service
	allowWrites bool
	protocol    *mcp.Server
}

// Protocol returns the configured MCP protocol server.
func (s *Server) Protocol() *mcp.Server {
	return s.protocol
}

// New creates a LedgerForge MCP server. Write tools are omitted unless
// AllowWrites is explicitly enabled.
func New(service Service, options Options) *Server {
	version := options.Version
	if version == "" {
		version = "dev"
	}

	s := &Server{
		service:     service,
		allowWrites: options.AllowWrites,
		protocol: mcp.NewServer(&mcp.Implementation{
			Name:    "ledgerforge-mcp",
			Title:   "LedgerForge MCP",
			Version: version,
		}, nil),
	}
	s.registerReadTools()
	if s.allowWrites {
		s.registerWriteTools()
	}
	return s
}

type paginationInput struct {
	Limit  int `json:"limit,omitempty" jsonschema:"maximum number of records to return; defaults to 20 and is capped at 100"`
	Offset int `json:"offset,omitempty" jsonschema:"zero-based number of records to skip; defaults to 0"`
}

type getLedgerInput struct {
	LedgerID string `json:"ledger_id" jsonschema:"the LedgerForge ledger ID"`
}

type getBalanceInput struct {
	BalanceID     string `json:"balance_id" jsonschema:"the LedgerForge balance ID"`
	IncludeQueued bool   `json:"include_queued,omitempty" jsonschema:"include queued debit and credit amounts"`
}

type getTransactionInput struct {
	TransactionID string `json:"transaction_id" jsonschema:"the LedgerForge transaction ID"`
}

type getTransactionByReferenceInput struct {
	Reference string `json:"reference" jsonschema:"the unique transaction reference"`
}

type getBalanceLineageInput struct {
	BalanceID string `json:"balance_id" jsonschema:"the balance whose fund lineage should be retrieved"`
}

type getTransactionLineageInput struct {
	TransactionID string `json:"transaction_id" jsonschema:"the transaction whose fund lineage should be retrieved"`
}

type createLedgerInput struct {
	Name     string         `json:"name" jsonschema:"name of the new ledger"`
	Metadata map[string]any `json:"metadata,omitempty" jsonschema:"optional ledger metadata"`
}

type createBalanceInput struct {
	LedgerID           string         `json:"ledger_id" jsonschema:"ledger that owns the new balance"`
	Currency           string         `json:"currency" jsonschema:"ISO currency code for the balance"`
	CurrencyMultiplier float64        `json:"currency_multiplier,omitempty" jsonschema:"minor-unit multiplier, such as 100 for cents"`
	IdentityID         string         `json:"identity_id,omitempty" jsonschema:"optional identity linked to this balance"`
	Indicator          string         `json:"indicator,omitempty" jsonschema:"optional unique human-readable balance indicator"`
	Metadata           map[string]any `json:"metadata,omitempty" jsonschema:"optional balance metadata"`
	TrackFundLineage   bool           `json:"track_fund_lineage,omitempty" jsonschema:"enable fund lineage for this balance"`
	AllocationStrategy string         `json:"allocation_strategy,omitempty" jsonschema:"FIFO, LIFO, or PROPORTIONAL when fund lineage is enabled"`
}

type queueTransactionInput struct {
	Amount             float64              `json:"amount,omitempty" jsonschema:"transaction amount in major units"`
	PreciseAmount      string               `json:"precise_amount,omitempty" jsonschema:"optional transaction amount in minor units; takes precedence when supplied"`
	Precision          float64              `json:"precision,omitempty" jsonschema:"minor-unit multiplier, such as 100 for cents"`
	Rate               float64              `json:"rate,omitempty" jsonschema:"optional exchange rate"`
	OverdraftLimit     float64              `json:"overdraft_limit,omitempty" jsonschema:"optional permitted overdraft limit"`
	Source             string               `json:"source,omitempty" jsonschema:"source balance ID; provide this or sources"`
	Destination        string               `json:"destination,omitempty" jsonschema:"destination balance ID; provide this or destinations"`
	Sources            []model.Distribution `json:"sources,omitempty" jsonschema:"optional split source distributions"`
	Destinations       []model.Distribution `json:"destinations,omitempty" jsonschema:"optional split destination distributions"`
	Reference          string               `json:"reference" jsonschema:"unique idempotency reference for this transaction"`
	Currency           string               `json:"currency" jsonschema:"ISO currency code"`
	Description        string               `json:"description,omitempty" jsonschema:"optional transaction description"`
	AllowOverdraft     bool                 `json:"allow_overdraft,omitempty" jsonschema:"allow the source balance to overdraw"`
	Inflight           bool                 `json:"inflight,omitempty" jsonschema:"create an inflight transaction that must later be committed or voided"`
	SkipQueue          bool                 `json:"skip_queue,omitempty" jsonschema:"process synchronously instead of queueing"`
	Atomic             bool                 `json:"atomic,omitempty" jsonschema:"require all split transaction operations to succeed atomically"`
	Metadata           map[string]any       `json:"metadata,omitempty" jsonschema:"optional transaction metadata"`
	EffectiveDate      *time.Time           `json:"effective_date,omitempty" jsonschema:"optional RFC3339 effective date"`
	ScheduledFor       *time.Time           `json:"scheduled_for,omitempty" jsonschema:"optional RFC3339 future processing time"`
	InflightExpiryDate *time.Time           `json:"inflight_expiry_date,omitempty" jsonschema:"optional RFC3339 inflight expiration"`
	InflightCommitDate *time.Time           `json:"inflight_commit_date,omitempty" jsonschema:"optional RFC3339 scheduled inflight commit"`
}

type commitInflightTransactionInput struct {
	TransactionID string `json:"transaction_id" jsonschema:"inflight transaction ID to commit"`
	PreciseAmount string `json:"precise_amount,omitempty" jsonschema:"minor units to commit; omit or use 0 to commit the remaining amount"`
}

type voidInflightTransactionInput struct {
	TransactionID string `json:"transaction_id" jsonschema:"inflight transaction ID to void"`
}

func (s *Server) registerReadTools() {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, Title: "LedgerForge read"}
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_get_ledger", Description: "Retrieve a ledger by ID.", Annotations: readOnly}, wrap(s.getLedger))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_list_ledgers", Description: "List ledgers with bounded pagination.", Annotations: readOnly}, wrap(s.listLedgers))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_get_balance", Description: "Retrieve a balance by ID, including its current amounts.", Annotations: readOnly}, wrap(s.getBalance))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_list_balances", Description: "List balances with bounded pagination.", Annotations: readOnly}, wrap(s.listBalances))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_get_transaction", Description: "Retrieve a transaction by ID.", Annotations: readOnly}, wrap(s.getTransaction))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_get_transaction_by_reference", Description: "Retrieve a transaction by its unique reference.", Annotations: readOnly}, wrap(s.getTransactionByReference))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_list_transactions", Description: "List transactions with bounded pagination.", Annotations: readOnly}, wrap(s.listTransactions))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_get_balance_lineage", Description: "Retrieve fund lineage for a balance.", Annotations: readOnly}, wrap(s.getBalanceLineage))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_get_transaction_lineage", Description: "Retrieve fund lineage for a transaction.", Annotations: readOnly}, wrap(s.getTransactionLineage))
}

func (s *Server) registerWriteTools() {
	additive := &mcp.ToolAnnotations{DestructiveHint: boolPointer(false), OpenWorldHint: boolPointer(false), Title: "LedgerForge write"}
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_create_ledger", Description: "Create a new ledger. This permanently writes ledger state.", Annotations: additive}, wrap(s.createLedger))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_create_balance", Description: "Create a new balance. This permanently writes ledger state.", Annotations: additive}, wrap(s.createBalance))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_queue_transaction", Description: "Queue or synchronously process a financial transaction. Use the reference as an idempotency key.", Annotations: additive}, wrap(s.queueTransaction))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_commit_inflight_transaction", Description: "Commit all or part of an inflight transaction. This changes financial balances.", Annotations: &mcp.ToolAnnotations{OpenWorldHint: boolPointer(false), Title: "Commit inflight transaction"}}, wrap(s.commitInflightTransaction))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_void_inflight_transaction", Description: "Void an inflight transaction. This changes financial balances.", Annotations: &mcp.ToolAnnotations{OpenWorldHint: boolPointer(false), Title: "Void inflight transaction"}}, wrap(s.voidInflightTransaction))
}

func wrap[Input any](handler func(context.Context, *mcp.CallToolRequest, Input) (map[string]any, error)) mcp.ToolHandlerFor[Input, map[string]any] {
	return func(ctx context.Context, request *mcp.CallToolRequest, input Input) (*mcp.CallToolResult, map[string]any, error) {
		output, err := handler(ctx, request, input)
		return nil, output, err
	}
}

func (s *Server) getLedger(_ context.Context, _ *mcp.CallToolRequest, input getLedgerInput) (map[string]any, error) {
	if input.LedgerID == "" {
		return nil, fmt.Errorf("ledger_id is required")
	}
	ledger, err := s.service.GetLedgerByID(input.LedgerID)
	return map[string]any{"ledger": ledger}, err
}

func (s *Server) listLedgers(_ context.Context, _ *mcp.CallToolRequest, input paginationInput) (map[string]any, error) {
	limit, offset := normalizePagination(input)
	ledgers, err := s.service.GetAllLedgers(limit, offset)
	return map[string]any{"ledgers": ledgers, "limit": limit, "offset": offset}, err
}

func (s *Server) getBalance(ctx context.Context, _ *mcp.CallToolRequest, input getBalanceInput) (map[string]any, error) {
	if input.BalanceID == "" {
		return nil, fmt.Errorf("balance_id is required")
	}
	balance, err := s.service.GetBalanceByID(ctx, input.BalanceID, nil, input.IncludeQueued)
	return map[string]any{"balance": balance}, err
}

func (s *Server) listBalances(ctx context.Context, _ *mcp.CallToolRequest, input paginationInput) (map[string]any, error) {
	limit, offset := normalizePagination(input)
	balances, err := s.service.GetAllBalances(ctx, limit, offset)
	return map[string]any{"balances": balances, "limit": limit, "offset": offset}, err
}

func (s *Server) getTransaction(ctx context.Context, _ *mcp.CallToolRequest, input getTransactionInput) (map[string]any, error) {
	if input.TransactionID == "" {
		return nil, fmt.Errorf("transaction_id is required")
	}
	transaction, err := s.service.GetTransaction(ctx, input.TransactionID)
	return map[string]any{"transaction": transaction}, err
}

func (s *Server) getTransactionByReference(ctx context.Context, _ *mcp.CallToolRequest, input getTransactionByReferenceInput) (map[string]any, error) {
	if input.Reference == "" {
		return nil, fmt.Errorf("reference is required")
	}
	transaction, err := s.service.GetTransactionByRef(ctx, input.Reference)
	return map[string]any{"transaction": transaction}, err
}

func (s *Server) listTransactions(_ context.Context, _ *mcp.CallToolRequest, input paginationInput) (map[string]any, error) {
	limit, offset := normalizePagination(input)
	transactions, err := s.service.GetAllTransactions(limit, offset)
	return map[string]any{"transactions": transactions, "limit": limit, "offset": offset}, err
}

func (s *Server) getBalanceLineage(ctx context.Context, _ *mcp.CallToolRequest, input getBalanceLineageInput) (map[string]any, error) {
	if input.BalanceID == "" {
		return nil, fmt.Errorf("balance_id is required")
	}
	lineage, err := s.service.GetBalanceLineage(ctx, input.BalanceID)
	return map[string]any{"lineage": lineage}, err
}

func (s *Server) getTransactionLineage(ctx context.Context, _ *mcp.CallToolRequest, input getTransactionLineageInput) (map[string]any, error) {
	if input.TransactionID == "" {
		return nil, fmt.Errorf("transaction_id is required")
	}
	lineage, err := s.service.GetTransactionLineage(ctx, input.TransactionID)
	return map[string]any{"lineage": lineage}, err
}

func (s *Server) createLedger(_ context.Context, _ *mcp.CallToolRequest, input createLedgerInput) (map[string]any, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	auditWrite("ledgerforge_create_ledger", input.Name)
	ledger, err := s.service.CreateLedger(model.Ledger{Name: input.Name, MetaData: input.Metadata})
	return map[string]any{"ledger": ledger}, err
}

func (s *Server) createBalance(ctx context.Context, _ *mcp.CallToolRequest, input createBalanceInput) (map[string]any, error) {
	if input.LedgerID == "" || input.Currency == "" {
		return nil, fmt.Errorf("ledger_id and currency are required")
	}
	auditWrite("ledgerforge_create_balance", input.LedgerID)
	balance, err := s.service.CreateBalance(ctx, model.Balance{
		LedgerID:           input.LedgerID,
		Currency:           input.Currency,
		CurrencyMultiplier: input.CurrencyMultiplier,
		IdentityID:         input.IdentityID,
		Indicator:          input.Indicator,
		MetaData:           input.Metadata,
		TrackFundLineage:   input.TrackFundLineage,
		AllocationStrategy: input.AllocationStrategy,
	})
	return map[string]any{"balance": balance}, err
}

func (s *Server) queueTransaction(ctx context.Context, _ *mcp.CallToolRequest, input queueTransactionInput) (map[string]any, error) {
	if input.Reference == "" || input.Currency == "" {
		return nil, fmt.Errorf("reference and currency are required")
	}
	if (input.Source == "" && len(input.Sources) == 0) || (input.Destination == "" && len(input.Destinations) == 0) {
		return nil, fmt.Errorf("a source or sources and a destination or destinations are required")
	}

	preciseAmount, err := parsePreciseAmount(input.PreciseAmount)
	if err != nil {
		return nil, err
	}
	if input.Amount <= 0 && (preciseAmount == nil || preciseAmount.Sign() <= 0) {
		return nil, fmt.Errorf("amount or a positive precise_amount is required")
	}

	auditWrite("ledgerforge_queue_transaction", input.Reference)
	transaction, err := s.service.QueueTransaction(ctx, &model.Transaction{
		Amount:             input.Amount,
		PreciseAmount:      preciseAmount,
		Precision:          input.Precision,
		Rate:               input.Rate,
		OverdraftLimit:     input.OverdraftLimit,
		Source:             input.Source,
		Destination:        input.Destination,
		Sources:            input.Sources,
		Destinations:       input.Destinations,
		Reference:          input.Reference,
		Currency:           input.Currency,
		Description:        input.Description,
		AllowOverdraft:     input.AllowOverdraft,
		Inflight:           input.Inflight,
		SkipQueue:          input.SkipQueue,
		Atomic:             input.Atomic,
		MetaData:           input.Metadata,
		EffectiveDate:      input.EffectiveDate,
		ScheduledFor:       dereferenceTime(input.ScheduledFor),
		InflightExpiryDate: dereferenceTime(input.InflightExpiryDate),
		InflightCommitDate: dereferenceTime(input.InflightCommitDate),
	})
	return map[string]any{"transaction": transaction}, err
}

func (s *Server) commitInflightTransaction(ctx context.Context, _ *mcp.CallToolRequest, input commitInflightTransactionInput) (map[string]any, error) {
	if input.TransactionID == "" {
		return nil, fmt.Errorf("transaction_id is required")
	}
	amount, err := parsePreciseAmount(input.PreciseAmount)
	if err != nil {
		return nil, err
	}
	if amount == nil {
		amount = big.NewInt(0)
	}
	auditWrite("ledgerforge_commit_inflight_transaction", input.TransactionID)
	transaction, err := s.service.CommitInflightTransaction(ctx, input.TransactionID, amount)
	return map[string]any{"transaction": transaction}, err
}

func (s *Server) voidInflightTransaction(ctx context.Context, _ *mcp.CallToolRequest, input voidInflightTransactionInput) (map[string]any, error) {
	if input.TransactionID == "" {
		return nil, fmt.Errorf("transaction_id is required")
	}
	auditWrite("ledgerforge_void_inflight_transaction", input.TransactionID)
	transaction, err := s.service.VoidInflightTransaction(ctx, input.TransactionID)
	return map[string]any{"transaction": transaction}, err
}

func normalizePagination(input paginationInput) (int, int) {
	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}
	offset := input.Offset
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func parsePreciseAmount(value string) (*big.Int, error) {
	if value == "" {
		return nil, nil
	}
	amount, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return nil, fmt.Errorf("precise_amount must be a base-10 integer")
	}
	return amount, nil
}

func dereferenceTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func boolPointer(value bool) *bool {
	return &value
}

func auditWrite(tool, subject string) {
	logrus.WithFields(logrus.Fields{"mcp_tool": tool, "subject": subject}).Info("LedgerForge MCP write tool invoked")
}
