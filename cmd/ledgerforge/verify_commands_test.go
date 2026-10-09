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
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/devaccuracy/ledgerforge"
	"github.com/devaccuracy/ledgerforge/config"
	"github.com/devaccuracy/ledgerforge/database"
	"github.com/devaccuracy/ledgerforge/internal/integrity"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/require"
)

type verificationDatasource struct {
	database.IDataSource
	snapshot    integrity.Snapshot
	audit       model.ChainVerifyResult
	checkpoints []model.ChainCheckpoint
	anchors     []model.ChainCheckpoint
}

func (d *verificationDatasource) ReadIntegritySnapshot(context.Context) (integrity.Snapshot, error) {
	return d.snapshot, nil
}
func (d *verificationDatasource) VerifyChainSnapshot(context.Context, func(int64)) (model.ChainVerifyResult, error) {
	return d.audit, nil
}
func (d *verificationDatasource) VerifyChainSnapshotWithAnchors(_ context.Context, _ func(int64), anchors []model.ChainCheckpoint) (model.ChainVerifyResult, error) {
	d.anchors = anchors
	return d.audit, nil
}
func (d *verificationDatasource) ExportChainCheckpoints(context.Context) ([]model.ChainCheckpoint, error) {
	return d.checkpoints, nil
}
func verificationInstance(t *testing.T, ds *verificationDatasource) *ledgerforgeInstance {
	t.Helper()
	redis := miniredis.RunT(t)
	cfg := &config.Configuration{Redis: config.RedisConfig{Dns: redis.Addr()}}
	previous := config.ConfigStore.Load()
	config.ConfigStore.Store(cfg)
	t.Cleanup(func() {
		if previous != nil {
			config.ConfigStore.Store(previous)
		}
	})
	engine, err := ledgerforge.NewLedgerForge(ds)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, engine.Close()) })
	return &ledgerforgeInstance{ledgerforge: engine, cnf: cfg}
}
func TestVerifyCommandJSONAndHumanOutput(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		ds := &verificationDatasource{snapshot: integrity.Snapshot{Balances: []integrity.Balance{{ID: "balance", Currency: "USD", Balance: "1", Credit: "0", Debit: "0", InflightBalance: "0", InflightCredit: "0", InflightDebit: "0"}}}}
		b := verificationInstance(t, ds)
		cmd := verifyCommands(b)
		var out bytes.Buffer
		cmd.SetOut(&out)
		if jsonOutput {
			cmd.SetArgs([]string{"--json"})
		}
		err := cmd.Execute()
		require.Error(t, err)
		if jsonOutput {
			var report integrity.Report
			require.NoError(t, json.Unmarshal(out.Bytes(), &report))
			require.False(t, report.Verified)
			require.NotEmpty(t, report.Issues)
		} else {
			require.Contains(t, out.String(), "Checked 1 balances")
			require.Contains(t, out.String(), "mismatch")
		}
	}
	ds := &verificationDatasource{}
	cmd := verifyCommands(verificationInstance(t, ds))
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, cmd.Execute())
}
func TestAuditCommandSeparatesPendingAndTampering(t *testing.T) {
	for _, test := range []struct {
		name    string
		result  model.ChainVerifyResult
		message string
	}{
		{"pending", model.ChainVerifyResult{Verified: true, Pending: 2, Tampering: []model.ChainIssue{}}, "coverage pending"},
		{"tampering", model.ChainVerifyResult{Verified: false, BrokenSeq: 3, Reason: "hash mismatch", Tampering: []model.ChainIssue{{Sequence: 3, Reason: "hash mismatch"}}}, "BROKEN"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ds := &verificationDatasource{audit: test.result}
			cmd := auditVerifyCommands(verificationInstance(t, ds))
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs([]string{"--json"})
			err := cmd.Execute()
			require.ErrorContains(t, err, test.message)
			var result model.ChainVerifyResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Equal(t, test.result, result)
		})
	}
}
func TestAuditExportAndCompatibleAlias(t *testing.T) {
	ds := &verificationDatasource{audit: model.ChainVerifyResult{Verified: true, HeadHash: model.ChainGenesisHash}, checkpoints: []model.ChainCheckpoint{{Sequence: 1, Count: 1, HeadHash: model.ChainGenesisHash}}}
	b := verificationInstance(t, ds)
	cmd := auditExportCommands(b)
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	var roots []model.ChainCheckpoint
	require.NoError(t, json.Unmarshal(out.Bytes(), &roots))
	require.Equal(t, ds.checkpoints, roots)
	alias := verifyChainCommands(b)
	require.Equal(t, "verify-chain", alias.Name())
	alias.SetOut(&bytes.Buffer{})
	require.NoError(t, alias.Execute())
	for _, path := range [][]string{{"verify"}, {"verify-chain"}, {"audit", "verify"}, {"audit", "export"}} {
		cmd, args, err := NewCLI().cmd.Find(path)
		require.NoError(t, err)
		require.Empty(t, args)
		require.Equal(t, path[len(path)-1], cmd.Name())
	}
}

func TestAuditCommandExternalAnchorFile(t *testing.T) {
	ds := &verificationDatasource{audit: model.ChainVerifyResult{Verified: true}}
	b := verificationInstance(t, ds)
	path := filepath.Join(t.TempDir(), "checkpoints.json")
	roots := []model.ChainCheckpoint{{Sequence: 1, Count: 1, HeadHash: model.ChainGenesisHash}}
	data, err := json.Marshal(roots)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))
	cmd := auditVerifyCommands(b)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--anchor", path})
	require.NoError(t, cmd.Execute())
	require.Equal(t, roots, ds.anchors)
	for _, invalid := range []string{"[]", "{invalid"} {
		require.NoError(t, os.WriteFile(path, []byte(invalid), 0600))
		cmd := auditVerifyCommands(b)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetArgs([]string{"--anchor", path})
		require.Error(t, cmd.Execute())
	}
}
