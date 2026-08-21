-- FTS hiện chỉ index title+path+summary (000042); summary lại sinh từ 3000 ký tự
-- đầu nên số/mã/tên riêng trong thân tài liệu vô hình. Thêm 16KB đầu nội dung.
ALTER TABLE vault_documents ADD COLUMN IF NOT EXISTS content_excerpt TEXT NOT NULL DEFAULT '';

ALTER TABLE vault_documents DROP COLUMN IF EXISTS tsv;
ALTER TABLE vault_documents ADD COLUMN tsv tsvector
    GENERATED ALWAYS AS (
        to_tsvector('simple',
            coalesce(title, '') || ' ' ||
            coalesce(path, '') || ' ' ||
            coalesce(summary, '') || ' ' ||
            coalesce(content_excerpt, '')
        )
    ) STORED;
CREATE INDEX IF NOT EXISTS idx_vault_docs_tsv ON vault_documents USING gin(tsv);
