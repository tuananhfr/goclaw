-- Switch GoClaw's global embedding dimension from 1536 to 1024 for local
-- embedding models such as BGE-M3. Existing vectors are discarded because
-- embeddings from different models/dimensions are not comparable.

DROP INDEX IF EXISTS idx_agents_embedding;
DROP INDEX IF EXISTS idx_mem_vec;
DROP INDEX IF EXISTS idx_skills_embedding;
DROP INDEX IF EXISTS idx_tt_embedding;
DROP INDEX IF EXISTS idx_kg_entity_vec;
DROP INDEX IF EXISTS idx_episodic_vec;
DROP INDEX IF EXISTS idx_episodic_embedding_hnsw;
DROP INDEX IF EXISTS idx_vault_docs_embedding;

ALTER TABLE agents
    ALTER COLUMN embedding TYPE vector(1024) USING NULL::vector(1024);

ALTER TABLE memory_chunks
    ALTER COLUMN embedding TYPE vector(1024) USING NULL::vector(1024);

ALTER TABLE skills
    ALTER COLUMN embedding TYPE vector(1024) USING NULL::vector(1024);

ALTER TABLE team_tasks
    ALTER COLUMN embedding TYPE vector(1024) USING NULL::vector(1024);

ALTER TABLE kg_entities
    ALTER COLUMN embedding TYPE vector(1024) USING NULL::vector(1024);

ALTER TABLE episodic_summaries
    ALTER COLUMN embedding TYPE vector(1024) USING NULL::vector(1024);

ALTER TABLE vault_documents
    ALTER COLUMN embedding TYPE vector(1024) USING NULL::vector(1024);

TRUNCATE TABLE embedding_cache;
ALTER TABLE embedding_cache
    ALTER COLUMN embedding TYPE vector(1024) USING NULL::vector(1024);

CREATE INDEX IF NOT EXISTS idx_agents_embedding ON agents USING hnsw(embedding vector_cosine_ops);
CREATE INDEX IF NOT EXISTS idx_mem_vec ON memory_chunks USING hnsw(embedding vector_cosine_ops);
CREATE INDEX IF NOT EXISTS idx_skills_embedding ON skills USING hnsw(embedding vector_cosine_ops);
CREATE INDEX IF NOT EXISTS idx_tt_embedding ON team_tasks USING hnsw (embedding vector_cosine_ops);
CREATE INDEX IF NOT EXISTS idx_kg_entity_vec ON kg_entities USING hnsw(embedding vector_cosine_ops);
CREATE INDEX IF NOT EXISTS idx_episodic_vec ON episodic_summaries USING hnsw(embedding vector_cosine_ops)
    WHERE embedding IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_episodic_embedding_hnsw ON episodic_summaries USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64) WHERE embedding IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_vault_docs_embedding ON vault_documents
    USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);
