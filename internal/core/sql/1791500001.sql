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
-- Rate and the transaction immutability policy are owned by 1781162340.sql.
ALTER TABLE ledgerforge.transactions ADD COLUMN overdraft_limit NUMERIC NOT NULL DEFAULT 0;
ALTER TABLE ledgerforge.transactions ADD COLUMN queue_action TEXT NOT NULL DEFAULT '';

CREATE TABLE ledgerforge.refund_claims (
    original_transaction TEXT PRIMARY KEY REFERENCES ledgerforge.transactions(transaction_id),
    reversal_transaction TEXT NOT NULL UNIQUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT refund_claim_intent FOREIGN KEY (reversal_transaction)
        REFERENCES ledgerforge.transactions(transaction_id) DEFERRABLE INITIALLY DEFERRED
);

-- Existing reversal intents also own their original's claim.
INSERT INTO ledgerforge.refund_claims (original_transaction, reversal_transaction)
SELECT DISTINCT ON (t.parent_transaction) t.parent_transaction, t.transaction_id
FROM ledgerforge.transactions t
JOIN ledgerforge.transactions original ON original.transaction_id = t.parent_transaction
WHERE t.reference = t.parent_transaction || '_refund'
ORDER BY t.parent_transaction, t.id;

-- +migrate StatementBegin
CREATE FUNCTION ledgerforge.claim_transaction_lifecycle() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    parent ledgerforge.transactions%ROWTYPE;
    consumed NUMERIC;
BEGIN
    IF COALESCE(NEW.parent_transaction, '') = '' THEN
        IF NEW.queue_action <> '' THEN
            RAISE EXCEPTION 'Lifecycle action requires an inflight parent';
        END IF;
        RETURN NEW;
    END IF;

    SELECT * INTO parent FROM ledgerforge.transactions
        WHERE transaction_id = NEW.parent_transaction FOR UPDATE;
    IF NOT FOUND THEN
        IF NEW.queue_action <> '' THEN
            RAISE EXCEPTION 'Lifecycle action requires an inflight parent';
        END IF;
        RETURN NEW;
    END IF;

    IF NEW.reference = parent.transaction_id || '_refund' THEN
        IF parent.status <> 'APPLIED' THEN
            RAISE EXCEPTION 'Only APPLIED transactions can be refunded';
        END IF;
        IF NEW.source IS DISTINCT FROM parent.destination OR NEW.destination IS DISTINCT FROM parent.source
            OR NEW.precise_amount IS DISTINCT FROM parent.precise_amount THEN
            RAISE EXCEPTION 'Refund must reverse the original legs';
        END IF;
        INSERT INTO ledgerforge.refund_claims (original_transaction, reversal_transaction)
            VALUES (parent.transaction_id, NEW.transaction_id);
        RETURN NEW;
    END IF;

    -- A queued commit's worker/recovery child consumes its existing reservation.
    IF parent.queue_action = 'COMMIT' THEN
        IF NEW.status NOT IN ('APPLIED', 'REJECTED') OR
            NEW.precise_amount IS DISTINCT FROM parent.precise_amount OR
            NEW.source IS DISTINCT FROM parent.source OR NEW.destination IS DISTINCT FROM parent.destination OR
            NEW.precision IS DISTINCT FROM parent.precision OR NEW.currency IS DISTINCT FROM parent.currency THEN
            RAISE EXCEPTION 'Invalid queued inflight transition';
        END IF;
        IF EXISTS (SELECT 1 FROM ledgerforge.transactions WHERE parent_transaction = parent.transaction_id) THEN
            RAISE unique_violation USING MESSAGE = 'Queued inflight transition already processed',
                CONSTRAINT = 'idx_transactions_reference_unique';
        END IF;
        RETURN NEW;
    END IF;

    IF parent.status = 'INFLIGHT' AND NEW.status IN ('APPLIED', 'VOID', 'QUEUED') THEN
        IF NEW.status = 'QUEUED' AND NEW.queue_action <> 'COMMIT' THEN
            RAISE EXCEPTION 'Queued inflight transition requires a commit action';
        END IF;
        IF NEW.precise_amount IS NULL OR NEW.precise_amount <= 0 OR
            NEW.source IS DISTINCT FROM parent.source OR NEW.destination IS DISTINCT FROM parent.destination OR
            NEW.precision IS DISTINCT FROM parent.precision OR NEW.currency IS DISTINCT FROM parent.currency OR
            NEW.rate IS DISTINCT FROM parent.rate THEN
            RAISE EXCEPTION 'Invalid inflight transition amount or legs';
        END IF;
        SELECT COALESCE(SUM(t.precise_amount), 0) INTO consumed
            FROM ledgerforge.transactions t
            WHERE t.parent_transaction = parent.transaction_id AND (
                t.status IN ('APPLIED', 'VOID') OR
                (t.status = 'QUEUED' AND t.queue_action = 'COMMIT' AND NOT EXISTS (
                    SELECT 1 FROM ledgerforge.transactions child
                    WHERE child.parent_transaction = t.transaction_id AND child.status = 'REJECTED'
                ))
            );
        IF consumed + NEW.precise_amount > parent.precise_amount THEN
            RAISE EXCEPTION 'Cannot commit more than the remaining amount';
        END IF;
    ELSIF NEW.queue_action <> '' THEN
        RAISE EXCEPTION 'Lifecycle action requires an inflight parent';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER claim_transaction_lifecycle_trigger BEFORE INSERT ON ledgerforge.transactions
    FOR EACH ROW EXECUTE FUNCTION ledgerforge.claim_transaction_lifecycle();
-- +migrate StatementEnd

-- +migrate Down
DROP TRIGGER claim_transaction_lifecycle_trigger ON ledgerforge.transactions;
DROP FUNCTION ledgerforge.claim_transaction_lifecycle();
DROP TABLE ledgerforge.refund_claims;
ALTER TABLE ledgerforge.transactions DROP COLUMN queue_action;
ALTER TABLE ledgerforge.transactions DROP COLUMN overdraft_limit;
