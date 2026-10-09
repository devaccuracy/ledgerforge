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

package integrity

import (
	"context"
	"testing"

	"github.com/devaccuracy/ledgerforge/model"
)

func fixture() Snapshot {
	row := func(id, status, parent, amount string) model.ChainRow {
		return model.ChainRow{TransactionID: id, Status: status, ParentTransaction: parent, Source: "a", Destination: "b", Amount: amount, PreciseAmount: amount, Precision: "100", Rate: "1", Currency: "USD"}
	}
	return Snapshot{
		Balances: []Balance{
			{ID: "a", Currency: "USD", Balance: "-110", Credit: "0", Debit: "110", InflightBalance: "0", InflightCredit: "0", InflightDebit: "0"},
			{ID: "b", Currency: "USD", Balance: "110", Credit: "110", Debit: "0", InflightBalance: "0", InflightCredit: "0", InflightDebit: "0"}},
		Transactions: []model.ChainRow{row("transfer", "APPLIED", "", "100"), row("hold", "INFLIGHT", "", "30"), row("commit", "APPLIED", "hold", "10"), row("void", "VOID", "hold", "20"), row("queue", "QUEUED", "", "999"), row("schedule", "SCHEDULED", "", "999"), row("reject", "REJECTED", "", "999")},
	}
}
func TestReplayLifecycle(t *testing.T) {
	s := fixture()
	if r := Check(s); !r.Verified {
		t.Fatalf("valid lifecycle: %+v", r.Issues)
	}
	s.Transactions = s.Transactions[:3]
	s.Balances[0].InflightDebit = "20"
	s.Balances[0].InflightBalance = "-20"
	s.Balances[1].InflightCredit = "20"
	s.Balances[1].InflightBalance = "20"
	if r := Check(s); !r.Verified {
		t.Fatalf("partial reservation: %+v", r.Issues)
	}
}

func TestReplayLegacyCustomerMetadataTypes(t *testing.T) {
	s := fixture()
	for i := range s.Transactions {
		s.Transactions[i].ExecutionControls = `{"economic_version":0,"allow_overdraft":"merchant_label","inflight":7,"atomic":[],"queue_lane":true,"queued_parent":9}`
	}
	if r := Check(s); !r.Verified {
		t.Fatalf("legacy customer metadata poisoned replay: %+v", r.Issues)
	}
	s.Transactions[0].ExecutionControls = `{"economic_version":1,"allow_overdraft":"merchant_label"}`
	if r := Check(s); r.Verified {
		t.Fatal("malformed current execution controls accepted")
	}
}
func TestReplayFindings(t *testing.T) {
	cases := []struct {
		name, kind string
		mutate     func(*Snapshot)
	}{
		{"balance identity", "mismatch", func(s *Snapshot) { s.Balances[0].Balance = "0" }},
		{"counter", "invalid_counter", func(s *Snapshot) { s.Balances[0].Debit = "-1" }},
		{"inflight identity", "mismatch", func(s *Snapshot) { s.Balances[0].InflightBalance = "1" }},
		{"fractional scale", "invalid_scale", func(s *Snapshot) { s.Transactions[0].Precision = "0.5" }},
		{"missing precise amount", "unsupported_legacy", func(s *Snapshot) { s.Transactions[0].PreciseAmount = "" }},
		{"legacy rate", "unsupported_legacy", func(s *Snapshot) { s.Transactions[0].Rate = "2" }},
		{"invalid rate", "invalid_rate", func(s *Snapshot) { s.Transactions[0].Rate = "NaN" }},
		{"overcommit", "lifecycle_bound", func(s *Snapshot) { s.Transactions[2].PreciseAmount = "31" }},
		{"wrong void remainder", "lifecycle_bound", func(s *Snapshot) { s.Transactions[3].PreciseAmount = "19" }},
		{"wrong child endpoints", "inconsistent_child", func(s *Snapshot) { s.Transactions[2].Destination = "a" }},
		{"duplicate void", "duplicate_void", func(s *Snapshot) {
			s.Transactions = append(s.Transactions, s.Transactions[3])
			s.Transactions[len(s.Transactions)-1].TransactionID = "void2"
		}},
		{"missing parent", "unsupported_legacy", func(s *Snapshot) { s.Transactions[2].ParentTransaction = "missing" }},
		{"duplicate reversal", "duplicate_reversal", func(s *Snapshot) {
			for _, id := range []string{"refund1", "refund2"} {
				r := s.Transactions[0]
				r.TransactionID = id
				r.ParentTransaction = "transfer"
				r.Source = "b"
				r.Destination = "a"
				s.Transactions = append(s.Transactions, r)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture()
			tc.mutate(&s)
			report := Check(s)
			if report.Verified {
				t.Fatal("corruption accepted")
			}
			for _, issue := range report.Issues {
				if issue.Kind == tc.kind {
					return
				}
			}
			t.Fatalf("missing %s: %+v", tc.kind, report.Issues)
		})
	}
}
func TestCrossCurrencyTransfersDoNotClaimConservation(t *testing.T) {
	s := fixture()
	s.Balances[1].Currency = "EUR"
	// Current arithmetic supports different currency labels with equal minor
	// units; this is deliberately excluded from a per-currency conservation check.
	if r := Check(s); !r.Verified {
		t.Fatalf("unsupported conservation assertion: %+v", r.Issues)
	}
}

type snapshotReader struct{ snapshot Snapshot }

func (r snapshotReader) ReadIntegritySnapshot(context.Context) (Snapshot, error) {
	return r.snapshot, nil
}
func TestVerifyReadOnlyService(t *testing.T) {
	r, err := Verify(context.Background(), snapshotReader{fixture()})
	if err != nil || !r.Verified {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestPendingInflightLifecycleBounds(t *testing.T) {
	s := fixture()
	s.Transactions = s.Transactions[:2]
	s.Balances[0].Debit = "100"
	s.Balances[0].Balance = "-100"
	s.Balances[0].InflightDebit = "30"
	s.Balances[0].InflightBalance = "-30"
	s.Balances[1].Credit = "100"
	s.Balances[1].Balance = "100"
	s.Balances[1].InflightCredit = "30"
	s.Balances[1].InflightBalance = "30"
	pending := s.Transactions[1]
	pending.TransactionID = "pending"
	pending.ParentTransaction = "hold"
	pending.Status = "QUEUED"
	pending.PreciseAmount = "20"
	s.Transactions = append(s.Transactions, pending)
	if r := Check(s); !r.Verified {
		t.Fatalf("pending intent must not release reservation: %+v", r.Issues)
	}
	for _, amount := range []string{"31", "", "-1"} {
		s.Transactions[2].PreciseAmount = amount
		if r := Check(s); r.Verified {
			t.Fatalf("invalid pending amount %q accepted", amount)
		}
	}
	s.Transactions[2].PreciseAmount = "20"
	s.Transactions[2].Destination = "a"
	if r := Check(s); r.Verified {
		t.Fatal("inconsistent pending child accepted")
	}
}

func TestEconomicChildrenThroughPersistedIntents(t *testing.T) {
	s := fixture()
	commitIntent := s.Transactions[2]
	commitIntent.TransactionID = "commit_intent"
	commitIntent.Status = "QUEUED"
	voidIntent := s.Transactions[3]
	voidIntent.TransactionID = "void_intent"
	voidIntent.Status = "QUEUED"
	s.Transactions[2].ParentTransaction = commitIntent.TransactionID
	s.Transactions[3].ParentTransaction = voidIntent.TransactionID
	s.Transactions = append(s.Transactions, commitIntent, voidIntent)
	if r := Check(s); !r.Verified {
		t.Fatalf("completed intents were replayed or releases omitted: %+v", r.Issues)
	}
	refundIntent := s.Transactions[0]
	refundIntent.TransactionID = "refund_intent"
	refundIntent.Status = "QUEUED"
	refundIntent.ParentTransaction = "transfer"
	refundIntent.Source = "b"
	refundIntent.Destination = "a"
	refund := refundIntent
	refund.TransactionID = "refund"
	refund.ParentTransaction = "refund_intent"
	refund.Status = "APPLIED"
	s.Transactions = append(s.Transactions, refundIntent, refund)
	s.Balances[0].Credit = "100"
	s.Balances[0].Balance = "-10"
	s.Balances[1].Debit = "100"
	s.Balances[1].Balance = "10"
	if r := Check(s); !r.Verified {
		t.Fatalf("refund intent was treated as economic parent: %+v", r.Issues)
	}
	duplicate := refund
	duplicate.TransactionID = "duplicate"
	s.Transactions = append(s.Transactions, duplicate)
	r := Check(s)
	for _, issue := range r.Issues {
		if issue.Kind == "duplicate_reversal" {
			return
		}
	}
	t.Fatalf("duplicate reversal hidden behind intent: %+v", r.Issues)
}
