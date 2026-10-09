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

package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/devaccuracy/ledgerforge/internal/policy"
	"github.com/devaccuracy/ledgerforge/model"
)

func (d *Datasource) GetKeyPolicy(ctx context.Context, id string) (*model.KeyPolicy, error) {
	var raw []byte
	var version int64
	err := d.Conn.QueryRowContext(ctx, `SELECT document,version FROM ledgerforge.key_policies WHERE api_key_id=$1 AND enabled`, id).Scan(&raw, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p model.KeyPolicy
	if err = json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	p.APIKeyID = id
	p.Version = version
	return &p, nil
}

func (d *Datasource) PutKeyPolicy(ctx context.Context, p model.KeyPolicy) (*model.KeyPolicy, error) {
	if p.APIKeyID == "" {
		return nil, fmt.Errorf("api_key_id is required")
	}
	if err := policy.Validate(p); err != nil {
		return nil, err
	}
	tx, err := d.Conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO ledgerforge.key_policies(api_key_id,document) VALUES($1,$2)
 ON CONFLICT(api_key_id) DO UPDATE SET document=EXCLUDED.document,version=key_policies.version+1,enabled=TRUE RETURNING version`, p.APIKeyID, string(raw)).Scan(&p.Version)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM ledgerforge.policy_rules WHERE api_key_id=$1`, p.APIKeyID); err != nil {
		return nil, err
	}
	for _, r := range p.Rules {
		_, err = tx.ExecContext(ctx, `INSERT INTO ledgerforge.policy_rules(api_key_id,currency,precision,max_transaction,daily_limit,approval_above) VALUES($1,$2,$3,NULLIF($4,'')::numeric,NULLIF($5,'')::numeric,NULLIF($6,'')::numeric)`, p.APIKeyID, r.Currency, r.Precision, r.MaxTransaction, r.DailyLimit, r.ApprovalAbove)
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &p, nil
}

func (d *Datasource) DeleteKeyPolicy(ctx context.Context, id string) error {
	// Retain the version and durable admissions when disabling a policy.
	_, err := d.Conn.ExecContext(ctx, `UPDATE ledgerforge.key_policies SET enabled=FALSE,version=version+1 WHERE api_key_id=$1`, id)
	return err
}

func policyFingerprint(legs []model.PolicyLeg) (string, []byte, error) {
	canonical := append([]model.PolicyLeg(nil), legs...)
	for i := range canonical {
		canonical[i].TransactionID = ""
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(raw)
	stored, err := json.Marshal(legs)
	return hex.EncodeToString(sum[:]), stored, err
}

func scanAdmission(row interface{ Scan(...interface{}) error }) (*model.PolicyAdmission, error) {
	var a model.PolicyAdmission
	var raw []byte
	err := row.Scan(&a.ID, &a.APIKeyID, &a.Identity, &a.Version, &a.Day, &a.Decision, &a.DecidedBy, &raw)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(raw, &a.Legs)
	return &a, err
}

const admissionColumns = `admission_id,api_key_id,identity,version,day,decision,decided_by,legs`

func (d *Datasource) AdmitPolicy(ctx context.Context, key, identity string, legs []model.PolicyLeg) (*model.PolicyAdmission, error) {
	tx, err := d.Conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// Policy updates/deletion and admissions serialize on the same key row.
	var raw []byte
	var version int64
	var enabled bool
	err = tx.QueryRowContext(ctx, `SELECT document,version,enabled FROM ledgerforge.key_policies WHERE api_key_id=$1 FOR UPDATE`, key).Scan(&raw, &version, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	fingerprint, stored, err := policyFingerprint(legs)
	if err != nil {
		return nil, err
	}
	var oldFingerprint string
	err = tx.QueryRowContext(ctx, `SELECT fingerprint FROM ledgerforge.policy_admissions WHERE api_key_id=$1 AND identity=$2`, key, identity).Scan(&oldFingerprint)
	if err == nil {
		if fingerprint != oldFingerprint {
			return nil, policy.ErrIdentityConflict
		}
		a, e := scanAdmission(tx.QueryRowContext(ctx, `SELECT `+admissionColumns+` FROM ledgerforge.policy_admissions WHERE api_key_id=$1 AND identity=$2`, key, identity))
		if e != nil {
			return nil, e
		}
		if a.Decision == "rejected" {
			return nil, policy.ErrDenied
		}
		var released bool
		e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ledgerforge.policy_bindings WHERE admission_id=$1 AND released>0)`, a.ID).Scan(&released)
		if e != nil {
			return nil, e
		}
		if released {
			return nil, policy.ErrDenied
		}
		return a, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if !enabled {
		return nil, nil
	}
	var p model.KeyPolicy
	if err = json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	rules := map[string]model.SpendingRule{}
	for _, r := range p.Rules {
		rules[policy.RuleKey(r.Currency, r.Precision)] = r
	}
	totals := map[string]*big.Int{}
	for _, leg := range legs {
		n, e := policy.Integer(leg.Amount)
		if e != nil {
			return nil, e
		}
		k := policy.RuleKey(leg.Currency, leg.Precision)
		if _, ok := rules[k]; !ok {
			return nil, fmt.Errorf("%w: currency or precision is not allowed", policy.ErrDenied)
		}
		if !policy.Allowed(p.BalanceIDs, leg.Source) || !policy.Allowed(p.BalanceIDs, leg.Destination) || !policy.Allowed(p.LedgerIDs, leg.SourceLedger) || !policy.Allowed(p.LedgerIDs, leg.DestinationLedger) {
			return nil, fmt.Errorf("%w: balance or ledger is not allowed", policy.ErrDenied)
		}
		if totals[k] == nil {
			totals[k] = new(big.Int)
		}
		totals[k].Add(totals[k], n)
	}
	if len(legs) == 0 {
		return nil, fmt.Errorf("policy admission requires transaction legs")
	}
	keys := make([]string, 0, len(totals))
	for k := range totals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	decision := "not_required"
	for _, k := range keys {
		r, n := rules[k], totals[k]
		if r.MaxTransaction != "" {
			limit, _ := policy.Integer(r.MaxTransaction)
			if n.Cmp(limit) > 0 {
				return nil, fmt.Errorf("%w: aggregate exceeds max_transaction", policy.ErrDenied)
			}
		}
		if r.ApprovalAbove != "" {
			limit, _ := policy.Integer(r.ApprovalAbove)
			if n.Cmp(limit) > 0 {
				decision = "pending"
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ledgerforge.policy_usage(api_key_id,currency,precision,day) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, key, r.Currency, r.Precision, day)
		if err != nil {
			return nil, err
		}
		var used string
		err = tx.QueryRowContext(ctx, `SELECT used::text FROM ledgerforge.policy_usage WHERE api_key_id=$1 AND currency=$2 AND precision=$3 AND day=$4 FOR UPDATE`, key, r.Currency, r.Precision, day).Scan(&used)
		if err != nil {
			return nil, err
		}
		current, _ := policy.Integer(used)
		current.Add(current, n)
		if r.DailyLimit != "" {
			limit, _ := policy.Integer(r.DailyLimit)
			if current.Cmp(limit) > 0 {
				return nil, fmt.Errorf("%w: daily_limit exhausted", policy.ErrDenied)
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE ledgerforge.policy_usage SET used=$5 WHERE api_key_id=$1 AND currency=$2 AND precision=$3 AND day=$4`, key, r.Currency, r.Precision, day, current.String())
		if err != nil {
			return nil, err
		}
	}
	a := &model.PolicyAdmission{ID: model.GenerateUUIDWithSuffix("adm"), APIKeyID: key, Identity: identity, Version: version, Day: day, Decision: decision, Legs: legs}
	_, err = tx.ExecContext(ctx, `INSERT INTO ledgerforge.policy_admissions(admission_id,api_key_id,identity,version,day,fingerprint,decision,legs) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, a.ID, key, identity, version, day, fingerprint, decision, string(stored))
	if err != nil {
		return nil, err
	}
	for _, leg := range legs {
		_, err = tx.ExecContext(ctx, `INSERT INTO ledgerforge.policy_bindings(reference,transaction_id,admission_id,source,destination,currency,precision,amount) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, leg.Reference, leg.TransactionID, a.ID, leg.Source, leg.Destination, leg.Currency, leg.Precision, leg.Amount)
		if err != nil {
			return nil, fmt.Errorf("policy reference is already admitted: %w", err)
		}
	}
	return a, tx.Commit()
}

// Bindings use immutable database identities, never the generated-by metadata.
const bindingByID = `WITH RECURSIVE ancestors AS (
 SELECT $1::text AS transaction_id,0 AS depth
 UNION ALL SELECT t.parent_transaction,a.depth+1 FROM ledgerforge.transactions t JOIN ancestors a ON t.transaction_id=a.transaction_id WHERE t.parent_transaction<>'' AND t.parent_transaction<>t.transaction_id AND a.depth<16
) SELECT b.reference FROM ledgerforge.policy_bindings b JOIN ancestors a ON a.transaction_id=b.transaction_id ORDER BY a.depth LIMIT 1`

func (d *Datasource) policyReference(ctx context.Context, id string) (string, error) {
	var ref string
	err := d.Conn.QueryRowContext(ctx, bindingByID, id).Scan(&ref)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return ref, err
}

func (d *Datasource) VerifyPolicyLeg(ctx context.Context, t *model.Transaction) (*model.PolicyAdmission, error) {
	var id, source, destination, currency, amount, released string
	var precision float64
	err := d.Conn.QueryRowContext(ctx, `SELECT admission_id,source,destination,currency,precision,amount::text,released::text FROM ledgerforge.policy_bindings WHERE reference=$1 OR reference||'_q'=$1`, t.Reference).Scan(&id, &source, &destination, &currency, &precision, &amount, &released)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if t.PreciseAmount == nil || source != t.Source || destination != t.Destination || currency != t.Currency || precision != t.Precision || amount != t.PreciseAmount.String() || released != "0" {
		return nil, policy.ErrIdentityConflict
	}
	a, err := scanAdmission(d.Conn.QueryRowContext(ctx, `SELECT `+admissionColumns+` FROM ledgerforge.policy_admissions WHERE admission_id=$1`, id))
	if err != nil {
		return nil, err
	}
	if a.Decision == "rejected" {
		return nil, policy.ErrDenied
	}
	return a, nil
}

func (d *Datasource) CheckPolicyCommit(ctx context.Context, id string) error {
	ref, err := d.policyReference(ctx, id)
	if err != nil || ref == "" {
		return err
	}
	var decision string
	err = d.Conn.QueryRowContext(ctx, `SELECT a.decision FROM ledgerforge.policy_admissions a JOIN ledgerforge.policy_bindings b USING(admission_id) WHERE b.reference=$1`, ref).Scan(&decision)
	if err != nil {
		return err
	}
	if decision != "approved" && decision != "not_required" {
		return policy.ErrApprovalRequired
	}
	return nil
}

func (d *Datasource) ListPolicyApprovals(ctx context.Context, key string) ([]model.PolicyAdmission, error) {
	rows, err := d.Conn.QueryContext(ctx, `SELECT `+admissionColumns+` FROM ledgerforge.policy_admissions WHERE decision='pending' AND ($1='' OR api_key_id=$1) ORDER BY day,admission_id LIMIT 100`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PolicyAdmission{}
	for rows.Next() {
		a, e := scanAdmission(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (d *Datasource) DecidePolicyApproval(ctx context.Context, id string, p policy.Principal, approve bool) (*model.PolicyAdmission, error) {
	if !p.CanApprove() {
		return nil, policy.ErrDenied
	}
	tx, err := d.Conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	a, err := scanAdmission(tx.QueryRowContext(ctx, `SELECT `+admissionColumns+` FROM ledgerforge.policy_admissions WHERE admission_id=$1 FOR UPDATE`, id))
	if err != nil {
		return nil, err
	}
	if a.APIKeyID == p.APIKeyID {
		return nil, fmt.Errorf("%w: initiating key cannot approve or reject its own request", policy.ErrDenied)
	}
	decision := "rejected"
	if approve {
		decision = "approved"
	}
	if a.Decision == decision {
		return a, tx.Commit()
	}
	if a.Decision != "pending" {
		return nil, fmt.Errorf("approval decision is final")
	}
	actor := p.APIKeyID
	if p.Master {
		actor = "master"
	}
	_, err = tx.ExecContext(ctx, `UPDATE ledgerforge.policy_admissions SET decision=$2,decided_by=$3,decided_at=now() WHERE admission_id=$1`, id, decision, actor)
	if err != nil {
		return nil, err
	}
	a.Decision = decision
	a.DecidedBy = actor
	return a, tx.Commit()
}

func (d *Datasource) PolicyHoldIDs(ctx context.Context, id string) ([]string, error) {
	rows, err := d.Conn.QueryContext(ctx, `SELECT t.transaction_id FROM ledgerforge.transactions t JOIN ledgerforge.policy_bindings b ON t.reference=b.reference OR t.reference=b.reference||'_q' WHERE b.admission_id=$1 AND t.status='INFLIGHT' ORDER BY t.transaction_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err = rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (d *Datasource) ReleasePolicyLeg(ctx context.Context, id string) error {
	ref, err := d.policyReference(ctx, id)
	if err != nil || ref == "" {
		return err
	}
	tx, err := d.Conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var key, currency, released, admission string
	var precision float64
	var day time.Time
	err = tx.QueryRowContext(ctx, `SELECT b.admission_id,a.api_key_id,b.currency,b.precision,a.day,b.released::text FROM ledgerforge.policy_bindings b JOIN ledgerforge.policy_admissions a USING(admission_id) WHERE b.reference=$1 FOR UPDATE OF b`, ref).Scan(&admission, &key, &currency, &precision, &day, &released)
	if err != nil {
		return err
	}
	// The database trigger and fallback use the same lifecycle-aware amount.
	// Failed commit attempts retain the original hold's daily reservation.
	var releasable string
	err = tx.QueryRowContext(ctx, `SELECT ledgerforge.policy_releasable_capacity($1)::text`, ref).Scan(&releasable)
	if err != nil {
		return err
	}
	unused, err := policy.Integer(releasable)
	if err != nil {
		return fmt.Errorf("invalid persisted policy release: %w", err)
	}
	prior, _ := policy.Integer(released)
	delta := new(big.Int).Sub(unused, prior)
	if delta.Sign() <= 0 {
		return nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE ledgerforge.policy_usage SET used=used-$5::numeric WHERE api_key_id=$1 AND currency=$2 AND precision=$3 AND day=$4`, key, currency, precision, day, delta.String())
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE ledgerforge.policy_bindings SET released=$2 WHERE reference=$1`, ref, unused.String())
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE ledgerforge.policy_admissions a SET decision='rejected',decided_by='system:void-or-failure',decided_at=now() WHERE admission_id=$1 AND decision='pending' AND NOT EXISTS(SELECT 1 FROM ledgerforge.policy_bindings b WHERE b.admission_id=a.admission_id AND b.released<b.amount)`, admission)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (d *Datasource) PolicyCapacity(ctx context.Context, key string) ([]model.PolicyCapacity, error) {
	p, err := d.GetKeyPolicy(ctx, key)
	if err != nil || p == nil {
		return nil, err
	}
	day := time.Now().UTC().Truncate(24 * time.Hour)
	out := []model.PolicyCapacity{}
	for _, r := range p.Rules {
		used := "0"
		err = d.Conn.QueryRowContext(ctx, `SELECT used::text FROM ledgerforge.policy_usage WHERE api_key_id=$1 AND currency=$2 AND precision=$3 AND day=$4`, key, r.Currency, r.Precision, day).Scan(&used)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		c := model.PolicyCapacity{Currency: r.Currency, Precision: r.Precision, Day: day, Used: used}
		if r.DailyLimit != "" {
			limit, _ := policy.Integer(r.DailyLimit)
			n, _ := policy.Integer(used)
			limit.Sub(limit, n)
			if limit.Sign() < 0 {
				limit.SetInt64(0)
			}
			v := limit.String()
			c.Remaining = &v
		}
		out = append(out, c)
	}
	return out, nil
}
