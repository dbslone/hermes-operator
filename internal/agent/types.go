// Package agent holds the HermesAgent API types, the reconciler and the
// create/list/delete service shared by the UI, REST API and MCP endpoint.
//
// HermesAgent is handled as an unstructured object so the repo needs no code
// generation: the CRD YAML in the chart is the single schema definition.
package agent

import (
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

const (
	Group   = "hermesoperator.io"
	Version = "v1alpha1"
	Kind    = "HermesAgent"

	APIPort       = 8642
	DashboardPort = 9119
)

var GVK = schema.GroupVersionKind{Group: Group, Version: Version, Kind: Kind}

// Spec is the HermesAgent spec.
type Spec struct {
	// Image overrides the operator-wide default Hermes image.
	Image string `json:"image,omitempty"`
	// Model is rendered into config.yaml on first boot.
	Model *Model `json:"model,omitempty"`
	// Config is a full config.yaml; when set it replaces the generated one.
	Config string `json:"config,omitempty"`
	// Soul is written to SOUL.md on first boot.
	Soul string `json:"soul,omitempty"`
	// Env and EnvFrom are passed to the container (API keys etc).
	Env     []corev1.EnvVar        `json:"env,omitempty"`
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`
	// Resources overrides the operator-wide default requests/limits.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	Storage   *Storage                     `json:"storage,omitempty"`
	// Dashboard also starts the Hermes web dashboard (port 9119).
	Dashboard bool `json:"dashboard,omitempty"`
	// Suspended scales the agent to zero and keeps its data.
	Suspended bool `json:"suspended,omitempty"`
	// RetainData keeps the PVC when the agent is deleted.
	RetainData bool `json:"retainData,omitempty"`
}

type Model struct {
	Provider string `json:"provider,omitempty"`
	Name     string `json:"name,omitempty"`
	BaseURL  string `json:"baseURL,omitempty"`
}

type Storage struct {
	Size             string  `json:"size,omitempty"`
	StorageClassName *string `json:"storageClassName,omitempty"`
}

// Defaults are operator-wide values applied to agents that don't set their own.
type Defaults struct {
	Image         string                      `json:"image,omitempty"`
	Resources     corev1.ResourceRequirements `json:"resources,omitempty"`
	Storage       Storage                     `json:"storage,omitempty"`
	EnvFromSecret string                      `json:"envFromSecret,omitempty"`
}

// DefaultImage is used when neither the agent nor the defaults file sets one.
const DefaultImage = "nousresearch/hermes-agent:stable"

// LoadDefaults reads operator-wide defaults from a YAML file. An empty path
// returns built-in defaults.
func LoadDefaults(path string) (Defaults, error) {
	d := Defaults{Image: DefaultImage}
	if path == "" {
		return d, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return d, err
	}
	if err := yaml.Unmarshal(b, &d); err != nil {
		return d, err
	}
	return d, nil
}

func New() *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(GVK)
	return u
}

func NewList() *unstructured.UnstructuredList {
	l := &unstructured.UnstructuredList{}
	l.SetGroupVersionKind(schema.GroupVersionKind{Group: Group, Version: Version, Kind: Kind + "List"})
	return l
}

func SpecOf(u *unstructured.Unstructured) (Spec, error) {
	var s Spec
	m, _, err := unstructured.NestedMap(u.Object, "spec")
	if err != nil {
		return s, err
	}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(m, &s)
	return s, err
}
