package adi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/acassiovilasboas/genoma/internal/core"
	"github.com/acassiovilasboas/genoma/internal/persistence"
	"github.com/acassiovilasboas/genoma/internal/shared"
)

// Handler holds all ADI route handlers and their dependencies.
type Handler struct {
	relRepo     *persistence.RelationalRepo
	docRepo     *persistence.DocumentRepo
	vecRepo     *persistence.VectorRepo
	unified     *persistence.UnifiedPersistence
	sandbox     core.SandboxExecutorInterface
	orchestrator *core.FlowOrchestrator
	router      *core.SemanticRouter
}

// NewHandler creates a new ADI handler.
func NewHandler(
	relRepo *persistence.RelationalRepo,
	docRepo *persistence.DocumentRepo,
	vecRepo *persistence.VectorRepo,
	unified *persistence.UnifiedPersistence,
	sandbox core.SandboxExecutorInterface,
	orchestrator *core.FlowOrchestrator,
	router *core.SemanticRouter,
) *Handler {
	return &Handler{
		relRepo:      relRepo,
		docRepo:      docRepo,
		vecRepo:      vecRepo,
		unified:      unified,
		sandbox:      sandbox,
		orchestrator: orchestrator,
		router:       router,
	}
}

// RegisterRoutes registers all ADI API routes on the chi router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Route("/api/v1", func(r chi.Router) {
		// Nodes
		r.Post("/nodes", h.CreateNode)
		r.Get("/nodes", h.ListNodes)
		r.Get("/nodes/{nodeID}", h.GetNode)
		r.Put("/nodes/{nodeID}", h.UpdateNode)
		r.Delete("/nodes/{nodeID}", h.DeleteNode)

		// Flows
		r.Post("/flows", h.CreateFlow)
		r.Get("/flows", h.ListFlows)
		r.Get("/flows/{flowID}", h.GetFlow)
		r.Delete("/flows/{flowID}", h.DeleteFlow)
		r.Post("/flows/{flowID}/execute", h.ExecuteFlow)

		// Knowledge
		r.Post("/knowledge/ingest", h.IngestKnowledge)
		r.Post("/knowledge/search", h.SearchKnowledge)
		r.Delete("/knowledge/{docID}", h.DeleteKnowledge)

		// Testing
		r.Post("/tests/run", h.RunTest)
	})
}

// --- Node Handlers ---

func (h *Handler) CreateNode(w http.ResponseWriter, r *http.Request) {
	var req CreateNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.JSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Name == "" || req.Purpose == "" || req.ScriptLang == "" {
		shared.JSONError(w, http.StatusBadRequest, "name, purpose, and script_lang are required")
		return
	}

	id := shared.NewID()
	node := &persistence.NodeDefRow{
		ID:            id,
		Name:          req.Name,
		Purpose:       req.Purpose,
		InputSchema:   req.InputSchema,
		OutputSchema:  req.OutputSchema,
		Tools:         req.Tools,
		ScriptLang:    req.ScriptLang,
		ScriptContent: req.ScriptContent,
		MaxRetries:    req.MaxRetries,
		TimeoutSec:    req.TimeoutSec,
		Metadata:      req.Metadata,
	}

	if node.MaxRetries <= 0 {
		node.MaxRetries = 3
	}
	if node.TimeoutSec <= 0 {
		node.TimeoutSec = 30
	}
	if len(node.InputSchema) == 0 {
		node.InputSchema = json.RawMessage(`{}`)
	}
	if len(node.OutputSchema) == 0 {
		node.OutputSchema = json.RawMessage(`{}`)
	}
	if len(node.Tools) == 0 {
		node.Tools = json.RawMessage(`[]`)
	}

	if err := h.relRepo.SaveNodeDefinition(r.Context(), node); err != nil {
		shared.JSONError(w, http.StatusInternalServerError, "failed to save node: "+err.Error())
		return
	}

	saved, _ := h.relRepo.GetNodeDefinition(r.Context(), id)
	shared.JSON(w, http.StatusCreated, saved)
}

func (h *Handler) ListNodes(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	nodes, total, err := h.relRepo.ListNodeDefinitions(r.Context(), limit, offset)
	if err != nil {
		shared.JSONError(w, http.StatusInternalServerError, "failed to list nodes: "+err.Error())
		return
	}
	shared.JSONList(w, http.StatusOK, nodes, total, limit, offset)
}

func (h *Handler) GetNode(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "nodeID")
	node, err := h.relRepo.GetNodeDefinition(r.Context(), nodeID)
	if err != nil {
		shared.JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if node == nil {
		shared.JSONError(w, http.StatusNotFound, "node not found")
		return
	}
	shared.JSON(w, http.StatusOK, node)
}

func (h *Handler) UpdateNode(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "nodeID")

	existing, err := h.relRepo.GetNodeDefinition(r.Context(), nodeID)
	if err != nil {
		shared.JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		shared.JSONError(w, http.StatusNotFound, "node not found")
		return
	}

	var req UpdateNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.JSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Purpose != nil {
		existing.Purpose = *req.Purpose
	}
	if req.InputSchema != nil {
		existing.InputSchema = *req.InputSchema
	}
	if req.OutputSchema != nil {
		existing.OutputSchema = *req.OutputSchema
	}
	if req.Tools != nil {
		existing.Tools = *req.Tools
	}
	if req.ScriptLang != nil {
		existing.ScriptLang = *req.ScriptLang
	}
	if req.ScriptContent != nil {
		existing.ScriptContent = *req.ScriptContent
	}
	if req.MaxRetries != nil {
		existing.MaxRetries = *req.MaxRetries
	}
	if req.TimeoutSec != nil {
		existing.TimeoutSec = *req.TimeoutSec
	}
	if req.Metadata != nil {
		existing.Metadata = *req.Metadata
	}

	if err := h.relRepo.SaveNodeDefinition(r.Context(), existing); err != nil {
		shared.JSONError(w, http.StatusInternalServerError, "failed to update node: "+err.Error())
		return
	}

	shared.JSON(w, http.StatusOK, existing)
}

func (h *Handler) DeleteNode(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "nodeID")
	if err := h.relRepo.DeleteNodeDefinition(r.Context(), nodeID); err != nil {
		shared.JSONError(w, http.StatusNotFound, err.Error())
		return
	}
	shared.JSON(w, http.StatusOK, map[string]string{"deleted": nodeID})
}

// --- Flow Handlers ---

func (h *Handler) CreateFlow(w http.ResponseWriter, r *http.Request) {
	var req CreateFlowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.JSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Name == "" || req.Description == "" || req.EntryNodeID == "" {
		shared.JSONError(w, http.StatusBadRequest, "name, description, and entry_node_id are required")
		return
	}

	id := shared.NewID()
	flow := &persistence.FlowGraphRow{
		ID:          id,
		Name:        req.Name,
		Description: req.Description,
		EntryNodeID: req.EntryNodeID,
		NodeIDs:     req.NodeIDs,
		Edges:       req.Edges,
		Metadata:    req.Metadata,
	}

	if err := h.relRepo.SaveFlowGraph(r.Context(), flow); err != nil {
		shared.JSONError(w, http.StatusInternalServerError, "failed to save flow: "+err.Error())
		return
	}

	saved, _ := h.relRepo.GetFlowGraph(r.Context(), id)
	shared.JSON(w, http.StatusCreated, saved)
}

func (h *Handler) ListFlows(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	flows, total, err := h.relRepo.ListFlowGraphs(r.Context(), limit, offset)
	if err != nil {
		shared.JSONError(w, http.StatusInternalServerError, "failed to list flows: "+err.Error())
		return
	}
	shared.JSONList(w, http.StatusOK, flows, total, limit, offset)
}

func (h *Handler) GetFlow(w http.ResponseWriter, r *http.Request) {
	flowID := chi.URLParam(r, "flowID")
	flow, err := h.relRepo.GetFlowGraph(r.Context(), flowID)
	if err != nil {
		shared.JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if flow == nil {
		shared.JSONError(w, http.StatusNotFound, "flow not found")
		return
	}
	shared.JSON(w, http.StatusOK, flow)
}

func (h *Handler) DeleteFlow(w http.ResponseWriter, r *http.Request) {
	flowID := chi.URLParam(r, "flowID")
	if err := h.relRepo.DeleteFlowGraph(r.Context(), flowID); err != nil {
		shared.JSONError(w, http.StatusNotFound, err.Error())
		return
	}
	shared.JSON(w, http.StatusOK, map[string]string{"deleted": flowID})
}

func (h *Handler) ExecuteFlow(w http.ResponseWriter, r *http.Request) {
	flowID := chi.URLParam(r, "flowID")

	var req ExecuteFlowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.JSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Load flow graph from database
	flowRow, err := h.relRepo.GetFlowGraph(r.Context(), flowID)
	if err != nil {
		shared.JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if flowRow == nil {
		shared.JSONError(w, http.StatusNotFound, "flow not found")
		return
	}

	// Reconstruct FlowGraph from database
	graph := core.NewFlowGraph(flowRow.Name, flowRow.Description)
	graph.ID = flowRow.ID

	// Load nodes
	for _, nodeID := range flowRow.NodeIDs {
		nodeRow, err := h.relRepo.GetNodeDefinition(r.Context(), nodeID)
		if err != nil || nodeRow == nil {
			shared.JSONError(w, http.StatusBadRequest, "node not found: "+nodeID)
			return
		}
		nodeDef := &core.NodeDefinition{
			ID:            nodeRow.ID,
			Name:          nodeRow.Name,
			Purpose:       nodeRow.Purpose,
			InputSchema:   nodeRow.InputSchema,
			OutputSchema:  nodeRow.OutputSchema,
			ScriptLang:    core.ScriptLanguage(nodeRow.ScriptLang),
			ScriptContent: nodeRow.ScriptContent,
			MaxRetries:    nodeRow.MaxRetries,
			TimeoutSec:    nodeRow.TimeoutSec,
			CreatedAt:     nodeRow.CreatedAt,
			UpdatedAt:     nodeRow.UpdatedAt,
		}
		graph.AddNode(nodeDef)
	}

	// Parse edges
	var edges []*core.Edge
	if err := json.Unmarshal(flowRow.Edges, &edges); err == nil {
		for _, edge := range edges {
			graph.Edges = append(graph.Edges, edge)
		}
	}

	graph.EntryNodeID = flowRow.EntryNodeID

	// Execute
	result, err := h.orchestrator.Execute(r.Context(), graph, req.Input)
	if err != nil {
		shared.JSONError(w, http.StatusInternalServerError, "flow execution failed: "+err.Error())
		return
	}

	shared.JSON(w, http.StatusOK, result)
}

// --- Knowledge Handlers ---

func (h *Handler) IngestKnowledge(w http.ResponseWriter, r *http.Request) {
	var req IngestKnowledgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.JSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Content == "" {
		shared.JSONError(w, http.StatusBadRequest, "content is required")
		return
	}

	if req.ContentType == "" {
		req.ContentType = "knowledge"
	}

	// Create entity through unified persistence
	entity, err := h.unified.CreateEntity(r.Context(), persistence.CreateEntityRequest{
		EntityType: "knowledge",
		Data: map[string]any{
			"title":        req.Title,
			"content":      req.Content,
			"content_type": req.ContentType,
		},
		Metadata:    req.Metadata,
		ContentText: req.Content,
		Actor:       "adi",
	})
	if err != nil {
		shared.JSONError(w, http.StatusInternalServerError, "ingestion failed: "+err.Error())
		return
	}

	shared.JSON(w, http.StatusCreated, entity)
}

func (h *Handler) SearchKnowledge(w http.ResponseWriter, r *http.Request) {
	var req SearchKnowledgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.JSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Query == "" {
		shared.JSONError(w, http.StatusBadRequest, "query is required")
		return
	}
	if req.TopK <= 0 {
		req.TopK = 10
	}

	entities, err := h.unified.SearchEntities(r.Context(), req.Query, req.TopK)
	if err != nil {
		shared.JSONError(w, http.StatusInternalServerError, "search failed: "+err.Error())
		return
	}

	shared.JSON(w, http.StatusOK, entities)
}

func (h *Handler) DeleteKnowledge(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "docID")

	if err := h.unified.DeleteEntity(r.Context(), docID, "adi"); err != nil {
		shared.JSONError(w, http.StatusNotFound, err.Error())
		return
	}

	shared.JSON(w, http.StatusOK, map[string]string{"deleted": docID})
}

// --- Test Handlers ---

func (h *Handler) RunTest(w http.ResponseWriter, r *http.Request) {
	var req RunTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.JSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Script == "" || req.Language == "" {
		shared.JSONError(w, http.StatusBadRequest, "script and language are required")
		return
	}

	execReq := core.ExecutionRequest{
		Script:   req.Script,
		Language: core.ScriptLanguage(req.Language),
		Input:    req.Input,
	}

	result, err := h.sandbox.Execute(r.Context(), execReq)
	testID := shared.NewID()

	resp := TestResult{
		ID:     testID,
		Status: "success",
	}

	if result != nil {
		resp.Output = result.Output
		resp.Logs = result.Logs
		resp.Duration = result.Duration.String()
		if result.Error != "" {
			resp.Error = result.Error
		}
	}

	if err != nil {
		resp.Status = "failed"
		resp.Error = err.Error()
		shared.JSON(w, http.StatusOK, resp) // 200 even on test failure (the test ran)
		return
	}

	shared.JSON(w, http.StatusOK, resp)
}

// --- Helpers ---

func parsePagination(r *http.Request) (limit, offset int) {
	limit = 50
	offset = 0

	if v := r.URL.Query().Get("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil && l > 0 && l <= 200 {
			limit = l
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if o, err := strconv.Atoi(v); err == nil && o >= 0 {
			offset = o
		}
	}
	return
}
