DROP INDEX IF EXISTS idx_tekshot_jobs_status_locked;
CREATE INDEX IF NOT EXISTS idx_tekshot_jobs_status_locked
    ON tekshot_jobs(status, locked_until, created_at);

ALTER TABLE tekshot_jobs DROP COLUMN IF EXISTS priority;
