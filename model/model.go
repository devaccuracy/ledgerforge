package model

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/shopspring/decimal"
)

// GenerateUUIDWithSuffix generates a UUID with a given module name as a suffix.
// This is useful for creating unique identifiers with context-specific prefixes.
func GenerateUUIDWithSuffix(module string) string {
	id := uuid.New() // Generate a new UUID.
	uuidStr := id.String()
	idWithSuffix := fmt.Sprintf("%s_%s", module, uuidStr) // Append the module as a suffix to the UUID.
	return idWithSuffix
}

// Int64ToBigInt converts an int64 value to a *big.Int.
// This is useful for handling large numbers in computations such as financial transactions.
func Int64ToBigInt(value int64) *big.Int {
	return big.NewInt(value) // Create a new big.Int from an int64 value.
}

// HashTxn generates a SHA-256 hash of a transaction's relevant fields.
// This ensures the integrity of the transaction by creating a unique hash from its details.
func (transaction *Transaction) HashTxn() string {
	data := fmt.Sprintf("%s%s%s%s%s", strconv.FormatFloat(transaction.Amount, 'f', -1, 64), transaction.Reference, transaction.Currency, transaction.Source, transaction.Destination)
	hash := sha256.Sum256([]byte(data)) // Hash the concatenated data.
	return hex.EncodeToString(hash[:])  // Return the hex-encoded hash.
}

// compare compares two *big.Int values based on the provided condition (e.g., >, <, ==).
// Returns true if the condition holds, otherwise false.
func compare(value *big.Int, condition string, compareTo *big.Int) bool {
	cmp := value.Cmp(compareTo) // Compare value and compareTo.
	switch condition {
	case ">":
		return cmp > 0
	case "<":
		return cmp < 0
	case ">=":
		return cmp >= 0
	case "<=":
		return cmp <= 0
	case "!=":
		return cmp != 0
	case "==":
		return cmp == 0
	}
	return false
}

// InitializeBalanceFields initializes all the fields of the Balance struct that might be nil.
// This ensures that all balance-related fields have valid *big.Int values for further operations.
func (balance *Balance) InitializeBalanceFields() {
	if balance.InflightDebitBalance == nil {
		balance.InflightDebitBalance = big.NewInt(0)
	}
	if balance.InflightCreditBalance == nil {
		balance.InflightCreditBalance = big.NewInt(0)
	}
	if balance.InflightBalance == nil {
		balance.InflightBalance = big.NewInt(0)
	}
	if balance.DebitBalance == nil {
		balance.DebitBalance = big.NewInt(0)
	}
	if balance.CreditBalance == nil {
		balance.CreditBalance = big.NewInt(0)
	}
	if balance.Balance == nil {
		balance.Balance = big.NewInt(0)
	}
}

// addCredit adds the specified amount to the credit balances (either inflight or regular).
// inflight indicates whether the credit is inflight or not.
func (balance *Balance) addCredit(amountBigInt *big.Int, inflight bool) {
	balance.InitializeBalanceFields() // Ensure balance fields are initialized.

	if inflight {
		balance.InflightCreditBalance.Add(balance.InflightCreditBalance, amountBigInt)
	} else {
		balance.CreditBalance.Add(balance.CreditBalance, amountBigInt)
	}
}

// addDebit adds the specified amount to the debit balances (either inflight or regular).
// inflight indicates whether the debit is inflight or not.
func (balance *Balance) addDebit(amountBigInt *big.Int, inflight bool) {
	balance.InitializeBalanceFields()
	if inflight {
		balance.InflightDebitBalance.Add(balance.InflightDebitBalance, amountBigInt)
	} else {
		balance.DebitBalance.Add(balance.DebitBalance, amountBigInt)
	}
}

// computeBalance computes the overall balance for inflight and normal balances.
// inflight indicates whether the inflight balance or regular balance should be computed.
func (balance *Balance) computeBalance(inflight bool) {
	balance.InitializeBalanceFields()
	if inflight {
		balance.InflightBalance.Sub(balance.InflightCreditBalance, balance.InflightDebitBalance)
		return
	}
	balance.Balance.Sub(balance.CreditBalance, balance.DebitBalance)
}

// canProcessTransaction checks if a transaction can be processed given the source balance.
// It returns an error if the balance is insufficient and overdraft is not allowed.
// This function includes inflight balances and optionally queued balances in the available balance calculation
// to prevent creating transactions that would exceed the actual available funds.
func canProcessTransaction(transaction *Transaction, sourceBalance *Balance) error {
	if transaction.AllowOverdraft && transaction.OverdraftLimit == 0 {
		// If unconditional overdraft is allowed, skip all balance checks
		return nil
	}

	// Initialize balance fields to ensure inflight balances are not nil
	sourceBalance.InitializeBalanceFields()

	// Convert transaction.PreciseAmount to *big.Int for comparison.
	transactionAmount := transaction.PreciseAmount

	// Calculate available balance by subtracting inflight balances from committed balance
	// This ensures inflight balances are considered when checking if new transactions can be processed
	availableBalance := new(big.Int).Sub(sourceBalance.Balance, sourceBalance.InflightDebitBalance)

	// If queued balances are available (when enable_queued_checks is on), also subtract queued debit balance
	// This provides an even more comprehensive check by considering pending queued transactions
	if sourceBalance.QueuedDebitBalance != nil {
		availableBalance = new(big.Int).Sub(availableBalance, sourceBalance.QueuedDebitBalance)
	}

	if availableBalance.Cmp(transactionAmount) >= 0 {
		// Sufficient funds considering inflight and queued debits
		return nil
	}

	// Insufficient funds, check if within overdraft limit
	if transaction.OverdraftLimit > 0 {
		// Calculate the resulting balance after transaction, considering inflight and queued debits
		resultingBalance := new(big.Int).Sub(availableBalance, transactionAmount)

		// Convert overdraft limit to big.Int with precision applied using
		// decimal arithmetic: int64(limit*precision) truncates binary-float
		// products (0.29*100 -> 28), rejecting transactions exactly at the
		// configured limit.
		overdraftLimitPrecise := decimal.NewFromFloat(transaction.OverdraftLimit).
			Mul(decimal.NewFromFloat(transaction.Precision)).Round(0).BigInt()
		overdraftLimitBigInt := overdraftLimitPrecise

		// Negative of overdraft limit (as balance will be negative)
		negativeOverdraftLimit := new(big.Int).Neg(overdraftLimitBigInt)

		// Check if resulting balance is within overdraft limit
		if resultingBalance.Cmp(negativeOverdraftLimit) >= 0 {
			return nil
		}
		return fmt.Errorf("transaction exceeds overdraft limit")
	}

	// Insufficient funds and no overdraft allowed
	return fmt.Errorf("insufficient funds in source balance")
}

// checkSufficientInflightDebit returns an error when the inflight debit balance
// cannot cover amount.
func (balance *Balance) checkSufficientInflightDebit(amount *big.Int) error {
	if balance.InflightDebitBalance.Cmp(amount) < 0 {
		return fmt.Errorf("insufficient inflight debit balance: have %s, need %s",
			balance.InflightDebitBalance.String(), amount.String())
	}
	return nil
}

// checkSufficientInflightCredit mirrors checkSufficientInflightDebit for the credit side.
func (balance *Balance) checkSufficientInflightCredit(amount *big.Int) error {
	if balance.InflightCreditBalance.Cmp(amount) < 0 {
		return fmt.Errorf("insufficient inflight credit balance: have %s, need %s",
			balance.InflightCreditBalance.String(), amount.String())
	}
	return nil
}

// CommitInflightDebit commits a debit from the inflight balance and adds it to the
// debit balance, returning an error if the inflight hold cannot cover the amount.
func (balance *Balance) CommitInflightDebit(transaction *Transaction) error {
	balance.InitializeBalanceFields()
	transactionAmount := ApplyPrecision(transaction)

	if err := balance.checkSufficientInflightDebit(transactionAmount); err != nil {
		return err
	}
	// Deduct from inflight and add to regular debit balance.
	balance.InflightDebitBalance.Sub(balance.InflightDebitBalance, transactionAmount)
	balance.DebitBalance.Add(balance.DebitBalance, transactionAmount)
	balance.computeBalance(true)  // Recompute inflight balance.
	balance.computeBalance(false) // Recompute regular balance.
	return nil
}

// CommitInflightCredit commits a credit from the inflight balance and adds it to the credit balance.
func (balance *Balance) CommitInflightCredit(transaction *Transaction) error {
	balance.InitializeBalanceFields()
	transactionAmount := ApplyPrecision(transaction)

	if err := balance.checkSufficientInflightCredit(transactionAmount); err != nil {
		return err
	}
	// Deduct from inflight and add to regular credit balance.
	balance.InflightCreditBalance.Sub(balance.InflightCreditBalance, transactionAmount)
	balance.CreditBalance.Add(balance.CreditBalance, transactionAmount)
	balance.computeBalance(true)  // Recompute inflight balance.
	balance.computeBalance(false) // Recompute regular balance.
	return nil
}

// RollbackInflightCredit rolls back (decreases) the inflight credit balance by the specified amount.
func (balance *Balance) RollbackInflightCredit(amount *big.Int) error {
	balance.InitializeBalanceFields()
	if err := balance.checkSufficientInflightCredit(amount); err != nil {
		return err
	}
	balance.InflightCreditBalance.Sub(balance.InflightCreditBalance, amount)
	balance.computeBalance(true) // Update inflight balance.
	return nil
}

// RollbackInflightDebit rolls back (decreases) the inflight debit balance by the specified amount.
func (balance *Balance) RollbackInflightDebit(amount *big.Int) error {
	balance.InitializeBalanceFields()
	if err := balance.checkSufficientInflightDebit(amount); err != nil {
		return err
	}
	balance.InflightDebitBalance.Sub(balance.InflightDebitBalance, amount)
	balance.computeBalance(true) // Update inflight balance.
	return nil
}

// ApplyPrecision handles both operations involving precision:
// 1. If PreciseAmount exists: converts it to a decimal Amount
// 2. If Amount exists: converts it to a PreciseAmount
// This function is now a wrapper that sets default precision if needed and calls applyPrecisionLogic.
func ApplyPrecision(transaction *Transaction) *big.Int {
	// Preserve invalid input for validation instead of panicking in decimal math.
	if err := transaction.validateMonetaryFields(); err != nil {
		return big.NewInt(0)
	}
	if transaction.Precision == 0 {
		transaction.Precision = 1
	}
	return applyPrecisionLogic(transaction)
}

// applyPrecisionLogic contains the core logic for converting amounts based on precision.
// It assumes transaction.Precision has been set appropriately before this call.
func applyPrecisionLogic(transaction *Transaction) *big.Int {
	// Ensure precision is not zero to avoid division by zero, though ApplyPrecision and ApplyPrecisionWithDBLookup should handle this.
	if transaction.Precision == 0 {
		logrus.Warn("applyPrecisionLogic called with transaction.Precision = 0, defaulting to 1")
		transaction.Precision = 1
	}

	if transaction.PreciseAmount != nil && transaction.PreciseAmount.Cmp(big.NewInt(0)) > 0 {
		convertPreciseToDecimal(transaction)
		return transaction.PreciseAmount
	}

	transaction.PreciseAmount = convertDecimalToPrecise(transaction)
	// Always materialize AmountString, including for a precise amount of 0.
	// The persist path writes AmountString into a numeric column; leaving it
	// blank makes Postgres reject the insert with invalid input syntax.
	convertPreciseToDecimal(transaction)
	return transaction.PreciseAmount
}

// ApplyPrecisionWithDBLookupChecked resolves precision authoritatively without
// process-global state. API callers use their injected datasource instead.
func ApplyPrecisionWithDBLookupChecked(transaction *Transaction, db *sql.DB) (*big.Int, error) {
	if transaction == nil || db == nil || transaction.TransactionID == "" {
		return nil, errors.New("database and transaction ID are required for precision lookup")
	}
	var precision float64
	err := db.QueryRow(`SELECT precision FROM ledgerforge.transactions
 WHERE transaction_id = $1 OR parent_transaction = $1
 ORDER BY (transaction_id = $1) DESC, transaction_id LIMIT 1`, transaction.TransactionID).Scan(&precision)
	if err != nil {
		return nil, fmt.Errorf("fetch transaction precision: %w", err)
	}
	if err := ValidatePrecision(precision); err != nil {
		return nil, err
	}
	if precision == 0 {
		return nil, errors.New("stored transaction precision must be positive")
	}
	copy := *transaction
	copy.Precision = precision
	amount, err := ApplyPrecisionChecked(&copy)
	if err != nil {
		return nil, err
	}
	*transaction = copy
	return amount, nil
}

// ApplyPrecisionWithDBLookup is retained for compatibility. Lookup failures
// return nil; callers needing the cause should use the checked variant.
func ApplyPrecisionWithDBLookup(transaction *Transaction, db *sql.DB) *big.Int {
	amount, err := ApplyPrecisionWithDBLookupChecked(transaction, db)
	if err != nil {
		logrus.WithError(err).Warn("could not resolve transaction precision")
		return nil
	}
	return amount
}

// convertPreciseToDecimal converts the precise integer amount to a decimal value
// by dividing by precision, storing the exact string representation
func convertPreciseToDecimal(transaction *Transaction) {
	// Use the decimal package for exact decimal arithmetic
	preciseAmountStr := transaction.PreciseAmount.String()
	preciseAmountDec, _ := decimal.NewFromString(preciseAmountStr)

	// Create decimal for precision
	precisionDec := decimal.NewFromFloat(transaction.Precision)

	// Perform division with exact decimal arithmetic
	// Preserve every decimal place for terminating divisions. Other integer
	// scales have repeating decimal expansions; PreciseAmount stays authoritative.
	denominator := precisionDec.BigInt()
	places := int32(0)
	for _, factor := range []int64{2, 5} {
		count := int32(0)
		divisor := big.NewInt(factor)
		for new(big.Int).Mod(denominator, divisor).Sign() == 0 {
			denominator.Quo(denominator, divisor)
			count++
		}
		if count > places {
			places = count
		}
	}
	if denominator.Cmp(big.NewInt(1)) != 0 {
		places = int32(len(precisionDec.BigInt().String()) + 16)
	}
	resultDec := preciseAmountDec.DivRound(precisionDec, places)

	// Store the exact string representation
	transaction.AmountString = resultDec.String()

	// Also store the float64 for backward compatibility
	// This may still have precision issues but is kept for existing code
	transaction.Amount, _ = resultDec.Float64()
}

// convertDecimalToPrecise converts a decimal amount to precise integer
// by multiplying by precision
func convertDecimalToPrecise(transaction *Transaction) *big.Int {
	amount, err := ParseAmount(transaction.decimalAmount(), transaction.Precision)
	if err != nil {
		return big.NewInt(0)
	}
	return amount
}

// ValidateAmount is the exported ingest check used by QueueTransaction so
// zero/empty amounts never land on the queue.
func (transaction *Transaction) ValidateAmount() error {
	return transaction.validate()
}

// validate checks all monetary inputs before conversion or persistence.
func (transaction *Transaction) validate() error {
	if err := transaction.validateMonetaryFields(); err != nil {
		return err
	}
	if transaction.PreciseAmount != nil {
		if transaction.PreciseAmount.Sign() <= 0 {
			return errors.New("transaction precise amount must be positive")
		}
		return nil
	}
	_, err := ParseAmount(transaction.decimalAmount(), transaction.Precision)
	return err
}

// UpdateBalances updates the balances for both the source and destination based on the transaction details.
// It ensures precision is applied and checks for overdraft before updating.
func UpdateBalances(transaction *Transaction, source, destination *Balance) error {
	// Apply precision to get precise amount
	amount, err := ApplyPrecisionChecked(transaction)
	if err != nil {
		return err
	}

	transaction.PreciseAmount = amount

	// Check if source has sufficient funds
	err = canProcessTransaction(transaction, source)
	if err != nil {
		return err
	}

	source.InitializeBalanceFields()
	destination.InitializeBalanceFields()

	// Update source balance with original precise amount
	source.addDebit(transaction.PreciseAmount, transaction.Inflight)
	source.computeBalance(transaction.Inflight)

	// Credit the destination the same precise amount that was debited from
	// the source. (A per-transaction FX rate was removed: it was unused in
	// practice and broke inflight commit, which credited the rated amount on
	// hold but the un-rated amount on commit.)
	destination.addCredit(transaction.PreciseAmount, transaction.Inflight)
	destination.computeBalance(transaction.Inflight)

	return nil
}

// CheckCondition checks if a balance meets the condition specified by a BalanceMonitor.
// It compares various balance fields (e.g., debit balance, credit balance) against the precise value.
func (bm *BalanceMonitor) CheckCondition(b *Balance) bool {
	switch bm.Condition.Field {
	case "debit_balance":
		return compare(b.DebitBalance, bm.Condition.Operator, bm.Condition.PreciseValue)
	case "credit_balance":
		return compare(b.CreditBalance, bm.Condition.Operator, bm.Condition.PreciseValue)
	case "balance":
		return compare(b.Balance, bm.Condition.Operator, bm.Condition.PreciseValue)
	case "inflight_debit_balance":
		return compare(b.InflightDebitBalance, bm.Condition.Operator, bm.Condition.PreciseValue)
	case "inflight_credit_balance":
		return compare(b.InflightCreditBalance, bm.Condition.Operator, bm.Condition.PreciseValue)
	case "inflight_balance":
		return compare(b.InflightBalance, bm.Condition.Operator, bm.Condition.PreciseValue)
	}
	return false
}

// ToInternalTransaction converts an ExternalTransaction to an InternalTransaction.
// This is useful when reconciling external transactions with internal records.
func (et *ExternalTransaction) ToInternalTransaction() *Transaction {
	return &Transaction{
		TransactionID: et.ID,
		Amount:        et.Amount,
		Reference:     et.Reference,
		Currency:      et.Currency,
		CreatedAt:     et.Date,
		Description:   et.Description,
	}
}

// ApplyRate applies the exchange rate to the precise amount and returns a *big.Int.
// The rate is applied after precision to maintain accuracy.
func ApplyRate(preciseAmount *big.Int, rate float64) *big.Int {
	if rate == 0 {
		rate = 1
	}

	// Create a new big.Float from the precise amount
	preciseAmountFloat := new(big.Float).SetInt(preciseAmount)

	// Create a big.Float for the rate
	rateFloat := new(big.Float).SetFloat64(rate)

	// Multiply the amount by the rate
	result := new(big.Float).Mul(preciseAmountFloat, rateFloat)

	// Convert back to big.Int (rounding if necessary)
	resultBigInt := new(big.Int)
	result.Int(resultBigInt)

	return resultBigInt
}
