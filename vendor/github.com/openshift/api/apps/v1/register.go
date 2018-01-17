package v1

import (
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	LegacyGroupName = ""
	GroupName       = "apps.openshift.io"
)

var (
	SchemeGroupVersion       = schema.GroupVersion{Group: GroupName, Version: "v1"}
	LegacySchemeGroupVersion = schema.GroupVersion{Group: LegacyGroupName, Version: "v1"}

	LegacySchemeBuilder    = runtime.NewSchemeBuilder(addLegacyKnownTypes)
	AddToSchemeInCoreGroup = LegacySchemeBuilder.AddToScheme

	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
	AddToScheme   = SchemeBuilder.AddToScheme
)

func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

// Adds the list of known types to api.Scheme.
func addLegacyKnownTypes(scheme *runtime.Scheme) error {
	types := []runtime.Object{
		&DeploymentConfig{},
		&DeploymentConfigList{},
		&DeploymentConfigRollback{},
		&DeploymentRequest{},
		&DeploymentLog{},
		&DeploymentLogOptions{},
		&extensionsv1beta1.Scale{},
	}
	scheme.AddKnownTypes(LegacySchemeGroupVersion, types...)
	return nil
}

// Adds the list of known types to api.Scheme.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&DeploymentConfig{},
		&DeploymentConfigList{},
		&DeploymentConfigRollback{},
		&DeploymentRequest{},
		&DeploymentLog{},
		&DeploymentLogOptions{},
		&extensionsv1beta1.Scale{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}
