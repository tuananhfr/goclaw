-- Fix team deletion when team-scoped vault documents are orphaned by
-- ON DELETE SET NULL. Team-scoped vault docs normally have agent_id NULL, so
-- changing them to scope='personal' violates vault_documents_scope_consistency.
CREATE OR REPLACE FUNCTION vault_docs_team_null_scope_fix()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.team_id IS NULL AND OLD.team_id IS NOT NULL THEN
        IF NEW.agent_id IS NOT NULL THEN
            NEW.scope := 'personal';
        ELSE
            NEW.scope := 'shared';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
