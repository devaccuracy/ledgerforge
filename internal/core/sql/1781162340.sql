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

-- +migrate Up
-- Retain rate as historical audit evidence. Current arithmetic ignores this
-- compatibility request field; execution controls identify that schema.
CREATE TABLE ledgerforge.chain_schema_transitions (
 column_name TEXT PRIMARY KEY,
 restored_default JSONB NOT NULL,
 last_legacy_journal_id BIGINT NOT NULL
);
INSERT INTO ledgerforge.chain_schema_transitions
SELECT 'rate', '1'::jsonb, COALESCE(max(id),0) FROM ledgerforge.transaction_journal
WHERE action_type = 'INSERT' AND succeeded
HAVING NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'ledgerforge' AND table_name = 'transactions' AND column_name = 'rate');
ALTER TABLE ledgerforge.transactions ADD COLUMN IF NOT EXISTS rate NUMERIC NOT NULL DEFAULT 1;
ALTER TABLE ledgerforge.transactions ADD COLUMN chain_version SMALLINT;
-- NULL version on an existing sealed row denotes v1; no whole-table backfill.
ALTER TABLE ledgerforge.transactions ADD CONSTRAINT transaction_chain_complete CHECK (
    (chain_seq IS NULL AND chain_prev_hash IS NULL AND chain_hash IS NULL AND chain_version IS NULL)
    OR (chain_seq IS NOT NULL AND chain_seq > 0 AND COALESCE(chain_version, 1) IN (1, 2, 3)
        AND chain_prev_hash IS NOT NULL AND chain_prev_hash ~ '^[0-9a-f]{64}$'
        AND chain_hash IS NOT NULL AND chain_hash ~ '^[0-9a-f]{64}$')
) NOT VALID;
CREATE TABLE ledgerforge.chain_checkpoints (
    sequence BIGINT PRIMARY KEY CHECK (sequence > 0),
    entry_count BIGINT NOT NULL CHECK (entry_count = sequence),
    head_hash CHAR(64) NOT NULL CHECK (head_hash ~ '^[0-9a-f]{64}$'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO ledgerforge.chain_checkpoints (sequence, entry_count, head_hash)
SELECT last_seq, last_seq, head_hash FROM ledgerforge.chain_state WHERE last_seq > 0;
-- +migrate StatementBegin
CREATE OR REPLACE FUNCTION ledgerforge.prevent_transaction_update()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'Cannot delete transaction data';
    END IF;
    -- Protect every non-bookkeeping column, including future financial fields.
    IF (to_jsonb(NEW) - ARRAY['meta_data', 'chain_seq', 'chain_prev_hash', 'chain_hash', 'chain_version'])
        IS DISTINCT FROM (to_jsonb(OLD) - ARRAY['meta_data', 'chain_seq', 'chain_prev_hash', 'chain_hash', 'chain_version']) THEN
        RAISE EXCEPTION 'Cannot modify transaction data. Only meta_data and unsealed chain fields can be updated.';
    END IF;
    IF OLD.chain_seq IS NOT NULL AND (
        NEW.chain_seq IS DISTINCT FROM OLD.chain_seq OR
        NEW.chain_prev_hash IS DISTINCT FROM OLD.chain_prev_hash OR
        NEW.chain_hash IS DISTINCT FROM OLD.chain_hash OR
        to_jsonb(NEW)->'chain_version' IS DISTINCT FROM to_jsonb(OLD)->'chain_version') THEN
        RAISE EXCEPTION 'Cannot modify a sealed transaction commitment.';
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS prevent_transaction_update_trigger ON ledgerforge.transactions;
CREATE TRIGGER prevent_transaction_update_trigger BEFORE UPDATE OR DELETE ON ledgerforge.transactions
FOR EACH ROW EXECUTE FUNCTION ledgerforge.prevent_transaction_update();
-- +migrate StatementEnd

-- +migrate Down
-- Retain the financial/DELETE protection and sealed v1 bookkeeping policy.
-- The optional JSON lookup above also works after chain_version is removed.
DROP TABLE ledgerforge.chain_checkpoints;
DROP TABLE ledgerforge.chain_schema_transitions;
DROP INDEX IF EXISTS ledgerforge.idx_journal_insert_transaction;
ALTER TABLE ledgerforge.transactions DROP CONSTRAINT transaction_chain_complete;
ALTER TABLE ledgerforge.transactions DROP COLUMN chain_version;
-- Historical rate belongs to the base schema and must survive rollback.
