ALTER TABLE skills ADD COLUMN folder TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_skills_folder ON skills(folder);
