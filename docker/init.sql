-- Genoma Framework — Database Initialization
-- This file runs automatically when the PostgreSQL container starts for the first time.
-- It combines all migrations into a single init script.

-- ============================================
-- Migration 001: Core Schema
-- ============================================

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- entity_refs is the bridge table connecting relational ↔ document ↔ vector stores.
CREATE TABLE IF NOT EXISTS entity_refs (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    entity_type VARCHAR(100) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_entity_refs_type ON entity_refs(entity_type);
CREATE INDEX IF NOT EXISTS idx_entity_refs_created ON entity_refs(created_at);

-- audit_logs tracks all changes to entities.
CREATE TABLE IF NOT EXISTS audit_logs (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    entity_type VARCHAR(100) NOT NULL,
    entity_id   UUID NOT NULL,
    action      VARCHAR(50) NOT NULL,
    actor       VARCHAR(200),
    metadata    JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_entity ON audit_logs(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_logs(action);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at);

-- transactions stores financial transaction records.
CREATE TABLE IF NOT EXISTS transactions (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    type        VARCHAR(50) NOT NULL,
    amount      DECIMAL(18,2),
    currency    VARCHAR(3) DEFAULT 'BRL',
    status      VARCHAR(20) NOT NULL DEFAULT 'pending',
    reference   VARCHAR(200),
    metadata    JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tx_status ON transactions(status);
CREATE INDEX IF NOT EXISTS idx_tx_type ON transactions(type);
CREATE INDEX IF NOT EXISTS idx_tx_created ON transactions(created_at);

-- node_definitions stores the blueprint of nodes.
CREATE TABLE IF NOT EXISTS node_definitions (
    id             VARCHAR(26) PRIMARY KEY,
    name           VARCHAR(200) NOT NULL,
    purpose        TEXT NOT NULL,
    input_schema   JSONB NOT NULL DEFAULT '{}',
    output_schema  JSONB NOT NULL DEFAULT '{}',
    tools          JSONB NOT NULL DEFAULT '[]',
    script_lang    VARCHAR(20) NOT NULL DEFAULT 'python',
    script_content TEXT NOT NULL DEFAULT '',
    max_retries    INT NOT NULL DEFAULT 3,
    timeout_sec    INT NOT NULL DEFAULT 30,
    metadata       JSONB,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- flow_graphs stores the graph definitions.
CREATE TABLE IF NOT EXISTS flow_graphs (
    id             VARCHAR(26) PRIMARY KEY,
    name           VARCHAR(200) NOT NULL,
    description    TEXT NOT NULL,
    entry_node_id  VARCHAR(26) NOT NULL,
    edges          JSONB NOT NULL DEFAULT '[]',
    metadata       JSONB,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- flow_graph_nodes is a junction table linking flows to their nodes.
CREATE TABLE IF NOT EXISTS flow_graph_nodes (
    flow_id VARCHAR(26) NOT NULL REFERENCES flow_graphs(id) ON DELETE CASCADE,
    node_id VARCHAR(26) NOT NULL REFERENCES node_definitions(id) ON DELETE CASCADE,
    PRIMARY KEY (flow_id, node_id)
);

-- ============================================
-- Migration 002: Document Store
-- ============================================

CREATE TABLE IF NOT EXISTS document_store (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    entity_ref_id   UUID NOT NULL REFERENCES entity_refs(id) ON DELETE CASCADE,
    schema_version  INT NOT NULL DEFAULT 1,
    data            JSONB NOT NULL DEFAULT '{}',
    metadata        JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_doc_entity ON document_store(entity_ref_id);
CREATE INDEX IF NOT EXISTS idx_doc_data ON document_store USING GIN(data);

-- ============================================
-- Migration 003: Vector Store
-- ============================================

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS vector_store (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    entity_ref_id   UUID REFERENCES entity_refs(id) ON DELETE CASCADE,
    content_type    VARCHAR(50) NOT NULL,
    content_text    TEXT NOT NULL,
    embedding       vector(384) NOT NULL,
    metadata        JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_vector_type ON vector_store(content_type);

-- NOTE: ivfflat index requires data to exist first, so we skip it in init.
-- It can be created later with:
-- CREATE INDEX idx_vector_embedding ON vector_store
--     USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

-- ============================================
-- Initialization Complete
-- ============================================
