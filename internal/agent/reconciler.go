package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	hashAnnotation = "hermesoperator.io/template-hash"
	hermesUID      = 10000
)

type Reconciler struct {
	client.Client
	Defaults Defaults
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(New()).
		Owns(&appsv1.StatefulSet{}).
		Complete(r)
}

func labels(name string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "hermes-agent",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/managed-by": "hermes-operator",
	}
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	u := New()
	if err := r.Get(ctx, req.NamespacedName, u); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !u.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, nil
	}
	spec, err := SpecOf(u)
	if err != nil {
		return ctrl.Result{}, r.setStatus(ctx, u, "Failed", false, "invalid spec: "+err.Error())
	}

	name, ns := u.GetName(), u.GetNamespace()
	owner := *metav1.NewControllerRef(u, GVK)
	lbl := labels(name)

	// Generated credentials for the agent's API server / dashboard. Created
	// once and never overwritten so clients keep working across reconciles.
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name + "-api", Namespace: ns}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		secret.Labels, secret.OwnerReferences = lbl, []metav1.OwnerReference{owner}
		if secret.Data == nil {
			secret.Data = map[string][]byte{}
		}
		for _, k := range []string{"api-key", "dashboard-password"} {
			if len(secret.Data[k]) == 0 {
				secret.Data[k] = []byte(randomToken())
			}
		}
		return nil
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Seed files (config.yaml / SOUL.md), copied into the data volume on first boot only.
	seed := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name + "-seed", Namespace: ns}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, seed, func() error {
		seed.Labels, seed.OwnerReferences = lbl, []metav1.OwnerReference{owner}
		seed.Data = map[string]string{}
		if cfg := renderConfig(spec); cfg != "" {
			seed.Data["config.yaml"] = cfg
		}
		if spec.Soul != "" {
			seed.Data["SOUL.md"] = spec.Soul
		}
		return nil
	}); err != nil {
		return ctrl.Result{}, err
	}

	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Labels, svc.OwnerReferences = lbl, []metav1.OwnerReference{owner}
		svc.Spec.Selector = lbl
		svc.Spec.Ports = []corev1.ServicePort{{Name: "api", Port: APIPort, TargetPort: intstr.FromInt32(APIPort)}}
		if spec.Dashboard {
			svc.Spec.Ports = append(svc.Spec.Ports,
				corev1.ServicePort{Name: "dashboard", Port: DashboardPort, TargetPort: intstr.FromInt32(DashboardPort)})
		}
		return nil
	}); err != nil {
		return ctrl.Result{}, err
	}

	sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, sts, func() error {
		return r.mutateStatefulSet(sts, spec, lbl, owner)
	}); err != nil {
		return ctrl.Result{}, err
	}

	phase, msg := "Pending", ""
	ready := sts.Status.ReadyReplicas > 0
	switch {
	case spec.Suspended:
		phase = "Suspended"
	case ready:
		phase = "Ready"
	}
	return ctrl.Result{}, r.setStatus(ctx, u, phase, ready, msg)
}

func (r *Reconciler) mutateStatefulSet(sts *appsv1.StatefulSet, spec Spec, lbl map[string]string, owner metav1.OwnerReference) error {
	name := sts.Name
	image := firstNonEmpty(spec.Image, r.Defaults.Image)
	res := r.Defaults.Resources
	if spec.Resources != nil {
		res = *spec.Resources
	}
	storage := r.Defaults.Storage
	if spec.Storage != nil {
		if spec.Storage.Size != "" {
			storage.Size = spec.Storage.Size
		}
		if spec.Storage.StorageClassName != nil {
			storage.StorageClassName = spec.Storage.StorageClassName
		}
	}
	if storage.Size == "" {
		storage.Size = "2Gi"
	}
	// "" means "cluster default class" to users, but an explicit empty
	// storageClassName on a PVC disables dynamic provisioning.
	if storage.StorageClassName != nil && *storage.StorageClassName == "" {
		storage.StorageClassName = nil
	}

	env := []corev1.EnvVar{
		{Name: "API_SERVER_ENABLED", Value: "true"},
		{Name: "API_SERVER_HOST", Value: "0.0.0.0"},
		{Name: "API_SERVER_KEY", ValueFrom: secretKey(name+"-api", "api-key")},
	}
	ports := []corev1.ContainerPort{{Name: "api", ContainerPort: APIPort}}
	if spec.Dashboard {
		env = append(env,
			corev1.EnvVar{Name: "HERMES_DASHBOARD", Value: "1"},
			corev1.EnvVar{Name: "HERMES_DASHBOARD_BASIC_AUTH_USERNAME", Value: "admin"},
			corev1.EnvVar{Name: "HERMES_DASHBOARD_BASIC_AUTH_PASSWORD", ValueFrom: secretKey(name+"-api", "dashboard-password")})
		ports = append(ports, corev1.ContainerPort{Name: "dashboard", ContainerPort: DashboardPort})
	}
	env = append(env, spec.Env...)

	var envFrom []corev1.EnvFromSource
	if s := r.Defaults.EnvFromSecret; s != "" {
		envFrom = append(envFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: s}, Optional: ptr(true)}})
	}
	envFrom = append(envFrom, spec.EnvFrom...)

	tmpl := corev1.PodTemplateSpec{
		// The seed hash restarts the pod when spec.soul / spec.config change.
		ObjectMeta: metav1.ObjectMeta{Labels: lbl, Annotations: map[string]string{
			"hermesoperator.io/seed-hash": shortHash(renderConfig(spec) + "\x00" + spec.Soul)}},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{
				Name:            "seed",
				Image:           image,
				Command:         []string{"sh", "-c", seedScript},
				SecurityContext: &corev1.SecurityContext{RunAsUser: ptr(int64(0))},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "data", MountPath: "/opt/data"},
					{Name: "seed", MountPath: "/seed"},
				},
			}},
			Containers: []corev1.Container{{
				Name:         "hermes",
				Image:        image,
				Args:         []string{"gateway", "run"},
				Env:          env,
				EnvFrom:      envFrom,
				Ports:        ports,
				Resources:    res,
				VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/opt/data"}},
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{
						Path: "/health", Port: intstr.FromInt32(APIPort)}},
					InitialDelaySeconds: 10, PeriodSeconds: 10, FailureThreshold: 6,
				},
			}},
			Volumes: []corev1.Volume{{Name: "seed", VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: name + "-seed"}}}}},
		},
	}

	// Only touch the pod template when the desired state changed; the API
	// server defaults many fields, so comparing against the live object would
	// cause a no-op update on every reconcile.
	raw, _ := json.Marshal(tmpl)
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:8])

	sts.Labels, sts.OwnerReferences = lbl, []metav1.OwnerReference{owner}
	replicas := int32(1)
	if spec.Suspended {
		replicas = 0
	}
	sts.Spec.Replicas = &replicas
	retention := appsv1.DeletePersistentVolumeClaimRetentionPolicyType
	if spec.RetainData {
		retention = appsv1.RetainPersistentVolumeClaimRetentionPolicyType
	}
	sts.Spec.PersistentVolumeClaimRetentionPolicy = &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
		WhenDeleted: retention, WhenScaled: appsv1.RetainPersistentVolumeClaimRetentionPolicyType}

	if sts.CreationTimestamp.IsZero() { // immutable fields
		sts.Spec.ServiceName = name
		sts.Spec.Selector = &metav1.LabelSelector{MatchLabels: lbl}
		sts.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{{
			ObjectMeta: metav1.ObjectMeta{Name: "data"},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				StorageClassName: storage.StorageClassName,
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(storage.Size)}},
			},
		}}
	}
	if sts.Annotations[hashAnnotation] != hash {
		sts.Spec.Template = tmpl
		if sts.Annotations == nil {
			sts.Annotations = map[string]string{}
		}
		sts.Annotations[hashAnnotation] = hash
	}
	return nil
}

func (r *Reconciler) setStatus(ctx context.Context, u *unstructured.Unstructured, phase string, ready bool, msg string) error {
	status := map[string]interface{}{
		"phase":        phase,
		"ready":        ready,
		"message":      msg,
		"endpoint":     fmt.Sprintf("http://%s.%s.svc:%d", u.GetName(), u.GetNamespace(), APIPort),
		"apiKeySecret": u.GetName() + "-api",
	}
	if old, _, _ := unstructured.NestedMap(u.Object, "status"); fmt.Sprint(old) == fmt.Sprint(status) {
		return nil
	}
	u.Object["status"] = status
	return r.Status().Update(ctx, u)
}

// seedScript applies spec.config / spec.soul to the data volume. A file is
// (re)written only when its content in the spec changed since it was last
// applied (tracked by a hash marker on the volume) or it doesn't exist yet.
// Edits made inside the pod therefore persist until the spec itself changes.
const seedScript = `set -e
chown 10000:10000 /opt/data
for f in config.yaml SOUL.md; do
  [ -f "/seed/$f" ] || continue
  marker="/opt/data/.operator-seed-$f.sha256"
  new=$(sha256sum "/seed/$f" | cut -d' ' -f1)
  old=$(cat "$marker" 2>/dev/null || true)
  if [ "$new" != "$old" ] || [ ! -f "/opt/data/$f" ]; then
    cp "/seed/$f" "/opt/data/$f"
    echo "$new" > "$marker"
    chown 10000:10000 "/opt/data/$f" "$marker"
  fi
done`

func renderConfig(s Spec) string {
	if s.Config != "" {
		return s.Config
	}
	if s.Model == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("model:\n")
	for _, kv := range [][2]string{{"default", s.Model.Name}, {"provider", s.Model.Provider}, {"base_url", s.Model.BaseURL}} {
		if kv[1] != "" {
			fmt.Fprintf(&b, "  %s: %q\n", kv[0], kv[1])
		}
	}
	return b.String()
}

func secretKey(name, key string) *corev1.EnvVarSource {
	return &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: name}, Key: key}}
}

func randomToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func ptr[T any](v T) *T { return &v }
