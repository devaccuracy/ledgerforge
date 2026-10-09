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

package main

import (
	"encoding/json"
	"fmt"

	"github.com/devaccuracy/ledgerforge/internal/integrity"
	"github.com/spf13/cobra"
)

func verifyCommands(b *ledgerforgeInstance) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use: "verify", Short: "Check persisted ledger balances and lifecycle evidence without repairing data",
		Args: cobra.NoArgs, SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			reader, ok := b.ledgerforge.GetDataSource().(integrity.Reader)
			if !ok {
				return fmt.Errorf("datasource does not support integrity snapshots")
			}
			report, err := integrity.Verify(cmd.Context(), reader)
			if err != nil {
				return fmt.Errorf("ledger verification: %w", err)
			}
			if jsonOutput {
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(report); err != nil {
					return err
				}
			} else {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Checked %d balances and %d transactions: %d issues\n", report.Balances, report.Transactions, len(report.Issues)); err != nil {
					return err
				}
				for _, issue := range report.Issues {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s balance=%s transaction=%s field=%s: %s (expected=%s actual=%s)\n", issue.Kind, issue.BalanceID, issue.TransactionID, issue.Field, issue.Message, issue.Expected, issue.Actual); err != nil {
						return err
					}
				}
			}
			if !report.Verified {
				return fmt.Errorf("ledger verification found %d issues", len(report.Issues))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print the integrity report as JSON")
	return cmd
}
