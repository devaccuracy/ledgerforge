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
	"encoding/json"
	"errors"
	"math/big"
	"strconv"

	"github.com/shopspring/decimal"
)

type numericControlTokens struct{ precision, overdraftLimit string }

func readNumericControlTokens(data []byte) (numericControlTokens, error) {
	var fields struct {
		Precision      json.RawMessage `json:"precision"`
		OverdraftLimit json.RawMessage `json:"overdraft_limit"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return numericControlTokens{}, err
	}
	token := func(raw json.RawMessage) string {
		if string(raw) == "null" {
			return ""
		}
		return string(raw)
	}
	return numericControlTokens{precision: token(fields.Precision), overdraftLimit: token(fields.OverdraftLimit)}, nil
}

// Controls must survive both decoding and ledger decimal arithmetic unchanged.
// Precision is an exact integer; large overdraft caps must also avoid binary
// rounding. Ordinary fractional caps keep their decimal round-trip semantics.
func validateExactControl(raw string, value float64, integer bool) error {
	if raw == "" {
		return nil
	}
	original, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return errors.New("invalid numeric control")
	}
	// A Go caller may replace a decoded field before validating the request.
	if original != value {
		return nil
	}
	if len(raw) > 4096 {
		return errors.New("numeric control exceeds supported range")
	}
	exact, err := decimal.NewFromString(raw)
	if err != nil || exact.Exponent() < -4096 || exact.Exponent() > 4096 {
		return errors.New("numeric control exceeds supported range")
	}
	if integer && !exact.IsInteger() {
		return ErrPrecisionMustBeInteger
	}
	if integer || exact.Abs().Cmp(decimal.NewFromInt(1<<53)) >= 0 {
		binaryValue := new(big.Rat).SetFloat64(value)
		if binaryValue == nil || exact.Rat().Cmp(binaryValue) != 0 {
			return errors.New("numeric control loses decimal precision; use an exactly representable value")
		}
	}
	if !exact.Equal(decimal.NewFromFloat(value)) {
		return errors.New("numeric control loses decimal precision; use an exactly representable value")
	}
	return nil
}

func (b *CreateBalance) UnmarshalJSON(data []byte) error {
	type plain CreateBalance
	decoded := plain(*b)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	controls, err := readNumericControlTokens(data)
	if err != nil {
		return err
	}
	*b = CreateBalance(decoded)
	if controls.precision != "" {
		b.precisionDecimal = controls.precision
	}
	return nil
}

func (c *MonitorCondition) UnmarshalJSON(data []byte) error {
	type plain MonitorCondition
	decoded := plain(*c)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	controls, err := readNumericControlTokens(data)
	if err != nil {
		return err
	}
	*c = MonitorCondition(decoded)
	if controls.precision != "" {
		c.precisionDecimal = controls.precision
	}
	return nil
}
