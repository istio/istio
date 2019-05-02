package v1alpha1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators"
)

const (
	CatalogSourceCRDAPIVersion = operators.GroupName + "/" + GroupVersion
	CatalogSourceKind          = "CatalogSource"
)

// SourceType indicates the type of backing store for a CatalogSource
type SourceType string

const (
	SourceTypeInternal  SourceType = "internal"
	SourceTypeConfigmap SourceType = "configmap"
	SourceTypeGrpc      SourceType = "grpc"
)

type CatalogSourceSpec struct {
	SourceType SourceType `json:"sourceType"`
	ConfigMap  string     `json:"configMap,omitempty"`
	Image      string     `json:"image,omitempty"`
	Secrets    []string   `json:"secrets,omitempty"`

	// Metadata
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	Publisher   string `json:"publisher,omitempty"`
	Icon        Icon   `json:"icon,omitempty"`
}

type RegistryServiceStatus struct {
	Protocol         string      `json:"protocol,omitempty"`
	ServiceName      string      `json:"serviceName,omitempty"`
	ServiceNamespace string      `json:"serviceNamespace,omitempty"`
	Port             string      `json:"port,omitempty"`
	CreatedAt        metav1.Time `json:"createdAt,omitempty"`
}

func (s *RegistryServiceStatus) Address() string {
	return fmt.Sprintf("%s.%s.svc.cluster.local:%s", s.ServiceName, s.ServiceNamespace, s.Port)
}

type CatalogSourceStatus struct {
	ConfigMapResource     *ConfigMapResourceReference `json:"configMapReference,omitempty"`
	RegistryServiceStatus *RegistryServiceStatus      `json:"registryService,omitempty"`
	LastSync              metav1.Time                 `json:"lastSync,omitempty"`
}
type ConfigMapResourceReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`

	UID             types.UID `json:"uid,omitempty"`
	ResourceVersion string    `json:"resourceVersion,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
type CatalogSource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   CatalogSourceSpec   `json:"spec"`
	Status CatalogSourceStatus `json:"status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CatalogSourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []CatalogSource `json:"items"`
}
