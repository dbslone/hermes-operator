package agent

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

func TestCreateValidatesName(t *testing.T) {
	svc := &Service{Client: newClient(t), Namespace: "ns"}
	for _, bad := range []string{"", "Upper", "1abc", "has_underscore", "-lead", "trail-", strings.Repeat("a", 41)} {
		if _, err := svc.Create(context.Background(), CreateRequest{Name: bad}); err == nil {
			t.Errorf("name %q should be rejected", bad)
		}
	}
	for _, ok := range []string{"a", "bot", "research-bot-2", strings.Repeat("a", 40)} {
		if _, err := svc.Create(context.Background(), CreateRequest{Name: ok}); err != nil {
			t.Errorf("name %q should be accepted: %v", ok, err)
		}
	}
}

func TestCreateStoresKeysInOwnedSecret(t *testing.T) {
	c := newClient(t)
	svc := &Service{Client: c, Namespace: "ns"}
	ctx := context.Background()
	sum, err := svc.Create(ctx, CreateRequest{
		Name:  "bot",
		Model: &Model{Provider: "openrouter", Name: "m"},
		Env:   map[string]string{"OPENROUTER_API_KEY": "sk-secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Provider != "openrouter" || sum.Model != "m" || sum.Phase != "Pending" {
		t.Errorf("summary = %+v", sum)
	}

	sec := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "ns", Name: "bot-env"}, sec); err != nil {
		t.Fatal(err)
	}
	if len(sec.OwnerReferences) != 1 || sec.OwnerReferences[0].Kind != Kind {
		t.Errorf("secret should be owned by the agent so it is garbage-collected: %+v", sec.OwnerReferences)
	}

	// The key must reach the pod only through envFrom, never via the CR spec.
	u := New()
	_ = c.Get(ctx, types.NamespacedName{Namespace: "ns", Name: "bot"}, u)
	spec, _ := SpecOf(u)
	if len(spec.EnvFrom) != 1 || spec.EnvFrom[0].SecretRef.Name != "bot-env" {
		t.Errorf("envFrom = %+v", spec.EnvFrom)
	}
	if strings.Contains(strings.ToLower(toJSON(t, u.Object)), "sk-secret") {
		t.Error("API key leaked into the HermesAgent object")
	}
}

func TestCreateDuplicateRollsBackSecret(t *testing.T) {
	c := newClient(t)
	svc := &Service{Client: c, Namespace: "ns"}
	ctx := context.Background()
	if _, err := svc.Create(ctx, CreateRequest{Name: "bot"}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Create(ctx, CreateRequest{Name: "bot", Env: map[string]string{"K": "v"}})
	if !apierrors.IsAlreadyExists(err) {
		t.Fatalf("expected AlreadyExists, got %v", err)
	}
	sec := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "ns", Name: "bot-env"}, sec); !apierrors.IsNotFound(err) {
		t.Errorf("secret from the failed create should be rolled back, got %v", err)
	}
}

func TestListGetDelete(t *testing.T) {
	svc := &Service{Client: newClient(t), Namespace: "ns"}
	ctx := context.Background()
	for _, n := range []string{"a", "b"} {
		if _, err := svc.Create(ctx, CreateRequest{Name: n}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := svc.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v, err %v", list, err)
	}
	if _, err := svc.Get(ctx, "a"); err != nil {
		t.Error(err)
	}
	if err := svc.Delete(ctx, "a"); err != nil {
		t.Error(err)
	}
	if _, err := svc.Get(ctx, "a"); !apierrors.IsNotFound(err) {
		t.Errorf("expected not found after delete, got %v", err)
	}
	if err := svc.Delete(ctx, "a"); err != nil {
		t.Errorf("delete should be idempotent: %v", err)
	}
}

func TestListIsScopedToListNamespace(t *testing.T) {
	// With a namespaced Role the operator may only list its own namespace;
	// an unscoped List would be a cluster-wide request and get Forbidden.
	c := newClient(t)
	ctx := context.Background()
	other := New()
	other.SetName("elsewhere")
	other.SetNamespace("other")
	other.Object["spec"] = map[string]interface{}{}
	if err := c.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	mine := &Service{Client: c, Namespace: "ns", ListNamespace: "ns"}
	if _, err := mine.Create(ctx, CreateRequest{Name: "mine"}); err != nil {
		t.Fatal(err)
	}
	list, _ := mine.List(ctx)
	if len(list) != 1 || list[0].Name != "mine" {
		t.Errorf("scoped list = %+v, want only [mine]", list)
	}
	all, _ := (&Service{Client: c, Namespace: "ns"}).List(ctx)
	if len(all) != 2 {
		t.Errorf("unscoped list = %d agents, want 2", len(all))
	}
}
