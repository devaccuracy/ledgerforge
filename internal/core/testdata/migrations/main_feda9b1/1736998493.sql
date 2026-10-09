-- +migrate Up
ALTER TABLE ledgerforge.transactions ALTER COLUMN rate TYPE FLOAT;

-- +migrate Down
ALTER TABLE ledgerforge.transactions ALTER COLUMN rate TYPE BIGINT;
