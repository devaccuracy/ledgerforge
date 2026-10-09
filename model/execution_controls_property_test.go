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

package model

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDecodeExecutionControlsLegacyTypes(t *testing.T) {
	for _, value := range []string{`"merchant_label"`, `"true"`, `7`, `[]`, `{}`, `null`, `false`, `true`} {
		t.Run(value, func(t *testing.T) {
			data := []byte(`{"economic_version":0,"allow_overdraft":` + value + `,"inflight":` + value + `,"atomic":` + value + `,"skip_queue":` + value + `,"queued_parent":` + value + `,"queue_lane":` + value + `,"shadow_for":` + value + `}`)
			original := bytes.Clone(data)
			controls, err := DecodeExecutionControls(data)
			if err != nil {
				t.Fatal(err)
			}
			wantFlag := value == "true"
			if controls.AllowOverdraft != wantFlag || controls.Inflight != wantFlag || controls.Atomic != wantFlag || controls.SkipQueue {
				t.Fatalf("legacy boolean type check lost: %+v", controls)
			}
			var raw interface{}
			if err := json.Unmarshal([]byte(value), &raw); err != nil {
				t.Fatal(err)
			}
			wantString, _ := raw.(string)
			if controls.QueuedParent != wantString || controls.ShadowFor != wantString || controls.QueueLane != "normal" {
				t.Fatalf("legacy string defaults lost: %+v", controls)
			}
			if !bytes.Equal(original, data) {
				t.Fatal("decoding changed canonical evidence")
			}
		})
	}
	controls, err := DecodeExecutionControls([]byte(`{"economic_version":0,"queue_lane":"hot"}`))
	if err != nil || controls.QueueLane != "hot" {
		t.Fatalf("legacy hot lane lost: %+v %v", controls, err)
	}
}

func TestDecodeExecutionControlsCurrentTypesRemainStrict(t *testing.T) {
	for _, field := range []string{`"allow_overdraft":"merchant_label"`, `"inflight":7`, `"atomic":[]`, `"skip_queue":{}`, `"queued_parent":9`, `"queue_lane":true`, `"shadow_for":false`} {
		t.Run(field, func(t *testing.T) {
			data := []byte(`{"economic_version":1,` + field + `}`)
			if _, err := DecodeExecutionControls(data); err == nil {
				t.Fatal("invalid current-version control accepted")
			}
			var txn Transaction
			if err := DecodeTransactionMetadata(append(append([]byte(`{"customer":"note","__execution_controls":`), data...), '}'), &txn); err == nil {
				t.Fatal("transaction hydration bypassed strict controls")
			}
		})
	}
}
