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
	"strconv"
)

// Keep the incoming decimal token until monetary conversion. Public float64
// fields remain available for existing Go clients.
func exactJSONAmount(data []byte) (string, error) {
	var payload struct {
		Amount numericJSONToken `json:"amount"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", err
	}
	if len(payload.Amount) == 0 || string(payload.Amount) == "null" {
		return "", nil
	}
	return string(payload.Amount), nil
}

func (t *RecordTransaction) UnmarshalJSON(data []byte) error {
	type plain RecordTransaction
	decoded := plain(*t)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	amount, err := exactJSONAmount(data)
	if err != nil {
		return err
	}
	controls, err := readNumericControlTokens(data)
	if err != nil {
		return err
	}
	*t = RecordTransaction(decoded)
	if amount != "" {
		t.amountDecimal = amount
	}
	if controls.precision != "" {
		t.controls.precision = controls.precision
	}
	if controls.overdraftLimit != "" {
		t.controls.overdraftLimit = controls.overdraftLimit
	}
	return nil
}

func (t *InflightUpdate) UnmarshalJSON(data []byte) error {
	type plain InflightUpdate
	decoded := plain(*t)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	amount, err := exactJSONAmount(data)
	if err != nil {
		return err
	}
	*t = InflightUpdate(decoded)
	if amount != "" {
		t.amountDecimal = amount
	}
	return nil
}

func (t *BulkInflightCommitItem) UnmarshalJSON(data []byte) error {
	type plain BulkInflightCommitItem
	decoded := plain(*t)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	amount, err := exactJSONAmount(data)
	if err != nil {
		return err
	}
	*t = BulkInflightCommitItem(decoded)
	if amount != "" {
		t.amountDecimal = amount
	}
	return nil
}

func exactAmount(raw string, amount float64) string {
	if raw != "" {
		if original, err := strconv.ParseFloat(raw, 64); err == nil && original == amount {
			return raw
		}
	}
	return strconv.FormatFloat(amount, 'f', -1, 64)
}

func (t *RecordTransaction) ExactAmount() string      { return exactAmount(t.amountDecimal, t.Amount) }
func (t *InflightUpdate) ExactAmount() string         { return exactAmount(t.amountDecimal, t.Amount) }
func (t *BulkInflightCommitItem) ExactAmount() string { return exactAmount(t.amountDecimal, t.Amount) }
