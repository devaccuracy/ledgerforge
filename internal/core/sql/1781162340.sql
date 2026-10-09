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
-- The current engine has unit-rate legs. Persist that effective rate as audit
-- evidence; the compatibility request field remains ignored by arithmetic.
ALTER TABLE ledgerforge.transactions ADD COLUMN IF NOT EXISTS rate NUMERIC NOT NULL DEFAULT 1;
ALTER TABLE ledgerforge.transactions ADD COLUMN chain_version SMALLINT;
UPDATE ledgerforge.transactions SET chain_version = 1 WHERE chain_seq IS NOT NULL;
ALTER TABLE ledgerforge.transactions ADD CONSTRAINT transaction_chain_complete CHECK (
    (chain_seq IS NULL AND chain_prev_hash IS NULL AND chain_hash IS NULL AND chain_version IS NULL)
    OR (chain_seq IS NOT NULL AND chain_seq > 0 AND chain_version IS NOT NULL AND chain_version IN (1, 2)
        AND chain_prev_hash IS NOT NULL AND chain_prev_hash ~ '^[0-9a-f]{64}$'
        AND chain_hash IS NOT NULL AND chain_hash ~ '^[0-9a-f]{64}$')
);
CREATE TABLE ledgerforge.chain_checkpoints (
    sequence BIGINT PRIMARY KEY CHECK (sequence > 0),
    entry_count BIGINT NOT NULL CHECK (entry_count = sequence),
    head_hash CHAR(64) NOT NULL CHECK (head_hash ~ '^[0-9a-f]{64}$'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO ledgerforge.chain_checkpoints (sequence, entry_count, head_hash)
SELECT last_seq, last_seq, head_hash FROM ledgerforge.chain_state WHERE last_seq > 0;
CREATE INDEX IF NOT EXISTS idx_journal_insert_transaction ON ledgerforge.transaction_journal (transaction_id)
WHERE action_type = 'INSERT' AND succeeded;

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
DROP INDEX IF EXISTS ledgerforge.idx_journal_insert_transaction;
ALTER TABLE ledgerforge.transactions DROP CONSTRAINT transaction_chain_complete;
ALTER TABLE ledgerforge.transactions DROP COLUMN chain_version;
ALTER TABLE ledgerforge.transactions DROP COLUMN rate;
