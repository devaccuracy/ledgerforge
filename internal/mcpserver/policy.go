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

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/devaccuracy/ledgerforge/internal/policy"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type identityService interface {
	GetAPIKeyByKey(context.Context, string) (*model.APIKey, error)
}
type policyService interface {
	GetKeyPolicy(context.Context, string) (*model.KeyPolicy, error)
	PolicyCapacity(context.Context, string) ([]model.PolicyCapacity, error)
	ListPolicyApprovals(context.Context) ([]model.PolicyAdmission, error)
}

func (s *Server) identityContext(ctx context.Context, resource, action string) (context.Context, error) {
	if s.apiKey == "" {
		return ctx, nil
	}
	resolver, ok := s.service.(identityService)
	if !ok {
		return ctx, fmt.Errorf("API-key identity is unavailable")
	}
	key, err := resolver.GetAPIKeyByKey(ctx, s.apiKey)
	if err != nil || key == nil || !key.IsValid() {
		return ctx, fmt.Errorf("MCP API-key identity is invalid, expired, or revoked")
	}
	allowed := resource == "policies" || resource == "approvals"
	for _, scope := range key.Scopes {
		parts := strings.Split(scope, ":")
		if len(parts) == 2 && (parts[0] == resource || parts[0] == "*") && (parts[1] == action || parts[1] == "*") {
			allowed = true
		}
	}
	if !allowed {
		return ctx, fmt.Errorf("MCP identity lacks %s:%s", resource, action)
	}
	return policy.WithPrincipal(ctx, policy.Principal{APIKeyID: key.APIKeyID, Scopes: key.Scopes}), nil
}

func identityWrap[Input any](s *Server, resource, action string, handler func(context.Context, *mcp.CallToolRequest, Input) (map[string]any, error)) mcp.ToolHandlerFor[Input, map[string]any] {
	return wrap(func(ctx context.Context, r *mcp.CallToolRequest, input Input) (map[string]any, error) {
		ctx, err := s.identityContext(ctx, resource, action)
		if err != nil {
			return nil, err
		}
		return handler(ctx, r, input)
	})
}

func (s *Server) effectivePolicy(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (map[string]any, error) {
	ctx, err := s.identityContext(ctx, "policies", "read")
	if err != nil {
		return nil, err
	}
	p := policy.FromContext(ctx)
	if p.APIKeyID == "" {
		return nil, fmt.Errorf("configure --api-key to inspect effective spending policy")
	}
	service, ok := s.service.(policyService)
	if !ok {
		return nil, fmt.Errorf("policy service unavailable")
	}
	result, err := service.GetKeyPolicy(ctx, p.APIKeyID)
	if err != nil {
		return nil, err
	}
	capacity, err := service.PolicyCapacity(ctx, p.APIKeyID)
	return map[string]any{"policy": result, "capacity": capacity}, err
}

func (s *Server) pendingApprovals(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (map[string]any, error) {
	ctx, err := s.identityContext(ctx, "approvals", "read")
	if err != nil {
		return nil, err
	}
	service, ok := s.service.(policyService)
	if !ok {
		return nil, fmt.Errorf("policy service unavailable")
	}
	result, err := service.ListPolicyApprovals(ctx)
	return map[string]any{"approvals": result}, err
}

func (s *Server) registerPolicyTools() {
	if s.apiKey == "" {
		return
	}
	read := &mcp.ToolAnnotations{ReadOnlyHint: true}
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_effective_policy", Description: "The configured API key's server-enforced spending rules and remaining UTC daily capacity.", Annotations: read}, wrap(s.effectivePolicy))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_remaining_capacity", Description: "Remaining exact minor-unit capacity for the current UTC admission day.", Annotations: read}, wrap(s.remainingCapacity))
	mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_pending_approvals", Description: "Pending policy approvals. An initiating key cannot approve its own requests.", Annotations: read}, wrap(s.pendingApprovals))
	if s.allowWrites {
		mcp.AddTool(s.protocol, &mcp.Tool{Name: "ledgerforge_submit_policy_transaction", Description: "Submit an exact minor-unit transfer as the configured API key. Above the policy threshold the server holds funds for independent approval.", Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPointer(false)}}, identityWrap(s, "transactions", "write", s.transferFunds))
	}
	for _, entry := range []struct{ uri, name string }{
		{"ledgerforge://policy/effective", "effective-policy"},
		{"ledgerforge://policy/pending-approvals", "pending-approvals"},
		{"ledgerforge://policy/capacity", "remaining-capacity"},
	} {
		s.protocol.AddResource(&mcp.Resource{URI: entry.uri, Name: entry.name, MIMEType: "application/json"}, s.readPolicyResource)
	}
}

func (s *Server) readPolicyResource(ctx context.Context, r *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	var value map[string]any
	var err error
	switch r.Params.URI {
	case "ledgerforge://policy/capacity":
		value, err = s.remainingCapacity(ctx, nil, struct{}{})
	case "ledgerforge://policy/effective":
		value, err = s.effectivePolicy(ctx, nil, struct{}{})
	default:
		value, err = s.pendingApprovals(ctx, nil, struct{}{})
	}
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: r.Params.URI, MIMEType: "application/json", Text: string(raw)}}}, nil
}

func (s *Server) remainingCapacity(ctx context.Context, r *mcp.CallToolRequest, input struct{}) (map[string]any, error) {
	value, err := s.effectivePolicy(ctx, r, input)
	if err != nil {
		return nil, err
	}
	return map[string]any{"capacity": value["capacity"]}, nil
}
