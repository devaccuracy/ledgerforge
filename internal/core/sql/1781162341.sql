-- Copyright 2026 LedgerForge Contributors.
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

-- +migrate Up notransaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_journal_insert_transaction
ON ledgerforge.transaction_journal (transaction_id) WHERE action_type = 'INSERT' AND succeeded;
ALTER TABLE ledgerforge.transactions VALIDATE CONSTRAINT transaction_chain_complete;

-- +migrate Down notransaction
DROP INDEX CONCURRENTLY IF EXISTS ledgerforge.idx_journal_insert_transaction;
