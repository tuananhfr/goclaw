-- Restore the previous behavior.
CREATE OR REPLACE FUNCTION vault_docs_team_null_scope_fix()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.team_id IS NULL AND OLD.team_id IS NOT NULL THEN
        NEW.scope := 'personal';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
