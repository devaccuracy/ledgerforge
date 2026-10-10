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
	"fmt"

	"github.com/spf13/cobra"
)

// Build metadata, set at release time with
// -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func versionString() string {
	return fmt.Sprintf("ledgerforge %s (commit %s, built %s)", version, commit, date)
}

// versionCommand prints build metadata without loading configuration or
// connecting to any service.
func versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "version",
		Short:             "Print the LedgerForge version",
		Args:              cobra.NoArgs,
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), versionString())
			return err
		},
	}
}
