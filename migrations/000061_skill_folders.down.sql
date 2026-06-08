DROP INDEX IF EXISTS idx_skills_folder;

ALTER TABLE skills DROP COLUMN IF EXISTS folder;
