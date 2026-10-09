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

import "time"

// Policy amounts are exact integer minor units encoded as decimal strings.
// A currency is authorised only at the specified minor-unit multiplier.
type SpendingRule struct {
	Currency       string  `json:"currency"`
	Precision      float64 `json:"precision"`
	MaxTransaction string  `json:"max_transaction,omitempty"`
	DailyLimit     string  `json:"daily_limit,omitempty"`
	ApprovalAbove  string  `json:"approval_above,omitempty"`
}
type KeyPolicy struct {
	APIKeyID   string         `json:"api_key_id"`
	Version    int64          `json:"version"`
	Rules      []SpendingRule `json:"rules"`
	LedgerIDs  []string       `json:"ledger_ids,omitempty"`
	BalanceIDs []string       `json:"balance_ids,omitempty"`
}
type PolicyLeg struct {
	TransactionID     string  `json:"transaction_id"`
	Reference         string  `json:"reference"`
	Source            string  `json:"source"`
	Destination       string  `json:"destination"`
	SourceLedger      string  `json:"source_ledger"`
	DestinationLedger string  `json:"destination_ledger"`
	Currency          string  `json:"currency"`
	Precision         float64 `json:"precision"`
	Amount            string  `json:"amount"`
}
type PolicyAdmission struct {
	ID        string      `json:"admission_id"`
	APIKeyID  string      `json:"api_key_id"`
	Identity  string      `json:"identity"`
	Version   int64       `json:"policy_version"`
	Day       time.Time   `json:"day"`
	Decision  string      `json:"decision"`
	DecidedBy string      `json:"decided_by,omitempty"`
	Legs      []PolicyLeg `json:"legs"`
}
type PolicyCapacity struct {
	Currency  string    `json:"currency"`
	Precision float64   `json:"precision"`
	Day       time.Time `json:"day"`
	Used      string    `json:"used_minor"`
	Remaining *string   `json:"remaining_minor,omitempty"`
}
