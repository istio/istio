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

	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/helmreconciler"
	"istio.io/operator/pkg/util"
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

	// ordered by which types should be deleted, first to last
	namespacedResources = []schema.GroupVersionKind{
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
		{Group: "authentication.istio.io", Version: "v1alpha1", Kind: "Policy"},
		{Group: "certmanager.k8s.io", Version: "v1beta1", Kind: "Certificate"},
		{Group: "certmanager.k8s.io", Version: "v1beta1", Kind: "Challenge"},
		{Group: "certmanager.k8s.io", Version: "v1beta1", Kind: "Issuer"},
		{Group: "certmanager.k8s.io", Version: "v1beta1", Kind: "Order"},
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "adapter"},
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "attributemanifest"},
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "handler"},
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "instance"},
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "HTTPAPISpec"},
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "HTTPAPISpecBinding"},
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "QuotaSpec"},
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "QuotaSpecBinding"},
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "rule"},
		{Group: "config.istio.io", Version: "v1alpha2", Kind: "template"},
		{Group: "networking.istio.io", Version: "v1alpha3", Kind: "DestinationRule"},
		{Group: "networking.istio.io", Version: "v1alpha3", Kind: "EnvoyFilter"},
		{Group: "networking.istio.io", Version: "v1alpha3", Kind: "Gateway"},
		{Group: "networking.istio.io", Version: "v1alpha3", Kind: "ServiceEntry"},
		{Group: "networking.istio.io", Version: "v1alpha3", Kind: "Sidecar"},
		{Group: "networking.istio.io", Version: "v1alpha3", Kind: "VirtualService"},
		{Group: "rbac.istio.io", Version: "v1alpha1", Kind: "ClusterRbacConfig"},
		{Group: "rbac.istio.io", Version: "v1alpha1", Kind: "RbacConfig"},
		{Group: "rbac.istio.io", Version: "v1alpha1", Kind: "ServiceRole"},
		{Group: "rbac.istio.io", Version: "v1alpha1", Kind: "ServiceRoleBinding"},
		{Group: "security.istio.io", Version: "v1beta1", Kind: "AuthorizationPolicy"},
		{Group: "security.istio.io", Version: "v1beta1", Kind: "RequestAuthentication"},
	}

	// ordered by which types should be deleted, first to last
	nonNamespacedResources = []schema.GroupVersionKind{
		{Group: "admissionregistration.k8s.io", Version: "v1beta1", Kind: "MutatingWebhookConfiguration"},
		{Group: "admissionregistration.k8s.io", Version: "v1beta1", Kind: "ValidatingWebhookConfiguration"},
		{Group: "certmanager.k8s.io", Version: "v1beta1", Kind: "ClusterIssuer"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"},
		{Group: "authentication.istio.io", Version: "v1alpha1", Kind: "MeshPolicy"},
		{Group: "apiextensions.k8s.io", Version: "v1beta1", Kind: "CustomResourceDefinition"},
	}
)

// NewPruningDetails creates a new PruningDetails object specific to the instance.
func NewIstioPruningDetails(instance *v1alpha2.IstioControlPlane) helmreconciler.PruningDetails {
	name := instance.GetName()
	generation := strconv.FormatInt(instance.GetGeneration(), 10)
	return &helmreconciler.SimplePruningDetails{
		OwnerLabels: map[string]string{
			OwnerNameKey:  name,
			OwnerGroupKey: util.IstioOperatorGVK.Group,
			OwnerKindKey:  util.IstioOperatorGVK.Kind,
		},
		OwnerAnnotations: map[string]string{
			OwnerGenerationKey: generation,
		},
		NamespacedResources:    namespacedResources,
		NonNamespacedResources: nonNamespacedResources,
	}
}
