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

package api

import (
	"errors"
	"net/http"

	"github.com/devaccuracy/ledgerforge/internal/policy"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/gin-gonic/gin"
)

func policyError(c *gin.Context, err error) {
	if errors.Is(err, policy.ErrDenied) || errors.Is(err, policy.ErrApprovalRequired) || errors.Is(err, policy.ErrIdentityConflict) {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": "policy operation could not be completed"})
}

func requirePolicyManager(c *gin.Context) bool {
	if policy.FromContext(c.Request.Context()).CanManagePolicies() {
		return true
	}
	policyError(c, policy.ErrDenied)
	return false
}

func (a Api) PutKeyPolicy(c *gin.Context) {
	if !requirePolicyManager(c) {
		return
	}
	var p model.KeyPolicy
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid policy JSON"})
		return
	}
	p.APIKeyID = c.Param("id")
	if err := policy.Validate(p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := a.ledgerforge.PutKeyPolicy(c.Request.Context(), p)
	if err != nil {
		policyError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (a Api) GetKeyPolicy(c *gin.Context) {
	if !requirePolicyManager(c) {
		return
	}
	result, err := a.ledgerforge.GetKeyPolicy(c.Request.Context(), c.Param("id"))
	if err != nil {
		policyError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"policy": result})
}

func (a Api) DeleteKeyPolicy(c *gin.Context) {
	if !requirePolicyManager(c) {
		return
	}
	if err := a.ledgerforge.DeleteKeyPolicy(c.Request.Context(), c.Param("id")); err != nil {
		policyError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (a Api) EffectivePolicy(c *gin.Context) {
	key := policy.FromContext(c.Request.Context()).APIKeyID
	if key == "" {
		policyError(c, policy.ErrDenied)
		return
	}
	result, err := a.ledgerforge.GetKeyPolicy(c.Request.Context(), key)
	if err != nil {
		policyError(c, err)
		return
	}
	capacity, err := a.ledgerforge.PolicyCapacity(c.Request.Context(), key)
	if err != nil {
		policyError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"policy": result, "capacity": capacity})
}

func (a Api) ListPolicyApprovals(c *gin.Context) {
	result, err := a.ledgerforge.ListPolicyApprovals(c.Request.Context())
	if err != nil {
		policyError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"approvals": result})
}

func (a Api) ApprovePolicyTransaction(c *gin.Context) { a.decidePolicy(c, true) }
func (a Api) RejectPolicyTransaction(c *gin.Context)  { a.decidePolicy(c, false) }
func (a Api) decidePolicy(c *gin.Context, approve bool) {
	result, err := a.ledgerforge.DecidePolicyApproval(c.Request.Context(), c.Param("id"), approve)
	if err != nil {
		policyError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
