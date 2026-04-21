package chat

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/acassiovilasboas/genoma/internal/core"
	"github.com/acassiovilasboas/genoma/internal/shared"
)

// Message represents a chat message.
type Message struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	Role      string         `json:"role"` // "user", "system", "assistant"
	Content   string         `json:"content"`
	Data      map[string]any `json:"data,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// ChatRequest is the REST chat message request.
type ChatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// ChatResponse is the REST chat response.
type ChatResponse struct {
	SessionID string       `json:"session_id"`
	Reply     string       `json:"reply"`
	FlowID    string       `json:"flow_id,omitempty"`
	Result    *core.FlowResult `json:"result,omitempty"`
}

// Handler handles chat endpoints.
type Handler struct {
	router       *core.SemanticRouter
	orchestrator *core.FlowOrchestrator
	stateBus     *core.StateBus
	events       *shared.EventBus
}

// NewHandler creates a new chat handler.
func NewHandler(
	router *core.SemanticRouter,
	orchestrator *core.FlowOrchestrator,
	stateBus *core.StateBus,
	events *shared.EventBus,
) *Handler {
	return &Handler{
		router:       router,
		orchestrator: orchestrator,
		stateBus:     stateBus,
		events:       events,
	}
}

// RegisterRoutes registers chat API routes.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Route("/api/v1/chat", func(r chi.Router) {
		r.Post("/message", h.HandleMessage)
		r.Get("/ws/{sessionID}", h.HandleWebSocket)
		r.Get("/sessions/{sessionID}", h.GetSessionHistory)
	})
}

// HandleMessage processes a chat message via REST.
func (h *Handler) HandleMessage(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.JSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Message == "" {
		shared.JSONError(w, http.StatusBadRequest, "message is required")
		return
	}

	if req.SessionID == "" {
		req.SessionID = shared.NewID()
	}

	// 1. Route the message to a flow
	routeResult, err := h.router.Route(r.Context(), req.Message)
	if err != nil {
		slog.Warn("routing failed", "error", err, "message", req.Message)
		shared.JSON(w, http.StatusOK, ChatResponse{
			SessionID: req.SessionID,
			Reply:     "Desculpe, não consegui entender sua solicitação. Poderia reformular?",
		})
		return
	}

	// 2. Execute the matched flow
	flowResult, err := h.orchestrator.Execute(r.Context(), routeResult.FlowGraph, map[string]any{
		"message":    req.Message,
		"session_id": req.SessionID,
	})

	resp := ChatResponse{
		SessionID: req.SessionID,
		FlowID:    routeResult.FlowID,
		Result:    flowResult,
	}

	if err != nil {
		resp.Reply = "Ocorreu um erro ao processar sua solicitação."
		slog.Error("flow execution failed", "error", err, "flow_id", routeResult.FlowID)
	} else if flowResult.Output != nil {
		if reply, ok := flowResult.Output["reply"].(string); ok {
			resp.Reply = reply
		} else {
			data, _ := json.Marshal(flowResult.Output)
			resp.Reply = string(data)
		}
	}

	// Save conversation context
	h.stateBus.SetConversationContext(r.Context(), req.SessionID, map[string]any{
		"last_message": req.Message,
		"last_flow":    routeResult.FlowID,
		"last_reply":   resp.Reply,
		"updated_at":   time.Now(),
	})

	shared.JSON(w, http.StatusOK, resp)
}

// HandleWebSocket handles WebSocket chat connections.
func (h *Handler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		slog.Error("websocket accept failed", "error", err)
		return
	}
	defer conn.CloseNow()

	slog.Info("websocket connected", "session_id", sessionID)

	ctx := r.Context()

	// Subscribe to flow events for this session
	eventCh := h.events.Subscribe(ctx, "orchestrator")

	// Forward events to the WebSocket client
	go func() {
		for event := range eventCh {
			wsjson.Write(ctx, conn, map[string]any{
				"type":  "event",
				"event": event,
			})
		}
	}()

	// Read messages from WebSocket
	for {
		var msg Message
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			slog.Info("websocket closed", "session_id", sessionID, "error", err)
			break
		}

		msg.ID = shared.NewID()
		msg.SessionID = sessionID
		msg.Timestamp = time.Now()

		// Process message
		go func(m Message) {
			// Send "thinking" status
			wsjson.Write(ctx, conn, map[string]any{
				"type":   "status",
				"status": "processing",
			})

			// Route and execute
			routeResult, err := h.router.Route(ctx, m.Content)
			if err != nil {
				wsjson.Write(ctx, conn, map[string]any{
					"type":    "message",
					"role":    "assistant",
					"content": "Desculpe, não consegui entender sua solicitação.",
				})
				return
			}

			flowResult, err := h.orchestrator.Execute(ctx, routeResult.FlowGraph, map[string]any{
				"message":    m.Content,
				"session_id": sessionID,
			})

			reply := "Ocorreu um erro ao processar sua solicitação."
			if err == nil && flowResult.Output != nil {
				if r, ok := flowResult.Output["reply"].(string); ok {
					reply = r
				}
			}

			wsjson.Write(ctx, conn, map[string]any{
				"type":    "message",
				"role":    "assistant",
				"content": reply,
				"flow_id": routeResult.FlowID,
				"result":  flowResult,
			})
		}(msg)
	}

	conn.Close(websocket.StatusNormalClosure, "session ended")
}

// GetSessionHistory returns the conversation context for a session.
func (h *Handler) GetSessionHistory(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	ctx, err := h.stateBus.GetConversationContext(r.Context(), sessionID)
	if err != nil {
		shared.JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	shared.JSON(w, http.StatusOK, ctx)
}
