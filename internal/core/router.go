package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// EmbeddingClient defines the interface for generating text embeddings.
type EmbeddingClient interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// HTTPEmbeddingClient calls the embeddings micro-service via HTTP.
type HTTPEmbeddingClient struct {
	baseURL    string
	httpClient *http.Client
	dimensions int
}

// NewHTTPEmbeddingClient creates a new HTTP embedding client.
func NewHTTPEmbeddingClient(baseURL string, timeout time.Duration, dimensions int) *HTTPEmbeddingClient {
	return &HTTPEmbeddingClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		dimensions: dimensions,
	}
}

// Embed generates embeddings for the given texts via the embeddings micro-service.
func (c *HTTPEmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody, err := json.Marshal(map[string]any{
		"texts": texts,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embed", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embed service returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}

	return result.Embeddings, nil
}

// PersistenceInterface defines the interface for vector search operations.
type PersistenceInterface interface {
	SearchSimilarFlows(ctx context.Context, embedding []float32, topK int) ([]VectorSearchResult, error)
	StoreFlowEmbedding(ctx context.Context, flowID, description string, embedding []float32) error
}

// VectorSearchResult represents a similarity search result.
type VectorSearchResult struct {
	FlowID     string  `json:"flow_id"`
	Score      float64 `json:"score"` // Cosine similarity score (0-1)
	ContentText string `json:"content_text"`
}

// SemanticRouter analyzes user intent and routes to the appropriate flow graph.
type SemanticRouter struct {
	embeddingClient EmbeddingClient
	persistence     PersistenceInterface
	stateBus        *StateBus
	flows           map[string]*FlowGraph // in-memory flow registry
	minConfidence   float64
}

// NewSemanticRouter creates a new semantic router.
func NewSemanticRouter(
	embeddingClient EmbeddingClient,
	persistence PersistenceInterface,
	stateBus *StateBus,
	minConfidence float64,
) *SemanticRouter {
	if minConfidence <= 0 {
		minConfidence = 0.7
	}
	return &SemanticRouter{
		embeddingClient: embeddingClient,
		persistence:     persistence,
		stateBus:        stateBus,
		flows:           make(map[string]*FlowGraph),
		minConfidence:   minConfidence,
	}
}

// RoutingResult contains the flow selected by the semantic router.
type RoutingResult struct {
	FlowID     string     `json:"flow_id"`
	FlowGraph  *FlowGraph `json:"flow_graph"`
	Confidence float64    `json:"confidence"`
	Fallback   bool       `json:"fallback"`
}

// RegisterFlow registers a flow graph for semantic routing.
// Generates and stores an embedding for the flow's description.
func (sr *SemanticRouter) RegisterFlow(ctx context.Context, graph *FlowGraph) error {
	sr.flows[graph.ID] = graph

	// Generate embedding for the flow description
	embeddings, err := sr.embeddingClient.Embed(ctx, []string{graph.Description})
	if err != nil {
		return fmt.Errorf("generate flow embedding: %w", err)
	}

	if len(embeddings) == 0 {
		return fmt.Errorf("no embedding returned for flow %s", graph.ID)
	}

	// Store in vector database
	if err := sr.persistence.StoreFlowEmbedding(ctx, graph.ID, graph.Description, embeddings[0]); err != nil {
		return fmt.Errorf("store flow embedding: %w", err)
	}

	slog.Info("flow registered for routing",
		"flow_id", graph.ID,
		"flow_name", graph.Name,
	)

	return nil
}

// Route analyzes the user message and returns the best matching flow.
func (sr *SemanticRouter) Route(ctx context.Context, message string) (*RoutingResult, error) {
	// 1. Check embedding cache
	cached, err := sr.stateBus.GetCachedEmbedding(ctx, message)
	if err != nil {
		slog.Warn("failed to get cached embedding", "error", err)
	}

	var embedding []float32
	if cached != nil {
		embedding = cached
	} else {
		// 2. Generate embedding for the user message
		embeddings, err := sr.embeddingClient.Embed(ctx, []string{message})
		if err != nil {
			return nil, fmt.Errorf("generate message embedding: %w", err)
		}
		if len(embeddings) == 0 {
			return nil, fmt.Errorf("no embedding returned for message")
		}
		embedding = embeddings[0]

		// Cache the embedding
		sr.stateBus.CacheEmbedding(ctx, message, embedding)
	}

	// 3. Search for similar flow descriptions in vector store
	results, err := sr.persistence.SearchSimilarFlows(ctx, embedding, 5)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	if len(results) == 0 {
		return nil, &ErrNoRouteFound{Message: message}
	}

	// 4. Select best match
	best := results[0]

	slog.Info("semantic routing result",
		"message", message,
		"best_flow", best.FlowID,
		"confidence", best.Score,
		"min_confidence", sr.minConfidence,
	)

	// 5. Check confidence threshold
	if best.Score < sr.minConfidence {
		return &RoutingResult{
			FlowID:     best.FlowID,
			Confidence: best.Score,
			Fallback:   true,
		}, &ErrNoRouteFound{
			Message:       message,
			BestMatch:     best.FlowID,
			BestMatchConf: best.Score,
		}
	}

	// 6. Return the matching flow graph
	graph, exists := sr.flows[best.FlowID]
	if !exists {
		return nil, fmt.Errorf("flow %s found in vector store but not in registry", best.FlowID)
	}

	return &RoutingResult{
		FlowID:     best.FlowID,
		FlowGraph:  graph,
		Confidence: best.Score,
		Fallback:   false,
	}, nil
}

// UnregisterFlow removes a flow from the routing registry.
func (sr *SemanticRouter) UnregisterFlow(flowID string) {
	delete(sr.flows, flowID)
}

// GetRegisteredFlows returns all registered flow IDs.
func (sr *SemanticRouter) GetRegisteredFlows() []string {
	ids := make([]string, 0, len(sr.flows))
	for id := range sr.flows {
		ids = append(ids, id)
	}
	return ids
}
