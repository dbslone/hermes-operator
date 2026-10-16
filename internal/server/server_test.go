package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"hermes-operator/internal/agent"
)

func newServer(t *testing.T) (*httptest.Server, *agent.Service) {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	svc := &agent.Service{
		Client:    fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(agent.New()).Build(),
		Namespace: "ns",
	}
	mux := buildMux(svc)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, svc
}

func rpc(t *testing.T, url, method, params string) map[string]any {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `"`
	if params != "" {
		body += `,"params":` + params
	}
	resp, err := http.Post(url+"/mcp", "application/json", strings.NewReader(body+"}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func toolText(t *testing.T, resp map[string]any) (string, bool) {
	t.Helper()
	res := resp["result"].(map[string]any)
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	return text, res["isError"].(bool)
}

func TestUI(t *testing.T) {
	ts, _ := newServer(t)
	resp, err := http.Get(ts.URL + "/")
	if err != nil || resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("UI not served: %v %v", resp, err)
	}
	if r, _ := http.Get(ts.URL + "/nope"); r.StatusCode != 404 {
		t.Errorf("unknown path = %d, want 404", r.StatusCode)
	}
}

func TestRESTLifecycle(t *testing.T) {
	ts, _ := newServer(t)

	resp, _ := http.Post(ts.URL+"/api/agents", "application/json",
		strings.NewReader(`{"name":"bot","model":{"provider":"openrouter","name":"m"}}`))
	if resp.StatusCode != 200 {
		t.Fatalf("create status %d", resp.StatusCode)
	}
	resp, _ = http.Post(ts.URL+"/api/agents", "application/json", strings.NewReader(`{"name":"bot"}`))
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate create = %d, want 409", resp.StatusCode)
	}
	resp, _ = http.Post(ts.URL+"/api/agents", "application/json", strings.NewReader(`{"name":"Bad_Name"}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid name = %d, want 400", resp.StatusCode)
	}
	resp, _ = http.Post(ts.URL+"/api/agents", "application/json", strings.NewReader(`not json`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad json = %d, want 400", resp.StatusCode)
	}

	resp, _ = http.Get(ts.URL + "/api/agents")
	var list []agent.Summary
	_ = json.NewDecoder(resp.Body).Decode(&list)
	if len(list) != 1 || list[0].Name != "bot" || list[0].Provider != "openrouter" {
		t.Errorf("list = %+v", list)
	}

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/agents/bot", nil)
	if resp, _ = http.DefaultClient.Do(req); resp.StatusCode != 200 {
		t.Errorf("delete = %d", resp.StatusCode)
	}
	resp, _ = http.Get(ts.URL + "/api/agents")
	list = nil
	_ = json.NewDecoder(resp.Body).Decode(&list)
	if len(list) != 0 {
		t.Errorf("agent still listed after delete: %+v", list)
	}
}

func TestMCPProtocol(t *testing.T) {
	ts, _ := newServer(t)

	init := rpc(t, ts.URL, "initialize", `{}`)
	if init["result"].(map[string]any)["serverInfo"] == nil {
		t.Errorf("initialize = %v", init)
	}
	if rpc(t, ts.URL, "ping", "")["result"] == nil {
		t.Error("ping should return a result")
	}

	list := rpc(t, ts.URL, "tools/list", "")["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, tl := range list {
		m := tl.(map[string]any)
		names[m["name"].(string)] = true
		if m["inputSchema"].(map[string]any)["type"] != "object" {
			t.Errorf("tool %v has no object inputSchema", m["name"])
		}
	}
	for _, want := range []string{"list_agents", "get_agent", "create_agent", "delete_agent"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}

	if e := rpc(t, ts.URL, "bogus", "")["error"].(map[string]any); e["code"].(float64) != -32601 {
		t.Errorf("unknown method error = %v", e)
	}

	// notifications (no id) get 202 and no body
	resp, _ := http.Post(ts.URL+"/mcp", "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("notification = %d, want 202", resp.StatusCode)
	}
	if resp, _ = http.Get(ts.URL + "/mcp"); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /mcp = %d, want 405", resp.StatusCode)
	}
}

func TestMCPAgentLifecycle(t *testing.T) {
	ts, svc := newServer(t)

	text, isErr := toolText(t, rpc(t, ts.URL, "tools/call",
		`{"name":"create_agent","arguments":{"name":"from-mcp","provider":"anthropic","model":"claude-sonnet-5-5","soul":"hi","env":{"ANTHROPIC_API_KEY":"k"}}}`))
	if isErr {
		t.Fatalf("create_agent failed: %s", text)
	}
	sum, err := svc.Get(context.Background(), "from-mcp")
	if err != nil || sum.Provider != "anthropic" || sum.Model != "claude-sonnet-5-5" {
		t.Fatalf("agent not created as requested: %+v %v", sum, err)
	}

	if text, _ = toolText(t, rpc(t, ts.URL, "tools/call", `{"name":"list_agents"}`)); !strings.Contains(text, "from-mcp") {
		t.Errorf("list_agents = %s", text)
	}
	if text, isErr = toolText(t, rpc(t, ts.URL, "tools/call", `{"name":"get_agent","arguments":{"name":"from-mcp"}}`)); isErr {
		t.Errorf("get_agent: %s", text)
	}

	// failures are reported as tool errors, not transport errors
	if text, isErr = toolText(t, rpc(t, ts.URL, "tools/call", `{"name":"create_agent","arguments":{"name":"BAD"}}`)); !isErr {
		t.Errorf("invalid name should be a tool error, got %s", text)
	}
	if _, isErr = toolText(t, rpc(t, ts.URL, "tools/call", `{"name":"nope"}`)); !isErr {
		t.Error("unknown tool should be a tool error")
	}
	if _, isErr = toolText(t, rpc(t, ts.URL, "tools/call", `{"name":"get_agent","arguments":{"name":"missing"}}`)); !isErr {
		t.Error("missing agent should be a tool error")
	}

	if _, isErr = toolText(t, rpc(t, ts.URL, "tools/call", `{"name":"delete_agent","arguments":{"name":"from-mcp"}}`)); isErr {
		t.Error("delete_agent failed")
	}
	if _, err := svc.Get(context.Background(), "from-mcp"); err == nil {
		t.Error("agent still exists after delete_agent")
	}
}
