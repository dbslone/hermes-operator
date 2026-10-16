package models

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuiltinProviders(t *testing.T) {
	ps, err := LoadProviders("")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]Provider{}
	for _, p := range ps {
		ids[p.ID] = p
	}
	for _, want := range []string{"openrouter", "anthropic", "openai-api"} {
		if ids[want].ModelsURL == "" {
			t.Errorf("missing built-in provider %s", want)
		}
	}
	if ids["openrouter"].NeedsKey() || !ids["anthropic"].NeedsKey() || !ids["openai-api"].NeedsKey() {
		t.Error("key requirements wrong")
	}
}

func TestLoadProvidersMerge(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "p.yaml")
	_ = os.WriteFile(f, []byte("- id: openrouter\n  label: Mine\n  modelsURL: https://x/m\n  auth: none\n- id: nvidia\n  label: NIM\n  modelsURL: https://n/m\n  auth: bearer\n"), 0o600)
	ps, err := LoadProviders(f)
	if err != nil {
		t.Fatal(err)
	}
	var or, nv *Provider
	for i := range ps {
		switch ps[i].ID {
		case "openrouter":
			or = &ps[i]
		case "nvidia":
			nv = &ps[i]
		}
	}
	if or == nil || or.Label != "Mine" || nv == nil {
		t.Errorf("override/add failed: %+v %+v", or, nv)
	}

	_ = os.WriteFile(f, []byte("- label: no id\n"), 0o600)
	if _, err := LoadProviders(f); err == nil {
		t.Error("expected error for provider without id")
	}
	_ = os.WriteFile(f, []byte("not: a list"), 0o600)
	if _, err := LoadProviders(f); err == nil {
		t.Error("expected error for malformed file")
	}
	if _, err := LoadProviders(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Error("expected error for missing file")
	}
}

func fake(t *testing.T, hits *int32, h http.HandlerFunc) *Catalog {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		h(w, r)
	}))
	t.Cleanup(ts.Close)
	return NewCatalog([]Provider{
		{ID: "pub", Label: "Pub", ModelsURL: ts.URL, Auth: "none"},
		{ID: "bear", Label: "Bear", ModelsURL: ts.URL, Auth: "bearer", KeyEnv: "BEAR_KEY"},
		{ID: "anth", Label: "Anth", ModelsURL: ts.URL, Auth: "anthropic", KeyEnv: "A_KEY"},
		{ID: "bad", Label: "Bad", ModelsURL: "http://127.0.0.1:1/x", Auth: "none"},
		{ID: "empty", Label: "Empty"},
	})
}

func TestModelsFetchSortAndAuth(t *testing.T) {
	var hits int32
	var gotAuth, gotKey, gotVer string
	c := fake(t, &hits, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotKey, gotVer = r.Header.Get("Authorization"), r.Header.Get("x-api-key"), r.Header.Get("anthropic-version")
		_, _ = w.Write([]byte(`{"data":[{"id":"b-model"},{"id":""},{"id":"a-model"}]}`))
	})
	ctx := context.Background()

	got, err := c.Models(ctx, "pub", "")
	if err != nil || strings.Join(got, ",") != "a-model,b-model" {
		t.Fatalf("got %v %v", got, err)
	}
	if gotAuth != "" || gotKey != "" {
		t.Error("no credentials should be sent for auth none")
	}
	if _, err := c.Models(ctx, "bear", "sk-1"); err != nil || gotAuth != "Bearer sk-1" {
		t.Errorf("bearer: %v %q", err, gotAuth)
	}
	if _, err := c.Models(ctx, "anth", "ak-1"); err != nil || gotKey != "ak-1" || gotVer == "" {
		t.Errorf("anthropic: %v %q %q", err, gotKey, gotVer)
	}
}

func TestModelsCache(t *testing.T) {
	var hits int32
	c := fake(t, &hits, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"data":[{"id":"m"}]}`)) })
	now := time.Now()
	c.now = func() time.Time { return now }
	ctx := context.Background()

	_, _ = c.Models(ctx, "pub", "")
	_, _ = c.Models(ctx, "pub", "")
	if hits != 1 {
		t.Errorf("public list should be cached, hits=%d", hits)
	}
	// Keyed lists are cached per key: another key must not reuse the first key's result.
	_, _ = c.Models(ctx, "bear", "k1")
	_, _ = c.Models(ctx, "bear", "k1")
	_, _ = c.Models(ctx, "bear", "k2")
	if hits != 3 {
		t.Errorf("keyed cache wrong, hits=%d", hits)
	}
	now = now.Add(c.TTL + time.Second)
	_, _ = c.Models(ctx, "pub", "")
	if hits != 4 {
		t.Errorf("expired entry should refetch, hits=%d", hits)
	}
}

func TestModelsErrors(t *testing.T) {
	var hits int32
	status := http.StatusOK
	body := `{"data":[]}`
	c := fake(t, &hits, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	ctx := context.Background()

	if _, err := c.Models(ctx, "nope", ""); err == nil {
		t.Error("unknown provider")
	}
	if _, err := c.Models(ctx, "empty", ""); err == nil {
		t.Error("provider without modelsURL")
	}
	if _, err := c.Models(ctx, "bear", ""); err == nil || !strings.Contains(err.Error(), "BEAR_KEY") {
		t.Errorf("missing key should name the env var: %v", err)
	}
	if _, err := c.Models(ctx, "bad", ""); err == nil {
		t.Error("connection error")
	}
	status = http.StatusUnauthorized
	if _, err := c.Models(ctx, "bear", "wrong"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("non-200 should surface status: %v", err)
	}
	status, body = http.StatusOK, `not json`
	if _, err := c.Models(ctx, "pub", ""); err == nil {
		t.Error("bad json")
	}
	if hits == 0 {
		t.Error("server never hit")
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.Models(cctx, "anth", "k"); err == nil {
		t.Error("cancelled context")
	}
}

func TestBadURL(t *testing.T) {
	c := NewCatalog([]Provider{{ID: "x", Label: "X", ModelsURL: "http://[::1", Auth: "none"}})
	if _, err := c.Models(context.Background(), "x", ""); err == nil {
		t.Error("malformed URL should error")
	}
}
