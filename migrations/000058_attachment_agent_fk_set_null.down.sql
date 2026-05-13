-- Revert: restore default RESTRICT behavior on created_by_agent_id FK.

ALTER TABLE team_task_attachments
  DROP CONSTRAINT IF EXISTS team_task_attachments_created_by_agent_id_fkey;

ALTER TABLE team_task_attachments
  ADD CONSTRAINT team_task_attachments_created_by_agent_id_fkey
  FOREIGN KEY (created_by_agent_id) REFERENCES agents(id);
