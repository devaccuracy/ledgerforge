/*
Copyright 2024 Blnk Finance Authors.

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
	"math/big"
	"sync"
	"time"
)

// Balance represents a financial balance with credit, debit, inflight, and queued sub-balances.
type Balance struct {
	ID                    int64                  `json:"-"`
	Balance               *big.Int               `json:"balance"`
	Version               int64                  `json:"version"`
	InflightBalance       *big.Int               `json:"inflight_balance"`
	CreditBalance         *big.Int               `json:"credit_balance"`
	InflightCreditBalance *big.Int               `json:"inflight_credit_balance"`
	DebitBalance          *big.Int               `json:"debit_balance"`
	InflightDebitBalance  *big.Int               `json:"inflight_debit_balance"`
	QueuedDebitBalance    *big.Int               `json:"queued_debit_balance,omitempty"`
	QueuedCreditBalance   *big.Int               `json:"queued_credit_balance,omitempty"`
	CurrencyMultiplier    float64                `json:"currency_multiplier"`
	LedgerID              string                 `json:"ledger_id"`
	IdentityID            string                 `json:"identity_id"`
	BalanceID             string                 `json:"balance_id"`
	Indicator             string                 `json:"indicator,omitempty"`
	Currency              string                 `json:"currency"`
	Identity              *Identity              `json:"identity,omitempty"`
	Ledger                *Ledger                `json:"ledger,omitempty"`
	CreatedAt             time.Time              `json:"created_at"`
	InflightExpiresAt     time.Time              `json:"inflight_expires_at"`
	MetaData              map[string]interface{} `json:"meta_data"`
	TrackFundLineage      bool                   `json:"track_fund_lineage"`
	AllocationStrategy    string                 `json:"allocation_strategy,omitempty"`
}

// BalanceMonitor defines a condition-based alert that triggers a callback when a balance meets the specified criteria.
type BalanceMonitor struct {
	MonitorID   string         `json:"monitor_id"`
	BalanceID   string         `json:"balance_id"`
	Description string         `json:"description,omitempty"`
	CallBackURL string         `json:"-"`
	CreatedAt   time.Time      `json:"created_at"`
	Condition   AlertCondition `json:"condition"`
}

// LineageMapping tracks the relationship between a balance, its fund provider, and associated shadow/aggregate balances.
type LineageMapping struct {
	ID                 int64     `json:"id"`
	BalanceID          string    `json:"balance_id"`
	Provider           string    `json:"provider"`
	ShadowBalanceID    string    `json:"shadow_balance_id"`
	AggregateBalanceID string    `json:"aggregate_balance_id"`
	IdentityID         string    `json:"identity_id"`
	CreatedAt          time.Time `json:"created_at"`
}

// BalanceFilter defines filter criteria for querying balances by range, currency, ledger, and time period.
type BalanceFilter struct {
	ID                 int64     `json:"id"`
	BalanceRange       string    `json:"balance_range"`
	CreditBalanceRange string    `json:"credit_balance_range"`
	DebitBalanceRange  string    `json:"debit_balance_range"`
	Currency           string    `json:"currency"`
	LedgerID           string    `json:"ledger_id"`
	From               time.Time `json:"from"`
	To                 time.Time `json:"to"`
}

// BalanceTracker tracks in-memory balance state and access frequency for hot-path optimization.
type BalanceTracker struct {
	Balances    map[string]*Balance
	Frequencies map[string]int
	Mutex       sync.Mutex
}
// AlertCondition specifies the field, operator, value, and precision for a balance monitor alert threshold.
type AlertCondition struct {
	Value        float64  `json:"value"`
	Precision    float64  `json:"precision"`
	PreciseValue *big.Int `json:"precise_value"`
	Field        string   `json:"field"`
	Operator     string   `json:"operator"`
}
