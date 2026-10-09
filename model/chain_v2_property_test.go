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
	"reflect"
	"testing"
	"testing/quick"
	"time"
)

func TestChainV1Golden(t *testing.T) {
	row := ChainRow{TransactionID: "txn_1", Source: "bln_a", Destination: "bln_b", Amount: "100", PreciseAmount: "10000", Currency: "USD", Status: "APPLIED", Reference: "ref_1", CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	const expected = "52d9d82497b0850917fc358c57edef9c6aef97ce749e3f093404ebe6e855cd8e"
	hash, err := ComputeChainHashVersion(1, ChainGenesisHash, row)
	if err != nil || hash != expected {
		t.Fatalf("v1 changed: %s %v", hash, err)
	}
}

func TestChainV3CoversEveryField(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	row := ChainRow{ID: "123", TransactionID: "txn", Source: "a", Destination: "b", Amount: "1.00000000000000000001", PreciseAmount: "100000000000000000001", Precision: "100000000000000000000", Rate: "1", Currency: "USD", Status: "APPLIED", Reference: "ref", ParentTransaction: "parent", EffectiveDate: &at, CreatedAt: at}
	base, _ := ComputeChainHashVersion(3, ChainGenesisHash, row)
	for i := 0; i < reflect.TypeOf(row).NumField(); i++ {
		changed := row
		f := reflect.ValueOf(&changed).Elem().Field(i)
		switch f.Kind() {
		case reflect.String:
			f.SetString(f.String() + "|changed")
		case reflect.Pointer:
			later := at.Add(time.Second)
			f.Set(reflect.ValueOf(&later))
		case reflect.Struct:
			f.Set(reflect.ValueOf(at.Add(time.Second)))
		default:
			t.Fatalf("untested field %s", reflect.TypeOf(row).Field(i).Name)
		}
		hash, err := ComputeChainHashVersion(3, ChainGenesisHash, changed)
		if err != nil || hash == base {
			t.Fatalf("field %s not committed", reflect.TypeOf(row).Field(i).Name)
		}
	}
	local := row
	zone := time.FixedZone("test", 5*3600)
	local.CreatedAt = at.In(zone)
	effective := at.In(zone)
	local.EffectiveDate = &effective
	hash, _ := ComputeChainHashVersion(3, ChainGenesisHash, local)
	if hash != base {
		t.Fatal("timezone changed commitment")
	}
	if _, err := ComputeChainHashVersion(99, ChainGenesisHash, row); err == nil {
		t.Fatal("unknown version accepted")
	}
}

func TestChainV2DelimiterProperty(t *testing.T) {
	property := func(a, b, c string) bool {
		first := ChainRow{Source: a + "|" + b, Destination: c}
		second := ChainRow{Source: a, Destination: b + "|" + c}
		// The old encoding cannot distinguish these field boundaries.
		if ComputeChainHash(ChainGenesisHash, first) != ComputeChainHash(ChainGenesisHash, second) {
			return false
		}
		h1, _ := ComputeChainHashVersion(2, ChainGenesisHash, first)
		h2, _ := ComputeChainHashVersion(2, ChainGenesisHash, second)
		return h1 != h2
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func FuzzChainV2Encoding(f *testing.F) {
	f.Add("a|b", "c", "1000000000000000000000.0001")
	f.Add("", "\x00|é", "0")
	f.Fuzz(func(t *testing.T, source, destination, amount string) {
		row := ChainRow{Source: source, Destination: destination, Amount: amount}
		first, err := ComputeChainHashVersion(2, ChainGenesisHash, row)
		second, err2 := ComputeChainHashVersion(2, ChainGenesisHash, row)
		if err != nil || err2 != nil || first != second || len(first) != 64 {
			t.Fatal("unstable encoding")
		}
	})
}

func TestChainV2GoldenRemainsVerifiable(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	row := ChainRow{ID: "123", TransactionID: "txn", Source: "a", Destination: "b", Amount: "1.00000000000000000001", PreciseAmount: "100000000000000000001", Precision: "100000000000000000000", Rate: "1", Currency: "USD", Status: "APPLIED", Reference: "ref", ParentTransaction: "parent", EffectiveDate: &at, CreatedAt: at}
	hash, err := ComputeChainHashVersion(2, ChainGenesisHash, row)
	if err != nil || hash != "d2f09c7c672f103995ad972dda4e61ed901bc1b220c1ebcfe5e5f04c74f590fe" {
		t.Fatalf("v2 changed: %s %v", hash, err)
	}
	row.OverdraftLimit = "5"
	row.QueueAction = "COMMIT"
	row.ExecutionControls = `{"allow_overdraft":true}`
	row.ScheduledFor = &at
	withControls, err := ComputeChainHashVersion(2, ChainGenesisHash, row)
	if err != nil || hash != withControls {
		t.Fatal("new fields changed historic v2 commitment")
	}
}
