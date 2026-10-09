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

package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ChainGenesisHash is the starting head of the transaction hash chain — 64
// zeros. ledgerforge.chain_state.head_hash begins here.
const ChainGenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// ChainRow carries the canonical, immutable fields of a transaction that the
// hash chain seals. The chainer and the verifier both build it from the same DB
// columns so a recomputed hash is reproducible. Numeric values are read as
// exact PostgreSQL text, never converted through floating point.
type ChainRow struct {
	ID                string
	Precision         string
	Rate              string
	ParentTransaction string
	EffectiveDate     *time.Time
	TransactionID     string
	Source            string
	Destination       string
	Amount            string // NUMERIC rendered as text
	PreciseAmount     string // NUMERIC rendered as text (the authoritative ledger amount)
	Currency          string
	Status            string
	Reference         string
	CreatedAt         time.Time
}

// ComputeChainHash is the legacy v1 encoding, retained for existing commitments.
// New seals use ComputeChainHashVersion with canonical version 2.
func ComputeChainHash(prevHash string, r ChainRow) string {
	canonical := strings.Join([]string{
		prevHash,
		r.TransactionID,
		r.Source,
		r.Destination,
		r.Amount,
		r.PreciseAmount,
		r.Currency,
		r.Status,
		r.Reference,
		r.CreatedAt.UTC().Format(time.RFC3339Nano),
	}, "|")
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// ChainState mirrors the ledgerforge.chain_state bookmark row.
type ChainState struct {
	ChainKey    string
	LastSeq     int64
	HeadHash    string
	GenesisHash string
	GenesisAt   time.Time
	UpdatedAt   time.Time
}

// ChainedTransaction is a transaction read back in chain order for
// verification: its canonical fields plus the stored chain linkage.
type ChainedTransaction struct {
	CanonicalVersion int
	Row              ChainRow
	ChainSeq         int64
	ChainPrevHash    string
	ChainHash        string
}

// CanonicalChainV2 is a domain-separated, length-delimited encoding. Each length
// is an unsigned big-endian uint64 byte count. Numeric text retains its exact
// stored representation; timestamps denote UTC instants. Mutable metadata is
// intentionally excluded from the financial commitment.
func CanonicalChainV2(prevHash string, r ChainRow) []byte {
	var b bytes.Buffer
	effective := ""
	if r.EffectiveDate != nil {
		effective = r.EffectiveDate.UTC().Format(time.RFC3339Nano)
	}
	for _, field := range []string{"ledgerforge.transaction-chain", "2", prevHash,
		r.ID, r.TransactionID, r.Source, r.Destination, r.Amount, r.PreciseAmount,
		r.Precision, r.Rate, r.Currency, r.Status, r.Reference, r.ParentTransaction,
		effective, r.CreatedAt.UTC().Format(time.RFC3339Nano)} {
		_ = binary.Write(&b, binary.BigEndian, uint64(len(field)))
		b.WriteString(field)
	}
	return b.Bytes()
}

// ComputeChainHashVersion preserves v1 commitments already in the database.
func ComputeChainHashVersion(version int, prevHash string, r ChainRow) (string, error) {
	switch version {
	case 1:
		return ComputeChainHash(prevHash, r), nil
	case 2:
		sum := sha256.Sum256(CanonicalChainV2(prevHash, r))
		return hex.EncodeToString(sum[:]), nil
	default:
		return "", fmt.Errorf("unsupported chain canonical version %s", strconv.Itoa(version))
	}
}

// ChainCheckpoint is an exportable root. External signatures or anchors can
// attest to it independently of the database that holds the chain.
type ChainCheckpoint struct {
	Sequence  int64     `json:"sequence"`
	Count     int64     `json:"count"`
	HeadHash  string    `json:"head_hash"`
	CreatedAt time.Time `json:"created_at"`
}

// ChainVerifyResult distinguishes broken commitments from asynchronous coverage.
type ChainVerifyResult struct {
	Verified    bool         `json:"verified"`
	LastSeq     int64        `json:"last_sequence"`
	HeadHash    string       `json:"head_hash"`
	BrokenSeq   int64        `json:"broken_sequence,omitempty"`
	BrokenTxnID string       `json:"broken_transaction_id,omitempty"`
	Reason      string       `json:"reason,omitempty"`
	Pending     int64        `json:"pending"`
	Tampering   []ChainIssue `json:"tampering"`
}

type ChainIssue struct {
	Sequence      int64  `json:"sequence,omitempty"`
	TransactionID string `json:"transaction_id,omitempty"`
	Reason        string `json:"reason"`
}
