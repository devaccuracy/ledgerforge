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
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionDoesNotLoadConfiguration(t *testing.T) {
	previous := version
	version = "1.2.3"
	t.Cleanup(func() { version = previous })

	for _, args := range [][]string{{"version"}, {"--version"}} {
		cli := NewCLI()
		var out bytes.Buffer
		cli.cmd.SetOut(&out)
		cli.cmd.SetErr(&out)
		// A missing config file would fail the root pre-run hook.
		cli.cmd.SetArgs(append([]string{"--config", "/nonexistent/ledgerforge.json"}, args...))

		require.NoError(t, cli.cmd.Execute(), "args %v", args)
		require.Equal(t, "ledgerforge 1.2.3 (commit unknown, built unknown)\n", out.String(), "args %v", args)
	}
}
