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

// Package policy implements server-enforced API-key spending admissions.
package policy

import "context"

type Principal struct {
	APIKeyID string
	Master   bool
	Scopes   []string
}
type principalKey struct{}
type internalKey struct{}

// WithPrincipal is called only by trusted authentication adapters.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	p.Scopes = append([]string(nil), p.Scopes...)
	return context.WithValue(ctx, principalKey{}, p)
}

func FromContext(ctx context.Context) Principal {
	p, _ := ctx.Value(principalKey{}).(Principal)
	return p
}

func (p Principal) CanManagePolicies() bool { return p.Master || p.has("policies:write") }
func (p Principal) CanApprove() bool        { return p.Master || p.has("approvals:write") }
func (p Principal) has(scope string) bool {
	for _, s := range p.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// Internal marks an already-admitted fan-out, never a caller assertion.
func Internal(ctx context.Context) context.Context {
	return context.WithValue(ctx, internalKey{}, true)
}

func IsInternal(ctx context.Context) bool { v, _ := ctx.Value(internalKey{}).(bool); return v }
