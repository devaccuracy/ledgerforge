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
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/devaccuracy/ledgerforge/model"
)

var ErrDenied = errors.New("spending policy denied transaction")
var ErrApprovalRequired = errors.New("transaction requires an independent approval decision")
var ErrIdentityConflict = errors.New("policy idempotency identity already has different transaction details")

type Store interface {
	GetKeyPolicy(context.Context, string) (*model.KeyPolicy, error)
	PutKeyPolicy(context.Context, model.KeyPolicy) (*model.KeyPolicy, error)
	DeleteKeyPolicy(context.Context, string) error
	AdmitPolicy(context.Context, string, string, []model.PolicyLeg) (*model.PolicyAdmission, error)
	VerifyPolicyLeg(context.Context, *model.Transaction) (*model.PolicyAdmission, error)
	CheckPolicyCommit(context.Context, string) error
	ReleasePolicyLeg(context.Context, string) error
	ListPolicyApprovals(context.Context, string) ([]model.PolicyAdmission, error)
	DecidePolicyApproval(context.Context, string, Principal, bool) (*model.PolicyAdmission, error)
	PolicyCapacity(context.Context, string) ([]model.PolicyCapacity, error)
	PolicyHoldIDs(context.Context, string) ([]string, error)
}

type Engine struct{ Store Store }

func (e Engine) Admit(ctx context.Context, identity string, legs []model.PolicyLeg) (*model.PolicyAdmission, error) {
	p := FromContext(ctx)
	if p.Master || p.APIKeyID == "" || IsInternal(ctx) {
		return nil, nil
	}
	if identity == "" {
		return nil, fmt.Errorf("policy admission requires an idempotency reference")
	}
	return e.Store.AdmitPolicy(ctx, p.APIKeyID, identity, legs)
}

func Apply(a *model.PolicyAdmission, t *model.Transaction) {
	if a == nil {
		return
	}
	if t.MetaData == nil {
		t.MetaData = map[string]interface{}{}
	}
	t.MetaData["policy_admission_id"] = a.ID
	t.MetaData["policy_approval"] = a.Decision
	if a.Decision != "not_required" {
		t.Inflight = true
		t.MetaData["inflight"] = true
		// Settlement schedules never supply independent approval authority.
		t.InflightCommitDate = time.Time{}
	}
}
