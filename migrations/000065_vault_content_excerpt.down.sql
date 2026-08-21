ALTER TABLE vault_documents DROP COLUMN IF EXISTS tsv;
ALTER TABLE vault_documents DROP COLUMN IF EXISTS content_excerpt;
ALTER TABLE vault_documents ADD COLUMN tsv tsvector
    GENERATED ALWAYS AS (
        to_tsvector('simple',
            coalesce(title, '') || ' ' ||
            coalesce(path, '') || ' ' ||
            coalesce(summary, '')
        )
    ) STORED;
CREATE INDEX IF NOT EXISTS idx_vault_docs_tsv ON vault_documents USING gin(tsv);
