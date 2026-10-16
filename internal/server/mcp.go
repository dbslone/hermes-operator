package server

import (
	"encoding/json"
	"io"
	"net/http"

	"hermes-operator/internal/agent"
)

// Minimal stateless MCP server (Streamable HTTP transport, JSON responses).
// Hand-rolled on purpose: four tools don't justify an SDK dependency.

type rpcRequest struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func obj(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

var tools = []map[string]any{
	{
		"name":        "list_agents",
		"description": "List all Hermes agents with their phase and in-cluster endpoint.",
		"inputSchema": obj(map[string]any{}),
	},
	{
		"name":        "get_agent",
		"description": "Get one Hermes agent by name.",
		"inputSchema": obj(map[string]any{"name": str("Agent name")}, "name"),
	},
	{
		"name": "create_agent",
		"description": "Create a new Hermes agent. It starts within a minute; its OpenAI-compatible API " +
			"is served at the returned endpoint, authenticated with the key in the returned apiKeySecret.",
		"inputSchema": obj(map[string]any{
			"name":     str("DNS-style name, e.g. research-bot"),
			"provider": str("Model provider, e.g. openrouter, anthropic, openai"),
			"model":    str("Model name"),
			"base_url": str("Custom OpenAI-compatible base URL (optional)"),
			"soul":     str("Personality / system instructions written to SOUL.md (optional)"),
			"env": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"},
				"description": "Environment variables stored in a Secret, e.g. {\"OPENROUTER_API_KEY\": \"...\"}. " +
					"Omit to use the operator's shared provider keys."},
			"dashboard": map[string]any{"type": "boolean", "description": "Also enable the Hermes web dashboard"},
		}, "name"),
	},
	{
		"name":        "delete_agent",
		"description": "Delete a Hermes agent and its data.",
		"inputSchema": obj(map[string]any{"name": str("Agent name")}, "name"),
	},
}

func mcpHandler(svc *agent.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req rpcRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON-RPC", http.StatusBadRequest)
			return
		}
		if len(req.ID) == 0 { // notification
			w.WriteHeader(http.StatusAccepted)
			return
		}
		resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
		switch req.Method {
		case "initialize":
			resp.Result = map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "hermes-operator", "version": "0.1.0"},
			}
		case "ping":
			resp.Result = map[string]any{}
		case "tools/list":
			resp.Result = map[string]any{"tools": tools}
		case "tools/call":
			resp.Result = callTool(r, svc, req.Params)
		default:
			resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}

func callTool(r *http.Request, svc *agent.Service, params json.RawMessage) map[string]any {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return toolResult(nil, err)
	}
	var a struct {
		Name      string            `json:"name"`
		Provider  string            `json:"provider"`
		Model     string            `json:"model"`
		BaseURL   string            `json:"base_url"`
		Soul      string            `json:"soul"`
		Env       map[string]string `json:"env"`
		Dashboard bool              `json:"dashboard"`
	}
	if len(p.Arguments) > 0 {
		if err := json.Unmarshal(p.Arguments, &a); err != nil {
			return toolResult(nil, err)
		}
	}
	ctx := r.Context()
	switch p.Name {
	case "list_agents":
		v, err := svc.List(ctx)
		return toolResult(v, err)
	case "get_agent":
		v, err := svc.Get(ctx, a.Name)
		return toolResult(v, err)
	case "create_agent":
		req := agent.CreateRequest{Name: a.Name, Soul: a.Soul, Env: a.Env, Dashboard: a.Dashboard}
		if a.Provider != "" || a.Model != "" || a.BaseURL != "" {
			req.Model = &agent.Model{Provider: a.Provider, Name: a.Model, BaseURL: a.BaseURL}
		}
		v, err := svc.Create(ctx, req)
		return toolResult(v, err)
	case "delete_agent":
		err := svc.Delete(ctx, a.Name)
		return toolResult(map[string]bool{"deleted": err == nil}, err)
	}
	return toolResult(nil, errString("unknown tool: "+p.Name))
}

type errString string

func (e errString) Error() string { return string(e) }

func toolResult(v any, err error) map[string]any {
	text, isErr := "", false
	if err != nil {
		text, isErr = err.Error(), true
	} else {
		b, _ := json.MarshalIndent(v, "", "  ")
		text = string(b)
	}
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isErr}
}
