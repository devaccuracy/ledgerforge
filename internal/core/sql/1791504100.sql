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
ALTER TABLE ledgerforge.transactions ADD COLUMN execution_controls JSONB;
ALTER TABLE ledgerforge.transactions ADD CONSTRAINT transaction_execution_controls_object
CHECK (execution_controls IS NULL OR jsonb_typeof(execution_controls) = 'object') NOT VALID;

-- +migrate StatementBegin
-- Keep raw legacy values in this evidence projection. Runtime hydration applies
-- the original type checks; normalizing here would change canonical hash bytes.
CREATE FUNCTION ledgerforge.transaction_execution_controls(txn_id TEXT, controls JSONB)
RETURNS JSONB LANGUAGE sql STABLE AS $$
 SELECT COALESCE(controls, (
 SELECT jsonb_build_object(
  'economic_version', 0,
  'reserved_metadata', COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each(CASE WHEN jsonb_typeof(new_data->'meta_data')='object' THEN new_data->'meta_data' ELSE '{}'::jsonb END) WHERE key IN ('LEDGERFORGE_LINEAGE_PROVIDER','_lineage_type','_provider','_main_balance','_allocation')), '{}'::jsonb),
  'allow_overdraft', COALESCE(new_data->'meta_data'->'allow_overdraft', 'false'::jsonb),
  'inflight', COALESCE(new_data->'meta_data'->'inflight', 'false'::jsonb),
  'atomic', COALESCE(new_data->'meta_data'->'atomic', 'false'::jsonb),
  'skip_queue', COALESCE(new_data->'meta_data'->'skip_queue', 'false'::jsonb),
  'queued_parent', COALESCE(new_data->'meta_data'->'QUEUED_PARENT_TRANSACTION', '""'::jsonb),
  'queue_lane', COALESCE(new_data->'meta_data'->'queue_lane', '"normal"'::jsonb),
  'shadow_for', COALESCE(new_data->'meta_data'->'_shadow_for', '""'::jsonb)
 ) FROM ledgerforge.transaction_journal
 WHERE transaction_id = txn_id AND action_type = 'INSERT' AND succeeded ORDER BY id LIMIT 1
 ), '{}'::jsonb)
$$;
CREATE FUNCTION ledgerforge.transaction_metadata(txn_id TEXT, metadata JSONB)
RETURNS JSONB LANGUAGE sql STABLE AS $$
 SELECT CASE WHEN jsonb_typeof(metadata) NOT IN ('object','null') THEN metadata ELSE (CASE WHEN jsonb_typeof(metadata) = 'object' THEN metadata ELSE '{}'::jsonb END)
 || jsonb_build_object('__execution_controls', ledgerforge.transaction_execution_controls(txn_id, t.execution_controls)) END
 FROM ledgerforge.transactions t WHERE t.transaction_id = txn_id
$$;
-- +migrate StatementEnd

-- +migrate Down
DROP FUNCTION ledgerforge.transaction_metadata(TEXT, JSONB);
DROP FUNCTION ledgerforge.transaction_execution_controls(TEXT, JSONB);
ALTER TABLE ledgerforge.transactions DROP CONSTRAINT transaction_execution_controls_object;
ALTER TABLE ledgerforge.transactions DROP COLUMN execution_controls;
