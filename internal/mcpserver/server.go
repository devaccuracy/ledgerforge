// Package mcpserver exposes selected LedgerForge operations through the Model
// Context Protocol. It is intentionally transport-agnostic; cmd/ledgerforge-mcp
// runs it over standard input and output.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"strings"
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
	GetBalanceAtTime(ctx context.Context, balanceID string, targetTime time.Time, fromSource bool) (*model.Balance, error)
	GetBalanceByIndicator(ctx context.Context, indicator, currency string) (*model.Balance, error)
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
	RefundTransaction(ctx context.Context, transactionID string, skipQueue bool) (*model.Transaction, error)
	CreateBulkTransactions(ctx context.Context, request *model.BulkTransactionRequest) (*model.BulkTransactionResult, error)
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
		}, &mcp.ServerOptions{Instructions: serverInstructions(options.AllowWrites)}),
	}
	s.registerReadTools()
	s.registerResources()
	s.registerPrompts()
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

type getBalanceByIndicatorInput struct {
	Indicator string `json:"indicator" jsonschema:"the human-readable balance indicator"`
	Currency  string `json:"currency" jsonschema:"ISO currency code associated with the indicator"`
}

type getBalanceAtTimeInput struct {
	BalanceID           string    `json:"balance_id" jsonschema:"the LedgerForge balance ID"`
	At                  time.Time `json:"at" jsonschema:"required RFC3339 timestamp for the historical balance snapshot"`
	CalculateFromSource bool      `json:"calculate_from_source,omitempty" jsonschema:"calculate from source transactions instead of using stored snapshots; slower but useful for audits"`
}

type getTransactionInput struct {
	TransactionID string `json:"transaction_id" jsonschema:"the LedgerForge transaction ID"`
}

type getTransactionByReferenceInput struct {
	Reference string `json:"reference" jsonschema:"the unique transaction reference"`
}

type getTransactionContextInput struct {
	TransactionID string `json:"transaction_id,omitempty" jsonschema:"transaction ID; provide exactly one of transaction_id or reference"`
	Reference     string `json:"reference,omitempty" jsonschema:"transaction reference; provide exactly one of transaction_id or reference"`
	IncludeQueued bool   `json:"include_queued,omitempty" jsonschema:"include queued amounts when retrieving source and destination balances"`
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
	Confirm  bool           `json:"confirm" jsonschema:"must be true after an authorized human has approved this write"`
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
	Confirm            bool           `json:"confirm" jsonschema:"must be true after an authorized human has approved this write"`
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
	Confirm            bool                 `json:"confirm" jsonschema:"must be true after an authorized human has approved this write"`
}

type commitInflightTransactionInput struct {
	TransactionID string `json:"transaction_id" jsonschema:"inflight transaction ID to commit"`
	PreciseAmount string `json:"precise_amount,omitempty" jsonschema:"minor units to commit; omit or use 0 to commit the remaining amount"`
	Confirm       bool   `json:"confirm" jsonschema:"must be true after an authorized human has approved this financial state change"`
}

type voidInflightTransactionInput struct {
	TransactionID string `json:"transaction_id" jsonschema:"inflight transaction ID to void"`
	Confirm       bool   `json:"confirm" jsonschema:"must be true after an authorized human has approved this financial state change"`
}

// transferFundsInput is the preferred, exact-amount payment workflow. AmountMinor
// is an integer minor-unit amount: for USD with multiplier 100, $12.34 is "1234".
type transferFundsInput struct {
	SourceBalanceID      string         `json:"source_balance_id" jsonschema:"source balance ID"`
	DestinationBalanceID string         `json:"destination_balance_id" jsonschema:"destination balance ID"`
	AmountMinor          string         `json:"amount_minor" jsonschema:"positive integer amount in minor units; never use a floating-point amount"`
	Currency             string         `json:"currency" jsonschema:"ISO currency code"`
	CurrencyMultiplier   float64        `json:"currency_multiplier" jsonschema:"minor-unit multiplier, such as 100 for cents"`
	Reference            string         `json:"reference" jsonschema:"unique idempotency reference for this transfer"`
	Description          string         `json:"description,omitempty" jsonschema:"optional transfer description"`
	AllowOverdraft       bool           `json:"allow_overdraft,omitempty" jsonschema:"allow the source balance to overdraw"`
	Inflight             bool           `json:"inflight,omitempty" jsonschema:"reserve funds as inflight until committed or voided"`
	ProcessImmediately   bool           `json:"process_immediately,omitempty" jsonschema:"record synchronously instead of queueing"`
	EffectiveDate        *time.Time     `json:"effective_date,omitempty" jsonschema:"optional RFC3339 effective date"`
	ScheduledFor         *time.Time     `json:"scheduled_for,omitempty" jsonschema:"optional RFC3339 future processing time"`
	Metadata             map[string]any `json:"metadata,omitempty" jsonschema:"optional transfer metadata"`
	Confirm              bool           `json:"confirm" jsonschema:"must be true after an authorized human has approved this financial state change"`
}

type refundTransactionInput struct {
	TransactionID      string `json:"transaction_id" jsonschema:"applied transaction ID to refund"`
	ProcessImmediately bool   `json:"process_immediately,omitempty" jsonschema:"record the refund synchronously instead of queueing"`
	Confirm            bool   `json:"confirm" jsonschema:"must be true after an authorized human has approved this financial state change"`
}

type bulkTransferInput struct {
	Transfers          []bulkTransferItem `json:"transfers" jsonschema:"one to 100 exact-amount transfers"`
	Inflight           bool               `json:"inflight,omitempty" jsonschema:"create all transfers as inflight"`
	Atomic             bool               `json:"atomic,omitempty" jsonschema:"roll back the batch if an individual transfer fails"`
	RunAsync           bool               `json:"run_async,omitempty" jsonschema:"return after dispatching the batch for asynchronous processing"`
	ProcessImmediately bool               `json:"process_immediately,omitempty" jsonschema:"process each transfer synchronously"`
	Confirm            bool               `json:"confirm" jsonschema:"must be true after an authorized human has approved this financial state change"`
}

type bulkTransferItem struct {
	SourceBalanceID      string         `json:"source_balance_id" jsonschema:"source balance ID"`
	DestinationBalanceID string         `json:"destination_balance_id" jsonschema:"destination balance ID"`
	AmountMinor          string         `json:"amount_minor" jsonschema:"positive integer amount in minor units"`
	Currency             string         `json:"currency" jsonschema:"ISO currency code"`
	CurrencyMultiplier   float64        `json:"currency_multiplier" jsonschema:"minor-unit multiplier, such as 100 for cents"`
	Reference            string         `json:"reference" jsonschema:"unique idempotency reference for this transfer"`
	Description          string         `json:"description,omitempty" jsonschema:"optional transfer description"`
	AllowOverdraft       bool           `json:"allow_overdraft,omitempty" jsonschema:"allow the source balance to overdraw"`
	EffectiveDate        *time.Time     `json:"effective_date,omitempty" jsonschema:"optional RFC3339 effective date"`
	ScheduledFor         *time.Time     `json:"scheduled_for,omitempty" jsonschema:"optional RFC3339 future processing time"`
	Metadata             map[string]any `json:"metadata,omitempty" jsonschema:"optional transfer metadata"`
}

func (s *Server) registerReadTools() {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, Title: "LedgerForge read"}
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_get_ledger", Description: "Retrieve a ledger by ID.", Annotations: readOnly}, wrap(s.getLedger))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_list_ledgers", Description: "List ledgers with bounded pagination.", Annotations: readOnly}, wrap(s.listLedgers))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_get_balance", Description: "Retrieve a balance by ID, including current amounts. Use the exact minor-unit summary for financial calculations.", Annotations: readOnly}, wrap(s.getBalance))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_find_balance", Description: "Find a balance by its human-readable indicator and currency.", Annotations: readOnly}, wrap(s.getBalanceByIndicator))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_get_balance_at_time", Description: "Retrieve an auditable historical balance state at an RFC3339 timestamp.", Annotations: readOnly}, wrap(s.getBalanceAtTime))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_list_balances", Description: "List balances with bounded pagination.", Annotations: readOnly}, wrap(s.listBalances))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_get_transaction", Description: "Retrieve a transaction by ID.", Annotations: readOnly}, wrap(s.getTransaction))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_get_transaction_by_reference", Description: "Retrieve a transaction by its unique reference.", Annotations: readOnly}, wrap(s.getTransactionByReference))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_get_transaction_context", Description: "Retrieve a transaction with source and destination balance summaries and safe next-action guidance.", Annotations: readOnly}, wrap(s.getTransactionContext))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_list_transactions", Description: "List transactions with bounded pagination.", Annotations: readOnly}, wrap(s.listTransactions))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_get_balance_lineage", Description: "Retrieve fund lineage for a balance.", Annotations: readOnly}, wrap(s.getBalanceLineage))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_get_transaction_lineage", Description: "Retrieve fund lineage for a transaction.", Annotations: readOnly}, wrap(s.getTransactionLineage))
}

func (s *Server) registerWriteTools() {
	additive := &mcp.ToolAnnotations{DestructiveHint: boolPointer(false), OpenWorldHint: boolPointer(false), Title: "LedgerForge write"}
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_create_ledger", Description: "Create a new ledger after explicit confirmation.", Annotations: additive}, wrap(s.createLedger))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_create_balance", Description: "Create a new balance after explicit confirmation.", Annotations: additive}, wrap(s.createBalance))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_transfer_funds", Description: "Preferred exact-amount transfer workflow. Amounts are integer minor units and require an idempotency reference plus explicit confirmation.", Annotations: additive}, wrap(s.transferFunds))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_create_bulk_transfers", Description: "Create up to 100 exact-amount transfers as one operational batch after explicit confirmation.", Annotations: additive}, wrap(s.createBulkTransfers))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_refund_transaction", Description: "Create a compensating refund for an eligible transaction after explicit confirmation.", Annotations: additive}, wrap(s.refundTransaction))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_queue_transaction", Description: "Advanced transaction workflow for split or scheduled transfers. Use ledgerforge_transfer_funds for standard payments.", Annotations: additive}, wrap(s.queueTransaction))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_commit_inflight_transaction", Description: "Commit all or part of an inflight transaction after explicit confirmation.", Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPointer(false), OpenWorldHint: boolPointer(false), Title: "Commit inflight transaction"}}, wrap(s.commitInflightTransaction))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_void_inflight_transaction", Description: "Void an inflight transaction after explicit confirmation. This releases reserved funds.", Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPointer(true), OpenWorldHint: boolPointer(false), Title: "Void inflight transaction"}}, wrap(s.voidInflightTransaction))
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
	return balanceResult(balance), err
}

func (s *Server) getBalanceByIndicator(ctx context.Context, _ *mcp.CallToolRequest, input getBalanceByIndicatorInput) (map[string]any, error) {
	if input.Indicator == "" || input.Currency == "" {
		return nil, fmt.Errorf("indicator and currency are required")
	}
	balance, err := s.service.GetBalanceByIndicator(ctx, input.Indicator, input.Currency)
	return balanceResult(balance), err
}

func (s *Server) getBalanceAtTime(ctx context.Context, _ *mcp.CallToolRequest, input getBalanceAtTimeInput) (map[string]any, error) {
	if input.BalanceID == "" || input.At.IsZero() {
		return nil, fmt.Errorf("balance_id and at are required")
	}
	balance, err := s.service.GetBalanceAtTime(ctx, input.BalanceID, input.At, input.CalculateFromSource)
	if err != nil {
		return nil, err
	}
	result := balanceResult(balance)
	result["at"] = input.At.UTC().Format(time.RFC3339Nano)
	result["calculation_method"] = "snapshot"
	if input.CalculateFromSource {
		result["calculation_method"] = "source_transactions"
	}
	return result, nil
}

func (s *Server) listBalances(ctx context.Context, _ *mcp.CallToolRequest, input paginationInput) (map[string]any, error) {
	limit, offset := normalizePagination(input)
	balances, err := s.service.GetAllBalances(ctx, limit, offset)
	return map[string]any{"balances": balances, "balance_summaries": balanceSummaries(balances), "limit": limit, "offset": offset}, err
}

func (s *Server) getTransaction(ctx context.Context, _ *mcp.CallToolRequest, input getTransactionInput) (map[string]any, error) {
	if input.TransactionID == "" {
		return nil, fmt.Errorf("transaction_id is required")
	}
	transaction, err := s.service.GetTransaction(ctx, input.TransactionID)
	return transactionResult(transaction), err
}

func (s *Server) getTransactionByReference(ctx context.Context, _ *mcp.CallToolRequest, input getTransactionByReferenceInput) (map[string]any, error) {
	if input.Reference == "" {
		return nil, fmt.Errorf("reference is required")
	}
	transaction, err := s.service.GetTransactionByRef(ctx, input.Reference)
	return transactionResult(&transaction), err
}

func (s *Server) getTransactionContext(ctx context.Context, _ *mcp.CallToolRequest, input getTransactionContextInput) (map[string]any, error) {
	if (input.TransactionID == "" && input.Reference == "") || (input.TransactionID != "" && input.Reference != "") {
		return nil, fmt.Errorf("provide exactly one of transaction_id or reference")
	}

	var (
		transaction *model.Transaction
		err         error
	)
	if input.TransactionID != "" {
		transaction, err = s.service.GetTransaction(ctx, input.TransactionID)
	} else {
		var found model.Transaction
		found, err = s.service.GetTransactionByRef(ctx, input.Reference)
		transaction = &found
	}
	if err != nil {
		return nil, err
	}

	result := transactionResult(transaction)
	if transaction.Source != "" {
		source, sourceErr := s.service.GetBalanceByID(ctx, transaction.Source, nil, input.IncludeQueued)
		if sourceErr != nil {
			return nil, fmt.Errorf("get source balance: %w", sourceErr)
		}
		result["source_balance"] = balanceSummary(source)
	}
	if transaction.Destination != "" {
		destination, destinationErr := s.service.GetBalanceByID(ctx, transaction.Destination, nil, input.IncludeQueued)
		if destinationErr != nil {
			return nil, fmt.Errorf("get destination balance: %w", destinationErr)
		}
		result["destination_balance"] = balanceSummary(destination)
	}
	result["recommended_next_actions"] = transactionNextActions(transaction)
	return result, nil
}

func (s *Server) listTransactions(_ context.Context, _ *mcp.CallToolRequest, input paginationInput) (map[string]any, error) {
	limit, offset := normalizePagination(input)
	transactions, err := s.service.GetAllTransactions(limit, offset)
	return map[string]any{"transactions": transactions, "transaction_summaries": transactionSummaries(transactions), "limit": limit, "offset": offset}, err
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
	if err := requireConfirmation(input.Confirm); err != nil {
		return nil, err
	}
	auditWrite("ledgerforge_create_ledger", input.Name)
	ledger, err := s.service.CreateLedger(model.Ledger{Name: input.Name, MetaData: input.Metadata})
	return map[string]any{"ledger": ledger}, err
}

func (s *Server) createBalance(ctx context.Context, _ *mcp.CallToolRequest, input createBalanceInput) (map[string]any, error) {
	if input.LedgerID == "" || input.Currency == "" {
		return nil, fmt.Errorf("ledger_id and currency are required")
	}
	if err := requireConfirmation(input.Confirm); err != nil {
		return nil, err
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
	if err := requireConfirmation(input.Confirm); err != nil {
		return nil, err
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
	return transactionResult(transaction), err
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
	if err := requireConfirmation(input.Confirm); err != nil {
		return nil, err
	}
	auditWrite("ledgerforge_commit_inflight_transaction", input.TransactionID)
	transaction, err := s.service.CommitInflightTransaction(ctx, input.TransactionID, amount)
	return transactionResult(transaction), err
}

func (s *Server) voidInflightTransaction(ctx context.Context, _ *mcp.CallToolRequest, input voidInflightTransactionInput) (map[string]any, error) {
	if input.TransactionID == "" {
		return nil, fmt.Errorf("transaction_id is required")
	}
	if err := requireConfirmation(input.Confirm); err != nil {
		return nil, err
	}
	auditWrite("ledgerforge_void_inflight_transaction", input.TransactionID)
	transaction, err := s.service.VoidInflightTransaction(ctx, input.TransactionID)
	return transactionResult(transaction), err
}

func (s *Server) transferFunds(ctx context.Context, _ *mcp.CallToolRequest, input transferFundsInput) (map[string]any, error) {
	if err := requireConfirmation(input.Confirm); err != nil {
		return nil, err
	}
	transaction, err := transactionFromTransfer(input)
	if err != nil {
		return nil, err
	}
	auditWrite("ledgerforge_transfer_funds", input.Reference)
	created, err := s.service.QueueTransaction(ctx, transaction)
	if err != nil {
		return nil, err
	}
	result := transactionResult(created)
	result["processing_mode"] = "queued"
	if input.ProcessImmediately {
		result["processing_mode"] = "synchronous"
	}
	return result, nil
}

func (s *Server) refundTransaction(ctx context.Context, _ *mcp.CallToolRequest, input refundTransactionInput) (map[string]any, error) {
	if input.TransactionID == "" {
		return nil, fmt.Errorf("transaction_id is required")
	}
	if err := requireConfirmation(input.Confirm); err != nil {
		return nil, err
	}
	auditWrite("ledgerforge_refund_transaction", input.TransactionID)
	transaction, err := s.service.RefundTransaction(ctx, input.TransactionID, input.ProcessImmediately)
	if err != nil {
		return nil, err
	}
	result := transactionResult(transaction)
	result["processing_mode"] = "queued"
	if input.ProcessImmediately {
		result["processing_mode"] = "synchronous"
	}
	return result, nil
}

func (s *Server) createBulkTransfers(ctx context.Context, _ *mcp.CallToolRequest, input bulkTransferInput) (map[string]any, error) {
	if len(input.Transfers) == 0 || len(input.Transfers) > maxPageSize {
		return nil, fmt.Errorf("transfers must contain between one and %d items", maxPageSize)
	}
	if err := requireConfirmation(input.Confirm); err != nil {
		return nil, err
	}

	transactions := make([]*model.Transaction, 0, len(input.Transfers))
	for index, transfer := range input.Transfers {
		transaction, err := transactionFromBulkTransfer(transfer, input.Inflight, input.ProcessImmediately)
		if err != nil {
			return nil, fmt.Errorf("transfer %d: %w", index+1, err)
		}
		transactions = append(transactions, transaction)
	}

	auditWrite("ledgerforge_create_bulk_transfers", fmt.Sprintf("%d transfers", len(transactions)))
	result, err := s.service.CreateBulkTransactions(ctx, &model.BulkTransactionRequest{
		Transactions: transactions,
		Inflight:     input.Inflight,
		Atomic:       input.Atomic,
		RunAsync:     input.RunAsync,
		SkipQueue:    input.ProcessImmediately,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"batch":             result,
		"atomic":            input.Atomic,
		"run_async":         input.RunAsync,
		"transaction_count": len(transactions),
	}, nil
}

func (s *Server) registerResources() {
	s.protocol.AddResource(&mcp.Resource{
		URI:         "ledgerforge://capabilities",
		Name:        "ledgerforge-capabilities",
		Title:       "LedgerForge MCP capabilities",
		Description: "Available financial workflows and the server's write-safety mode.",
		MIMEType:    "application/json",
	}, s.readCapabilitiesResource)

	for _, resource := range []*mcp.ResourceTemplate{
		{
			URITemplate: "ledgerforge://ledgers/{ledger_id}",
			Name:        "ledgerforge-ledger",
			Title:       "LedgerForge ledger",
			Description: "A ledger record by ID.",
			MIMEType:    "application/json",
		},
		{
			URITemplate: "ledgerforge://balances/{balance_id}",
			Name:        "ledgerforge-balance",
			Title:       "LedgerForge balance",
			Description: "A current balance with an exact minor-unit summary.",
			MIMEType:    "application/json",
		},
		{
			URITemplate: "ledgerforge://transactions/{transaction_id}",
			Name:        "ledgerforge-transaction",
			Title:       "LedgerForge transaction",
			Description: "A transaction with an exact minor-unit summary.",
			MIMEType:    "application/json",
		},
	} {
		s.protocol.AddResourceTemplate(resource, s.readEntityResource)
	}
}

func (s *Server) registerPrompts() {
	s.protocol.AddPrompt(&mcp.Prompt{
		Name:        "ledgerforge_investigate_balance",
		Title:       "Investigate a balance",
		Description: "Guide an audit-friendly analysis of a balance without changing ledger state.",
		Arguments: []*mcp.PromptArgument{{
			Name:        "balance_id",
			Description: "LedgerForge balance ID to investigate.",
			Required:    true,
		}},
	}, s.investigateBalancePrompt)

	s.protocol.AddPrompt(&mcp.Prompt{
		Name:        "ledgerforge_investigate_transaction",
		Title:       "Investigate a transaction",
		Description: "Guide a transaction investigation with balance context and lineage.",
		Arguments: []*mcp.PromptArgument{{
			Name:        "transaction_id",
			Description: "LedgerForge transaction ID to investigate.",
			Required:    true,
		}},
	}, s.investigateTransactionPrompt)

	s.protocol.AddPrompt(&mcp.Prompt{
		Name:        "ledgerforge_prepare_transfer",
		Title:       "Prepare a safe transfer",
		Description: "Review a proposed exact-amount transfer before using a write tool.",
		Arguments: []*mcp.PromptArgument{
			{Name: "source_balance_id", Description: "Source balance ID.", Required: true},
			{Name: "destination_balance_id", Description: "Destination balance ID.", Required: true},
			{Name: "amount_minor", Description: "Exact integer minor-unit amount.", Required: true},
			{Name: "currency", Description: "ISO currency code.", Required: true},
			{Name: "reference", Description: "Unique idempotency reference.", Required: true},
		},
	}, s.prepareTransferPrompt)
}

func (s *Server) readCapabilitiesResource(_ context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if request.Params.URI != "ledgerforge://capabilities" {
		return nil, mcp.ResourceNotFoundError(request.Params.URI)
	}
	writeMode := "read_only"
	if s.allowWrites {
		writeMode = "write_enabled_with_explicit_confirmation"
	}
	return jsonResource(request.Params.URI, map[string]any{
		"server":                "ledgerforge-mcp",
		"write_mode":            writeMode,
		"amount_representation": "All high-level financial amounts are base-10 integer minor-unit strings; do not use floating point for accounting decisions.",
		"recommended_workflows": []string{
			"Use ledgerforge_get_transaction_context to investigate a transfer and its involved balances.",
			"Use ledgerforge_get_balance_at_time for snapshot or source-transaction audit evidence.",
			"Use ledgerforge_transfer_funds for standard exact-amount transfers.",
			"Use ledgerforge_create_bulk_transfers for controlled batches of up to 100 transfers.",
		},
		"write_safety": "Write tools are unavailable unless the process starts with --allow-write and every write call includes confirm: true after authorized human approval.",
	})
}

func (s *Server) readEntityResource(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	entity, id, err := ledgerforgeResourceEntity(request.Params.URI)
	if err != nil {
		return nil, err
	}

	switch entity {
	case "ledgers":
		ledger, err := s.service.GetLedgerByID(id)
		if err != nil {
			return nil, err
		}
		return jsonResource(request.Params.URI, map[string]any{"ledger": ledger})
	case "balances":
		balance, err := s.service.GetBalanceByID(ctx, id, nil, true)
		if err != nil {
			return nil, err
		}
		return jsonResource(request.Params.URI, balanceResult(balance))
	case "transactions":
		transaction, err := s.service.GetTransaction(ctx, id)
		if err != nil {
			return nil, err
		}
		return jsonResource(request.Params.URI, transactionResult(transaction))
	default:
		return nil, mcp.ResourceNotFoundError(request.Params.URI)
	}
}

func (s *Server) investigateBalancePrompt(_ context.Context, request *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	balanceID, err := requiredPromptArgument(request, "balance_id")
	if err != nil {
		return nil, err
	}
	return promptResult("Investigate a balance without changing ledger state.", fmt.Sprintf(`Investigate LedgerForge balance %q.

1. Call ledgerforge_get_balance with include_queued=true, or read ledgerforge://balances/%s.
2. Treat every *_minor field as the authoritative exact value; use currency_multiplier only to present it to a human.
3. If fund lineage is enabled, call ledgerforge_get_balance_lineage.
4. Report the current, inflight, and queued positions separately. State the evidence and any uncertainty; do not initiate a write.`, balanceID, url.PathEscape(balanceID))), nil
}

func (s *Server) investigateTransactionPrompt(_ context.Context, request *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	transactionID, err := requiredPromptArgument(request, "transaction_id")
	if err != nil {
		return nil, err
	}
	return promptResult("Investigate a transaction with its accounting context.", fmt.Sprintf(`Investigate LedgerForge transaction %q.

1. Call ledgerforge_get_transaction_context with include_queued=true.
2. Confirm the reference, status, exact amount_minor, source, destination, and effective date.
3. For lineage-enabled flows, call ledgerforge_get_transaction_lineage.
4. If the transaction is inflight, describe commit and void consequences, but do not perform either without a separate authorized confirmation.`, transactionID)), nil
}

func (s *Server) prepareTransferPrompt(_ context.Context, request *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	arguments := make(map[string]string, 5)
	for _, name := range []string{"source_balance_id", "destination_balance_id", "amount_minor", "currency", "reference"} {
		value, err := requiredPromptArgument(request, name)
		if err != nil {
			return nil, err
		}
		arguments[name] = value
	}
	return promptResult("Prepare an exact-amount transfer for an authorized human to review.", fmt.Sprintf(`Prepare, but do not yet execute, this LedgerForge transfer:

- source balance: %q
- destination balance: %q
- amount_minor: %q
- currency: %q
- idempotency reference: %q

First retrieve both balances and verify currency, available funds, and that the reference has not already been used. Explain the exact amount in both minor units and human-readable units using each balance's currency_multiplier. Only after an authorized human explicitly approves the final details should you call ledgerforge_transfer_funds with confirm=true.`, arguments["source_balance_id"], arguments["destination_balance_id"], arguments["amount_minor"], arguments["currency"], arguments["reference"])), nil
}

func serverInstructions(allowWrites bool) string {
	mode := "This process is read-only."
	if allowWrites {
		mode = "Write tools are enabled, but every write requires confirm=true after an authorized human approves the final action."
	}
	return "LedgerForge is a financial system of record. Use exact integer minor-unit strings for financial decisions, never floating-point values. " + mode + " Prefer transaction context and balance-at-time workflows for investigations; preserve transaction references as idempotency keys."
}

func promptResult(description, text string) *mcp.GetPromptResult {
	return &mcp.GetPromptResult{
		Description: description,
		Messages: []*mcp.PromptMessage{{
			Role:    "user",
			Content: &mcp.TextContent{Text: text},
		}},
	}
}

func requiredPromptArgument(request *mcp.GetPromptRequest, name string) (string, error) {
	value := strings.TrimSpace(request.Params.Arguments[name])
	if value == "" {
		return "", fmt.Errorf("prompt argument %q is required", name)
	}
	return value, nil
}

func ledgerforgeResourceEntity(rawURI string) (string, string, error) {
	parsed, err := url.Parse(rawURI)
	if err != nil || parsed.Scheme != "ledgerforge" {
		return "", "", mcp.ResourceNotFoundError(rawURI)
	}
	entity := parsed.Host
	if entity != "ledgers" && entity != "balances" && entity != "transactions" {
		return "", "", mcp.ResourceNotFoundError(rawURI)
	}
	id := strings.TrimPrefix(parsed.EscapedPath(), "/")
	id, err = url.PathUnescape(id)
	if err != nil || id == "" || strings.Contains(id, "/") {
		return "", "", mcp.ResourceNotFoundError(rawURI)
	}
	return entity, id, nil
}

func jsonResource(uri string, value any) (*mcp.ReadResourceResult, error) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode resource: %w", err)
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
		URI:      uri,
		MIMEType: "application/json",
		Text:     string(encoded),
	}}}, nil
}

func balanceResult(balance *model.Balance) map[string]any {
	result := map[string]any{"balance": balance}
	if balance != nil {
		result["balance_summary"] = balanceSummary(balance)
	}
	return result
}

func balanceSummary(balance *model.Balance) map[string]any {
	if balance == nil {
		return nil
	}
	return map[string]any{
		"balance_id":            balance.BalanceID,
		"ledger_id":             balance.LedgerID,
		"identity_id":           balance.IdentityID,
		"indicator":             balance.Indicator,
		"currency":              balance.Currency,
		"currency_multiplier":   balance.CurrencyMultiplier,
		"balance_minor":         bigIntString(balance.Balance),
		"credit_minor":          bigIntString(balance.CreditBalance),
		"debit_minor":           bigIntString(balance.DebitBalance),
		"inflight_minor":        bigIntString(balance.InflightBalance),
		"inflight_credit_minor": bigIntString(balance.InflightCreditBalance),
		"inflight_debit_minor":  bigIntString(balance.InflightDebitBalance),
		"queued_credit_minor":   bigIntString(balance.QueuedCreditBalance),
		"queued_debit_minor":    bigIntString(balance.QueuedDebitBalance),
		"track_fund_lineage":    balance.TrackFundLineage,
		"allocation_strategy":   balance.AllocationStrategy,
		"created_at":            balance.CreatedAt,
		"amount_interpretation": "All *_minor values are exact base-10 integer minor units. Divide by currency_multiplier only for display.",
	}
}

func balanceSummaries(balances []model.Balance) []map[string]any {
	summaries := make([]map[string]any, 0, len(balances))
	for index := range balances {
		summaries = append(summaries, balanceSummary(&balances[index]))
	}
	return summaries
}

func transactionResult(transaction *model.Transaction) map[string]any {
	result := map[string]any{"transaction": transaction}
	if transaction != nil {
		result["transaction_summary"] = transactionSummary(transaction)
	}
	return result
}

func transactionSummary(transaction *model.Transaction) map[string]any {
	if transaction == nil {
		return nil
	}
	return map[string]any{
		"transaction_id":         transaction.TransactionID,
		"parent_transaction":     transaction.ParentTransaction,
		"reference":              transaction.Reference,
		"status":                 transaction.Status,
		"source_balance_id":      transaction.Source,
		"destination_balance_id": transaction.Destination,
		"currency":               transaction.Currency,
		"currency_multiplier":    transaction.Precision,
		"amount_minor":           bigIntString(transaction.PreciseAmount),
		"description":            transaction.Description,
		"inflight":               transaction.Inflight,
		"queued":                 !transaction.SkipQueue && transaction.Status == ledgerforge.StatusQueued,
		"effective_date":         transaction.EffectiveDate,
		"scheduled_for":          transaction.ScheduledFor,
		"created_at":             transaction.CreatedAt,
		"amount_interpretation":  "amount_minor is the exact base-10 integer amount. Divide by currency_multiplier only for display.",
	}
}

func transactionSummaries(transactions []model.Transaction) []map[string]any {
	summaries := make([]map[string]any, 0, len(transactions))
	for index := range transactions {
		summaries = append(summaries, transactionSummary(&transactions[index]))
	}
	return summaries
}

func transactionNextActions(transaction *model.Transaction) []string {
	if transaction == nil {
		return nil
	}
	if transaction.Inflight || transaction.Status == ledgerforge.StatusInflight {
		return []string{
			"Review the exact amount and both balance summaries before changing state.",
			"After authorized human approval, use ledgerforge_commit_inflight_transaction to settle the reservation or ledgerforge_void_inflight_transaction to release it.",
		}
	}
	if transaction.Status == ledgerforge.StatusQueued || transaction.Status == ledgerforge.StatusScheduled {
		return []string{"This transaction is not yet final. Re-read it later and keep its reference as the idempotency key."}
	}
	if transaction.Status == ledgerforge.StatusRejected || transaction.Status == ledgerforge.StatusFailed {
		return []string{"This transaction did not complete successfully. Investigate the recorded error or operational logs before attempting a new payment."}
	}
	if transaction.Status == ledgerforge.StatusVoid {
		return []string{"This inflight transaction was voided and its reservation was released. Do not attempt to commit it; investigate or create a new authorized payment if needed."}
	}
	return []string{"For an eligible completed payment, ledgerforge_refund_transaction creates a compensating entry; it does not alter the original transaction."}
}

func transactionFromTransfer(input transferFundsInput) (*model.Transaction, error) {
	if input.SourceBalanceID == "" || input.DestinationBalanceID == "" || input.Reference == "" || input.Currency == "" {
		return nil, fmt.Errorf("source_balance_id, destination_balance_id, reference, and currency are required")
	}
	if input.SourceBalanceID == input.DestinationBalanceID {
		return nil, fmt.Errorf("source_balance_id and destination_balance_id must differ")
	}
	if input.CurrencyMultiplier <= 0 {
		return nil, fmt.Errorf("currency_multiplier must be greater than zero")
	}
	amount, err := parsePositivePreciseAmount(input.AmountMinor, "amount_minor")
	if err != nil {
		return nil, err
	}
	return &model.Transaction{
		PreciseAmount:  amount,
		Precision:      input.CurrencyMultiplier,
		Source:         input.SourceBalanceID,
		Destination:    input.DestinationBalanceID,
		Reference:      input.Reference,
		Currency:       input.Currency,
		Description:    input.Description,
		AllowOverdraft: input.AllowOverdraft,
		Inflight:       input.Inflight,
		SkipQueue:      input.ProcessImmediately,
		MetaData:       input.Metadata,
		EffectiveDate:  input.EffectiveDate,
		ScheduledFor:   dereferenceTime(input.ScheduledFor),
	}, nil
}

func transactionFromBulkTransfer(input bulkTransferItem, inflight, processImmediately bool) (*model.Transaction, error) {
	return transactionFromTransfer(transferFundsInput{
		SourceBalanceID:      input.SourceBalanceID,
		DestinationBalanceID: input.DestinationBalanceID,
		AmountMinor:          input.AmountMinor,
		Currency:             input.Currency,
		CurrencyMultiplier:   input.CurrencyMultiplier,
		Reference:            input.Reference,
		Description:          input.Description,
		AllowOverdraft:       input.AllowOverdraft,
		Inflight:             inflight,
		ProcessImmediately:   processImmediately,
		EffectiveDate:        input.EffectiveDate,
		ScheduledFor:         input.ScheduledFor,
		Metadata:             input.Metadata,
		Confirm:              true,
	})
}

func requireConfirmation(confirmed bool) error {
	if !confirmed {
		return fmt.Errorf("confirm must be true after an authorized human has approved this write")
	}
	return nil
}

func parsePositivePreciseAmount(value, field string) (*big.Int, error) {
	amount, err := parsePreciseAmount(value)
	if err != nil || amount == nil || amount.Sign() <= 0 {
		if err != nil {
			return nil, fmt.Errorf("%s: %w", field, err)
		}
		return nil, fmt.Errorf("%s must be a positive base-10 integer", field)
	}
	return amount, nil
}

func bigIntString(value *big.Int) string {
	if value == nil {
		return "0"
	}
	return value.String()
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
