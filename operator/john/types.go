package john

import (
	"encoding/json"

	appsv1 "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1"
)

type ComponentSpec struct {
	// Selects whether this component is installed.
	Enabled *BoolValue `json:"enabled,omitempty"`
	// Namespace for the component.
	Namespace string `json:"namespace,omitempty"`
	// Name for the component.
	Name string `json:"name,omitempty"`
	// Labels for the component.
	Label map[string]string `json:"label,omitempty"`
	// Hub for the component (overrides top level hub setting).
	Hub string `json:"hub,omitempty"`
	// Tag for the component (overrides top level tag setting).
	// This is "any" because people put `tag: 1` in their config and we supported that historically.
	Tag any `json:"tag,omitempty"`
	// Kubernetes resource spec.
	Kubernetes *KubernetesResources `json:"k8s,omitempty"`
	// Raw is the raw inputs. This allows distinguishing unset vs zero-values for KubernetesResources
	Raw Map `json:"-"`
}

// KubernetesResources is a common set of Kubernetes resource configs for components.
type KubernetesResources struct {
	// Kubernetes affinity.
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// Deployment environment variables.
	Env []*corev1.EnvVar `json:"env,omitempty"`
	// Kubernetes HorizontalPodAutoscaler settings.
	HpaSpec *autoscaling.HorizontalPodAutoscalerSpec `json:"hpaSpec,omitempty"`
	// Kubernetes imagePullPolicy.
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`
	// Kubernetes nodeSelector.
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Kubernetes PodDisruptionBudget settings.
	PodDisruptionBudget *policy.PodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	// Kubernetes pod annotations.
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`
	// Kubernetes priorityClassName. Default for all resources unless overridden.
	PriorityClassName string `json:"priorityClassName,omitempty"`
	// Kubernetes readinessProbe settings.
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`
	// Kubernetes Deployment replicas setting.
	ReplicaCount uint32 `json:"replicaCount,omitempty"`
	// Kubernetes resources settings.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// Kubernetes Service settings.
	Service *corev1.ServiceSpec `json:"service,omitempty"`
	// Kubernetes deployment strategy.
	Strategy *appsv1.DeploymentStrategy `json:"strategy,omitempty"`
	// Kubernetes toleration
	Tolerations []*corev1.Toleration `json:"tolerations,omitempty"`
	// Kubernetes service annotations.
	ServiceAnnotations map[string]string `json:"serviceAnnotations,omitempty"`
	// Kubernetes pod security context
	SecurityContext *corev1.PodSecurityContext `json:"securityContext,omitempty"`
	// Kubernetes volumes
	// Volumes defines the collection of Volume to inject into the pod.
	Volumes []*corev1.Volume `json:"volumes,omitempty"`
	// Kubernetes volumeMounts
	// VolumeMounts defines the collection of VolumeMount to inject into containers.
	VolumeMounts []*corev1.VolumeMount `json:"volumeMounts,omitempty"`
	// Overlays for Kubernetes resources in rendered manifests.
	Overlays []KubernetesOverlay `json:"overlays,omitempty"`
}

// KubernetesOverlay for an existing Kubernetes resource.
type KubernetesOverlay struct {
	// Resource API version.
	ApiVersion string `json:"apiVersion,omitempty"`
	// Resource kind.
	Kind string `json:"kind,omitempty"`
	// Name of resource.
	// Namespace is always the component namespace.
	Name string `json:"name,omitempty"`
	// List of patches to apply to resource.
	Patches []Patch `json:"patches,omitempty"`
}

type Patch struct {
	// Path of the form a.[key1:value1].b.[:value2]
	// Where [key1:value1] is a selector for a key-value pair to identify a list element and [:value] is a value
	// selector to identify a list element in a leaf list.
	// All path intermediate nodes must exist.
	Path string `json:"path,omitempty"`
	// Value to add, delete or replace.
	// For add, the path should be a new leaf.
	// For delete, value should be unset.
	// For replace, path should reference an existing node.
	// All values are strings but are converted into appropriate type based on schema.
	Value any `json:"value,omitempty"`
}

type BoolValue struct {
	bool
}

func (b *BoolValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.GetValueOrFalse())
}

func (b *BoolValue) UnmarshalJSON(bytes []byte) error {
	bb := false
	if err := json.Unmarshal(bytes, &bb); err != nil {
		return err
	}
	*b = BoolValue{bb}
	return nil
}

func (b *BoolValue) GetValueOrFalse() bool {
	if b == nil {
		return false
	}
	return b.bool
}

func (b *BoolValue) GetValueOrTrue() bool {
	if b == nil {
		return true
	}
	return b.bool
}

var (
	_ json.Unmarshaler = &BoolValue{}
	_ json.Marshaler   = &BoolValue{}
)
