package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"github.com/acassiovilasboas/genoma/internal/adi"
	"github.com/acassiovilasboas/genoma/internal/build"
	"github.com/acassiovilasboas/genoma/internal/chat"
	"github.com/acassiovilasboas/genoma/internal/config"
	"github.com/acassiovilasboas/genoma/internal/core"
	"github.com/acassiovilasboas/genoma/internal/persistence"
	"github.com/acassiovilasboas/genoma/internal/sandbox"
	"github.com/acassiovilasboas/genoma/internal/shared"
)

func main() {
	// Setup structured logging
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	slog.Info("🧬 Genoma Framework starting...")

	cfg := config.Load()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- Infrastructure ---

	// PostgreSQL
	pool, err := persistence.NewPostgresPool(ctx, cfg.Database.DSN(), cfg.Database.MaxConns, cfg.Database.MinConns)
	if err != nil {
		slog.Error("failed to connect to PostgreSQL", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("failed to connect to Redis", "error", err)
		os.Exit(1)
	}
	defer rdb.Close()
	slog.Info("redis connected", "addr", cfg.Redis.Addr)

	// --- Repositories ---
	relRepo := persistence.NewRelationalRepo(pool)
	docRepo := persistence.NewDocumentRepo(pool)
	vecRepo := persistence.NewVectorRepo(pool)

	// --- Core Components ---

	// Embedding client
	embeddingClient := core.NewHTTPEmbeddingClient(
		cfg.Embedding.ServiceURL,
		cfg.Embedding.Timeout,
		cfg.Embedding.Dimensions,
	)

	// Unified persistence
	unified := persistence.NewUnifiedPersistence(pool, relRepo, docRepo, vecRepo, embeddingClient)

	// Event bus
	eventBus := shared.NewEventBus(rdb)

	// State bus
	stateBus := core.NewStateBus(rdb)

	// Contract validator
	validator := core.NewContractValidator()

	// Tool registry
	toolRegistry := core.NewToolRegistry()
	core.RegisterBuiltinTools(toolRegistry)

	// Sandbox executor
	sandboxDefaults := sandbox.DefaultLimits()
	sandboxDefaults.CPUQuota = cfg.Sandbox.DefaultCPUQuota
	sandboxDefaults.MemoryBytes = cfg.Sandbox.DefaultMemoryMB * 1024 * 1024
	sandboxDefaults.TimeoutSec = int(cfg.Sandbox.DefaultTimeout.Seconds())
	sandboxDefaults.NetworkDisabled = cfg.Sandbox.NetworkDisabled
	sandboxDefaults.MaxOutputBytes = cfg.Sandbox.MaxOutputBytes
	sandboxDefaults.PidsLimit = cfg.Sandbox.MaxPids

	sandboxExec, err := sandbox.NewExecutor(cfg.Sandbox.DockerHost, cfg.Sandbox.Image, sandboxDefaults)
	if err != nil {
		slog.Warn("sandbox executor not available (Docker may not be running)", "error", err)
		// Continue without sandbox — useful for development
	} else {
		slog.Info("sandbox executor ready", "image", cfg.Sandbox.Image)
	}

	// Orchestrator
	orchestrator := core.NewFlowOrchestrator(sandboxExec, stateBus, validator, toolRegistry, eventBus)

	// Semantic router
	semanticRouter := core.NewSemanticRouter(embeddingClient, unified, stateBus, 0.7)

	// --- HTTP Server ---
	r := chi.NewRouter()

	// Global middleware
	r.Use(shared.Recovery)
	r.Use(shared.Logger)
	r.Use(shared.CORS)

	// API Key auth (if configured)
	if cfg.Auth.APIKey != "" {
		r.Use(shared.APIKeyAuth(cfg.Auth.APIKey))
		slog.Info("API key authentication enabled")
	}

	// Health check (no auth)
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		shared.JSON(w, http.StatusOK, map[string]string{
			"status":    "healthy",
			"framework": "genoma",
			"version":   "0.1.0",
		})
	})

	// Register ADI routes
	adiHandler := adi.NewHandler(relRepo, docRepo, vecRepo, unified, sandboxExec, orchestrator, semanticRouter)
	adiHandler.RegisterRoutes(r)

	// Register Chat routes
	chatHandler := chat.NewHandler(semanticRouter, orchestrator, stateBus, eventBus)
	chatHandler.RegisterRoutes(r)

	// Build endpoint
	r.Post("/api/v1/build", func(w http.ResponseWriter, req *http.Request) {
		var buildCfg build.BuildConfig
		if err := json.NewDecoder(req.Body).Decode(&buildCfg); err != nil {
			shared.JSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		builder := build.NewBuilder(relRepo)
		artifact, err := builder.BuildArtifact(req.Context(), buildCfg)
		if err != nil {
			shared.JSONError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/x-tar")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.tar"`, buildCfg.AppName, buildCfg.Version))
		w.WriteHeader(http.StatusOK)
		io.Copy(w, artifact)
	})

	// --- Start server ---
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		slog.Info("shutdown signal received, stopping...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		srv.Shutdown(shutdownCtx)
		cancel()
	}()

	slog.Info("🧬 Genoma Framework ready",
		"addr", addr,
		"docs", fmt.Sprintf("http://%s/health", addr),
	)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}

	slog.Info("🧬 Genoma Framework stopped")
}
