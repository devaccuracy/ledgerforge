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
	"fmt"
	"math"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/devaccuracy/ledgerforge/model"
	"github.com/sirupsen/logrus"
	"github.com/texttheater/golang-levenshtein/levenshtein"
	"go.opentelemetry.io/otel"
)

// matchesRules checks whether an external transaction matches a group transaction based on specified matching rules.
// It iterates through the rules and criteria, evaluating whether the transactions meet the conditions for a match.
// Parameters:
// - externalTxn: The external transaction to match.
// - groupTxn: The internal or group transaction to compare against.
// - rules: A list of matching rules containing criteria to apply.
// Returns:
// - bool: True if the transactions match based on the rules, otherwise false.
func (s *Service) matchesRules(externalTxn *model.Transaction, groupTxn model.Transaction, rules []model.MatchingRule) bool {
	for _, rule := range rules {
		allCriteriaMet := true
		// Iterate through each rule's criteria to check if all conditions are satisfied.
		for _, criteria := range rule.Criteria {
			var criterionMet bool
			// Check each field specified in the criteria.
			switch criteria.Field {
			case "amount":
				// Compare amounts between the external and group transactions.
				criterionMet = s.matchesGroupAmount(externalTxn.Amount, groupTxn.Amount, criteria)
			case "date":
				// Compare the dates of the transactions.
				criterionMet = s.matchesGroupDate(externalTxn.CreatedAt, groupTxn.CreatedAt, criteria)
			case "description":
				// Compare the description fields for a match.
				criterionMet = s.matchesString(externalTxn.Description, groupTxn.Description, criteria)
			case "reference":
				// Compare the transaction references for a match.
				criterionMet = s.matchesString(externalTxn.Reference, groupTxn.Reference, criteria)
			case "currency":
				// Compare the currencies of the transactions.
				criterionMet = s.matchesCurrency(externalTxn.Currency, groupTxn.Currency, criteria)
			}
			// If any criterion is not met, mark the rule as not satisfied.
			if !criterionMet {
				allCriteriaMet = false
				break
			}
		}
		// If all criteria are satisfied, return true (the transactions match).
		if allCriteriaMet {
			return true
		}
	}
	return false
}

// oneToOneReconciliation performs a one-to-one reconciliation, where each external transaction is matched against
// a single internal transaction. The process is parallelized using goroutines.
// Parameters:
// - ctx: Context for managing cancellation and timeouts.
// - externalTxns: The list of external transactions to be reconciled.
// - matchingRules: The rules used to match external transactions to internal transactions.
// Returns:
// - []model.Match: A list of matched transactions.
// - []string: A list of unmatched transaction IDs.
func (s *Service) oneToOneReconciliation(ctx context.Context, externalTxns []*model.Transaction, matchingRules []model.MatchingRule) ([]model.Match, []string) {
	conf := s.config()
	maxWorkers := conf.MaxWorkers

	var matches []model.Match
	var unmatched []string

	matchChan := make(chan model.Match, len(externalTxns)) // Channel to collect matched transactions.
	unmatchedChan := make(chan string, len(externalTxns))  // Channel to collect unmatched transactions.
	var wg sync.WaitGroup                                  // WaitGroup to manage concurrent goroutines.
	sem := make(chan struct{}, maxWorkers)                 // Semaphore to limit concurrency

	// Iterate over each external transaction and attempt to match it against internal transactions.
	for _, externalTxn := range externalTxns {
		wg.Add(1)
		go func(extTxn *model.Transaction) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire token
			defer func() { <-sem }() // Release token

			err := s.findMatchingInternalTransaction(ctx, extTxn, matchingRules, matchChan, unmatchedChan)
			if err != nil {
				unmatchedChan <- extTxn.TransactionID
				logrus.Errorf("No match found for external transaction %s: %v", extTxn.TransactionID, err)
			}
		}(externalTxn)
	}

	// Close the channels after all goroutines are finished.
	go func() {
		wg.Wait()
		close(matchChan)
		close(unmatchedChan)
	}()

	// Collect matches and unmatched transactions from channels.
	for match := range matchChan {
		matches = append(matches, match)
	}
	for unmatchedID := range unmatchedChan {
		unmatched = append(unmatched, unmatchedID)
	}

	return matches, unmatched
}

// oneToManyReconciliation performs a one-to-many reconciliation, where each external transaction can match
// multiple internal transactions grouped by specific criteria.
// Parameters are the same as `oneToOneReconciliation`, with additional support for grouping criteria and a flag
// to determine if the external transactions are grouped.
// Returns:
// - []model.Match: A list of matched transactions.
// - []string: A list of unmatched transaction IDs.
func (s *Service) oneToManyReconciliation(ctx context.Context, externalTxns []*model.Transaction, groupCriteria string, matchingRules []model.MatchingRule, isExternalGrouped bool) ([]model.Match, []string) {
	conf := s.config()

	var matches []model.Match
	var unmatched []string

	matchChan := make(chan model.Match, len(externalTxns))
	unmatchedChan := make(chan string, len(externalTxns))
	var wg sync.WaitGroup

	// Initiate the one-to-many reconciliation process.
	err := s.oneToMany(ctx, externalTxns, matchingRules, isExternalGrouped, &wg, groupCriteria, conf.BatchSize, matchChan, unmatchedChan)
	if err != nil {
		logrus.Errorf("Error in one-to-many reconciliation: %v", err)
	}

	go func() {
		wg.Wait()
		close(matchChan)
		close(unmatchedChan)
	}()

	for match := range matchChan {
		matches = append(matches, match)
	}
	for unmatchedID := range unmatchedChan {
		unmatched = append(unmatched, unmatchedID)
	}

	return matches, unmatched
}

// manyToOneReconciliation performs a many-to-one reconciliation, where multiple internal transactions
// are grouped and matched against a single external transaction.
// Similar parameters as `oneToManyReconciliation`, but operates in reverse (many internal to one external).
// Returns:
// - []model.Match: A list of matched transactions.
// - []string: A list of unmatched transaction IDs.
func (s *Service) manyToOneReconciliation(ctx context.Context, internalTxns []*model.Transaction, uploadID string, groupCriteria string, matchingRules []model.MatchingRule, isExternalGrouped bool) ([]model.Match, []string) {
	conf := s.config()

	var matches []model.Match
	var unmatched []string

	matchChan := make(chan model.Match, len(internalTxns))
	unmatchedChan := make(chan string, len(internalTxns))
	var wg sync.WaitGroup

	// Initiate the many-to-one reconciliation process.
	err := s.manyToOne(ctx, internalTxns, uploadID, matchingRules, isExternalGrouped, &wg, groupCriteria, conf.BatchSize, matchChan, unmatchedChan)
	if err != nil {
		logrus.Errorf("Error in many-to-one reconciliation: %v", err)
	}

	// Close channels after processing.
	go func() {
		wg.Wait()
		close(matchChan)
		close(unmatchedChan)
	}()

	for match := range matchChan {
		matches = append(matches, match)
	}
	for unmatchedID := range unmatchedChan {
		unmatched = append(unmatched, unmatchedID)
	}

	return matches, unmatched
}

// manyToOne handles the core logic of many-to-one reconciliation.
// It groups external transactions and processes them in batches, comparing them to internal transactions.
// Parameters:
// - ctx: The context controlling the operation.
// - internalTxns: The internal transactions to match against.
// - matchingRules: The rules to apply during reconciliation.
// - isExternalGrouped: Whether the external transactions are grouped or not.
// - wg: The WaitGroup to manage goroutines.
// - groupingCriteria: The criteria used to group transactions.
// - batchSize: The size of each batch of transactions to process.
// - matchChan: Channel to collect matched transactions.
// - unMatchChan: Channel to collect unmatched transactions.
// Returns:
// - error: If any error occurs during processing.
func (s *Service) manyToOne(ctx context.Context, internalTxns []*model.Transaction, uploadID string, matchingRules []model.MatchingRule, isExternalGrouped bool, wg *sync.WaitGroup, groupingCriteria string, batchSize int, matchChan chan model.Match, unMatchChan chan string) error {
	offset := int64(0)
	ctx, span := otel.Tracer("ledgerforge.reconciliation").Start(ctx, "ProcessManyToOne")
	defer span.End()

	processedAny := false
	// Every input transaction must end up matched or unmatched; if the loop
	// exits before any group batch was processed, report them all unmatched
	// instead of silently dropping them from the reconciliation results.
	defer func() {
		if !processedAny {
			for _, txn := range internalTxns {
				unMatchChan <- txn.TransactionID
			}
		}
	}()

	// Loop to process transactions in batches.
	for {
		groupedExternalTxns, err := s.groupExternalTransactions(ctx, uploadID, groupingCriteria, batchSize, offset)
		if err != nil {
			logrus.Errorf("Error grouping external transactions: %v", err)
			break
		}
		if len(groupedExternalTxns) == 0 {
			span.AddEvent("No more grouped transactions to process")
			break
		}
		processedAny = true
		groupMap := s.buildGroupMap(groupedExternalTxns)
		var groupMapMu sync.Mutex
		err = s.processGroupedTransactions(internalTxns, groupedExternalTxns, groupMap, &groupMapMu, matchingRules, isExternalGrouped, wg, matchChan, unMatchChan)
		if err != nil {
			logrus.Errorf("Error processing grouped transactions: %v", err)
		}
		groupMapMu.Lock()
		groupMapEmpty := len(groupMap) == 0
		groupMapMu.Unlock()
		if groupMapEmpty {
			break
		}
		offset += int64(batchSize)
	}
	return nil
}

// groupExternalTransactions retrieves and groups external transactions based on the specified criteria.
// This is used in many-to-one reconciliation.
// Parameters:
// - ctx: The context controlling the operation.
// - groupingCriteria: The criteria to group transactions.
// - batchSize: The number of transactions to process in each batch.
// - offset: The offset for pagination.
// Returns:
// - map[string][]*model.Transaction: A map of grouped transactions.
// - error: If there is an error retrieving the transactions.
func (s *Service) groupExternalTransactions(ctx context.Context, uploadID string, groupingCriteria string, batchSize int, offset int64) (map[string][]*model.Transaction, error) {
	return s.datasource.FetchAndGroupExternalTransactions(ctx, uploadID, groupingCriteria, batchSize, offset)
}

// processGroupedTransactions handles the matching of transactions for both one-to-many and many-to-one reconciliations.
// It processes each transaction group in parallel, comparing them to the target transactions.
// Parameters:
// - singleTxns: The single transactions (either external or internal).
// - groupedTxns: The grouped transactions (either internal or external).
// - groupMap: A map indicating the groups that have not yet been matched.
// - matchingRules: The rules to apply for matching transactions.
// - isExternalGrouped: Whether the external transactions are grouped.
// - wg: The WaitGroup managing goroutines.
// - matchChan: Channel for collecting matches.
// - unMatchChan: Channel for collecting unmatched transaction IDs.
// Returns:
// - error: If any error occurs during processing.
func (s *Service) processGroupedTransactions(singleTxns []*model.Transaction, groupedTxns map[string][]*model.Transaction, groupMap map[string]bool, groupMapMu *sync.Mutex, matchingRules []model.MatchingRule, isExternalGrouped bool, wg *sync.WaitGroup, matchChan chan model.Match, unMatchChan chan string) error {
	conf := s.config()
	maxWorkers := conf.MaxWorkers
	sem := make(chan struct{}, maxWorkers) // Semaphore to limit concurrency

	for _, singleTxn := range singleTxns {
		wg.Add(1)
		go func(txn *model.Transaction) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire token
			defer func() { <-sem }() // Release token

			matched := s.matchSingleTransaction(txn, groupedTxns, groupMap, groupMapMu, matchingRules, isExternalGrouped, matchChan)
			if !matched {
				unMatchChan <- txn.TransactionID
			}
		}(singleTxn)
	}

	return nil
}

// matchSingleTransaction attempts to match a single transaction with a group of transactions based on matching rules.
// If a match is found, it sends the match to the match channel and marks the group as processed.
// Parameters:
// - singleTxn: The single transaction to match.
// - groupedTxns: The grouped transactions to compare against.
// - groupMap: A map tracking unprocessed groups.
// - matchingRules: The rules for matching transactions.
// - isExternalGrouped: Whether the external transactions are grouped.
// - matchChan: Channel for sending matches.
// Returns:
// - bool: True if a match is found, false otherwise.
func (s *Service) matchSingleTransaction(singleTxn *model.Transaction, groupedTxns map[string][]*model.Transaction, groupMap map[string]bool, groupMapMu *sync.Mutex, matchingRules []model.MatchingRule, isExternalGrouped bool, matchChan chan model.Match) bool {
	// Snapshot the candidate group keys under the lock so concurrent workers
	// don't iterate the map while another deletes from it.
	groupMapMu.Lock()
	keys := make([]string, 0, len(groupMap))
	for k := range groupMap {
		keys = append(keys, k)
	}
	groupMapMu.Unlock()

	for _, groupKey := range keys {
		if !s.matchesGroup(singleTxn, groupedTxns[groupKey], matchingRules) {
			continue
		}
		// Claim the group atomically: only the worker that deletes it owns the match.
		groupMapMu.Lock()
		if !groupMap[groupKey] {
			groupMapMu.Unlock()
			continue // already claimed by another worker
		}
		delete(groupMap, groupKey)
		groupMapMu.Unlock()

		for _, groupedTxn := range groupedTxns[groupKey] {
			var externalID, internalID string
			if isExternalGrouped {
				externalID = groupedTxn.TransactionID
				internalID = singleTxn.TransactionID
			} else {
				externalID = singleTxn.TransactionID
				internalID = groupedTxn.TransactionID
			}
			matchChan <- model.Match{
				ExternalTransactionID: externalID,
				InternalTransactionID: internalID,
				Amount:                groupedTxn.Amount,
				Date:                  groupedTxn.CreatedAt,
			}
		}
		return true
	}
	return false
}

// oneToMany performs the core logic for one-to-many reconciliation.
// Parameters are similar to `manyToOne`, but it processes transactions in reverse (one external to many internal).
// Returns:
// - error: If any error occurs during processing.
func (s *Service) oneToMany(ctx context.Context, singleTxn []*model.Transaction, matchingRules []model.MatchingRule, isExternalGrouped bool, wg *sync.WaitGroup, groupingCriteria string, batchSize int, matchChan chan model.Match, unMatchChan chan string) error {
	offset := int64(0)
	ctx, span := otel.Tracer("ledgerforge.reconciliation").Start(ctx, "ProcessOneToMany")
	defer span.End()

	processedAny := false
	// Mirror manyToOne: inputs must never silently vanish when no group
	// batch was processed.
	defer func() {
		if !processedAny {
			for _, txn := range singleTxn {
				unMatchChan <- txn.TransactionID
			}
		}
	}()

	for {
		txns, err := s.groupInternalTransactions(ctx, groupingCriteria, batchSize, offset)
		if err != nil {
			logrus.Errorf("Error grouping internal transactions: %v", err)
			break
		}
		if len(txns) == 0 {
			span.AddEvent("No more grouped transactions to process")
			break
		}
		processedAny = true
		groupMap := s.buildGroupMap(txns)
		var groupMapMu sync.Mutex
		err = s.processGroupedTransactions(singleTxn, txns, groupMap, &groupMapMu, matchingRules, isExternalGrouped, wg, matchChan, unMatchChan)
		if err != nil {
			logrus.Errorf("Error in one-to-many reconciliation: %v", err)
		}
		groupMapMu.Lock()
		groupMapEmpty := len(groupMap) == 0
		groupMapMu.Unlock()
		if groupMapEmpty {
			break
		}
		offset += int64(batchSize)
	}
	return nil
}

// buildGroupMap creates a map for tracking unprocessed transaction groups.
// Parameters:
// - groupedTxns: The grouped transactions.
// Returns:
// - map[string]bool: A map with group keys as true, indicating that they have not yet been processed.
func (s *Service) buildGroupMap(groupedTxns map[string][]*model.Transaction) map[string]bool {
	groupMap := make(map[string]bool)
	for key := range groupedTxns {
		groupMap[key] = true
	}
	return groupMap
}

// groupInternalTransactions groups internal transactions based on the specified criteria for reconciliation.
// Parameters are the same as `groupExternalTransactions`.
// Returns:
// - map[string][]*model.Transaction: A map of grouped internal transactions.
// - error: If any error occurs during grouping.
func (s *Service) groupInternalTransactions(ctx context.Context, groupingCriteria string, batchSize int, offset int64) (map[string][]*model.Transaction, error) {
	return s.datasource.GroupTransactions(ctx, groupingCriteria, batchSize, offset)
}

// findMatchingInternalTransaction attempts to find an internal transaction that matches the given external transaction.
// It processes the transactions in batches and applies the matching rules.
// Parameters:
// - ctx: The context controlling the operation.
// - externalTxn: The external transaction to match.
// - matchingRules: The rules for matching transactions.
// - matchChan: Channel to send matched transactions.
// - unMatchChan: Channel to send unmatched transaction IDs.
// Returns:
// - error: If any error occurs during processing.
func (s *Service) findMatchingInternalTransaction(ctx context.Context, externalTxn *model.Transaction, matchingRules []model.MatchingRule, matchChan chan model.Match, unMatchChan chan string) error {
	conf := s.config()

	var matchFound atomic.Bool
	minAmount, maxAmount, minDate, maxDate, currency := s.calculateMatchingBounds(externalTxn, matchingRules)
	err := s.runBatches(ctx, externalTxn.TransactionID, big.NewInt(int64(externalTxn.Amount)), conf.MaxWorkers,
		func(ctx context.Context, _ string, limit int, offset int64) ([]*model.Transaction, error) {
			return s.datasource.GetTransactionsByCriteria(ctx, minAmount, maxAmount, currency, minDate, maxDate, limit, offset)
		},
		func(ctx context.Context, internalTxn *model.Transaction) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !matchFound.Load() && s.matchesRules(externalTxn, *internalTxn, matchingRules) && matchFound.CompareAndSwap(false, true) {
				matchChan <- model.Match{
					ExternalTransactionID: externalTxn.TransactionID,
					InternalTransactionID: internalTxn.TransactionID,
					Amount:                externalTxn.Amount,
					Date:                  externalTxn.CreatedAt,
				}
			}
			return nil
		})
	if err != nil && err != context.Canceled {
		return err
	}

	if !matchFound.Load() {
		select {
		case unMatchChan <- externalTxn.TransactionID:
		default:
			return fmt.Errorf("failed to send unmatched transaction ID to channel")
		}
	}

	return nil
}

// matchesGroup compares a group of internal transactions with a single external transaction using matching rules.
// If a match is found, it returns true; otherwise, it returns false.
// Parameters:
// - externalTxn: The external transaction to compare.
// - group: The group of internal transactions to compare against.
// - matchingRules: The rules for matching transactions.
// Returns:
// - bool: True if the group matches the external transaction, false otherwise.
func (s *Service) matchesGroup(externalTxn *model.Transaction, group []*model.Transaction, matchingRules []model.MatchingRule) bool {
	var totalAmount float64
	var minDate, maxDate time.Time
	descriptions := make([]string, 0, len(group))
	references := make([]string, 0, len(group))
	currencies := make(map[string]bool)

	// Iterate over the group of internal transactions and accumulate information.
	for i, internalTxn := range group {
		totalAmount += internalTxn.Amount

		if i == 0 || internalTxn.CreatedAt.Before(minDate) {
			minDate = internalTxn.CreatedAt
		}
		if i == 0 || internalTxn.CreatedAt.After(maxDate) {
			maxDate = internalTxn.CreatedAt
		}

		descriptions = append(descriptions, internalTxn.Description)
		references = append(references, internalTxn.Reference)
		currencies[internalTxn.Currency] = true
	}

	// Create a virtual transaction representing the group for comparison.
	groupTxn := model.Transaction{
		Amount:      totalAmount,
		CreatedAt:   minDate, // Use the earliest date in the group.
		Description: strings.Join(descriptions, " | "),
		Reference:   strings.Join(references, " | "),
		Currency:    s.dominantCurrency(currencies), // Determine the dominant currency in the group.
	}

	// Use the matching rules to compare the group with the external transaction.
	return s.matchesRules(externalTxn, groupTxn, matchingRules)
}

// dominantCurrency returns the dominant currency in a group of transactions.
// If there is only one currency, it returns that currency, otherwise, it returns "MIXED".
func (s *Service) dominantCurrency(currencies map[string]bool) string {
	if len(currencies) == 1 {
		for currency := range currencies {
			return currency
		}
	}
	return "MIXED"
}

// matchesString compares the external and internal string values based on the matching criteria.
// Parameters:
// - externalValue: The value from the external transaction.
// - internalValue: The value from the internal transaction.
// - criteria: The matching criteria, including operator and allowable drift.
// Returns true if the values match according to the criteria, otherwise false.
func (s *Service) matchesString(externalValue, internalValue string, criteria model.MatchingCriteria) bool {
	switch criteria.Operator {
	case "equals":
		// Check if any part of the internal value matches the external value exactly.
		for _, part := range strings.Split(internalValue, " | ") {
			if strings.EqualFold(externalValue, part) {
				return true
			}
		}
		return false
	case "contains":
		// Check if the external value is contained in any part of the internal value.
		for _, part := range strings.Split(internalValue, " | ") {
			if s.partialMatch(externalValue, part, criteria.AllowableDrift) {
				return true
			}
		}
		return false
	}
	return false
}

// partialMatch compares two strings and checks if they match within a certain allowable drift, using Levenshtein distance.
// Parameters:
// - str1, str2: The strings to compare.
// - allowableDrift: The allowable difference between the two strings (as a percentage).
// Returns true if the strings match within the allowable drift, otherwise false.
func (s *Service) partialMatch(str1, str2 string, allowableDrift float64) bool {
	str1 = strings.ToLower(str1) // Convert to lowercase for case-insensitive comparison.
	str2 = strings.ToLower(str2)

	// Check if either string contains the other.
	if strings.Contains(str1, str2) || strings.Contains(str2, str1) {
		return true
	}

	// Calculate the Levenshtein distance between the strings.
	distance := levenshtein.DistanceForStrings([]rune(str1), []rune(str2), levenshtein.DefaultOptionsWithSub)

	// Calculate the maximum allowable distance based on the length of the longer string.
	maxLength := float64(max(len(str1), len(str2)))
	maxAllowedDistance := int(maxLength * (allowableDrift / 100))

	// Return true if the distance is within the allowable drift.
	return distance <= maxAllowedDistance
}

// matchesCurrency compares currency values, handling special cases like "MIXED" for group transactions.
// Parameters:
// - externalValue: The currency value from the external transaction.
// - internalValue: The currency value from the internal transaction.
// - criteria: The matching criteria.
// Returns true if the currencies match, otherwise false.
func (s *Service) matchesCurrency(externalValue, internalValue string, criteria model.MatchingCriteria) bool {
	if internalValue == "MIXED" {
		// TODO: Handle the special case where the internal value is "MIXED" (multiple currencies in a group).
		return true
	}
	return s.matchesString(externalValue, internalValue, criteria)
}

// matchesGroupAmount compares the amount of a single external transaction with the total amount of a grouped set of internal transactions.
// Parameters:
// - externalAmount: The amount from the external transaction.
// - groupAmount: The total amount from the group of internal transactions.
// - criteria: The matching criteria, including operator and allowable drift.
// Returns true if the amounts match according to the criteria, otherwise false.
func (s *Service) matchesGroupAmount(externalAmount, groupAmount float64, criteria model.MatchingCriteria) bool {
	switch criteria.Operator {
	case "equals":
		allowableDrift := math.Abs(groupAmount) * criteria.AllowableDrift
		return math.Abs(externalAmount-groupAmount) <= allowableDrift
	case "greater_than":
		return externalAmount > groupAmount
	case "less_than":
		return externalAmount < groupAmount
	}
	return false
}

// matchesGroupDate compares the date of a single external transaction with the earliest date in a group of internal transactions.
// Parameters:
// - externalDate: The date from the external transaction.
// - groupEarliestDate: The earliest date from the group of internal transactions.
// - criteria: The matching criteria, including operator and allowable drift.
// Returns true if the dates match according to the criteria, otherwise false.
func (s *Service) matchesGroupDate(externalDate, groupEarliestDate time.Time, criteria model.MatchingCriteria) bool {
	switch criteria.Operator {
	case "equals":
		difference := externalDate.Sub(groupEarliestDate)
		return math.Abs(difference.Seconds()) <= criteria.AllowableDrift
	case "after", "greater_than":
		return externalDate.After(groupEarliestDate)
	case "before", "less_than":
		return externalDate.Before(groupEarliestDate)
	}
	return false
}

// calculateMatchingBounds calculates the query bounds (amount, date) for matching external transactions against internal ones.
// It iterates through matching rules to find the widest possible range that satisfies all criteria.
func (s *Service) calculateMatchingBounds(externalTxn *model.Transaction, matchingRules []model.MatchingRule) (*float64, *float64, *time.Time, *time.Time, *string) {
	var minAmount, maxAmount *float64
	var minDate, maxDate *time.Time
	var currency *string

	c := externalTxn.Currency
	currency = &c

	var lowestMinAmount, highestMaxAmount float64
	var earliestMinDate, latestMaxDate time.Time

	firstAmount := true
	firstDate := true
	hasAmountCriteria := false
	hasDateCriteria := false

	for _, rule := range matchingRules {
		for _, criteria := range rule.Criteria {
			if criteria.Field == "amount" && criteria.Operator == "equals" {
				drift := criteria.AllowableDrift // fraction: 0.01 = 1%
				amt := externalTxn.Amount
				delta := math.Abs(amt) * drift
				low := amt - delta
				high := amt + delta

				if firstAmount {
					lowestMinAmount = low
					highestMaxAmount = high
					firstAmount = false
				} else {
					if low < lowestMinAmount {
						lowestMinAmount = low
					}
					if high > highestMaxAmount {
						highestMaxAmount = high
					}
				}
				hasAmountCriteria = true
			}
			if criteria.Field == "date" && criteria.Operator == "equals" {
				drift := time.Duration(criteria.AllowableDrift) * time.Second
				d := externalTxn.CreatedAt
				low := d.Add(-drift)
				high := d.Add(drift)

				if firstDate {
					earliestMinDate = low
					latestMaxDate = high
					firstDate = false
				} else {
					if low.Before(earliestMinDate) {
						earliestMinDate = low
					}
					if high.After(latestMaxDate) {
						latestMaxDate = high
					}
				}
				hasDateCriteria = true
			}
		}
	}

	if hasAmountCriteria {
		minAmount = &lowestMinAmount
		maxAmount = &highestMaxAmount
	}
	if hasDateCriteria {
		minDate = &earliestMinDate
		maxDate = &latestMaxDate
	}

	return minAmount, maxAmount, minDate, maxDate, currency
}
