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

package reconciliation

import (
	"context"
	"github.com/devaccuracy/ledgerforge/config"
	"github.com/devaccuracy/ledgerforge/database/mocks"
	"github.com/devaccuracy/ledgerforge/model"
	"math/big"
)

func testService(ds *mocks.MockDataSource) *Service {
	configuration := func() Config {
		cfg, _ := config.Fetch()
		if cfg == nil {
			return Config{}
		}
		return Config{MaxWorkers: cfg.Transaction.MaxWorkers, BatchSize: cfg.Transaction.BatchSize,
			ProgressInterval: cfg.Reconciliation.ProgressInterval}
	}
	service := NewService(ds, configuration, nil, ds.UpdateTransactionMetadata, func(model.Reconciliation) {})
	service.runBatches = func(ctx context.Context, id string, _ *big.Int, _ int, fetch FetchTransactions, process ProcessTransaction) error {
		for offset := int64(0); ; {
			transactions, err := fetch(ctx, id, service.config().BatchSize, offset)
			if err != nil {
				return err
			}
			if len(transactions) == 0 {
				return nil
			}
			for _, txn := range transactions {
				if err := process(ctx, txn); err != nil {
					return err
				}
			}
			offset += int64(len(transactions))
		}
	}
	return service
}
