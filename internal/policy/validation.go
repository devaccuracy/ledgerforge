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
	"math"
	"math/big"
	"strings"

	"github.com/devaccuracy/ledgerforge/model"
)

func Integer(s string) (*big.Int, error) {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || n.Sign() < 0 || n.String() != s {
		return nil, fmt.Errorf("policy amounts must be canonical nonnegative integers")
	}
	return n, nil
}

func Validate(p model.KeyPolicy) error {
	if len(p.Rules) == 0 {
		return fmt.Errorf("policy requires at least one currency rule")
	}
	seen := map[string]bool{}
	for _, r := range p.Rules {
		if r.Currency == "" || r.Currency != strings.ToUpper(r.Currency) || math.IsNaN(r.Precision) || math.IsInf(r.Precision, 0) || r.Precision < 1 || math.Trunc(r.Precision) != r.Precision {
			return fmt.Errorf("policy requires currency and integer precision")
		}
		k := RuleKey(r.Currency, r.Precision)
		if seen[k] {
			return fmt.Errorf("duplicate policy currency rule")
		}
		seen[k] = true
		for _, s := range []string{r.MaxTransaction, r.DailyLimit, r.ApprovalAbove} {
			if s != "" {
				if _, err := Integer(s); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func RuleKey(currency string, precision float64) string {
	return fmt.Sprintf("%s/%.0f", currency, precision)
}

func Allowed(list []string, id string) bool {
	if len(list) == 0 {
		return true
	}
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}
