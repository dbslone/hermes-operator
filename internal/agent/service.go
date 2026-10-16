package agent

import (
	"context"
	"fmt"
	"regexp"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var nameRe = regexp.MustCompile(`^[a-z]([-a-z0-9]{0,38}[a-z0-9])?$`)

// Service implements the operations exposed by the UI, REST API and MCP.
type Service struct {
	Client client.Client
	// Namespace is where new agents are created.
	Namespace string
	// ListNamespace limits List to one namespace ("" = all). It must match
	// what the operator's RBAC allows: a namespaced Role cannot list cluster-wide.
	ListNamespace string
}

type CreateRequest struct {
	Name        string                       `json:"name"`
	Model       *Model                       `json:"model,omitempty"`
	Soul        string                       `json:"soul,omitempty"`
	Image       string                       `json:"image,omitempty"`
	Env         map[string]string            `json:"env,omitempty"` // stored in a Secret, e.g. OPENROUTER_API_KEY
	Resources   *corev1.ResourceRequirements `json:"resources,omitempty"`
	StorageSize string                       `json:"storageSize,omitempty"`
	Dashboard   bool                         `json:"dashboard,omitempty"`
}

type Summary struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Phase        string `json:"phase"`
	Ready        bool   `json:"ready"`
	Provider     string `json:"provider,omitempty"`
	Model        string `json:"model,omitempty"`
	Endpoint     string `json:"endpoint,omitempty"`
	APIKeySecret string `json:"apiKeySecret,omitempty"`
	Dashboard    bool   `json:"dashboard"`
	Created      string `json:"created"`
}

func summarize(u *unstructured.Unstructured) Summary {
	spec, _ := SpecOf(u)
	s := Summary{Name: u.GetName(), Namespace: u.GetNamespace(), Dashboard: spec.Dashboard,
		Created: u.GetCreationTimestamp().Format("2006-01-02 15:04")}
	if spec.Model != nil {
		s.Provider, s.Model = spec.Model.Provider, spec.Model.Name
	}
	s.Phase, _, _ = unstructured.NestedString(u.Object, "status", "phase")
	s.Ready, _, _ = unstructured.NestedBool(u.Object, "status", "ready")
	s.Endpoint, _, _ = unstructured.NestedString(u.Object, "status", "endpoint")
	s.APIKeySecret, _, _ = unstructured.NestedString(u.Object, "status", "apiKeySecret")
	if s.Phase == "" {
		s.Phase = "Pending"
	}
	return s
}

func (s *Service) List(ctx context.Context) ([]Summary, error) {
	l := NewList()
	if err := s.Client.List(ctx, l, client.InNamespace(s.ListNamespace)); err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(l.Items))
	for i := range l.Items {
		out = append(out, summarize(&l.Items[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out, nil
}

func (s *Service) Get(ctx context.Context, name string) (*Summary, error) {
	u := New()
	if err := s.Client.Get(ctx, client.ObjectKey{Namespace: s.Namespace, Name: name}, u); err != nil {
		return nil, err
	}
	sum := summarize(u)
	return &sum, nil
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*Summary, error) {
	if !nameRe.MatchString(req.Name) {
		return nil, fmt.Errorf("invalid name %q: use lowercase letters, digits and '-', start with a letter, max 40 chars", req.Name)
	}
	spec := Spec{Image: req.Image, Model: req.Model, Soul: req.Soul, Resources: req.Resources, Dashboard: req.Dashboard}
	if req.StorageSize != "" {
		spec.Storage = &Storage{Size: req.StorageSize}
	}

	var sec *corev1.Secret
	if len(req.Env) > 0 {
		sec = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: req.Name + "-env", Namespace: s.Namespace, Labels: labels(req.Name)},
			StringData: req.Env,
		}
		if err := s.Client.Create(ctx, sec); err != nil {
			return nil, err
		}
		spec.EnvFrom = []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: sec.Name}}}}
	}

	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&spec)
	if err != nil {
		return nil, err
	}
	u := New()
	u.SetName(req.Name)
	u.SetNamespace(s.Namespace)
	u.Object["spec"] = m
	if err := s.Client.Create(ctx, u); err != nil {
		if sec != nil {
			_ = s.Client.Delete(ctx, sec)
		}
		return nil, err
	}
	if sec != nil { // let Kubernetes GC the secret together with the agent
		sec.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(u, GVK)}
		sec.StringData = nil
		if err := s.Client.Update(ctx, sec); err != nil {
			return nil, err
		}
	}
	sum := summarize(u)
	return &sum, nil
}

func (s *Service) Delete(ctx context.Context, name string) error {
	u := New()
	u.SetName(name)
	u.SetNamespace(s.Namespace)
	if err := s.Client.Delete(ctx, u); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}
