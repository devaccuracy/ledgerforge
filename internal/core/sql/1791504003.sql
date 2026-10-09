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
ALTER TABLE ledgerforge.api_keys ADD COLUMN parent_api_key_id TEXT REFERENCES ledgerforge.api_keys(api_key_id);
ALTER TABLE ledgerforge.api_keys ADD CONSTRAINT api_key_not_self_delegated CHECK (parent_api_key_id IS DISTINCT FROM api_key_id);
CREATE INDEX api_key_parent ON ledgerforge.api_keys(parent_api_key_id) WHERE parent_api_key_id IS NOT NULL;

-- +migrate StatementBegin
CREATE FUNCTION ledgerforge.protect_api_key_ancestry() RETURNS trigger AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW.parent_api_key_id IS DISTINCT FROM OLD.parent_api_key_id THEN
  RAISE EXCEPTION 'API key delegation ancestry is immutable';
 END IF;
 IF TG_OP='INSERT' AND NEW.parent_api_key_id IS NOT NULL AND EXISTS (
  WITH RECURSIVE ancestors AS (
   SELECT api_key_id,parent_api_key_id,1 AS depth FROM ledgerforge.api_keys WHERE api_key_id=NEW.parent_api_key_id
   UNION ALL SELECT k.api_key_id,k.parent_api_key_id,a.depth+1 FROM ledgerforge.api_keys k
    JOIN ancestors a ON k.api_key_id=a.parent_api_key_id WHERE a.depth<64
  ) SELECT 1 FROM ancestors WHERE depth>=64
 ) THEN RAISE EXCEPTION 'API key delegation exceeds maximum depth'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER protect_api_key_ancestry BEFORE INSERT OR UPDATE ON ledgerforge.api_keys
 FOR EACH ROW EXECUTE FUNCTION ledgerforge.protect_api_key_ancestry();
-- +migrate StatementEnd

CREATE TABLE ledgerforge.policy_admission_budgets (
 admission_id TEXT NOT NULL REFERENCES ledgerforge.policy_admissions(admission_id) ON DELETE CASCADE,
 api_key_id TEXT NOT NULL,
 currency TEXT NOT NULL,
 precision NUMERIC NOT NULL,
 day DATE NOT NULL,
 amount NUMERIC NOT NULL CHECK(amount>=0 AND amount=trunc(amount)),
 PRIMARY KEY(admission_id,api_key_id,currency,precision)
);
-- Existing admissions charged their initiating key only. Preserve that snapshot.
INSERT INTO ledgerforge.policy_admission_budgets(admission_id,api_key_id,currency,precision,day,amount)
 SELECT a.admission_id,a.api_key_id,b.currency,b.precision,a.day,sum(b.amount)
 FROM ledgerforge.policy_admissions a JOIN ledgerforge.policy_bindings b USING(admission_id)
 GROUP BY a.admission_id,a.api_key_id,b.currency,b.precision,a.day;

-- +migrate StatementBegin
CREATE FUNCTION ledgerforge.release_policy_budgets(target_admission TEXT,target_currency TEXT,target_precision NUMERIC,delta NUMERIC) RETURNS VOID AS $$
DECLARE budget RECORD;
BEGIN
 FOR budget IN SELECT * FROM ledgerforge.policy_admission_budgets
  WHERE admission_id=target_admission AND currency=target_currency AND precision=target_precision ORDER BY api_key_id
 LOOP
  PERFORM 1 FROM ledgerforge.policy_usage WHERE api_key_id=budget.api_key_id AND currency=budget.currency
   AND precision=budget.precision AND day=budget.day FOR UPDATE;
  UPDATE ledgerforge.policy_usage SET used=used-delta WHERE api_key_id=budget.api_key_id AND currency=budget.currency
   AND precision=budget.precision AND day=budget.day;
 END LOOP;
END;
$$ LANGUAGE plpgsql;
-- +migrate StatementEnd

-- +migrate StatementBegin
CREATE OR REPLACE FUNCTION ledgerforge.release_terminal_policy_reservation() RETURNS trigger AS $$
DECLARE
 binding ledgerforge.policy_bindings%ROWTYPE;
 admission ledgerforge.policy_admissions%ROWTYPE;
 binding_reference TEXT;
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
   JOIN ancestors a ON a.transaction_id=b.transaction_id ORDER BY a.depth LIMIT 1;
 IF binding_reference IS NULL THEN RETURN NEW; END IF;
 SELECT * INTO binding FROM ledgerforge.policy_bindings WHERE reference=binding_reference FOR UPDATE;
 SELECT * INTO admission FROM ledgerforge.policy_admissions WHERE admission_id=binding.admission_id;
 -- Failed commit attempts leave the original hold reserved. A void releases
 -- only its own amount, preserving accepted queued commits until they settle.
 unused := ledgerforge.policy_releasable_capacity(binding.reference);
 delta := unused-binding.released;
 IF delta>0 THEN
  PERFORM ledgerforge.release_policy_budgets(binding.admission_id,binding.currency,binding.precision,delta);
  UPDATE ledgerforge.policy_bindings SET released=unused WHERE reference=binding.reference;
 END IF;
 UPDATE ledgerforge.policy_admissions a SET decision='rejected',decided_by='system:void-or-failure',decided_at=now()
  WHERE admission_id=binding.admission_id AND decision='pending'
  AND NOT EXISTS(SELECT 1 FROM ledgerforge.policy_bindings b WHERE b.admission_id=a.admission_id AND b.released<b.amount);
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +migrate StatementEnd

-- +migrate Down
-- +migrate StatementBegin
CREATE OR REPLACE FUNCTION ledgerforge.release_terminal_policy_reservation() RETURNS trigger AS $$
DECLARE
 binding ledgerforge.policy_bindings%ROWTYPE;
 admission ledgerforge.policy_admissions%ROWTYPE;
 binding_reference TEXT;
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
   JOIN ancestors a ON a.transaction_id=b.transaction_id ORDER BY a.depth LIMIT 1;
 IF binding_reference IS NULL THEN RETURN NEW; END IF;
 SELECT * INTO binding FROM ledgerforge.policy_bindings WHERE reference=binding_reference FOR UPDATE;
 SELECT * INTO admission FROM ledgerforge.policy_admissions WHERE admission_id=binding.admission_id;
 -- Failed commit attempts leave the original hold reserved. A void releases
 -- only its own amount, preserving accepted queued commits until they settle.
 unused := ledgerforge.policy_releasable_capacity(binding.reference);
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
DROP FUNCTION ledgerforge.release_policy_budgets(TEXT,TEXT,NUMERIC,NUMERIC);
DROP TABLE ledgerforge.policy_admission_budgets;
DROP TRIGGER protect_api_key_ancestry ON ledgerforge.api_keys;
DROP FUNCTION ledgerforge.protect_api_key_ancestry();
DROP INDEX ledgerforge.api_key_parent;
ALTER TABLE ledgerforge.api_keys DROP CONSTRAINT api_key_not_self_delegated;
ALTER TABLE ledgerforge.api_keys DROP COLUMN parent_api_key_id;
