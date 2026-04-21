package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Document represents a document in the JSONB document store.
type Document struct {
	ID            string         `json:"id"`
	EntityRefID   string         `json:"entity_ref_id"`
	SchemaVersion int            `json:"schema_version"`
	Data          map[string]any `json:"data"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

// DocumentRepo handles document store operations using PostgreSQL JSONB.
type DocumentRepo struct {
	pool *pgxpool.Pool
}

// NewDocumentRepo creates a new document repository.
func NewDocumentRepo(pool *pgxpool.Pool) *DocumentRepo {
	return &DocumentRepo{pool: pool}
}

// SaveDocument creates or updates a document for an entity.
func (r *DocumentRepo) SaveDocument(ctx context.Context, entityRefID string, data, metadata map[string]any) (string, error) {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal data: %w", err)
	}
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("marshal metadata: %w", err)
	}

	var id string
	err = r.pool.QueryRow(ctx,
		`INSERT INTO document_store (entity_ref_id, data, metadata)
		 VALUES ($1, $2, $3)
		 RETURNING id`,
		entityRefID, dataJSON, metaJSON,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("save document: %w", err)
	}
	return id, nil
}

// SaveDocumentTx creates a document within an existing transaction.
func (r *DocumentRepo) SaveDocumentTx(ctx context.Context, tx pgx.Tx, entityRefID string, data, metadata map[string]any) (string, error) {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal data: %w", err)
	}
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("marshal metadata: %w", err)
	}

	var id string
	err = tx.QueryRow(ctx,
		`INSERT INTO document_store (entity_ref_id, data, metadata)
		 VALUES ($1, $2, $3)
		 RETURNING id`,
		entityRefID, dataJSON, metaJSON,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("save document tx: %w", err)
	}
	return id, nil
}

// GetDocument retrieves a document by entity reference ID.
func (r *DocumentRepo) GetDocument(ctx context.Context, entityRefID string) (*Document, error) {
	doc := &Document{}
	var dataJSON, metaJSON []byte
	err := r.pool.QueryRow(ctx,
		`SELECT id, entity_ref_id, schema_version, data, metadata, created_at, updated_at
		 FROM document_store WHERE entity_ref_id = $1
		 ORDER BY created_at DESC LIMIT 1`,
		entityRefID,
	).Scan(&doc.ID, &doc.EntityRefID, &doc.SchemaVersion, &dataJSON, &metaJSON, &doc.CreatedAt, &doc.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get document: %w", err)
	}

	json.Unmarshal(dataJSON, &doc.Data)
	if metaJSON != nil {
		json.Unmarshal(metaJSON, &doc.Metadata)
	}
	return doc, nil
}

// GetDocumentByID retrieves a document by its own ID.
func (r *DocumentRepo) GetDocumentByID(ctx context.Context, id string) (*Document, error) {
	doc := &Document{}
	var dataJSON, metaJSON []byte
	err := r.pool.QueryRow(ctx,
		`SELECT id, entity_ref_id, schema_version, data, metadata, created_at, updated_at
		 FROM document_store WHERE id = $1`,
		id,
	).Scan(&doc.ID, &doc.EntityRefID, &doc.SchemaVersion, &dataJSON, &metaJSON, &doc.CreatedAt, &doc.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get document by id: %w", err)
	}
	json.Unmarshal(dataJSON, &doc.Data)
	if metaJSON != nil {
		json.Unmarshal(metaJSON, &doc.Metadata)
	}
	return doc, nil
}

// UpdateDocument merges new data into an existing document's JSONB data field.
func (r *DocumentRepo) UpdateDocument(ctx context.Context, id string, data map[string]any) error {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal update data: %w", err)
	}

	tag, err := r.pool.Exec(ctx,
		`UPDATE document_store
		 SET data = data || $2, schema_version = schema_version + 1, updated_at = NOW()
		 WHERE id = $1`,
		id, dataJSON,
	)
	if err != nil {
		return fmt.Errorf("update document: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("document not found: %s", id)
	}
	return nil
}

// QueryDocuments searches documents using a JSONB containment query.
// The filter map is matched using the @> (contains) operator.
func (r *DocumentRepo) QueryDocuments(ctx context.Context, filter map[string]any, limit, offset int) ([]Document, error) {
	filterJSON, err := json.Marshal(filter)
	if err != nil {
		return nil, fmt.Errorf("marshal filter: %w", err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, entity_ref_id, schema_version, data, metadata, created_at, updated_at
		 FROM document_store WHERE data @> $1
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		filterJSON, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("query documents: %w", err)
	}
	defer rows.Close()

	var docs []Document
	for rows.Next() {
		var doc Document
		var dataJSON, metaJSON []byte
		err := rows.Scan(&doc.ID, &doc.EntityRefID, &doc.SchemaVersion, &dataJSON, &metaJSON, &doc.CreatedAt, &doc.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan document: %w", err)
		}
		json.Unmarshal(dataJSON, &doc.Data)
		if metaJSON != nil {
			json.Unmarshal(metaJSON, &doc.Metadata)
		}
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}

// DeleteDocument removes a document.
func (r *DocumentRepo) DeleteDocument(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM document_store WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("document not found: %s", id)
	}
	return nil
}
