-- Priority for the tekshot job queue.
--
-- Workers claim the oldest queued job (FIFO). A bulk producer (ERPcons price
-- announcements: 20+ batches per document) therefore parks every other
-- caller behind it for the whole run. Jobs now carry a priority; the claim
-- order becomes "highest priority first, then oldest". Bulk producers submit
-- negative priorities and interactive callers keep the default 0, so an
-- interactive job jumps ahead of the backlog and is picked up by the next
-- free worker.
ALTER TABLE tekshot_jobs ADD COLUMN IF NOT EXISTS priority INTEGER NOT NULL DEFAULT 0;

DROP INDEX IF EXISTS idx_tekshot_jobs_status_locked;
CREATE INDEX IF NOT EXISTS idx_tekshot_jobs_status_locked
    ON tekshot_jobs(status, locked_until, priority DESC, created_at);
