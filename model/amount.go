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
package model

import (
	"errors"
	"math"
	"math/big"
	"regexp"
	"strconv"

	"github.com/shopspring/decimal"
)

// ValidatePrecision accepts a finite positive integer scale. Zero means the
// existing default scale of one; stored precisions must be positive.
func ValidatePrecision(precision float64) error {
	if math.IsNaN(precision) || math.IsInf(precision, 0) || precision < 0 || math.Trunc(precision) != precision {
		return errors.New("precision must be a finite positive integer (or zero for the default)")
	}
	return nil
}

// ParseAmount parses a major-unit decimal exactly and rounds to the nearest
// minor unit, with halves away from zero. Positive amounts rounding to zero
// are rejected. Use precise_amount for integer amounts beyond float64 accuracy.
func ParseAmount(amount string, precision float64) (*big.Int, error) {
	if err := ValidatePrecision(precision); err != nil {
		return nil, err
	}
	if precision == 0 {
		precision = 1
	}
	value, err := parseMonetaryDecimal(amount)
	if err != nil {
		return nil, errors.New("invalid decimal amount")
	}
	if value.Sign() <= 0 {
		return nil, errors.New("transaction amount must be positive")
	}
	minor := value.Mul(decimal.NewFromFloat(precision)).Round(0).BigInt()
	if minor.Sign() <= 0 {
		return nil, errors.New("transaction amount rounds to zero minor units")
	}
	return minor, nil
}

func (transaction *Transaction) validateMonetaryFields() error {
	if transaction == nil {
		return errors.New("transaction is required")
	}
	if transaction.Amount < 0 {
		return errors.New("transaction amount must be positive")
	}
	if math.IsNaN(transaction.Amount) || math.IsInf(transaction.Amount, 0) {
		return errors.New("transaction amount must be finite and positive")
	}
	if transaction.AmountString != "" {
		value, err := parseMonetaryDecimal(transaction.AmountString)
		if err != nil || value.Sign() < 0 {
			return errors.New("transaction decimal amount must be finite and non-negative")
		}
	}
	if err := ValidatePrecision(transaction.Precision); err != nil {
		return err
	}
	if math.IsNaN(transaction.Rate) || math.IsInf(transaction.Rate, 0) || transaction.Rate < 0 {
		return errors.New("rate must be finite and positive (or zero for the default)")
	}
	if math.IsNaN(transaction.OverdraftLimit) || math.IsInf(transaction.OverdraftLimit, 0) || transaction.OverdraftLimit < 0 {
		return errors.New("overdraft limit must be finite and non-negative")
	}
	return nil
}

func (transaction *Transaction) decimalAmount() string {
	if transaction.AmountString != "" {
		return transaction.AmountString
	}
	return strconv.FormatFloat(transaction.Amount, 'f', -1, 64)
}

// ApplyPrecisionChecked validates monetary inputs before changing them.
func ApplyPrecisionChecked(transaction *Transaction) (*big.Int, error) {
	if err := transaction.validate(); err != nil {
		return nil, err
	}
	return ApplyPrecision(transaction), nil
}

// Bound decimal text and exponents before arithmetic to avoid unbounded
// allocations from short inputs such as 1e2147483647.
var monetaryDecimalPattern = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

func parseMonetaryDecimal(text string) (decimal.Decimal, error) {
	if len(text) > 4096 {
		return decimal.Zero, errors.New("decimal input exceeds 4096 characters")
	}
	if !monetaryDecimalPattern.MatchString(text) {
		return decimal.Zero, errors.New("invalid decimal format")
	}
	value, err := decimal.NewFromString(text)
	if err != nil {
		return decimal.Zero, err
	}
	if value.Exponent() < -4096 || value.Exponent() > 4096 {
		return decimal.Zero, errors.New("decimal exponent exceeds supported range")
	}
	return value, nil
}
