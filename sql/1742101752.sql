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

-- +migrate StatementBegin
CREATE OR REPLACE FUNCTION ledgerforge.reconcile_balance_from_transactions()
RETURNS TRIGGER AS $$
DECLARE
    v_debit_balance NUMERIC := 0;
    v_credit_balance NUMERIC := 0;
    v_balance NUMERIC := 0;
    v_debug_info JSONB;
BEGIN
    -- Check if the reconciliation flag is set in the metadata
    IF NEW.meta_data IS NOT NULL AND
       jsonb_typeof(NEW.meta_data) = 'object' AND
       NEW.meta_data ? 'LEDGERFORGE_RUN_RECONCILIATION' AND
       NEW.meta_data->>'LEDGERFORGE_RUN_RECONCILIATION' = 'SOURCE' THEN

        -- Calculate debit balance (where this balance is the source)
        SELECT COALESCE(SUM(CASE WHEN precise_amount IS NULL THEN amount ELSE precise_amount END), 0)
        INTO v_debit_balance
        FROM ledgerforge.transactions
        WHERE source = NEW.balance_id
        AND status = 'APPLIED';

        -- Calculate credit balance (where this balance is the destination)
        SELECT COALESCE(SUM(CASE WHEN precise_amount IS NULL THEN amount ELSE precise_amount END), 0)
        INTO v_credit_balance
        FROM ledgerforge.transactions
        WHERE destination = NEW.balance_id
        AND status = 'APPLIED';

        -- Calculate the final balance
        v_balance := v_credit_balance - v_debit_balance;

        -- Update the balance with recalculated values
        NEW.debit_balance := v_debit_balance;
        NEW.credit_balance := v_credit_balance;
        NEW.balance := v_balance;

        -- Build reconciliation result info
        v_debug_info := jsonb_build_object(
            'executed_at', now()::text,
            'previous_debit', OLD.debit_balance::text,
            'previous_credit', OLD.credit_balance::text,
            'previous_balance', OLD.balance::text,
            'recalculated_debit', v_debit_balance::text,
            'recalculated_credit', v_credit_balance::text,
            'recalculated_balance', v_balance::text,
            'difference', (v_balance - OLD.balance)::text
        );

        -- Remove the reconciliation flag from metadata
        NEW.meta_data := NEW.meta_data - 'LEDGERFORGE_RUN_RECONCILIATION';

        -- Add reconciliation result to metadata
        NEW.meta_data := jsonb_set(NEW.meta_data, '{LEDGERFORGE_RECONCILIATION_RESULT}', v_debug_info);
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +migrate StatementEnd

-- +migrate StatementBegin
DROP TRIGGER IF EXISTS reconcile_balance_trigger ON ledgerforge.balances;

CREATE TRIGGER reconcile_balance_trigger
BEFORE UPDATE ON ledgerforge.balances
FOR EACH ROW EXECUTE FUNCTION ledgerforge.reconcile_balance_from_transactions();
-- +migrate StatementEnd

-- +migrate Down

-- +migrate StatementBegin
DROP TRIGGER IF EXISTS reconcile_balance_trigger ON ledgerforge.balances;
DROP FUNCTION IF EXISTS ledgerforge.reconcile_balance_from_transactions();
-- +migrate StatementEnd