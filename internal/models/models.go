// Package models lists the models a provider currently offers by asking the
// provider's own API, so new models show up without an operator release.
package models

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"sigs.k8s.io/yaml"
)

//go:embed providers.yaml
var builtin []byte

// Provider is one entry of the provider dropdown. ModelsURL is operator
// configuration only: the UI never supplies a URL, so the lookup cannot be
// pointed at arbitrary hosts.
type Provider struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	ModelsURL string   `json:"modelsURL"`
	Auth      string   `json:"auth"` // none | bearer | anthropic
	KeyEnv    string   `json:"keyEnv,omitempty"`
	KeyURL    string   `json:"keyURL,omitempty"`   // documentation link; the operator never fetches it
	KeySteps  []string `json:"keySteps,omitempty"` // how to create or retrieve KeyEnv
}

// PublicProvider is what the UI sees: no models URL, so the browser cannot learn or choose the upstream.
// KeyURL is a page the user opens; it is not a host the operator calls.
type PublicProvider struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	NeedsKey bool     `json:"needsKey"`
	KeyEnv   string   `json:"keyEnv,omitempty"`
	KeyURL   string   `json:"keyURL,omitempty"`
	KeySteps []string `json:"keySteps,omitempty"`
}

// Public lists the providers for the UI dropdown.
func (c *Catalog) Public() []PublicProvider {
	out := make([]PublicProvider, 0, len(c.Providers))
	for _, p := range c.Providers {
		out = append(out, PublicProvider{
			ID: p.ID, Label: p.Label, NeedsKey: p.NeedsKey(), KeyEnv: p.KeyEnv,
			KeyURL: p.KeyURL, KeySteps: p.KeySteps,
		})
	}
	return out
}

// NeedsKey reports whether listing models requires an API key.
func (p Provider) NeedsKey() bool { return p.Auth == "bearer" || p.Auth == "anthropic" }

// LoadProviders returns the built-in providers, then adds or replaces entries
// from the YAML file at path (a list, same shape as providers.yaml) when set.
func LoadProviders(path string) ([]Provider, error) {
	var out []Provider
	if err := yaml.Unmarshal(builtin, &out); err != nil {
		return nil, err
	}
	if path == "" {
		return out, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var extra []Provider
	if err := yaml.Unmarshal(b, &extra); err != nil {
		return nil, err
	}
	for _, e := range extra {
		if e.ID == "" {
			return nil, fmt.Errorf("provider without id in %s", path)
		}
		replaced := false
		for i := range out {
			if out[i].ID == e.ID {
				out[i], replaced = e, true
			}
		}
		if !replaced {
			out = append(out, e)
		}
	}
	return out, nil
}

type entry struct {
	models  []string
	expires time.Time
}

// Catalog fetches and caches model lists.
type Catalog struct {
	Providers []Provider
	Client    *http.Client
	TTL       time.Duration
	now       func() time.Time

	mu    sync.Mutex
	cache map[string]entry
}

func NewCatalog(p []Provider) *Catalog {
	return &Catalog{Providers: p, Client: &http.Client{Timeout: 10 * time.Second}, TTL: 10 * time.Minute,
		now: time.Now, cache: map[string]entry{}}
}

// Models returns the sorted model ids for provider id. key may be empty for
// providers with auth "none". Lists fetched with a key are cached per key hash,
// so one caller's key never unlocks another's results.
func (c *Catalog) Models(ctx context.Context, id, key string) ([]string, error) {
	var p *Provider
	for i := range c.Providers {
		if c.Providers[i].ID == id {
			p = &c.Providers[i]
		}
	}
	if p == nil || p.ModelsURL == "" {
		return nil, fmt.Errorf("unknown provider %q", id)
	}
	if p.NeedsKey() && key == "" {
		return nil, fmt.Errorf("%s needs an API key (%s) to list models", p.Label, p.KeyEnv)
	}
	ck := id
	if p.NeedsKey() {
		sum := sha256.Sum256([]byte(key))
		ck += ":" + hex.EncodeToString(sum[:8])
	}
	c.mu.Lock()
	if e, ok := c.cache[ck]; ok && c.now().Before(e.expires) {
		c.mu.Unlock()
		return e.models, nil
	}
	c.mu.Unlock()

	list, err := c.fetch(ctx, p, key)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.cache[ck] = entry{list, c.now().Add(c.TTL)}
	c.mu.Unlock()
	return list, nil
}

func (c *Catalog) fetch(ctx context.Context, p *Provider, key string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.ModelsURL, nil)
	if err != nil {
		return nil, err
	}
	switch p.Auth {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+key)
	case "anthropic":
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.Label, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", p.Label, resp.Status)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("%s: unexpected response: %w", p.Label, err)
	}
	out := make([]string, 0, len(body.Data))
	for _, m := range body.Data {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	sort.Strings(out)
	return out, nil
}
