-- Vector store for semantic search and knowledge retrieval.
-- Requires pgvector extension.

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE vector_store (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    entity_ref_id   UUID REFERENCES entity_refs(id) ON DELETE CASCADE,
    content_type    VARCHAR(50) NOT NULL,  -- 'node_purpose', 'flow_description', 'knowledge', 'entity'
    content_text    TEXT NOT NULL,
    embedding       vector(384) NOT NULL,
    metadata        JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- IVFFlat index for approximate nearest neighbor search (cosine distance)
-- Requires at least 100 rows to be effective; falls back to sequential scan otherwise
CREATE INDEX idx_vector_embedding ON vector_store
    USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

-- Index on content type for filtered searches
CREATE INDEX idx_vector_type ON vector_store(content_type);

-- Index on entity reference
CREATE INDEX idx_vector_entity ON vector_store(entity_ref_id);
