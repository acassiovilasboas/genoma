package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditLog represents an audit log entry.
type AuditLog struct {
	ID         string         `json:"id"`
	EntityType string         `json:"entity_type"`
	EntityID   string         `json:"entity_id"`
	Action     string         `json:"action"`
	Actor      string         `json:"actor"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

// EntityRef represents a reference to an entity across all storage layers.
type EntityRef struct {
	ID         string    `json:"id"`
	EntityType string    `json:"entity_type"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Transaction represents a financial transaction.
type Transaction struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Amount    float64        `json:"amount"`
	Currency  string         `json:"currency"`
	Status    string         `json:"status"`
	Reference string         `json:"reference,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// RelationalRepo handles core relational data operations.
type RelationalRepo struct {
	pool *pgxpool.Pool
}

// NewRelationalRepo creates a new relational repository.
func NewRelationalRepo(pool *pgxpool.Pool) *RelationalRepo {
	return &RelationalRepo{pool: pool}
}

// --- Entity Refs ---

// CreateEntityRef creates a new entity reference.
func (r *RelationalRepo) CreateEntityRef(ctx context.Context, entityType string) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO entity_refs (entity_type) VALUES ($1) RETURNING id`,
		entityType,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create entity ref: %w", err)
	}
	return id, nil
}

// CreateEntityRefTx creates an entity ref within an existing transaction.
func (r *RelationalRepo) CreateEntityRefTx(ctx context.Context, tx pgx.Tx, entityType string) (string, error) {
	var id string
	err := tx.QueryRow(ctx,
		`INSERT INTO entity_refs (entity_type) VALUES ($1) RETURNING id`,
		entityType,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create entity ref: %w", err)
	}
	return id, nil
}

// GetEntityRef retrieves an entity reference by ID.
func (r *RelationalRepo) GetEntityRef(ctx context.Context, id string) (*EntityRef, error) {
	ref := &EntityRef{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, entity_type, created_at, updated_at FROM entity_refs WHERE id = $1`,
		id,
	).Scan(&ref.ID, &ref.EntityType, &ref.CreatedAt, &ref.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get entity ref: %w", err)
	}
	return ref, nil
}

// --- Audit Logs ---

// CreateAuditLog records an audit entry.
func (r *RelationalRepo) CreateAuditLog(ctx context.Context, log AuditLog) error {
	metaJSON, err := json.Marshal(log.Metadata)
	if err != nil {
		return fmt.Errorf("marshal audit metadata: %w", err)
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO audit_logs (entity_type, entity_id, action, actor, metadata)
		 VALUES ($1, $2, $3, $4, $5)`,
		log.EntityType, log.EntityID, log.Action, log.Actor, metaJSON,
	)
	if err != nil {
		return fmt.Errorf("create audit log: %w", err)
	}
	return nil
}

// CreateAuditLogTx records an audit entry within a transaction.
func (r *RelationalRepo) CreateAuditLogTx(ctx context.Context, tx pgx.Tx, log AuditLog) error {
	metaJSON, err := json.Marshal(log.Metadata)
	if err != nil {
		return fmt.Errorf("marshal audit metadata: %w", err)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO audit_logs (entity_type, entity_id, action, actor, metadata)
		 VALUES ($1, $2, $3, $4, $5)`,
		log.EntityType, log.EntityID, log.Action, log.Actor, metaJSON,
	)
	if err != nil {
		return fmt.Errorf("create audit log tx: %w", err)
	}
	return nil
}

// GetAuditLogs retrieves audit logs for a specific entity.
func (r *RelationalRepo) GetAuditLogs(ctx context.Context, entityType, entityID string) ([]AuditLog, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, entity_type, entity_id, action, actor, metadata, created_at
		 FROM audit_logs WHERE entity_type = $1 AND entity_id = $2
		 ORDER BY created_at DESC`,
		entityType, entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("query audit logs: %w", err)
	}
	defer rows.Close()

	var logs []AuditLog
	for rows.Next() {
		var log AuditLog
		var metaJSON []byte
		err := rows.Scan(&log.ID, &log.EntityType, &log.EntityID, &log.Action, &log.Actor, &metaJSON, &log.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan audit log: %w", err)
		}
		if metaJSON != nil {
			json.Unmarshal(metaJSON, &log.Metadata)
		}
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

// --- Node Definitions (Relational) ---

// SaveNodeDefinition persists a node definition.
func (r *RelationalRepo) SaveNodeDefinition(ctx context.Context, node *NodeDefRow) error {
	toolsJSON, _ := json.Marshal(node.Tools)
	_, err := r.pool.Exec(ctx,
		`INSERT INTO node_definitions (id, name, purpose, input_schema, output_schema, tools, script_lang, script_content, max_retries, timeout_sec, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 ON CONFLICT (id) DO UPDATE SET
		   name = EXCLUDED.name,
		   purpose = EXCLUDED.purpose,
		   input_schema = EXCLUDED.input_schema,
		   output_schema = EXCLUDED.output_schema,
		   tools = EXCLUDED.tools,
		   script_lang = EXCLUDED.script_lang,
		   script_content = EXCLUDED.script_content,
		   max_retries = EXCLUDED.max_retries,
		   timeout_sec = EXCLUDED.timeout_sec,
		   metadata = EXCLUDED.metadata,
		   updated_at = NOW()`,
		node.ID, node.Name, node.Purpose, node.InputSchema, node.OutputSchema,
		toolsJSON, node.ScriptLang, node.ScriptContent, node.MaxRetries, node.TimeoutSec, node.Metadata,
	)
	if err != nil {
		return fmt.Errorf("save node definition: %w", err)
	}
	return nil
}

// GetNodeDefinition retrieves a node definition by ID.
func (r *RelationalRepo) GetNodeDefinition(ctx context.Context, id string) (*NodeDefRow, error) {
	node := &NodeDefRow{}
	var toolsJSON, metaJSON []byte
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, purpose, input_schema, output_schema, tools, script_lang, script_content, max_retries, timeout_sec, metadata, created_at, updated_at
		 FROM node_definitions WHERE id = $1`,
		id,
	).Scan(&node.ID, &node.Name, &node.Purpose, &node.InputSchema, &node.OutputSchema,
		&toolsJSON, &node.ScriptLang, &node.ScriptContent, &node.MaxRetries, &node.TimeoutSec,
		&metaJSON, &node.CreatedAt, &node.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get node definition: %w", err)
	}
	json.Unmarshal(toolsJSON, &node.Tools)
	if metaJSON != nil {
		json.Unmarshal(metaJSON, &node.Metadata)
	}
	return node, nil
}

// ListNodeDefinitions retrieves all node definitions.
func (r *RelationalRepo) ListNodeDefinitions(ctx context.Context, limit, offset int) ([]NodeDefRow, int, error) {
	var total int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM node_definitions`).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count nodes: %w", err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, name, purpose, input_schema, output_schema, tools, script_lang, script_content, max_retries, timeout_sec, metadata, created_at, updated_at
		 FROM node_definitions ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list nodes: %w", err)
	}
	defer rows.Close()

	var nodes []NodeDefRow
	for rows.Next() {
		var node NodeDefRow
		var toolsJSON, metaJSON []byte
		err := rows.Scan(&node.ID, &node.Name, &node.Purpose, &node.InputSchema, &node.OutputSchema,
			&toolsJSON, &node.ScriptLang, &node.ScriptContent, &node.MaxRetries, &node.TimeoutSec,
			&metaJSON, &node.CreatedAt, &node.UpdatedAt)
		if err != nil {
			return nil, 0, fmt.Errorf("scan node: %w", err)
		}
		json.Unmarshal(toolsJSON, &node.Tools)
		if metaJSON != nil {
			json.Unmarshal(metaJSON, &node.Metadata)
		}
		nodes = append(nodes, node)
	}
	return nodes, total, rows.Err()
}

// DeleteNodeDefinition removes a node definition.
func (r *RelationalRepo) DeleteNodeDefinition(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM node_definitions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete node: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("node not found: %s", id)
	}
	return nil
}

// --- Flow Graphs (Relational) ---

// SaveFlowGraph persists a flow graph definition.
func (r *RelationalRepo) SaveFlowGraph(ctx context.Context, flow *FlowGraphRow) error {
	edgesJSON, _ := json.Marshal(flow.Edges)
	metaJSON, _ := json.Marshal(flow.Metadata)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx,
		`INSERT INTO flow_graphs (id, name, description, entry_node_id, edges, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (id) DO UPDATE SET
		   name = EXCLUDED.name,
		   description = EXCLUDED.description,
		   entry_node_id = EXCLUDED.entry_node_id,
		   edges = EXCLUDED.edges,
		   metadata = EXCLUDED.metadata,
		   updated_at = NOW()`,
		flow.ID, flow.Name, flow.Description, flow.EntryNodeID, edgesJSON, metaJSON,
	)
	if err != nil {
		return fmt.Errorf("upsert flow graph: %w", err)
	}

	// Update junction table
	_, err = tx.Exec(ctx, `DELETE FROM flow_graph_nodes WHERE flow_id = $1`, flow.ID)
	if err != nil {
		return fmt.Errorf("clear flow nodes: %w", err)
	}

	for _, nodeID := range flow.NodeIDs {
		_, err = tx.Exec(ctx,
			`INSERT INTO flow_graph_nodes (flow_id, node_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			flow.ID, nodeID,
		)
		if err != nil {
			return fmt.Errorf("link flow node: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// GetFlowGraph retrieves a flow graph by ID.
func (r *RelationalRepo) GetFlowGraph(ctx context.Context, id string) (*FlowGraphRow, error) {
	flow := &FlowGraphRow{}
	var edgesJSON, metaJSON []byte
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, description, entry_node_id, edges, metadata, created_at, updated_at
		 FROM flow_graphs WHERE id = $1`,
		id,
	).Scan(&flow.ID, &flow.Name, &flow.Description, &flow.EntryNodeID, &edgesJSON, &metaJSON, &flow.CreatedAt, &flow.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get flow graph: %w", err)
	}
	json.Unmarshal(edgesJSON, &flow.Edges)
	if metaJSON != nil {
		json.Unmarshal(metaJSON, &flow.Metadata)
	}

	// Get node IDs
	rows, err := r.pool.Query(ctx, `SELECT node_id FROM flow_graph_nodes WHERE flow_id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("get flow nodes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			return nil, fmt.Errorf("scan node id: %w", err)
		}
		flow.NodeIDs = append(flow.NodeIDs, nodeID)
	}

	return flow, rows.Err()
}

// ListFlowGraphs retrieves all flow graphs.
func (r *RelationalRepo) ListFlowGraphs(ctx context.Context, limit, offset int) ([]FlowGraphRow, int, error) {
	var total int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM flow_graphs`).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count flows: %w", err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, name, description, entry_node_id, edges, metadata, created_at, updated_at
		 FROM flow_graphs ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list flows: %w", err)
	}
	defer rows.Close()

	var flows []FlowGraphRow
	for rows.Next() {
		var flow FlowGraphRow
		var edgesJSON, metaJSON []byte
		err := rows.Scan(&flow.ID, &flow.Name, &flow.Description, &flow.EntryNodeID, &edgesJSON, &metaJSON, &flow.CreatedAt, &flow.UpdatedAt)
		if err != nil {
			return nil, 0, fmt.Errorf("scan flow: %w", err)
		}
		json.Unmarshal(edgesJSON, &flow.Edges)
		if metaJSON != nil {
			json.Unmarshal(metaJSON, &flow.Metadata)
		}
		flows = append(flows, flow)
	}
	return flows, total, rows.Err()
}

// DeleteFlowGraph removes a flow graph.
func (r *RelationalRepo) DeleteFlowGraph(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM flow_graphs WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete flow: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("flow not found: %s", id)
	}
	return nil
}

// BeginTx starts a new database transaction.
func (r *RelationalRepo) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return r.pool.Begin(ctx)
}

// --- Row types for persistence layer ---

// NodeDefRow is the database representation of a node definition.
type NodeDefRow struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Purpose       string          `json:"purpose"`
	InputSchema   json.RawMessage `json:"input_schema"`
	OutputSchema  json.RawMessage `json:"output_schema"`
	Tools         json.RawMessage `json:"tools"`
	ScriptLang    string          `json:"script_lang"`
	ScriptContent string          `json:"script_content"`
	MaxRetries    int             `json:"max_retries"`
	TimeoutSec    int             `json:"timeout_sec"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// FlowGraphRow is the database representation of a flow graph.
type FlowGraphRow struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	EntryNodeID string          `json:"entry_node_id"`
	Edges       json.RawMessage `json:"edges"`
	NodeIDs     []string        `json:"node_ids"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}
