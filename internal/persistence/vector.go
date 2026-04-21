package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
)

// VectorEntry represents a row in the vector store.
type VectorEntry struct {
	ID          string         `json:"id"`
	EntityRefID string         `json:"entity_ref_id,omitempty"`
	ContentType string         `json:"content_type"`
	ContentText string         `json:"content_text"`
	Embedding   []float32      `json:"embedding,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

// VectorSearchResult represents a similarity search result.
type VectorSearchResult struct {
	ID          string         `json:"id"`
	EntityRefID string         `json:"entity_ref_id,omitempty"`
	ContentType string         `json:"content_type"`
	ContentText string         `json:"content_text"`
	Score       float64        `json:"score"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// VectorRepo handles vector store operations using pgvector.
type VectorRepo struct {
	pool *pgxpool.Pool
}

// NewVectorRepo creates a new vector repository.
func NewVectorRepo(pool *pgxpool.Pool) *VectorRepo {
	return &VectorRepo{pool: pool}
}

// StoreEmbedding saves a text embedding to the vector store.
func (r *VectorRepo) StoreEmbedding(ctx context.Context, entityRefID, contentType, text string, embedding []float32, metadata map[string]any) (string, error) {
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("marshal metadata: %w", err)
	}

	vec := pgvector.NewVector(embedding)

	var id string
	err = r.pool.QueryRow(ctx,
		`INSERT INTO vector_store (entity_ref_id, content_type, content_text, embedding, metadata)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id`,
		nilIfEmpty(entityRefID), contentType, text, vec, metaJSON,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("store embedding: %w", err)
	}
	return id, nil
}

// StoreEmbeddingTx saves an embedding within an existing transaction.
func (r *VectorRepo) StoreEmbeddingTx(ctx context.Context, tx pgx.Tx, entityRefID, contentType, text string, embedding []float32, metadata map[string]any) (string, error) {
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("marshal metadata: %w", err)
	}

	vec := pgvector.NewVector(embedding)

	var id string
	err = tx.QueryRow(ctx,
		`INSERT INTO vector_store (entity_ref_id, content_type, content_text, embedding, metadata)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id`,
		nilIfEmpty(entityRefID), contentType, text, vec, metaJSON,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("store embedding tx: %w", err)
	}
	return id, nil
}

// SearchSimilar performs a cosine similarity search in the vector store.
func (r *VectorRepo) SearchSimilar(ctx context.Context, embedding []float32, contentType string, topK int) ([]VectorSearchResult, error) {
	vec := pgvector.NewVector(embedding)

	query := `
		SELECT id, entity_ref_id, content_type, content_text,
		       1 - (embedding <=> $1) AS score, metadata
		FROM vector_store
		WHERE ($2 = '' OR content_type = $2)
		ORDER BY embedding <=> $1
		LIMIT $3`

	rows, err := r.pool.Query(ctx, query, vec, contentType, topK)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	defer rows.Close()

	var results []VectorSearchResult
	for rows.Next() {
		var result VectorSearchResult
		var entityRefID *string
		var metaJSON []byte
		err := rows.Scan(&result.ID, &entityRefID, &result.ContentType, &result.ContentText, &result.Score, &metaJSON)
		if err != nil {
			return nil, fmt.Errorf("scan vector result: %w", err)
		}
		if entityRefID != nil {
			result.EntityRefID = *entityRefID
		}
		if metaJSON != nil {
			json.Unmarshal(metaJSON, &result.Metadata)
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

// DeleteByEntityRef removes all vectors associated with an entity reference.
func (r *VectorRepo) DeleteByEntityRef(ctx context.Context, entityRefID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM vector_store WHERE entity_ref_id = $1`,
		entityRefID,
	)
	if err != nil {
		return fmt.Errorf("delete vectors by entity: %w", err)
	}
	return nil
}

// DeleteByContentType removes all vectors of a specific content type.
func (r *VectorRepo) DeleteByContentType(ctx context.Context, contentType string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM vector_store WHERE content_type = $1`,
		contentType,
	)
	if err != nil {
		return fmt.Errorf("delete vectors by type: %w", err)
	}
	return nil
}

// DeleteByID removes a specific vector entry.
func (r *VectorRepo) DeleteByID(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM vector_store WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete vector: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("vector not found: %s", id)
	}
	return nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
