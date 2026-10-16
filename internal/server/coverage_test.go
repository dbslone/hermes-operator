package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"hermes-operator/internal/agent"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func newSvc() *agent.Service {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	return &agent.Service{Namespace: "ns", Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(agent.New()).Build()}
}

func TestRunServesAndShutsDownOnCancel(t *testing.T) {
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, addr, newSvc()) }()

	var resp *http.Response
	var err error
	for i := 0; i < 50; i++ {
		if resp, err = http.Get("http://" + addr + "/healthz"); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("server did not come up: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v after a clean shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

func TestRunReturnsListenError(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := Run(ctx, l.Addr().String(), newSvc()); err == nil {
		t.Error("expected an error when the port is already in use")
	}
}

func TestRespondMapsErrorsToStatusCodes(t *testing.T) {
	gr := schema.GroupResource{Group: "g", Resource: "r"}
	for _, tc := range []struct {
		err  error
		want int
	}{
		{apierrors.NewNotFound(gr, "x"), 404},
		{apierrors.NewAlreadyExists(gr, "x"), 409},
		{apierrors.NewForbidden(gr, "x", errors.New("no")), 500},
		{apierrors.NewInternalError(errors.New("x")), 500},
		{errors.New("validation"), 400},
	} {
		w := httptest.NewRecorder()
		respond(w, nil, tc.err)
		if w.Code != tc.want {
			t.Errorf("%v -> %d, want %d", tc.err, w.Code, tc.want)
		}
		if !strings.Contains(w.Body.String(), `"error"`) {
			t.Errorf("body = %s", w.Body.String())
		}
	}
}

func TestRESTListErrorIs500(t *testing.T) {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	svc := &agent.Service{Namespace: "ns", Client: fake.NewClientBuilder().WithScheme(s).WithInterceptorFuncs(interceptor.Funcs{
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			return apierrors.NewForbidden(schema.GroupResource{Resource: "hermesagents"}, "", errors.New("rbac"))
		}}).Build()}
	ts := httptest.NewServer(buildMux(svc))
	defer ts.Close()
	resp, _ := http.Get(ts.URL + "/api/agents")
	if resp.StatusCode != 500 {
		t.Errorf("status = %d, want 500 (RBAC problems are server-side)", resp.StatusCode)
	}
}

func TestMCPMalformedRequests(t *testing.T) {
	ts := httptest.NewServer(buildMux(newSvc()))
	defer ts.Close()

	post := func(body string) int {
		resp, err := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode
	}
	if c := post("not json"); c != 400 {
		t.Errorf("invalid JSON = %d, want 400", c)
	}
	if c := post(strings.Repeat("x", 2<<20)); c != 400 {
		t.Errorf("oversized body = %d, want 400", c)
	}

	// invalid tools/call params and arguments surface as tool errors
	for _, params := range []string{`"not-an-object"`, `{"name":"create_agent","arguments":"nope"}`} {
		text, isErr := toolText(t, rpc(t, ts.URL, "tools/call", params))
		if !isErr {
			t.Errorf("params %s should be a tool error, got %s", params, text)
		}
	}
}

func TestMCPCreateWithAllOptions(t *testing.T) {
	ts := httptest.NewServer(buildMux(newSvc()))
	defer ts.Close()
	args := fmt.Sprintf(`{"name":"create_agent","arguments":{"name":"full","provider":"custom","model":"m","base_url":"http://x/v1","soul":"s","dashboard":true,"env":{"A":"b"}}}`)
	if text, isErr := toolText(t, rpc(t, ts.URL, "tools/call", args)); isErr || !strings.Contains(text, `"dashboard": true`) {
		t.Errorf("create_agent = %s (err=%v)", text, isErr)
	}
}
