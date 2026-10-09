/*
Copyright 2024 Blnk Finance Authors.
Copyright 2025 LedgerForge Contributors.

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

package reconciliation

import (
	"context"
	"errors"
	"time"

	"github.com/devaccuracy/ledgerforge/model"
)

// contains checks whether a slice contains a specific string.
// Parameters:
// - slice: The slice to check.
// - item: The item to look for in the slice.
// Returns:
// - bool: True if the item is found in the slice, otherwise false.
func contains(slice []string, item string) bool {
	for _, a := range slice {
		if a == item {
			return true
		}
	}
	return false
}

// CreateMatchingRule creates a new matching rule after validating it.
// Parameters:
// - ctx: The context for managing the request.
// - rule: The matching rule to be created.
// Returns the created rule, or an error if validation or storage fails.
func (s *Service) CreateMatchingRule(ctx context.Context, rule model.MatchingRule) (*model.MatchingRule, error) {
	rule.RuleID = model.GenerateUUIDWithSuffix("rule") // Generate a unique rule ID.
	rule.CreatedAt = time.Now()
	rule.UpdatedAt = time.Now()

	// Validate the rule before storing it.
	err := s.validateRule(&rule)
	if err != nil {
		return nil, err
	}

	// Store the rule in the datasource.
	err = s.datasource.RecordMatchingRule(ctx, &rule)
	if err != nil {
		return nil, err
	}

	return &rule, nil
}

// GetMatchingRule retrieves a matching rule by its ID.
// Parameters:
// - ctx: The context for managing the request.
// - id: The ID of the matching rule to retrieve.
// Returns the matching rule, or an error if retrieval fails.
func (s *Service) GetMatchingRule(ctx context.Context, id string) (*model.MatchingRule, error) {
	rule, err := s.datasource.GetMatchingRule(ctx, id)
	if err != nil {
		return nil, err
	}
	return rule, nil
}

// UpdateMatchingRule updates an existing matching rule.
// It retrieves the existing rule, validates the new data, and then updates the rule.
// Parameters:
// - ctx: The context for managing the request.
// - rule: The updated rule data.
// Returns the updated rule, or an error if validation or update fails.
func (s *Service) UpdateMatchingRule(ctx context.Context, rule model.MatchingRule) (*model.MatchingRule, error) {
	// Retrieve the existing rule by its ID.
	existingRule, err := s.GetMatchingRule(ctx, rule.RuleID)
	if err != nil {
		return nil, err
	}

	// Preserve the original creation time and update the modified time.
	rule.CreatedAt = existingRule.CreatedAt
	rule.UpdatedAt = time.Now()

	// Validate the updated rule.
	err = s.validateRule(&rule)
	if err != nil {
		return nil, err
	}

	// Update the rule in the datasource.
	err = s.datasource.UpdateMatchingRule(ctx, &rule)
	if err != nil {
		return nil, err
	}

	return &rule, nil
}

// DeleteMatchingRule deletes a matching rule by its ID.
// Parameters:
// - ctx: The context for managing the request.
// - id: The ID of the rule to delete.
// Returns an error if deletion fails.
func (s *Service) DeleteMatchingRule(ctx context.Context, id string) error {
	return s.datasource.DeleteMatchingRule(ctx, id)
}

// ListMatchingRules retrieves all matching rules.
// Parameters:
// - ctx: The context for managing the request.
// Returns a list of matching rules, or an error if retrieval fails.
func (s *Service) ListMatchingRules(ctx context.Context) ([]*model.MatchingRule, error) {
	return s.datasource.GetMatchingRules(ctx)
}

// validateRule validates a matching rule, including checking its basic structure and criteria.
// Parameters:
// - rule: The rule to validate.
// Returns an error if the rule or any of its criteria are invalid.
func (s *Service) validateRule(rule *model.MatchingRule) error {
	// Validate basic structure of the rule.
	if err := s.validateRuleBasics(rule); err != nil {
		return err
	}

	// Validate each individual criterion.
	for _, criteria := range rule.Criteria {
		if err := s.validateCriteria(criteria); err != nil {
			return err
		}
	}

	return nil
}

// validateRuleBasics checks that the rule has a valid name and at least one criterion.
// Parameters:
// - rule: The rule to validate.
// Returns an error if the rule is missing required fields.
func (s *Service) validateRuleBasics(rule *model.MatchingRule) error {
	if rule.Name == "" {
		return errors.New("rule name is required")
	}

	if len(rule.Criteria) == 0 {
		return errors.New("at least one matching criteria is required")
	}

	return nil
}

// validateCriteria checks the validity of a criterion, including its field, operator, and drift values.
// Parameters:
// - criteria: The criteria to validate.
// Returns an error if the criteria are invalid.
func (s *Service) validateCriteria(criteria model.MatchingCriteria) error {
	if criteria.Field == "" || criteria.Operator == "" {
		return errors.New("field and operator are required for each criteria")
	}

	if err := s.validateOperator(criteria.Operator); err != nil {
		return err
	}

	if err := s.validateField(criteria.Field); err != nil {
		return err
	}

	return s.validateDrift(criteria)
}

// validateOperator checks if the provided operator is valid.
// Parameters:
// - operator: The operator to validate.
// Returns an error if the operator is invalid.
func (s *Service) validateOperator(operator string) error {
	validOperators := []string{"equals", "greater_than", "less_than", "contains"}
	if !contains(validOperators, operator) {
		return errors.New("invalid operator")
	}
	return nil
}

// validateField checks if the provided field is valid for a matching rule.
// Parameters:
// - field: The field to validate.
// Returns an error if the field is invalid.
func (s *Service) validateField(field string) error {
	validFields := []string{"amount", "date", "description", "reference", "currency"}
	if !contains(validFields, field) {
		return errors.New("invalid field")
	}
	return nil
}

// validateDrift checks the allowable drift for the given field and operator.
// Drift refers to acceptable deviations (e.g., percentage for amounts or seconds for dates).
// Parameters:
// - criteria: The criteria to validate.
// Returns an error if the drift is invalid.
func (s *Service) validateDrift(criteria model.MatchingCriteria) error {
	if criteria.Operator == "equals" {
		switch criteria.Field {
		case "amount":
			// Amount drift is a fraction of the amount: 0.01 allows 1%
			// deviation, 1 allows 100%.
			if criteria.AllowableDrift < 0 || criteria.AllowableDrift > 1 {
				return errors.New("drift for amount must be between 0 and 1 (fraction, e.g. 0.01 = 1%)")
			}
		case "date":
			if criteria.AllowableDrift < 0 {
				return errors.New("drift for date must be non-negative (seconds)")
			}
		}
	}
	return nil
}

// getMatchingRules retrieves a list of matching rules by their IDs.
// Parameters:
// - ctx: The context for managing the request.
// - matchingRuleIDs: The list of matching rule IDs to retrieve.
// Returns a list of matching rules, or an error if retrieval fails.
func (s *Service) getMatchingRules(ctx context.Context, matchingRuleIDs []string) ([]model.MatchingRule, error) {
	var rules []model.MatchingRule
	for _, id := range matchingRuleIDs {
		rule, err := s.GetMatchingRule(ctx, id)
		if err != nil {
			return nil, err
		}
		rules = append(rules, *rule)
	}
	return rules, nil
}
