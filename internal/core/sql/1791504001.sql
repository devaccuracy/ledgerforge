-- Copyright 2026 LedgerForge Contributors.
-- Licensed under the Apache License, Version 2.0 (the "License");
-- you may not use this file except in compliance with the License.
-- You may obtain a copy of the License at
--     http://www.apache.org/licenses/LICENSE-2.0
-- Unless required by applicable law or agreed to in writing, software
-- distributed under the License is distributed on an "AS IS" BASIS,
-- WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
-- See the License for the specific language governing permissions and
-- limitations under the License.

-- +migrate Up
-- +migrate StatementBegin
CREATE FUNCTION ledgerforge.enforce_policy_posting() RETURNS trigger AS $$
DECLARE
 binding ledgerforge.policy_bindings%ROWTYPE;
 admission ledgerforge.policy_admissions%ROWTYPE;
 parent_status TEXT;
 parent_action TEXT;
 binding_reference TEXT;
 original_leg BOOLEAN;
BEGIN
 SELECT b.reference INTO binding_reference FROM ledgerforge.policy_bindings b
 WHERE b.reference=NEW.reference OR b.reference||'_q'=NEW.reference LIMIT 1;
 original_leg := FOUND;
 IF NOT original_leg AND COALESCE(NEW.parent_transaction,'')<>'' THEN
  SELECT status,queue_action INTO parent_status,parent_action FROM ledgerforge.transactions WHERE transaction_id=NEW.parent_transaction;
  -- A refund of an applied posting is a new admission, not a hold settlement.
  IF parent_status='INFLIGHT' OR (parent_status='QUEUED' AND parent_action='COMMIT') THEN
   WITH RECURSIVE ancestors AS (
    SELECT NEW.parent_transaction::text AS transaction_id,0 AS depth
    UNION ALL SELECT t.parent_transaction,a.depth+1 FROM ledgerforge.transactions t
     JOIN ancestors a ON t.transaction_id=a.transaction_id
     WHERE t.parent_transaction<>'' AND t.parent_transaction<>t.transaction_id AND a.depth<16
   ) SELECT b.reference INTO binding_reference FROM ledgerforge.policy_bindings b
     JOIN ancestors a ON a.transaction_id=b.transaction_id ORDER BY a.depth LIMIT 1;
  END IF;
 END IF;
 IF binding_reference IS NULL THEN RETURN NEW; END IF;
 -- Posting and releasing a reservation serialize on the same binding row.
 SELECT * INTO binding FROM ledgerforge.policy_bindings WHERE reference=binding_reference FOR UPDATE;
 SELECT * INTO admission FROM ledgerforge.policy_admissions WHERE admission_id=binding.admission_id;
 IF NEW.source IS DISTINCT FROM binding.source OR NEW.destination IS DISTINCT FROM binding.destination
    OR NEW.currency IS DISTINCT FROM binding.currency OR NEW.precision IS DISTINCT FROM binding.precision
    OR NEW.precise_amount IS NULL OR NEW.precise_amount<0
    OR (original_leg AND NEW.precise_amount<>binding.amount)
    OR (NOT original_leg AND NEW.precise_amount>binding.amount) THEN
  RAISE EXCEPTION 'policy admission transaction details do not match' USING ERRCODE='23514';
 END IF;
 IF NEW.status NOT IN ('VOID','REJECTED') AND binding.released>0
    AND (original_leg OR NEW.precise_amount>binding.amount-binding.released) THEN
  RAISE EXCEPTION 'policy reservation was released' USING ERRCODE='23514';
 END IF;
 IF NEW.status NOT IN ('VOID','REJECTED') AND admission.decision='rejected' THEN
  RAISE EXCEPTION 'policy approval was rejected' USING ERRCODE='23514';
 END IF;
 IF NEW.status NOT IN ('VOID','REJECTED') AND (NEW.status='APPLIED' OR NEW.queue_action='COMMIT')
    AND admission.decision NOT IN ('approved','not_required') THEN
  RAISE EXCEPTION 'policy transaction requires independent approval' USING ERRCODE='23514';
 END IF;
 IF NEW.status='APPLIED' AND original_leg AND admission.decision='approved' THEN
  -- Approval authorizes commitment of the existing hold, never a replacement
  -- posting that skips the inflight balance transition.
  RAISE EXCEPTION 'approved policy transaction must settle its hold' USING ERRCODE='23514';
 END IF;
 NEW.meta_data := CASE WHEN jsonb_typeof(NEW.meta_data)='object' THEN NEW.meta_data ELSE '{}'::jsonb END
  || jsonb_build_object('policy_admission_id',admission.admission_id,'policy_approval',admission.decision);
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +migrate StatementEnd
CREATE TRIGGER enforce_policy_posting BEFORE INSERT ON ledgerforge.transactions
 FOR EACH ROW EXECUTE FUNCTION ledgerforge.enforce_policy_posting();

-- +migrate Down
DROP TRIGGER enforce_policy_posting ON ledgerforge.transactions;
DROP FUNCTION ledgerforge.enforce_policy_posting();
