-- Fix: allow agent deletion when attachments reference the agent.
-- Change created_by_agent_id FK from RESTRICT (default) to SET NULL.
-- This was missed when migration 024 recreated the table after migration 023
-- had already fixed all other agent FK constraints for hard-delete support.

ALTER TABLE team_task_attachments
  DROP CONSTRAINT IF EXISTS team_task_attachments_created_by_agent_id_fkey;

ALTER TABLE team_task_attachments
  ADD CONSTRAINT team_task_attachments_created_by_agent_id_fkey
  FOREIGN KEY (created_by_agent_id) REFERENCES agents(id) ON DELETE SET NULL;
