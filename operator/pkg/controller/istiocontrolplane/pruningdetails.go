// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package istiocontrolplane

import (
	"strconv"
	"sync"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/operator/pkg/helmreconciler"
)

const (
	// MetadataNamespace is the namespace for mesh metadata (labels, annotations)
	MetadataNamespace = "install.operator.istio.io"

	// OwnerNameKey represents the name of the owner to which the resource relates
	OwnerNameKey = MetadataNamespace + "/owner-name"
	// OwnerKindKey represents the kind of the owner to which the resource relates
	OwnerKindKey = MetadataNamespace + "/owner-kind"
	// OwnerGroupKey represents the group of the owner to which the resource relates
	OwnerGroupKey = MetadataNamespace + "/owner-group"

	// OwnerGenerationKey represents the generation to which the resource was last reconciled
	OwnerGenerationKey = MetadataNamespace + "/owner-generation"
)

var (
	// watchedResources contains all resources we will watch and reconcile when changed
	// Ideally this would also contain Istio CRDs, but there is a race condition here - we cannot watch
	// a type that does not yet exist.
	watchedResources = []schema.GroupVersionKind{
		{Group: "autoscaling", Version: "v2beta1", Kind: "HorizontalPodAutoscaler"},
		{Group: "policy", Version: "v1beta1", Kind: "PodDisruptionBudget"},
		{Group: "apps", Version: "v1", Kind: "StatefulSet"},
		{Group: "apps", Version: "v1", Kind: "Deployment"},
		{Group: "apps", Version: "v1", Kind: "DaemonSet"},
		{Group: "extensions", Version: "v1beta1", Kind: "Ingress"},
		{Group: "", Version: "v1", Kind: "Service"},
		{Group: "", Version: "v1", Kind: "Endpoints"},
		{Group: "", Version: "v1", Kind: "ConfigMap"},
		{Group: "", Version: "v1", Kind: "PersistentVolumeClaim"},
		{Group: "", Version: "v1", Kind: "Pod"},
		{Group: "", Version: "v1", Kind: "Secret"},
		{Group: "", Version: "v1", Kind: "ServiceAccount"},
		{Group: "rbac.authorization.k8s.io", Version: "v1beta1", Kind: "RoleBinding"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"},
		{Group: "rbac.authorization.k8s.io", Version: "v1beta1", Kind: "Role"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"},
		{Group: "admissionregistration.k8s.io", Version: "v1beta1", Kind: "MutatingWebhookConfiguration"},
		{Group: "admissionregistration.k8s.io", Version: "v1beta1", Kind: "ValidatingWebhookConfiguration"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"},
		{Group: "apiextensions.k8s.io", Version: "v1beta1", Kind: "CustomResourceDefinition"},
	}

	// namespacedResourceMap is the namespaced scoped resource map of Group/Kind/Version as key and bool as value
	// initial value of each Group/Kind/Version is 'false', which will be updated to 'true' if the operator creates the
	// corresponding resource, then the prune process will only try to delete resource of 'true' to accelerate the pruning loop
	// ordered by which types should be deleted, first to last
	namespacedResourceMap = map[schema.GroupVersionKind]bool{
		{Group: "autoscaling", Version: "v2beta1", Kind: "HorizontalPodAutoscaler"}:     false,
		{Group: "policy", Version: "v1beta1", Kind: "PodDisruptionBudget"}:              false,
		{Group: "apps", Version: "v1", Kind: "StatefulSet"}:                             false,
		{Group: "apps", Version: "v1", Kind: "Deployment"}:                              false,
		{Group: "apps", Version: "v1", Kind: "DaemonSet"}:                               false,
		{Group: "extensions", Version: "v1beta1", Kind: "Ingress"}:                      false,
		{Group: "", Version: "v1", Kind: "Service"}:                                     false,
		{Group: "", Version: "v1", Kind: "Endpoints"}:                                   false,
		{Group: "", Version: "v1", Kind: "ConfigMap"}:                                   false,
		{Group: "", Version: "v1", Kind: "PersistentVolumeClaim"}:                       false,
		{Group: "", Version: "v1", Kind: "Pod"}:                                         false,
		{Group: "", Version: "v1", Kind: "Secret"}:                                      false,
		{Group: "", Version: "v1", Kind: "ServiceAccount"}:                              false,
		{Group: "rbac.authorization.k8s.io", Version: "v1beta1", Kind: "RoleBinding"}:   false,
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"}:        false,
		{Group: "rbac.authorization.k8s.io", Version: "v1beta1", Kind: "Role"}:          false,
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"}:               false,
		{Group: "authentication.istio.io", Version: "v1alpha1", Kind: "Policy"}:         false,
		{Group: "certmanager.k8s.io", Version: "v1beta1", Kind: "Certificate"}:          false,
		{Group: "certmanager.k8s.io", Version: "v1beta1", Kind: "Challenge"}:            false,
		{Group: "certmanager.k8s.io", Version: "v1beta1", Kind: "Issuer"}:               false,
		{Group: "certmanager.k8s.io", Version: "v1beta1", Kind: "Order"}:                false,
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "adapter"}:                false,
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "attributemanifest"}:      false,
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "handler"}:                false,
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "instance"}:               false,
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "HTTPAPISpec"}:            false,
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "HTTPAPISpecBinding"}:     false,
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "QuotaSpec"}:              false,
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "QuotaSpecBinding"}:       false,
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "rule"}:                   false,
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "template"}:               false,
		{Group: "networking.istio.io", Version: "v1alpha3", Kind: "DestinationRule"}:    false,
		{Group: "networking.istio.io", Version: "v1alpha3", Kind: "EnvoyFilter"}:        false,
		{Group: "networking.istio.io", Version: "v1alpha3", Kind: "Gateway"}:            false,
		{Group: "networking.istio.io", Version: "v1alpha3", Kind: "ServiceEntry"}:       false,
		{Group: "networking.istio.io", Version: "v1alpha3", Kind: "Sidecar"}:            false,
		{Group: "networking.istio.io", Version: "v1alpha3", Kind: "VirtualService"}:     false,
		{Group: "rbac.istio.io", Version: "v1alpha1", Kind: "ClusterRbacConfig"}:        false,
		{Group: "rbac.istio.io", Version: "v1alpha1", Kind: "RbacConfig"}:               false,
		{Group: "rbac.istio.io", Version: "v1alpha1", Kind: "ServiceRole"}:              false,
		{Group: "rbac.istio.io", Version: "v1alpha1", Kind: "ServiceRoleBinding"}:       false,
		{Group: "security.istio.io", Version: "v1beta1", Kind: "AuthorizationPolicy"}:   false,
		{Group: "security.istio.io", Version: "v1beta1", Kind: "RequestAuthentication"}: false,
	}

	// nonNamespacedResourceMap is the cluster wide resource map of Group/Kind/Version as key and bool as value
	// initial value of each Group/Kind/Version is 'false', which will be updated to 'true' if the operator creates the
	// corresponding resource, then the prune process will only try to delete resource of 'true' to accelerate the pruning loop
	// ordered by which types should be deleted, first to last
	nonNamespacedResourceMap = map[schema.GroupVersionKind]bool{
		{Group: "admissionregistration.k8s.io", Version: "v1beta1", Kind: "MutatingWebhookConfiguration"}:   false,
		{Group: "admissionregistration.k8s.io", Version: "v1beta1", Kind: "ValidatingWebhookConfiguration"}: false,
		{Group: "certmanager.k8s.io", Version: "v1beta1", Kind: "ClusterIssuer"}:                            false,
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}:                            false,
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"}:                     false,
		{Group: "authentication.istio.io", Version: "v1alpha1", Kind: "MeshPolicy"}:                         false,
		//{Group: "apiextensions.k8s.io", Version: "v1beta1", Kind: "CustomResourceDefinition"}: false,
	}
)

// NewPruningDetails creates a new PruningDetails object specific to the instance.
func NewIstioPruningDetails(instance *v1alpha1.IstioOperator) helmreconciler.PruningDetails {
	name := instance.GetName()
	generation := strconv.FormatInt(instance.GetGeneration(), 10)
	return &helmreconciler.SimplePruningDetails{
		OwnerLabels: map[string]string{
			OwnerNameKey:  name,
			OwnerGroupKey: v1alpha1.IstioOperatorGVK.Group,
			OwnerKindKey:  v1alpha1.IstioOperatorGVK.Kind,
		},
		OwnerAnnotations: map[string]string{
			OwnerGenerationKey: generation,
		},
		NamespacedResourceMap:    namespacedResourceMap,
		NonNamespacedResourceMap: nonNamespacedResourceMap,
		PruningDetailsMU:         &sync.Mutex{},
	}
}
