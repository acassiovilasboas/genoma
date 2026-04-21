-- Document store for dynamic/evolving entity data.
-- Uses PostgreSQL JSONB for schema-flexible storage.

CREATE TABLE document_store (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    entity_ref_id   UUID NOT NULL REFERENCES entity_refs(id) ON DELETE CASCADE,
    schema_version  INT NOT NULL DEFAULT 1,
    data            JSONB NOT NULL DEFAULT '{}',
    metadata        JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index on entity reference for fast lookups
CREATE INDEX idx_doc_entity ON document_store(entity_ref_id);

-- GIN index on data for fast JSONB queries (@>, ?, ?&, ?| operators)
CREATE INDEX idx_doc_data ON document_store USING GIN(data);

-- GIN index on metadata
CREATE INDEX idx_doc_metadata ON document_store USING GIN(metadata);

-- Index on schema version for migration queries
CREATE INDEX idx_doc_version ON document_store(schema_version);
