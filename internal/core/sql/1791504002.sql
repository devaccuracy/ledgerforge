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
CREATE FUNCTION ledgerforge.release_terminal_policy_reservation() RETURNS trigger AS $$
DECLARE
 binding ledgerforge.policy_bindings%ROWTYPE;
 admission ledgerforge.policy_admissions%ROWTYPE;
 binding_reference TEXT;
 paid NUMERIC;
 unused NUMERIC;
 delta NUMERIC;
BEGIN
 IF NEW.status NOT IN ('VOID','REJECTED') THEN RETURN NEW; END IF;
 WITH RECURSIVE ancestors AS (
  SELECT NEW.transaction_id::text AS transaction_id,0 AS depth
  UNION ALL SELECT t.parent_transaction,a.depth+1 FROM ledgerforge.transactions t
   JOIN ancestors a ON t.transaction_id=a.transaction_id
   WHERE t.parent_transaction<>'' AND t.parent_transaction<>t.transaction_id AND a.depth<16
 ) SELECT b.reference INTO binding_reference FROM ledgerforge.policy_bindings b
   JOIN ancestors a ON a.transaction_id=b.transaction_id LIMIT 1;
 IF binding_reference IS NULL THEN RETURN NEW; END IF;
 SELECT * INTO binding FROM ledgerforge.policy_bindings WHERE reference=binding_reference FOR UPDATE;
 SELECT * INTO admission FROM ledgerforge.policy_admissions WHERE admission_id=binding.admission_id;
 WITH RECURSIVE family AS (
  SELECT t.transaction_id,t.status,t.precise_amount FROM ledgerforge.transactions t
   WHERE t.reference=binding.reference OR t.reference=binding.reference||'_q'
  UNION SELECT t.transaction_id,t.status,t.precise_amount FROM ledgerforge.transactions t
   JOIN family f ON t.parent_transaction=f.transaction_id
 ) SELECT COALESCE(sum(precise_amount) FILTER(WHERE status='APPLIED'),0) INTO paid FROM family;
 unused := greatest(binding.amount-paid,0);
 delta := unused-binding.released;
 IF delta>0 THEN
  UPDATE ledgerforge.policy_usage SET used=used-delta
   WHERE api_key_id=admission.api_key_id AND currency=binding.currency
   AND precision=binding.precision AND day=admission.day;
  UPDATE ledgerforge.policy_bindings SET released=unused WHERE reference=binding.reference;
 END IF;
 UPDATE ledgerforge.policy_admissions a SET decision='rejected',decided_by='system:void-or-failure',decided_at=now()
  WHERE admission_id=binding.admission_id AND decision='pending'
  AND NOT EXISTS(SELECT 1 FROM ledgerforge.policy_bindings b WHERE b.admission_id=a.admission_id AND b.released<b.amount);
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +migrate StatementEnd
CREATE TRIGGER release_terminal_policy_reservation AFTER INSERT ON ledgerforge.transactions
 FOR EACH ROW EXECUTE FUNCTION ledgerforge.release_terminal_policy_reservation();

-- +migrate Down
DROP TRIGGER release_terminal_policy_reservation ON ledgerforge.transactions;
DROP FUNCTION ledgerforge.release_terminal_policy_reservation();
