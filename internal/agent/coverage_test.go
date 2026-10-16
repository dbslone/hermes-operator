package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestLoadDefaults(t *testing.T) {
	d, err := LoadDefaults("")
	if err != nil || d.Image != DefaultImage {
		t.Fatalf("built-in defaults = %+v, %v", d, err)
	}

	f := filepath.Join(t.TempDir(), "d.yaml")
	body := "image: custom:1\nresources:\n  requests: {cpu: 750m, memory: 2Gi}\nstorage: {size: 10Gi, storageClassName: fast}\nenvFromSecret: keys\n"
	if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err = LoadDefaults(f)
	if err != nil {
		t.Fatal(err)
	}
	if d.Image != "custom:1" || d.Resources.Requests.Cpu().String() != "750m" || d.Storage.Size != "10Gi" ||
		*d.Storage.StorageClassName != "fast" || d.EnvFromSecret != "keys" {
		t.Errorf("defaults = %+v", d)
	}

	if _, err := LoadDefaults(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Error("missing file should error")
	}
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	_ = os.WriteFile(bad, []byte("image: [unclosed"), 0o600)
	if _, err := LoadDefaults(bad); err == nil {
		t.Error("invalid yaml should error")
	}
}

func TestReconcileNotFoundAndDeleting(t *testing.T) {
	c := newClient(t)
	r := &Reconciler{Client: c, Defaults: testDefaults}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "ns", Name: "gone"}}); err != nil {
		t.Errorf("missing agent must not error (deleted before reconcile): %v", err)
	}

	// An agent that is being deleted must not have children recreated.
	u := New()
	u.SetName("dying")
	u.SetNamespace("ns")
	u.SetFinalizers([]string{"test/hold"})
	u.Object["spec"] = map[string]interface{}{}
	ctx := context.Background()
	if err := c.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, u); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "ns", Name: "dying"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "ns", Name: "dying"}, &appsv1.StatefulSet{}); !apierrors.IsNotFound(err) {
		t.Errorf("statefulset created for a deleting agent: %v", err)
	}
}

func TestReconcileInvalidSpecReportsFailed(t *testing.T) {
	c := newClient(t)
	u := New()
	u.SetName("bad")
	u.SetNamespace("ns")
	u.Object["spec"] = map[string]interface{}{"dashboard": "not-a-bool"}
	ctx := context.Background()
	if err := c.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	r := &Reconciler{Client: c, Defaults: testDefaults}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "ns", Name: "bad"}}); err != nil {
		t.Fatal(err)
	}
	got := New()
	_ = c.Get(ctx, types.NamespacedName{Namespace: "ns", Name: "bad"}, got)
	if p := summarize(got).Phase; p != "Failed" {
		t.Errorf("phase = %q, want Failed", p)
	}
}

func TestReadyWhenStatefulSetReady(t *testing.T) {
	c := newClient(t)
	reconcile(t, c, "bot", Spec{})
	ctx := context.Background()
	sts := getSTS(t, c, "bot")
	sts.Status.ReadyReplicas = 1
	if err := c.Status().Update(ctx, sts); err != nil {
		t.Fatal(err)
	}
	reconcile(t, c, "bot", Spec{})
	u := New()
	_ = c.Get(ctx, types.NamespacedName{Namespace: "ns", Name: "bot"}, u)
	if s := summarize(u); s.Phase != "Ready" || !s.Ready {
		t.Errorf("summary = %+v, want Ready", s)
	}
}

func TestSpecOfRejectsNonObjectSpec(t *testing.T) {
	u := New()
	u.Object["spec"] = "oops"
	if _, err := SpecOf(u); err == nil {
		t.Error("expected error for non-object spec")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if firstNonEmpty("", "") != "" || firstNonEmpty("", "b", "c") != "b" {
		t.Error("firstNonEmpty")
	}
}

// failingClient returns a client whose Create/Delete/Update/List fail on demand.
func failingClient(t *testing.T, f interceptor.Funcs) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	return fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(New()).WithInterceptorFuncs(f).Build()
}

func TestServiceErrorPaths(t *testing.T) {
	boom := errors.New("boom")
	ctx := context.Background()

	t.Run("list error", func(t *testing.T) {
		svc := &Service{Namespace: "ns", Client: failingClient(t, interceptor.Funcs{
			List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error { return boom }})}
		if _, err := svc.List(ctx); !errors.Is(err, boom) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("secret create error aborts before the agent exists", func(t *testing.T) {
		c := failingClient(t, interceptor.Funcs{
			Create: func(ctx context.Context, cl client.WithWatch, o client.Object, opts ...client.CreateOption) error {
				if _, ok := o.(*metav1.PartialObjectMetadata); !ok && o.GetName() == "bot-env" {
					return boom
				}
				return cl.Create(ctx, o, opts...)
			}})
		svc := &Service{Namespace: "ns", Client: c}
		if _, err := svc.Create(ctx, CreateRequest{Name: "bot", Env: map[string]string{"K": "v"}}); !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
		if _, err := svc.Get(ctx, "bot"); !apierrors.IsNotFound(err) {
			t.Errorf("agent must not exist when its secret failed: %v", err)
		}
	})

	t.Run("owner-reference update error is surfaced", func(t *testing.T) {
		c := failingClient(t, interceptor.Funcs{
			Update: func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error { return boom }})
		svc := &Service{Namespace: "ns", Client: c}
		if _, err := svc.Create(ctx, CreateRequest{Name: "bot", Env: map[string]string{"K": "v"}}); !errors.Is(err, boom) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("delete error is surfaced but not-found is not", func(t *testing.T) {
		c := failingClient(t, interceptor.Funcs{
			Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error { return boom }})
		if err := (&Service{Namespace: "ns", Client: c}).Delete(ctx, "x"); !errors.Is(err, boom) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("storage size is honoured", func(t *testing.T) {
		c := newClient(t)
		svc := &Service{Namespace: "ns", Client: c}
		if _, err := svc.Create(ctx, CreateRequest{Name: "bot", StorageSize: "10Gi", Image: "x:1", Dashboard: true}); err != nil {
			t.Fatal(err)
		}
		reconcileExisting(t, c, "bot")
		if got := getSTS(t, c, "bot").Spec.VolumeClaimTemplates[0].Spec.Resources.Requests.Storage().String(); got != "10Gi" {
			t.Errorf("storage = %s", got)
		}
	})
}

func reconcileExisting(t *testing.T, c client.Client, name string) {
	t.Helper()
	r := &Reconciler{Client: c, Defaults: testDefaults}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "ns", Name: name}}); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileSurfacesAPIErrors(t *testing.T) {
	boom := errors.New("boom")
	for _, kind := range []string{"Secret", "ConfigMap", "Service", "StatefulSet"} {
		t.Run(kind, func(t *testing.T) {
			c := failingClient(t, interceptor.Funcs{
				Create: func(ctx context.Context, cl client.WithWatch, o client.Object, opts ...client.CreateOption) error {
					if typeName(o) == kind {
						return boom
					}
					return cl.Create(ctx, o, opts...)
				}})
			u := New()
			u.SetName("bot")
			u.SetNamespace("ns")
			u.Object["spec"] = map[string]interface{}{}
			if err := c.Create(context.Background(), u); err != nil {
				t.Fatal(err)
			}
			r := &Reconciler{Client: c, Defaults: testDefaults}
			_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "ns", Name: "bot"}})
			if !errors.Is(err, boom) {
				t.Errorf("creating %s failed but Reconcile returned %v (would not be retried)", kind, err)
			}
		})
	}
}

func typeName(o client.Object) string { return reflectName(o) }
