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
	"context"
	"database/sql"
	"errors"
	"math"
	"math/big"

	"github.com/devaccuracy/ledgerforge/internal/apierror"
	"github.com/devaccuracy/ledgerforge/model"
)

// Zero means the full remaining hold. Partial major-unit commits must use the
// original transaction's authoritative scale from the injected datasource.
func (a Api) resolveInflightAmount(ctx context.Context, id string, amount float64, exact string, precise *big.Int) (*big.Int, error) {
	if precise != nil {
		if precise.Sign() <= 0 {
			return nil, errors.New("precise_amount must be positive")
		}
		return new(big.Int).Set(precise), nil
	}
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
		return nil, errors.New("amount must be finite and positive")
	}
	if amount == 0 {
		return big.NewInt(0), nil
	}
	transaction, err := a.ledgerforge.GetTransaction(ctx, id)
	if err != nil {
		var apiErr apierror.APIError
		if !errors.Is(err, sql.ErrNoRows) && !(errors.As(err, &apiErr) && apierror.Normalize(apiErr.Code) == apierror.ErrGenNotFound) {
			return nil, err
		}
		// Synchronous split and bulk parents may exist only as child rows.
		// Every child must agree on the scale; mixed-scale batches are ambiguous.
		lookupErr := err
		var precision float64
		const batchSize = 100
		for offset := int64(0); ; offset += batchSize {
			children, err := a.ledgerforge.GetDataSource().GetTransactionsByParent(ctx, id, batchSize, offset)
			if err != nil {
				return nil, err
			}
			for _, child := range children {
				if child == nil || child.Precision == 0 {
					return nil, errors.New("transaction precision is unavailable")
				}
				if err := model.ValidatePrecision(child.Precision); err != nil {
					return nil, err
				}
				if precision != 0 && precision != child.Precision {
					return nil, errors.New("parent transaction has mixed precisions; use a child ID or precise_amount")
				}
				precision = child.Precision
			}
			if len(children) < batchSize {
				break
			}
		}
		if precision == 0 {
			return nil, lookupErr
		}
		transaction = &model.Transaction{Precision: precision}
	}
	if transaction == nil || transaction.Precision == 0 {
		return nil, errors.New("transaction precision is unavailable")
	}
	return model.ParseAmount(exact, transaction.Precision)
}
