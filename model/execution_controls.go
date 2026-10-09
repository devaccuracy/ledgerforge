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
	"fmt"
	"strings"
	"time"
)

// ExecutionControls is stored in an immutable JSONB column. It is independent
// of customer metadata, including for recovery and queued execution.
type ExecutionControls struct {
	ReservedMetadata   map[string]interface{} `json:"reserved_metadata"`
	EconomicVersion    int                    `json:"economic_version"`
	AllowOverdraft     bool                   `json:"allow_overdraft"`
	Inflight           bool                   `json:"inflight"`
	Atomic             bool                   `json:"atomic"`
	SkipQueue          bool                   `json:"skip_queue"`
	SkipBalanceUpdate  bool                   `json:"skip_balance_update"`
	ParentKind         string                 `json:"parent_kind"`
	QueuedParent       string                 `json:"queued_parent"`
	QueueLane          string                 `json:"queue_lane"`
	ShadowFor          string                 `json:"shadow_for"`
	GroupIDs           []string               `json:"group_ids"`
	Sources            []Distribution         `json:"sources"`
	Destinations       []Distribution         `json:"destinations"`
	InflightExpiryDate time.Time              `json:"inflight_expiry_date"`
	InflightCommitDate time.Time              `json:"inflight_commit_date"`
}

func (t *Transaction) ExecutionControls() ExecutionControls {
	reserved := make(map[string]interface{})
	for _, key := range []string{"LEDGERFORGE_LINEAGE_PROVIDER", "_lineage_type", "_provider", "_main_balance", "_allocation"} {
		if value, exists := t.ExecutionMetadata()[key]; exists {
			reserved[key] = value
		}
	}
	return ExecutionControls{
		ReservedMetadata: reserved, EconomicVersion: 1, AllowOverdraft: t.AllowOverdraft, Inflight: t.Inflight, Atomic: t.Atomic, SkipQueue: t.SkipQueue, SkipBalanceUpdate: t.SkipBalanceUpdate,
		ParentKind: t.ParentKind, QueuedParent: t.QueuedParent, QueueLane: t.QueueLane, ShadowFor: t.ShadowFor, GroupIDs: t.GroupIds, Sources: t.Sources, Destinations: t.Destinations,
		InflightExpiryDate: t.InflightExpiryDate.UTC(), InflightCommitDate: t.InflightCommitDate.UTC(),
	}
}

func (c ExecutionControls) Apply(t *Transaction) {
	t.ReservedMetadata = c.ReservedMetadata
	if t.ReservedMetadata == nil {
		t.ReservedMetadata = make(map[string]interface{})
	}
	t.AllowOverdraft, t.Inflight, t.Atomic, t.SkipQueue, t.SkipBalanceUpdate = c.AllowOverdraft, c.Inflight, c.Atomic, c.SkipQueue, c.SkipBalanceUpdate
	t.ParentKind, t.QueuedParent, t.QueueLane, t.ShadowFor = c.ParentKind, c.QueuedParent, c.QueueLane, c.ShadowFor
	t.GroupIds, t.Sources, t.Destinations = c.GroupIDs, c.Sources, c.Destinations
	t.InflightExpiryDate, t.InflightCommitDate = c.InflightExpiryDate, c.InflightCommitDate
}

// ExecutionMetadata returns immutable internal lineage decisions after a DB
// read, and creation-time metadata for a transaction that has not been accepted.
func (t *Transaction) ExecutionMetadata() map[string]interface{} {
	if t.ReservedMetadata != nil {
		return t.ReservedMetadata
	}
	return t.MetaData
}

// DecodeExecutionControls applies the historical type checks only to schema 0.
// It never normalizes the original evidence used by the hash chain.
func DecodeExecutionControls(data []byte) (ExecutionControls, error) {
	var schema struct {
		EconomicVersion int `json:"economic_version"`
	}
	var controls ExecutionControls
	if err := json.Unmarshal(data, &schema); err != nil {
		return controls, err
	}
	if schema.EconomicVersion != 0 {
		err := json.Unmarshal(data, &controls)
		return controls, err
	}
	var legacy map[string]interface{}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return controls, err
	}
	controls.ReservedMetadata, _ = legacy["reserved_metadata"].(map[string]interface{})
	controls.AllowOverdraft, _ = legacy["allow_overdraft"].(bool)
	controls.Inflight, _ = legacy["inflight"].(bool)
	controls.Atomic, _ = legacy["atomic"].(bool)
	// Main did not restore SkipQueue from metadata, even for a boolean value.
	controls.QueuedParent, _ = legacy["queued_parent"].(string)
	controls.ShadowFor, _ = legacy["shadow_for"].(string)
	controls.QueueLane = "normal"
	if lane, ok := legacy["queue_lane"].(string); ok && lane == "hot" {
		controls.QueueLane = lane
	}
	return controls, nil
}

// DecodeTransactionMetadata consumes the private SQL projection of immutable
// controls. The marker is removed before metadata leaves the database layer.
func DecodeTransactionMetadata(data []byte, t *Transaction) error {
	if err := json.Unmarshal(data, &t.MetaData); err != nil {
		return err
	}
	if raw, exists := t.MetaData["__execution_controls"]; exists {
		b, err := json.Marshal(raw)
		if err != nil {
			return err
		}
		c, err := DecodeExecutionControls(b)
		if err != nil {
			return err
		}
		c.Apply(t)
		delete(t.MetaData, "__execution_controls")
	}
	return nil
}

// ValidateCustomerTransactionMetadata protects execution, lineage and internal
// recovery/policy bookkeeping while leaving arbitrary customer keys mutable.
func ValidateCustomerTransactionMetadata(metadata map[string]interface{}) error {
	for key := range metadata {
		switch strings.ToLower(key) {
		case "allow_overdraft", "overdraft_limit", "inflight", "atomic", "skip_queue", "skip_balance_update", "queue_action", "queue_lane", "queue_pair_key", "queued_parent_transaction", "queued_parent", "parent_kind", "shadow_for", "_shadow_for", "group_ids", "sources", "destinations", "scheduled_for", "inflight_expiry_date", "inflight_commit_date", "__execution_controls", "economic_version", "execution_metadata", "ledgerforge_lineage_provider", "ledgerforge_fund_allocation", "_lineage_type", "_provider", "_main_balance", "_allocation":
			return fmt.Errorf("reserved transaction execution metadata key: %s", key)
		}
		if strings.HasPrefix(strings.ToLower(key), "policy_") || strings.HasPrefix(strings.ToLower(key), "recovery_") {
			return fmt.Errorf("reserved transaction execution metadata key: %s", key)
		}
	}
	return nil
}
