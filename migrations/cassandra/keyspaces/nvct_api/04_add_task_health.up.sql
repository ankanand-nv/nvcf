-- Add the JSON health payload used by current Cloud Tasks releases.
-- Fresh installs receive this column from 03_init_tables.up.sql. This
-- migration upgrades keyspaces created from the earlier canonical schema.

ALTER TABLE nvct_api.tasks_v2 ADD IF NOT EXISTS health TEXT;
