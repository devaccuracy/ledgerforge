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
CREATE FUNCTION ledgerforge.prevent_sealed_chain_update()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.chain_seq IS NOT NULL AND (
        NEW.id IS DISTINCT FROM OLD.id OR
        NEW.transaction_id IS DISTINCT FROM OLD.transaction_id OR
        NEW.source IS DISTINCT FROM OLD.source OR
        NEW.destination IS DISTINCT FROM OLD.destination OR
        NEW.reference IS DISTINCT FROM OLD.reference OR
        NEW.amount IS DISTINCT FROM OLD.amount OR
        NEW.precise_amount IS DISTINCT FROM OLD.precise_amount OR
        NEW.precision IS DISTINCT FROM OLD.precision OR
        NEW.rate IS DISTINCT FROM OLD.rate OR
        NEW.parent_transaction IS DISTINCT FROM OLD.parent_transaction OR
        NEW.effective_date IS DISTINCT FROM OLD.effective_date OR
        NEW.currency IS DISTINCT FROM OLD.currency OR
        NEW.status IS DISTINCT FROM OLD.status OR
        NEW.created_at IS DISTINCT FROM OLD.created_at OR
        NEW.chain_seq IS DISTINCT FROM OLD.chain_seq OR
        NEW.chain_prev_hash IS DISTINCT FROM OLD.chain_prev_hash OR
        NEW.chain_hash IS DISTINCT FROM OLD.chain_hash OR
        NEW.chain_version IS DISTINCT FROM OLD.chain_version) THEN
        RAISE EXCEPTION 'Cannot modify a sealed transaction commitment.';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER prevent_sealed_chain_update_trigger BEFORE UPDATE ON ledgerforge.transactions
FOR EACH ROW EXECUTE FUNCTION ledgerforge.prevent_sealed_chain_update();
-- +migrate StatementEnd

-- +migrate Down
DROP TRIGGER prevent_sealed_chain_update_trigger ON ledgerforge.transactions;
DROP FUNCTION ledgerforge.prevent_sealed_chain_update();
DROP TABLE ledgerforge.chain_checkpoints;
DROP INDEX IF EXISTS ledgerforge.idx_journal_insert_transaction;
ALTER TABLE ledgerforge.transactions DROP CONSTRAINT transaction_chain_complete;
ALTER TABLE ledgerforge.transactions DROP COLUMN chain_version;
ALTER TABLE ledgerforge.transactions DROP COLUMN rate;
