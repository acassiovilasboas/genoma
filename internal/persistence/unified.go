package persistence

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/acassiovilasboas/genoma/internal/core"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Entity represents a complete entity aggregated from all three storage layers.
type Entity struct {
	Ref      *EntityRef     `json:"ref"`
	Document *Document      `json:"document,omitempty"`
	Vectors  []VectorEntry  `json:"vectors,omitempty"`
}

// CreateEntityRequest holds data needed to create an entity across all layers.
type CreateEntityRequest struct {
	EntityType  string         `json:"entity_type"`
	Data        map[string]any `json:"data"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	ContentText string         `json:"content_text"` // Text to vectorize
	Actor       string         `json:"actor,omitempty"`
}

// UnifiedPersistence coordinates operations across all three storage layers
// (relational, document, vector) ensuring consistency via transactions.
type UnifiedPersistence struct {
	pool     *pgxpool.Pool
	rel      *RelationalRepo
	doc      *DocumentRepo
	vec      *VectorRepo
	embedder core.EmbeddingClient
}

// NewUnifiedPersistence creates a new unified persistence layer.
func NewUnifiedPersistence(
	pool *pgxpool.Pool,
	rel *RelationalRepo,
	doc *DocumentRepo,
	vec *VectorRepo,
	embedder core.EmbeddingClient,
) *UnifiedPersistence {
	return &UnifiedPersistence{
		pool:     pool,
		rel:      rel,
		doc:      doc,
		vec:      vec,
		embedder: embedder,
	}
}

// CreateEntity creates an entity across all three storage layers atomically.
func (u *UnifiedPersistence) CreateEntity(ctx context.Context, req CreateEntityRequest) (*Entity, error) {
	// Start transaction
	tx, err := u.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Create entity reference in relational store
	entityRefID, err := u.rel.CreateEntityRefTx(ctx, tx, req.EntityType)
	if err != nil {
		return nil, fmt.Errorf("create entity ref: %w", err)
	}

	// 2. Store document data in JSONB document store
	docID, err := u.doc.SaveDocumentTx(ctx, tx, entityRefID, req.Data, req.Metadata)
	if err != nil {
		return nil, fmt.Errorf("save document: %w", err)
	}

	// 3. Generate embedding and store in vector store
	var vectorID string
	if req.ContentText != "" && u.embedder != nil {
		embeddings, err := u.embedder.Embed(ctx, []string{req.ContentText})
		if err != nil {
			slog.Warn("failed to generate embedding, continuing without vector",
				"error", err,
				"entity_ref_id", entityRefID,
			)
		} else if len(embeddings) > 0 {
			vectorID, err = u.vec.StoreEmbeddingTx(ctx, tx, entityRefID, "entity",
				req.ContentText, embeddings[0], map[string]any{
					"entity_type": req.EntityType,
					"doc_id":      docID,
				})
			if err != nil {
				return nil, fmt.Errorf("store embedding: %w", err)
			}
		}
	}

	// 4. Record audit log
	err = u.rel.CreateAuditLogTx(ctx, tx, AuditLog{
		EntityType: req.EntityType,
		EntityID:   entityRefID,
		Action:     "CREATE",
		Actor:      req.Actor,
		Metadata: map[string]any{
			"doc_id":    docID,
			"vector_id": vectorID,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create audit log: %w", err)
	}

	// 5. Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	slog.Info("entity created across all stores",
		"entity_ref_id", entityRefID,
		"entity_type", req.EntityType,
		"doc_id", docID,
		"vector_id", vectorID,
	)

	// Return aggregated entity
	return u.GetEntity(ctx, entityRefID)
}

// GetEntity retrieves a complete entity from all three storage layers.
func (u *UnifiedPersistence) GetEntity(ctx context.Context, entityRefID string) (*Entity, error) {
	entity := &Entity{}

	// 1. Get entity reference
	ref, err := u.rel.GetEntityRef(ctx, entityRefID)
	if err != nil {
		return nil, fmt.Errorf("get entity ref: %w", err)
	}
	if ref == nil {
		return nil, nil
	}
	entity.Ref = ref

	// 2. Get document
	doc, err := u.doc.GetDocument(ctx, entityRefID)
	if err != nil {
		return nil, fmt.Errorf("get document: %w", err)
	}
	entity.Document = doc

	return entity, nil
}

// SearchEntities performs a semantic search and returns complete entities.
func (u *UnifiedPersistence) SearchEntities(ctx context.Context, query string, topK int) ([]Entity, error) {
	if u.embedder == nil {
		return nil, fmt.Errorf("embedding client not configured")
	}

	// Generate query embedding
	embeddings, err := u.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("generate query embedding: %w", err)
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned for query")
	}

	// Search vector store
	results, err := u.vec.SearchSimilar(ctx, embeddings[0], "entity", topK)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	// Hydrate results with full entity data
	entities := make([]Entity, 0, len(results))
	for _, result := range results {
		if result.EntityRefID == "" {
			continue
		}
		entity, err := u.GetEntity(ctx, result.EntityRefID)
		if err != nil {
			slog.Warn("failed to hydrate entity", "entity_ref_id", result.EntityRefID, "error", err)
			continue
		}
		if entity != nil {
			entities = append(entities, *entity)
		}
	}

	return entities, nil
}

// SearchSimilarFlows searches for flows by embedding similarity.
// Implements core.PersistenceInterface.
func (u *UnifiedPersistence) SearchSimilarFlows(ctx context.Context, embedding []float32, topK int) ([]core.VectorSearchResult, error) {
	results, err := u.vec.SearchSimilar(ctx, embedding, "flow_description", topK)
	if err != nil {
		return nil, err
	}

	coreResults := make([]core.VectorSearchResult, len(results))
	for i, r := range results {
		flowID := ""
		if r.Metadata != nil {
			if fid, ok := r.Metadata["flow_id"].(string); ok {
				flowID = fid
			}
		}
		coreResults[i] = core.VectorSearchResult{
			FlowID:      flowID,
			Score:       r.Score,
			ContentText: r.ContentText,
		}
	}
	return coreResults, nil
}

// StoreFlowEmbedding stores a flow description embedding for semantic routing.
// Implements core.PersistenceInterface.
func (u *UnifiedPersistence) StoreFlowEmbedding(ctx context.Context, flowID, description string, embedding []float32) error {
	_, err := u.vec.StoreEmbedding(ctx, "", "flow_description", description, embedding, map[string]any{
		"flow_id": flowID,
	})
	return err
}

// DeleteEntity removes an entity from all storage layers.
func (u *UnifiedPersistence) DeleteEntity(ctx context.Context, entityRefID, actor string) error {
	// Record audit before deletion
	ref, err := u.rel.GetEntityRef(ctx, entityRefID)
	if err != nil {
		return fmt.Errorf("get entity ref: %w", err)
	}
	if ref == nil {
		return fmt.Errorf("entity not found: %s", entityRefID)
	}

	u.rel.CreateAuditLog(ctx, AuditLog{
		EntityType: ref.EntityType,
		EntityID:   entityRefID,
		Action:     "DELETE",
		Actor:      actor,
	})

	// Delete from vector store (cascade from entity_refs won't cover content_type-only entries)
	u.vec.DeleteByEntityRef(ctx, entityRefID)

	// Delete entity_ref (cascades to document_store and vector_store via FK)
	_, err = u.pool.Exec(ctx, `DELETE FROM entity_refs WHERE id = $1`, entityRefID)
	if err != nil {
		return fmt.Errorf("delete entity: %w", err)
	}

	return nil
}
