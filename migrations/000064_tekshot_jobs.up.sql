CREATE TABLE IF NOT EXISTS tekshot_jobs (
    id UUID PRIMARY KEY,
    external_job_uuid TEXT NOT NULL UNIQUE,
    workspace_id TEXT NOT NULL DEFAULT '',
    workspace_uuid TEXT NOT NULL DEFAULT '',
    external_user_id TEXT NOT NULL DEFAULT '',
    job_type TEXT NOT NULL DEFAULT '',
    agent_key TEXT NOT NULL DEFAULT '',
    session_key TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'queued',
    progress_message TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    request_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    result_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    callback_url TEXT NOT NULL DEFAULT '',
    callback_token TEXT NOT NULL DEFAULT '',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    locked_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_tekshot_jobs_status_locked
    ON tekshot_jobs(status, locked_until, created_at);

CREATE INDEX IF NOT EXISTS idx_tekshot_jobs_workspace
    ON tekshot_jobs(workspace_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_tekshot_jobs_type_status
    ON tekshot_jobs(job_type, status, created_at DESC);
