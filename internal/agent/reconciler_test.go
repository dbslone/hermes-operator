package agent

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var testDefaults = Defaults{
	Image: "hermes:test",
	Resources: corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		},
		Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("4Gi")},
	},
	Storage:       Storage{Size: "2Gi"},
	EnvFromSecret: "shared-keys",
}

func newClient(t *testing.T) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(New()).Build()
}

// reconcile creates/updates the agent with the given spec and reconciles it once.
func reconcile(t *testing.T, c client.Client, name string, spec Spec) {
	t.Helper()
	ctx := context.Background()
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&spec)
	if err != nil {
		t.Fatal(err)
	}
	u := New()
	key := types.NamespacedName{Namespace: "ns", Name: name}
	if err := c.Get(ctx, key, u); err != nil {
		u.SetName(name)
		u.SetNamespace("ns")
		u.Object["spec"] = m
		if err := c.Create(ctx, u); err != nil {
			t.Fatal(err)
		}
	} else {
		u.Object["spec"] = m
		if err := c.Update(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	r := &Reconciler{Client: c, Defaults: testDefaults}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
}

func getSTS(t *testing.T, c client.Client, name string) *appsv1.StatefulSet {
	t.Helper()
	sts := &appsv1.StatefulSet{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: name}, sts); err != nil {
		t.Fatalf("get statefulset: %v", err)
	}
	return sts
}

func TestCreatesOwnedResources(t *testing.T) {
	c := newClient(t)
	reconcile(t, c, "bot", Spec{Model: &Model{Provider: "openrouter", Name: "m"}, Soul: "be brief"})
	ctx := context.Background()
	key := types.NamespacedName{Namespace: "ns", Name: "bot"}

	sts := getSTS(t, c, "bot")
	if len(sts.OwnerReferences) != 1 || sts.OwnerReferences[0].Kind != Kind {
		t.Errorf("statefulset not owned by HermesAgent: %+v", sts.OwnerReferences)
	}
	if *sts.Spec.Replicas != 1 {
		t.Errorf("replicas = %d, want 1", *sts.Spec.Replicas)
	}
	pod := sts.Spec.Template.Spec
	if got := pod.Containers[0].Image; got != "hermes:test" {
		t.Errorf("image = %q, want default", got)
	}
	if got := pod.Containers[0].Args; len(got) != 2 || got[0] != "gateway" || got[1] != "run" {
		t.Errorf("args = %v", got)
	}
	if len(sts.Spec.VolumeClaimTemplates) != 1 ||
		sts.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests.Storage().String() != "2Gi" {
		t.Errorf("unexpected PVC template: %+v", sts.Spec.VolumeClaimTemplates)
	}
	if sts.Spec.PersistentVolumeClaimRetentionPolicy.WhenDeleted != appsv1.DeletePersistentVolumeClaimRetentionPolicyType {
		t.Error("PVC should be deleted with the agent by default")
	}

	if err := c.Get(ctx, key, &corev1.Service{}); err != nil {
		t.Errorf("service: %v", err)
	}
	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "ns", Name: "bot-seed"}, cm); err != nil {
		t.Fatalf("seed configmap: %v", err)
	}
	if cm.Data["SOUL.md"] != "be brief" {
		t.Errorf("SOUL.md = %q", cm.Data["SOUL.md"])
	}
	if want := "model:\n  default: \"m\"\n  provider: \"openrouter\"\n"; cm.Data["config.yaml"] != want {
		t.Errorf("config.yaml = %q, want %q", cm.Data["config.yaml"], want)
	}

	// status
	u := New()
	if err := c.Get(ctx, key, u); err != nil {
		t.Fatal(err)
	}
	if got := summarize(u); got.Phase != "Pending" || got.Endpoint != "http://bot.ns.svc:8642" {
		t.Errorf("status = %+v", got)
	}
}

func TestDefaultResourcesAndOverride(t *testing.T) {
	c := newClient(t)
	reconcile(t, c, "default", Spec{})
	res := getSTS(t, c, "default").Spec.Template.Spec.Containers[0].Resources
	if res.Requests.Cpu().String() != "500m" || res.Requests.Memory().String() != "1Gi" || res.Limits.Memory().String() != "4Gi" {
		t.Errorf("default resources not applied: %+v", res)
	}
	if _, ok := res.Limits[corev1.ResourceCPU]; ok {
		t.Error("default must not set a CPU limit")
	}

	big := &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4"), corev1.ResourceMemory: resource.MustParse("8Gi")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("8"), corev1.ResourceMemory: resource.MustParse("16Gi")},
	}
	reconcile(t, c, "big", Spec{Resources: big})
	res = getSTS(t, c, "big").Spec.Template.Spec.Containers[0].Resources
	if res.Requests.Cpu().String() != "4" || res.Limits.Memory().String() != "16Gi" || res.Limits.Cpu().String() != "8" {
		t.Errorf("override not applied: %+v", res)
	}
}

func TestEnvAndEnvFrom(t *testing.T) {
	c := newClient(t)
	reconcile(t, c, "bot", Spec{
		Image:   "custom:1",
		EnvFrom: []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "mine"}}}},
		Env:     []corev1.EnvVar{{Name: "FOO", Value: "bar"}},
	})
	ct := getSTS(t, c, "bot").Spec.Template.Spec.Containers[0]
	if ct.Image != "custom:1" {
		t.Errorf("image = %q", ct.Image)
	}
	if len(ct.EnvFrom) != 2 || ct.EnvFrom[0].SecretRef.Name != "shared-keys" || ct.EnvFrom[1].SecretRef.Name != "mine" {
		t.Errorf("envFrom = %+v (want shared defaults then agent's)", ct.EnvFrom)
	}
	if !*ct.EnvFrom[0].SecretRef.Optional {
		t.Error("shared secret must be optional so a missing secret doesn't block agents")
	}
	env := map[string]corev1.EnvVar{}
	for _, e := range ct.Env {
		env[e.Name] = e
	}
	if env["API_SERVER_KEY"].ValueFrom == nil || env["API_SERVER_HOST"].Value != "0.0.0.0" || env["FOO"].Value != "bar" {
		t.Errorf("env = %+v", ct.Env)
	}
	if _, ok := env["HERMES_DASHBOARD"]; ok {
		t.Error("dashboard must be off by default")
	}
}

func TestDashboard(t *testing.T) {
	c := newClient(t)
	reconcile(t, c, "bot", Spec{Dashboard: true})
	ct := getSTS(t, c, "bot").Spec.Template.Spec.Containers[0]
	got := map[string]bool{}
	for _, e := range ct.Env {
		got[e.Name] = true
	}
	for _, n := range []string{"HERMES_DASHBOARD", "HERMES_DASHBOARD_BASIC_AUTH_USERNAME", "HERMES_DASHBOARD_BASIC_AUTH_PASSWORD"} {
		if !got[n] {
			t.Errorf("missing env %s (Hermes refuses non-loopback dashboards without auth)", n)
		}
	}
	svc := &corev1.Service{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "bot"}, svc); err != nil {
		t.Fatal(err)
	}
	if len(svc.Spec.Ports) != 2 {
		t.Errorf("service ports = %+v", svc.Spec.Ports)
	}
}

func TestSuspendAndRetain(t *testing.T) {
	c := newClient(t)
	reconcile(t, c, "bot", Spec{RetainData: true})
	if getSTS(t, c, "bot").Spec.PersistentVolumeClaimRetentionPolicy.WhenDeleted != appsv1.RetainPersistentVolumeClaimRetentionPolicyType {
		t.Error("retainData should retain the PVC")
	}
	reconcile(t, c, "bot", Spec{RetainData: true, Suspended: true})
	if r := *getSTS(t, c, "bot").Spec.Replicas; r != 0 {
		t.Errorf("suspended replicas = %d, want 0", r)
	}
	u := New()
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "bot"}, u)
	if p := summarize(u).Phase; p != "Suspended" {
		t.Errorf("phase = %q, want Suspended", p)
	}
}

func TestReconcileIsIdempotentAndPreservesKeys(t *testing.T) {
	c := newClient(t)
	spec := Spec{Soul: "v1"}
	reconcile(t, c, "bot", spec)
	ctx := context.Background()
	secKey := types.NamespacedName{Namespace: "ns", Name: "bot-api"}
	first := &corev1.Secret{}
	if err := c.Get(ctx, secKey, first); err != nil {
		t.Fatal(err)
	}
	if len(first.Data["api-key"]) == 0 || len(first.Data["dashboard-password"]) == 0 {
		t.Fatalf("generated credentials missing: %v", first.Data)
	}
	stsBefore := getSTS(t, c, "bot")

	reconcile(t, c, "bot", spec)
	second := &corev1.Secret{}
	_ = c.Get(ctx, secKey, second)
	if string(first.Data["api-key"]) != string(second.Data["api-key"]) {
		t.Error("api key regenerated on reconcile; clients would break")
	}
	if stsAfter := getSTS(t, c, "bot"); stsAfter.ResourceVersion != stsBefore.ResourceVersion {
		t.Errorf("statefulset rewritten without a spec change (rv %s -> %s)", stsBefore.ResourceVersion, stsAfter.ResourceVersion)
	}
}

func TestSoulChangeRollsPod(t *testing.T) {
	c := newClient(t)
	reconcile(t, c, "bot", Spec{Soul: "v1"})
	h1 := getSTS(t, c, "bot").Spec.Template.Annotations["hermesoperator.io/seed-hash"]
	reconcile(t, c, "bot", Spec{Soul: "v2"})
	h2 := getSTS(t, c, "bot").Spec.Template.Annotations["hermesoperator.io/seed-hash"]
	if h1 == "" || h1 == h2 {
		t.Errorf("seed hash did not change with spec.soul: %q -> %q", h1, h2)
	}
	cm := &corev1.ConfigMap{}
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "bot-seed"}, cm)
	if cm.Data["SOUL.md"] != "v2" {
		t.Errorf("seed configmap not updated: %q", cm.Data["SOUL.md"])
	}
}

func TestRenderConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec Spec
		want string
	}{
		{"none", Spec{}, ""},
		{"model only", Spec{Model: &Model{Name: "m"}}, "model:\n  default: \"m\"\n"},
		{"all fields", Spec{Model: &Model{Provider: "custom", Name: "m", BaseURL: "http://x/v1"}},
			"model:\n  default: \"m\"\n  provider: \"custom\"\n  base_url: \"http://x/v1\"\n"},
		{"raw config wins", Spec{Config: "a: b\n", Model: &Model{Name: "m"}}, "a: b\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderConfig(tc.spec); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEmptyStorageClassMeansClusterDefault(t *testing.T) {
	// The chart renders `storageClassName: ""` by default. Passing that through
	// verbatim makes PVCs unprovisionable, so it must become nil.
	empty := ""
	r := &Reconciler{Defaults: Defaults{Image: "i", Storage: Storage{Size: "1Gi", StorageClassName: &empty}}}
	sts := &appsv1.StatefulSet{}
	sts.Name = "x"
	if err := r.mutateStatefulSet(sts, Spec{}, labels("x"), metav1.OwnerReference{}); err != nil {
		t.Fatal(err)
	}
	if sc := sts.Spec.VolumeClaimTemplates[0].Spec.StorageClassName; sc != nil {
		t.Errorf("storageClassName = %q, want nil (cluster default)", *sc)
	}

	named := "fast"
	reconcileWith(t, "named", Spec{Storage: &Storage{StorageClassName: &named}})
}

func reconcileWith(t *testing.T, name string, spec Spec) {
	t.Helper()
	c := newClient(t)
	reconcile(t, c, name, spec)
	sc := getSTS(t, c, name).Spec.VolumeClaimTemplates[0].Spec.StorageClassName
	if spec.Storage != nil && (sc == nil || *sc != *spec.Storage.StorageClassName) {
		t.Errorf("explicit storage class not honoured: %v", sc)
	}
}
