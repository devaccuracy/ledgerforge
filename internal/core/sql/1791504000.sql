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
CREATE TABLE ledgerforge.key_policies (
 api_key_id TEXT PRIMARY KEY,
 version BIGINT NOT NULL DEFAULT 1,
 document JSONB NOT NULL,
 enabled BOOLEAN NOT NULL DEFAULT TRUE
);
CREATE TABLE ledgerforge.policy_rules (
 api_key_id TEXT NOT NULL REFERENCES ledgerforge.key_policies(api_key_id),
 currency TEXT NOT NULL,
 precision NUMERIC NOT NULL CHECK (precision > 0 AND precision = trunc(precision)),
 max_transaction NUMERIC CHECK (max_transaction >= 0 AND max_transaction = trunc(max_transaction)),
 daily_limit NUMERIC CHECK (daily_limit >= 0 AND daily_limit = trunc(daily_limit)),
 approval_above NUMERIC CHECK (approval_above >= 0 AND approval_above = trunc(approval_above)),
 PRIMARY KEY (api_key_id,currency,precision)
);
CREATE TABLE ledgerforge.policy_usage (
 api_key_id TEXT NOT NULL,
 currency TEXT NOT NULL,
 precision NUMERIC NOT NULL,
 day DATE NOT NULL,
 used NUMERIC NOT NULL DEFAULT 0 CHECK (used >= 0 AND used = trunc(used)),
 PRIMARY KEY (api_key_id,currency,precision,day)
);
CREATE TABLE ledgerforge.policy_admissions (
 admission_id TEXT PRIMARY KEY,
 api_key_id TEXT NOT NULL,
 identity TEXT NOT NULL,
 version BIGINT NOT NULL,
 day DATE NOT NULL,
 fingerprint TEXT NOT NULL,
 decision TEXT NOT NULL CHECK (decision IN ('not_required','pending','approved','rejected')),
 decided_by TEXT NOT NULL DEFAULT '',
 decided_at TIMESTAMPTZ,
 legs JSONB NOT NULL,
 UNIQUE (api_key_id,identity)
);
CREATE TABLE ledgerforge.policy_bindings (
 reference TEXT PRIMARY KEY,
 transaction_id TEXT NOT NULL UNIQUE,
 admission_id TEXT NOT NULL REFERENCES ledgerforge.policy_admissions(admission_id),
 source TEXT NOT NULL,
 destination TEXT NOT NULL,
 currency TEXT NOT NULL,
 precision NUMERIC NOT NULL,
 amount NUMERIC NOT NULL CHECK (amount >= 0 AND amount = trunc(amount)),
 released NUMERIC NOT NULL DEFAULT 0 CHECK (released >= 0 AND released <= amount)
);
CREATE INDEX policy_queued_reference ON ledgerforge.policy_bindings ((reference || '_q'));
CREATE INDEX policy_pending_approvals ON ledgerforge.policy_admissions(decision) WHERE decision = 'pending';

-- +migrate Down
DROP TABLE ledgerforge.policy_bindings;
DROP TABLE ledgerforge.policy_admissions;
DROP TABLE ledgerforge.policy_usage;
DROP TABLE ledgerforge.policy_rules;
DROP TABLE ledgerforge.key_policies;
