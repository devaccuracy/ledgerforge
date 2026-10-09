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
	"fmt"
	"math/big"
	"sort"

	"github.com/devaccuracy/ledgerforge/model"
)

// Snapshot contains only persisted economic evidence. Readers must obtain both
// collections in the same read-only REPEATABLE READ database transaction.
type Snapshot struct {
	Balances     []Balance
	Transactions []model.ChainRow
}

type Balance struct {
	ID              string
	Currency        string
	Balance         string
	Credit          string
	Debit           string
	InflightBalance string
	InflightCredit  string
	InflightDebit   string
}

type Reader interface {
	ReadIntegritySnapshot(context.Context) (Snapshot, error)
}

type Issue struct {
	Kind          string `json:"kind"`
	BalanceID     string `json:"balance_id,omitempty"`
	TransactionID string `json:"transaction_id,omitempty"`
	Field         string `json:"field,omitempty"`
	Expected      string `json:"expected,omitempty"`
	Actual        string `json:"actual,omitempty"`
	Message       string `json:"message"`
}

type Report struct {
	Verified     bool    `json:"verified"`
	Balances     int     `json:"balances"`
	Transactions int     `json:"transactions"`
	Issues       []Issue `json:"issues"`
}

// Verify never repairs or invokes reconciliation. Unsupported legacy evidence
// prevents a clean verdict, rather than silently assuming a monetary value.
func Verify(ctx context.Context, reader Reader) (Report, error) {
	snapshot, err := reader.ReadIntegritySnapshot(ctx)
	if err != nil {
		return Report{}, err
	}
	return Check(snapshot), nil
}

func integer(s string) (*big.Int, bool) {
	r, ok := new(big.Rat).SetString(s)
	if !ok || !r.IsInt() {
		return nil, false
	}
	return new(big.Int).Set(r.Num()), true
}

type counters struct{ credit, debit, inflightCredit, inflightDebit *big.Int }

func zeroCounters() counters { return counters{new(big.Int), new(big.Int), new(big.Int), new(big.Int)} }

// Check replays committed legs and reservation releases with integer arithmetic.
// The current engine ignores the retained rate field and credits the same minor
// units it debits. Legacy non-unit rates cannot identify which historic engine
// semantics produced a row, so those rows are explicitly unsupported.
func Check(snapshot Snapshot) Report {
	report := Report{Balances: len(snapshot.Balances), Transactions: len(snapshot.Transactions), Issues: []Issue{}}
	add := func(kind, balance, txn, message string) {
		report.Issues = append(report.Issues, Issue{Kind: kind, BalanceID: balance, TransactionID: txn, Message: message})
	}
	stored := make(map[string]Balance, len(snapshot.Balances))
	replay := make(map[string]counters, len(snapshot.Balances))
	uncertain := make(map[string]bool)
	compare := func(id, field, actual string, expected *big.Int) {
		n, ok := integer(actual)
		if !ok || n.Cmp(expected) != 0 {
			report.Issues = append(report.Issues, Issue{Kind: "mismatch", BalanceID: id, Field: field, Expected: expected.String(), Actual: actual, Message: field + " differs from economic replay or identity"})
		}
	}
	for _, b := range snapshot.Balances {
		stored[b.ID], replay[b.ID] = b, zeroCounters()
		c, cok := integer(b.Credit)
		d, dok := integer(b.Debit)
		ic, icok := integer(b.InflightCredit)
		id, idok := integer(b.InflightDebit)
		if cok && dok {
			compare(b.ID, "balance", b.Balance, new(big.Int).Sub(c, d))
		}
		if icok && idok {
			compare(b.ID, "inflight_balance", b.InflightBalance, new(big.Int).Sub(ic, id))
		}
		for _, field := range []struct{ name, value string }{{"credit_balance", b.Credit}, {"debit_balance", b.Debit}, {"inflight_credit_balance", b.InflightCredit}, {"inflight_debit_balance", b.InflightDebit}} {
			n, ok := integer(field.value)
			if !ok || n.Sign() < 0 {
				report.Issues = append(report.Issues, Issue{Kind: "invalid_counter", BalanceID: b.ID, Field: field.name, Actual: field.value, Message: "counter must be a non-negative integer"})
			}
		}
	}
	rows := make(map[string]model.ChainRow, len(snapshot.Transactions))
	controls := make(map[string]model.ExecutionControls)
	amounts := make(map[string]*big.Int, len(snapshot.Transactions))
	children := make(map[string][]model.ChainRow)
	for _, r := range snapshot.Transactions {
		if _, exists := rows[r.TransactionID]; exists {
			add("duplicate_transaction", "", r.TransactionID, "duplicate transaction identifier")
		}
		rows[r.TransactionID] = r
		var control model.ExecutionControls
		if r.ExecutionControls != "" {
			var err error
			control, err = model.DecodeExecutionControls([]byte(r.ExecutionControls))
			if err != nil {
				add("unsupported_legacy", "", r.TransactionID, "invalid execution schema evidence")
			}
		}
		controls[r.TransactionID] = control
		if control.EconomicVersion > 1 || control.EconomicVersion < 0 {
			add("unsupported_legacy", "", r.TransactionID, "unsupported economic schema version")
		}

		switch r.Status {
		case "QUEUED", "SCHEDULED", "REJECTED":
			// Intents have no economic legs. Their amounts are considered only for
			// lifecycle bounds when attached to an inflight parent.
			if a, ok := integer(r.PreciseAmount); ok && a.Sign() > 0 {
				amounts[r.TransactionID] = a
			}
			continue
		case "APPLIED", "INFLIGHT", "VOID":
		default:
			add("unsupported_legacy", "", r.TransactionID, "unsupported persisted status: "+r.Status)
			uncertain[r.Source], uncertain[r.Destination] = true, true
			continue
		}
		a, ok := integer(r.PreciseAmount)
		if !ok {
			add("unsupported_legacy", "", r.TransactionID, "missing or fractional precise_amount; major-unit fallback is unsafe")
			uncertain[r.Source], uncertain[r.Destination] = true, true
			continue
		}
		if a.Sign() <= 0 {
			add("invalid_amount", "", r.TransactionID, "economic amount must be positive")
		}
		scale, ok := integer(r.Precision)
		if !ok || scale.Sign() <= 0 {
			add("invalid_scale", "", r.TransactionID, "precision must be a positive integer scale")
		}
		rateText := r.Rate
		if r.HistoricalRate != "" {
			rateText = r.HistoricalRate
		}
		rate, ok := new(big.Rat).SetString(rateText)
		if !ok || rate.Sign() < 0 {
			add("invalid_rate", "", r.TransactionID, "rate must be finite and non-negative")
		} else if control.EconomicVersion != 1 && rate.Sign() != 0 && rate.Cmp(big.NewRat(1, 1)) != 0 {
			add("unsupported_legacy", "", r.TransactionID, "non-unit legacy rate has ambiguous historic credit semantics")
			uncertain[r.Source], uncertain[r.Destination] = true, true
		}
		amounts[r.TransactionID] = a
	}
	// Queued intents can sit between a terminal economic row and its economic
	// parent. Resolve that persisted lineage without replaying the intent itself.
	effectiveParents := make(map[string]model.ChainRow)
	completedIntents := make(map[string]bool)
	terminalCompletions := make(map[string]map[string]bool)
	for _, r := range snapshot.Transactions {
		parent, exists := rows[r.ParentTransaction]
		if controls[r.TransactionID].ParentKind == "group" {
			exists = false
		}
		seen := map[string]bool{r.TransactionID: true}
		terminal := r.Status == "APPLIED" || r.Status == "INFLIGHT" || r.Status == "VOID" || r.Status == "REJECTED"
		for exists && (parent.Status == "QUEUED" || parent.Status == "SCHEDULED") {
			if seen[parent.TransactionID] {
				add("inconsistent_child", "", r.TransactionID, "cycle in intent parent lineage")
				break
			}
			seen[parent.TransactionID] = true
			if terminal {
				completedIntents[parent.TransactionID] = true
				if terminalCompletions[parent.TransactionID] == nil {
					terminalCompletions[parent.TransactionID] = make(map[string]bool)
				}
				terminalCompletions[parent.TransactionID][r.TransactionID] = true
			}
			if r.Status != "REJECTED" && (parent.Source != r.Source || parent.Destination != r.Destination || parent.Currency != r.Currency || !equalNumber(parent.Precision, r.Precision) || !equalNumber(parent.Amount, r.Amount) || !equalNumber(parent.PreciseAmount, r.PreciseAmount)) {
				add("inconsistent_child", "", r.TransactionID, "terminal row differs from its persisted intent")
			}
			if terminal && r.Status != "REJECTED" && controls[parent.TransactionID].EconomicVersion == 1 {
				expected := "APPLIED"
				if controls[parent.TransactionID].Inflight && parent.QueueAction != "COMMIT" {
					expected = "INFLIGHT"
				}
				if r.Status != expected {
					add("inconsistent_child", "", r.TransactionID, "terminal status differs from immutable execution intent")
				}
			}
			next, found := rows[parent.ParentTransaction]
			if controls[parent.TransactionID].ParentKind == "group" {
				found = false
			}
			if !found {
				break
			}
			parent = next
		}
		if exists {
			effectiveParents[r.TransactionID] = parent
			children[parent.TransactionID] = append(children[parent.TransactionID], r)
		}
	}
	for intent, completions := range terminalCompletions {
		if len(completions) > 1 {
			add("inconsistent_child", "", intent, "economic intent has multiple terminal completions")
		}
	}

	for _, r := range snapshot.Transactions {
		a := amounts[r.TransactionID]
		if a == nil {
			continue
		}
		if r.Status == "QUEUED" || r.Status == "SCHEDULED" || r.Status == "REJECTED" {
			continue
		}
		for _, id := range []string{r.Source, r.Destination} {
			if _, ok := stored[id]; !ok {
				add("missing_balance", id, r.TransactionID, "economic leg refers to a missing balance")
			}
		}
		if r.Source == r.Destination {
			add("inconsistent_legs", r.Source, r.TransactionID, "economic legs refer to the same balance")
		}
		src, sok := replay[r.Source]
		dst, dok := replay[r.Destination]
		if !sok || !dok {
			continue
		}
		parent, hasParent := effectiveParents[r.TransactionID]
		if r.ParentTransaction != "" && !hasParent && controls[r.TransactionID].ParentKind != "group" {
			add("unsupported_legacy", "", r.TransactionID, "parent record is missing; lifecycle cannot be established")
		}
		if hasParent && parent.TransactionID == r.TransactionID {
			add("inconsistent_child", "", r.TransactionID, "transaction is its own parent")
		}
		same := hasParent && r.Source == parent.Source && r.Destination == parent.Destination
		reverse := hasParent && r.Source == parent.Destination && r.Destination == parent.Source
		if hasParent && (parent.Currency != r.Currency || !equalNumber(parent.Precision, r.Precision)) {
			add("inconsistent_child", "", r.TransactionID, "child currency or precision differs from parent")
		}
		if hasParent && !same && !reverse {
			add("inconsistent_child", "", r.TransactionID, "child endpoints do not match parent or reversal")
		}
		if reverse && parent.Status != "APPLIED" {
			add("invalid_reversal", "", r.TransactionID, "reversal parent is not APPLIED")
		}
		switch r.Status {
		case "APPLIED":
			src.debit.Add(src.debit, a)
			dst.credit.Add(dst.credit, a)
			if hasParent && parent.Status == "INFLIGHT" {
				if !same {
					add("inconsistent_child", "", r.TransactionID, "inflight commitment must retain original endpoints")
				} else {
					src.inflightDebit.Sub(src.inflightDebit, a)
					dst.inflightCredit.Sub(dst.inflightCredit, a)
				}
			}
			if reverse && amounts[parent.TransactionID] != nil && a.Cmp(amounts[parent.TransactionID]) != 0 {
				add("invalid_reversal", "", r.TransactionID, "reversal amount differs from applied parent")
			}
			if same && parent.Status == "APPLIED" {
				add("inconsistent_child", "", r.TransactionID, "applied child repeats an already applied parent")
			}
		case "INFLIGHT":
			src.inflightDebit.Add(src.inflightDebit, a)
			dst.inflightCredit.Add(dst.inflightCredit, a)
		case "VOID":
			if !hasParent || parent.Status != "INFLIGHT" || !same {
				add("inconsistent_child", "", r.TransactionID, "VOID must release an INFLIGHT parent with matching endpoints")
			} else {
				src.inflightDebit.Sub(src.inflightDebit, a)
				dst.inflightCredit.Sub(dst.inflightCredit, a)
			}
		}
	}
	for _, p := range snapshot.Transactions {
		cs := children[p.TransactionID]
		reversals := 0
		released, pending := new(big.Int), new(big.Int)
		voids := 0
		for _, c := range cs {
			if (c.Status == "QUEUED" || c.Status == "SCHEDULED") && completedIntents[c.TransactionID] {
				continue
			}
			if p.Status == "INFLIGHT" && (c.Status == "QUEUED" || c.Status == "SCHEDULED") {
				if amounts[c.TransactionID] == nil {
					add("unsupported_legacy", "", c.TransactionID, "pending inflight child has no positive precise request amount")
				}
				if c.Source != p.Source || c.Destination != p.Destination || c.Currency != p.Currency || !equalNumber(c.Precision, p.Precision) {
					add("inconsistent_child", "", c.TransactionID, "pending inflight child must retain endpoints, currency and precision")
				}
			}
			a := amounts[c.TransactionID]
			if a == nil || c.Status == "REJECTED" {
				continue
			}
			if c.Source == p.Destination && c.Destination == p.Source && c.Status == "APPLIED" {
				reversals++
			}
			if p.Status == "INFLIGHT" && c.Source == p.Source && c.Destination == p.Destination {
				switch c.Status {
				case "APPLIED":
					released.Add(released, a)
				case "VOID":
					released.Add(released, a)
					voids++
				case "QUEUED", "SCHEDULED":
					pending.Add(pending, a)
				default:
					add("inconsistent_child", "", c.TransactionID, "unexpected child of an inflight parent")
				}
			}
		}
		if reversals > 1 {
			add("duplicate_reversal", "", p.TransactionID, "multiple applied reversals of one transaction")
		}
		if p.Status == "INFLIGHT" && amounts[p.TransactionID] != nil {
			total := new(big.Int).Add(released, pending)
			if total.Cmp(amounts[p.TransactionID]) > 0 {
				add("lifecycle_bound", "", p.TransactionID, "committed, pending and voided amounts exceed original reservation")
			}
			if voids > 1 {
				add("duplicate_void", "", p.TransactionID, "multiple VOID children")
			}
			if voids > 0 && total.Cmp(amounts[p.TransactionID]) != 0 {
				add("lifecycle_bound", "", p.TransactionID, "VOID does not release exactly the remaining reservation")
			}
		}
	}
	// Compare stored totals only within complete connected components whose
	// applied transfers all have compatible currency, scale and unit rate.
	parents := make(map[string]string)
	var root func(string) string
	root = func(id string) string {
		if parents[id] == "" {
			parents[id] = id
		}
		if parents[id] != id {
			parents[id] = root(parents[id])
		}
		return parents[id]
	}
	excluded := make(map[string]bool)
	scales := make(map[string]string)
	for _, r := range snapshot.Transactions {
		if r.Status != "APPLIED" {
			continue
		}
		src, sok := stored[r.Source]
		dst, dok := stored[r.Destination]
		if !sok || !dok {
			continue
		}
		parents[root(r.Source)] = root(r.Destination)
		rate, validRate := new(big.Rat).SetString(r.Rate)
		compatible := validRate && (rate.Sign() == 0 || rate.Cmp(big.NewRat(1, 1)) == 0) && src.Currency == dst.Currency && src.Currency == r.Currency
		for _, id := range []string{r.Source, r.Destination} {
			if !compatible || uncertain[id] {
				excluded[id] = true
			}
			scale, validScale := integer(r.Precision)
			if !validScale {
				excluded[id] = true
				continue
			}
			if previous := scales[id]; previous != "" && previous != scale.String() {
				excluded[id] = true
			}
			scales[id] = scale.String()
		}
	}
	totals := make(map[string]*big.Int)
	skip := make(map[string]bool)
	for _, b := range snapshot.Balances {
		if parents[b.ID] == "" {
			continue
		}
		group := root(b.ID)
		if excluded[b.ID] {
			skip[group] = true
		}
		credit, cok := integer(b.Credit)
		debit, dok := integer(b.Debit)
		if !cok || !dok {
			skip[group] = true
			continue
		}
		if totals[group] == nil {
			totals[group] = new(big.Int)
		}
		totals[group].Add(totals[group], new(big.Int).Sub(credit, debit))
	}
	for group, delta := range totals {
		if !skip[group] && delta.Sign() != 0 {
			add("conservation", group, "", fmt.Sprintf("compatible rate-one component has credit minus debit total %s", delta))
		}
	}

	for _, b := range snapshot.Balances {
		if uncertain[b.ID] {
			continue
		}
		c := replay[b.ID]
		compare(b.ID, "credit_balance", b.Credit, c.credit)
		compare(b.ID, "debit_balance", b.Debit, c.debit)
		compare(b.ID, "inflight_credit_balance", b.InflightCredit, c.inflightCredit)
		compare(b.ID, "inflight_debit_balance", b.InflightDebit, c.inflightDebit)
	}
	sort.SliceStable(report.Issues, func(i, j int) bool {
		a, b := report.Issues[i], report.Issues[j]
		if a.BalanceID != b.BalanceID {
			return a.BalanceID < b.BalanceID
		}
		if a.TransactionID != b.TransactionID {
			return a.TransactionID < b.TransactionID
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Field < b.Field
	})
	report.Verified = len(report.Issues) == 0
	return report
}

func equalNumber(a, b string) bool {
	left, ok := new(big.Rat).SetString(a)
	if !ok {
		return false
	}
	right, ok := new(big.Rat).SetString(b)
	return ok && left.Cmp(right) == 0
}
