-- Core relational schema for Genoma framework.
-- Stores entity references, audit logs, and financial transactions.

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- entity_refs is the bridge table connecting relational ↔ document ↔ vector stores.
-- Every entity in the system has a reference here.
CREATE TABLE entity_refs (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    entity_type VARCHAR(100) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_entity_refs_type ON entity_refs(entity_type);
CREATE INDEX idx_entity_refs_created ON entity_refs(created_at);

-- audit_logs tracks all changes to entities for compliance and debugging.
CREATE TABLE audit_logs (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    entity_type VARCHAR(100) NOT NULL,
    entity_id   UUID NOT NULL,
    action      VARCHAR(50) NOT NULL,  -- CREATE, UPDATE, DELETE, EXECUTE
    actor       VARCHAR(200),
    metadata    JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_entity ON audit_logs(entity_type, entity_id);
CREATE INDEX idx_audit_action ON audit_logs(action);
CREATE INDEX idx_audit_created ON audit_logs(created_at);

-- transactions stores financial transaction records.
CREATE TABLE transactions (
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

CREATE INDEX idx_tx_status ON transactions(status);
CREATE INDEX idx_tx_type ON transactions(type);
CREATE INDEX idx_tx_created ON transactions(created_at);

-- node_definitions stores the blueprint of nodes.
CREATE TABLE node_definitions (
    id             VARCHAR(26) PRIMARY KEY,  -- ULID
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
CREATE TABLE flow_graphs (
    id             VARCHAR(26) PRIMARY KEY,  -- ULID
    name           VARCHAR(200) NOT NULL,
    description    TEXT NOT NULL,
    entry_node_id  VARCHAR(26) NOT NULL,
    edges          JSONB NOT NULL DEFAULT '[]',
    metadata       JSONB,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- flow_graph_nodes is a junction table linking flows to their nodes.
CREATE TABLE flow_graph_nodes (
    flow_id VARCHAR(26) NOT NULL REFERENCES flow_graphs(id) ON DELETE CASCADE,
    node_id VARCHAR(26) NOT NULL REFERENCES node_definitions(id) ON DELETE CASCADE,
    PRIMARY KEY (flow_id, node_id)
);
