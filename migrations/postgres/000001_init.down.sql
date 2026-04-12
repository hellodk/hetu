-- 000001_init.down.sql
-- Drops all tables created by 000001_init.up.sql.
-- WARNING: This is destructive and will lose all data.

DROP TABLE IF EXISTS lb_processed_objects;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS recommendations;
DROP TABLE IF EXISTS rca_reports;
DROP TABLE IF EXISTS incidents;
DROP TABLE IF EXISTS error_groups;
