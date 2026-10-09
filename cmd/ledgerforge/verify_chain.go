/*
Copyright 2024 Blnk Finance Authors.

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
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/devaccuracy/ledgerforge/model"
	"github.com/spf13/cobra"
)

// verifyChainCommands retains the original command as a compatible alias for
// audit verify. Both use the consistent snapshot verifier.
func verifyChainCommands(b *ledgerforgeInstance) *cobra.Command {
	cmd := auditVerifyCommands(b)
	cmd.Use = "verify-chain"
	return cmd
}

func auditCommands(b *ledgerforgeInstance) *cobra.Command {
	cmd := &cobra.Command{Use: "audit", Short: "Verify and export the transaction hash chain"}
	cmd.AddCommand(auditVerifyCommands(b), auditExportCommands(b))
	return cmd
}

func auditVerifyCommands(b *ledgerforgeInstance) *cobra.Command {
	var jsonOutput bool
	var anchorPath string
	cmd := &cobra.Command{
		Use: "verify", Short: "Verify chain links, financial commitments, checkpoints and coverage",
		Args: cobra.NoArgs, SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var result model.ChainVerifyResult
			var err error
			if anchorPath != "" {
				data, readErr := os.ReadFile(anchorPath)
				if readErr != nil {
					return fmt.Errorf("read external checkpoints: %w", readErr)
				}
				var anchors []model.ChainCheckpoint
				if err := json.Unmarshal(data, &anchors); err != nil {
					return fmt.Errorf("decode external checkpoints: %w", err)
				}
				if len(anchors) == 0 {
					return fmt.Errorf("external checkpoint file contains no roots")
				}
				reader, ok := b.ledgerforge.GetDataSource().(interface {
					VerifyChainSnapshotWithAnchors(context.Context, func(int64), []model.ChainCheckpoint) (model.ChainVerifyResult, error)
				})
				if !ok {
					return fmt.Errorf("datasource does not support external checkpoint verification")
				}
				result, err = reader.VerifyChainSnapshotWithAnchors(cmd.Context(), nil, anchors)
			} else {
				result, err = b.ledgerforge.VerifyChain(cmd.Context(), nil)
			}
			if err != nil {
				return fmt.Errorf("audit verification: %w", err)
			}
			if jsonOutput {
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result); err != nil {
					return err
				}
			} else {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Chain: %d sealed, head %s, %d pending, %d tampering findings\n", result.LastSeq, result.HeadHash, result.Pending, len(result.Tampering)); err != nil {
					return err
				}
				for _, issue := range result.Tampering {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "TAMPERING seq=%d transaction=%s: %s\n", issue.Sequence, issue.TransactionID, issue.Reason); err != nil {
						return err
					}
				}
			}
			if !result.Verified {
				return fmt.Errorf("chain BROKEN at seq %d (txn %s): %s", result.BrokenSeq, result.BrokenTxnID, result.Reason)
			}
			if result.Pending > 0 {
				return fmt.Errorf("chain intact; coverage pending for %d committed rows", result.Pending)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print the audit report as JSON")
	cmd.Flags().StringVar(&anchorPath, "anchor", "", "Compare roots with a previously exported checkpoint JSON file")
	return cmd
}

func auditExportCommands(b *ledgerforgeInstance) *cobra.Command {
	return &cobra.Command{
		Use: "export", Short: "Print checkpoint roots as JSON for external signing or anchoring",
		Args: cobra.NoArgs, SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			reader, ok := b.ledgerforge.GetDataSource().(interface {
				ExportChainCheckpoints(context.Context) ([]model.ChainCheckpoint, error)
			})
			if !ok {
				return fmt.Errorf("datasource does not support checkpoint export")
			}
			checkpoints, err := reader.ExportChainCheckpoints(cmd.Context())
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(checkpoints)
		},
	}
}
