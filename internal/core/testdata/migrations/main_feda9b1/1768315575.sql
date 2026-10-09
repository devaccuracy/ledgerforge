-- Copyright 2024 Blnk Finance Authors.
--
-- Licensed under the Apache License, Version 2.0 (the "License");
-- you may not use this file except in compliance with the License.
-- You may obtain a copy of the License at
--
--     http://www.apache.org/licenses/LICENSE-2.0
--
-- Unless required by applicable law or agreed to in writing, software
-- distributed under the License is distributed on an "AS IS" BASIS,
-- WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
-- See the License for the specific language governing permissions and
-- limitations under the License.

-- +migrate Up

-- Transactions table indexes
CREATE INDEX IF NOT EXISTS ledgerforge_transactions_currency_created_at_idx ON ledgerforge.transactions (currency, created_at DESC);
CREATE INDEX IF NOT EXISTS ledgerforge_transactions_created_at_source_idx ON ledgerforge.transactions (created_at, source);
CREATE INDEX IF NOT EXISTS ledgerforge_transactions_created_at_destination_idx ON ledgerforge.transactions (created_at, destination);
CREATE INDEX IF NOT EXISTS ledgerforge_txn_created_at_not_queued_idx ON ledgerforge.transactions (created_at DESC) WHERE status <> 'QUEUED';
CREATE INDEX IF NOT EXISTS ledgerforge_txn_currency_created_at_desc_idx ON ledgerforge.transactions (currency, created_at DESC) INCLUDE (precision);
CREATE INDEX IF NOT EXISTS idx_transactions_status ON ledgerforge.transactions (status);
CREATE INDEX IF NOT EXISTS idx_transactions_currency ON ledgerforge.transactions (currency);
CREATE INDEX IF NOT EXISTS idx_transactions_source ON ledgerforge.transactions (source);
CREATE INDEX IF NOT EXISTS idx_transactions_destination ON ledgerforge.transactions (destination);
CREATE INDEX IF NOT EXISTS idx_transactions_parent_transaction ON ledgerforge.transactions (parent_transaction);
CREATE INDEX IF NOT EXISTS idx_transactions_meta_data ON ledgerforge.transactions USING gin (meta_data);

-- Balances table indexes
CREATE INDEX IF NOT EXISTS ledgerforge_balances_created_at_idx ON ledgerforge.balances (created_at DESC);
CREATE INDEX IF NOT EXISTS ledgerforge_balances_identity_id_idx ON ledgerforge.balances (identity_id);
CREATE INDEX IF NOT EXISTS ledgerforge_balances_ledger_id_idx ON ledgerforge.balances (ledger_id);
CREATE INDEX IF NOT EXISTS ledgerforge_balances_currency_identity_id_idx ON ledgerforge.balances (currency, identity_id);

-- Identity table indexes
CREATE INDEX IF NOT EXISTS ledgerforge_identity_created_at_idx ON ledgerforge.identity (created_at DESC);
CREATE INDEX IF NOT EXISTS ledgerforge_identity_country_created_at_idx ON ledgerforge.identity (country, created_at DESC);

-- Ledgers table indexes
CREATE INDEX IF NOT EXISTS ledgerforge_ledgers_created_at_idx ON ledgerforge.ledgers (created_at DESC);

-- +migrate Down

-- Drop transactions table indexes
DROP INDEX IF EXISTS ledgerforge.ledgerforge_transactions_currency_created_at_idx;
DROP INDEX IF EXISTS ledgerforge.ledgerforge_transactions_created_at_source_idx;
DROP INDEX IF EXISTS ledgerforge.ledgerforge_transactions_created_at_destination_idx;
DROP INDEX IF EXISTS ledgerforge.ledgerforge_txn_created_at_not_queued_idx;
DROP INDEX IF EXISTS ledgerforge.ledgerforge_txn_currency_created_at_desc_idx;
DROP INDEX IF EXISTS ledgerforge.idx_transactions_currency;
DROP INDEX IF EXISTS ledgerforge.idx_transactions_source;
DROP INDEX IF EXISTS ledgerforge.idx_transactions_destination;
DROP INDEX IF EXISTS ledgerforge.idx_transactions_meta_data;

-- Drop balances table indexes
DROP INDEX IF EXISTS ledgerforge.ledgerforge_balances_created_at_idx;
DROP INDEX IF EXISTS ledgerforge.ledgerforge_balances_identity_id_idx;
DROP INDEX IF EXISTS ledgerforge.ledgerforge_balances_ledger_id_idx;
DROP INDEX IF EXISTS ledgerforge.ledgerforge_balances_currency_identity_id_idx;

-- Drop identity table indexes
DROP INDEX IF EXISTS ledgerforge.ledgerforge_identity_created_at_idx;
DROP INDEX IF EXISTS ledgerforge.ledgerforge_identity_country_created_at_idx;

-- Drop ledgers table indexes
DROP INDEX IF EXISTS ledgerforge.ledgerforge_ledgers_created_at_idx;

