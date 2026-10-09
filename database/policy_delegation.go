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
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/devaccuracy/ledgerforge/internal/policy"
	"github.com/devaccuracy/ledgerforge/model"
)

type policyQuerier interface {
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}

// Ancestry is immutable. Unknown IDs represent trusted in-process roots; HTTP
// and MCP identities always resolve an existing key through authentication.
func keyAncestry(ctx context.Context, q policyQuerier, key string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `WITH RECURSIVE ancestors AS (
  SELECT $1::text AS api_key_id,0 AS depth
  UNION ALL SELECT k.parent_api_key_id,a.depth+1 FROM ledgerforge.api_keys k
   JOIN ancestors a USING(api_key_id) WHERE k.parent_api_key_id IS NOT NULL AND a.depth<64
 ) SELECT api_key_id,depth FROM ancestors ORDER BY depth`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		var depth int
		if err = rows.Scan(&id, &depth); err != nil {
			return nil, err
		}
		if depth >= 64 {
			return nil, fmt.Errorf("%w: excessive delegation depth", policy.ErrDenied)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func lockPolicyAncestry(ctx context.Context, tx *sql.Tx, key string) ([]string, error) {
	ids, err := keyAncestry(ctx, tx, key)
	if err != nil {
		return nil, err
	}
	ordered := append([]string(nil), ids...)
	sort.Strings(ordered)
	// Lock absent policy rows too, so adding an ancestor policy cannot race a
	// descendant admission. Every operation takes the same global key order.
	for _, id := range ordered {
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('policy:'||$1,0))`, id); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func ancestorPolicies(ctx context.Context, q policyQuerier, ids []string) ([]model.KeyPolicy, error) {
	policies := []model.KeyPolicy{}
	for _, id := range ids {
		var raw []byte
		var version int64
		err := q.QueryRowContext(ctx, `SELECT document,version FROM ledgerforge.key_policies WHERE api_key_id=$1 AND enabled`, id).Scan(&raw, &version)
		if err == sql.ErrNoRows {
			continue
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
		policies = append(policies, p)
	}
	return policies, nil
}

// A key never decides its own request. No descendant of the initiator can be
// an independent approver. Within one delegation root, only the root itself
// with an explicit approvals:write scope has non-delegated authority. Scopes
// such as *:* do not confer approval authority. A reviewer in another root may
// delegate approvals:write within that independent authority. Master bypasses
// family restrictions, but is authenticated separately from all API keys.
// Keys predating ancestry persistence are roots: historical delegation cannot
// be recovered from caller metadata. Operators must rotate previously delegated
// keys to establish their ancestry before treating them as constrained agents.
func independentApprover(ctx context.Context, q policyQuerier, initiator string, p policy.Principal) error {
	if p.Master {
		return nil
	}
	if initiator == p.APIKeyID {
		return policy.ErrDenied
	}
	initiatorIDs, err := keyAncestry(ctx, q, initiator)
	if err != nil {
		return err
	}
	approverIDs, err := keyAncestry(ctx, q, p.APIKeyID)
	if err != nil {
		return err
	}
	for _, id := range approverIDs {
		if id == initiator {
			return policy.ErrDenied
		}
	}
	sameRoot := initiatorIDs[len(initiatorIDs)-1] == approverIDs[len(approverIDs)-1]
	if sameRoot && len(approverIDs) != 1 {
		return policy.ErrDenied
	}
	return nil
}

// Delegated policy managers may narrow their local policy, but cannot remove
// or relax the ancestral restrictions that bound their delegated authority.
func checkPolicyManagementTarget(ctx context.Context, q policyQuerier, target string) error {
	principal := policy.FromContext(ctx)
	if principal.Master || principal.APIKeyID == "" {
		return nil
	}
	ancestors, err := keyAncestry(ctx, q, principal.APIKeyID)
	if err != nil {
		return err
	}
	for _, id := range ancestors[1:] {
		if id == target {
			return fmt.Errorf("%w: delegated keys cannot manage ancestor policies", policy.ErrDenied)
		}
	}
	return nil
}
