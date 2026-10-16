// Package server serves the web UI, a small REST API and an MCP endpoint.
package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"hermes-operator/internal/agent"
)

//go:embed ui.html
var uiHTML []byte

func Run(ctx context.Context, addr string, svc *agent.Service) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(uiHTML)
	})
	mux.HandleFunc("GET /api/agents", func(w http.ResponseWriter, r *http.Request) {
		list, err := svc.List(r.Context())
		respond(w, list, err)
	})
	mux.HandleFunc("POST /api/agents", func(w http.ResponseWriter, r *http.Request) {
		var req agent.CreateRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			respond(w, nil, err)
			return
		}
		a, err := svc.Create(r.Context(), req)
		respond(w, a, err)
	})
	mux.HandleFunc("DELETE /api/agents/{name}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]bool{"deleted": true}, svc.Delete(r.Context(), r.PathValue("name")))
	})
	mux.Handle("/mcp", mcpHandler(svc))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func respond(w http.ResponseWriter, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		code := http.StatusBadRequest
		switch {
		case apierrors.IsNotFound(err):
			code = http.StatusNotFound
		case apierrors.IsAlreadyExists(err):
			code = http.StatusConflict
		case apierrors.IsForbidden(err), apierrors.IsInternalError(err):
			code = http.StatusInternalServerError
		}
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}
