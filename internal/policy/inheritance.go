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

package policy

import (
	"fmt"

	"github.com/devaccuracy/ledgerforge/model"
)

func minimum(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	x, _ := Integer(a)
	y, _ := Integer(b)
	if x.Cmp(y) <= 0 {
		return a
	}
	return b
}

func intersectIDs(a, b []string) ([]string, bool) {
	if len(a) == 0 {
		return append([]string(nil), b...), false
	}
	if len(b) == 0 {
		return append([]string(nil), a...), false
	}
	out := []string{}
	for _, id := range a {
		if Allowed(b, id) {
			out = append(out, id)
		}
	}
	return out, len(out) == 0
}

// Intersect keeps only currencies and destinations permitted by every ancestor.
// Empty configured allowlists mean unrestricted; an empty intersection denies all.
func Intersect(key string, policies []model.KeyPolicy) *model.KeyPolicy {
	if len(policies) == 0 {
		return nil
	}
	result := policies[0]
	result.APIKeyID = key
	result.Rules = append([]model.SpendingRule(nil), result.Rules...)
	result.LedgerIDs = append([]string(nil), result.LedgerIDs...)
	result.BalanceIDs = append([]string(nil), result.BalanceIDs...)
	result.Version = 0
	result.InheritedFrom = nil
	for i, p := range policies {
		result.Version += p.Version
		if p.APIKeyID != key {
			result.InheritedFrom = append(result.InheritedFrom, p.APIKeyID)
		}
		result.DenyAll = result.DenyAll || p.DenyAll
		if i == 0 {
			continue
		}
		rules := []model.SpendingRule{}
		for _, a := range result.Rules {
			for _, b := range p.Rules {
				if RuleKey(a.Currency, a.Precision) != RuleKey(b.Currency, b.Precision) {
					continue
				}
				a.MaxTransaction = minimum(a.MaxTransaction, b.MaxTransaction)
				a.DailyLimit = minimum(a.DailyLimit, b.DailyLimit)
				a.ApprovalAbove = minimum(a.ApprovalAbove, b.ApprovalAbove)
				rules = append(rules, a)
			}
		}
		result.Rules = rules
		var empty bool
		result.LedgerIDs, empty = intersectIDs(result.LedgerIDs, p.LedgerIDs)
		result.DenyAll = result.DenyAll || empty
		result.BalanceIDs, empty = intersectIDs(result.BalanceIDs, p.BalanceIDs)
		result.DenyAll = result.DenyAll || empty
	}
	if len(result.Rules) == 0 {
		result.DenyAll = true
	}
	return &result
}

// ValidateNarrowing rejects a local child policy that claims any permission
// broader than its ancestors. An absent child policy inherits them unchanged.
func ValidateNarrowing(child model.KeyPolicy, parent *model.KeyPolicy) error {
	if parent == nil || child.DenyAll {
		return nil
	}
	if parent.DenyAll {
		return fmt.Errorf("%w: ancestor denies all spending", ErrDenied)
	}
	for _, ids := range [][2][]string{{child.LedgerIDs, parent.LedgerIDs}, {child.BalanceIDs, parent.BalanceIDs}} {
		if len(ids[1]) == 0 {
			continue
		}
		if len(ids[0]) == 0 {
			return fmt.Errorf("%w: child allowlist must narrow its ancestors", ErrDenied)
		}
		for _, id := range ids[0] {
			if !Allowed(ids[1], id) {
				return fmt.Errorf("%w: child allowlist exceeds its ancestors", ErrDenied)
			}
		}
	}
	for _, c := range child.Rules {
		var ancestor *model.SpendingRule
		for _, p := range parent.Rules {
			if RuleKey(c.Currency, c.Precision) == RuleKey(p.Currency, p.Precision) {
				copy := p
				ancestor = &copy
				break
			}
		}
		if ancestor == nil {
			return fmt.Errorf("%w: child currency exceeds its ancestors", ErrDenied)
		}
		for _, limits := range [][2]string{{c.MaxTransaction, ancestor.MaxTransaction}, {c.DailyLimit, ancestor.DailyLimit}, {c.ApprovalAbove, ancestor.ApprovalAbove}} {
			if limits[1] == "" {
				continue
			}
			if limits[0] == "" || minimum(limits[0], limits[1]) != limits[0] {
				return fmt.Errorf("%w: child limit exceeds its ancestors", ErrDenied)
			}
		}
	}
	return nil
}
