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

package helmreconciler

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/object"
	"istio.io/pkg/log"
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
)

var (

	// ordered by which types should be deleted, first to last
	namespacedResources = []schema.GroupVersionKind{
		{Group: "autoscaling", Version: "v2beta1", Kind: "HorizontalPodAutoscaler"},
		{Group: "policy", Version: "v1beta1", Kind: "PodDisruptionBudget"},
		{Group: "apps", Version: "v1", Kind: "StatefulSet"},
		{Group: "apps", Version: "v1", Kind: "Deployment"},
		{Group: "apps", Version: "v1", Kind: "DaemonSet"},
		{Group: "extensions", Version: "v1beta1", Kind: "Ingress"},
		{Group: "", Version: "v1", Kind: "Service"},
		// Endpoints are dynamically created, never from charts.
		// {Group: "", Version: "v1", Kind: "Endpoints"},
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
		{Group: "security.istio.io", Version: "v1beta1", Kind: "PeerAuthentication"},
	}

	// ordered by which types should be deleted, first to last
	nonNamespacedResources = []schema.GroupVersionKind{
		{Group: "admissionregistration.k8s.io", Version: "v1beta1", Kind: "MutatingWebhookConfiguration"},
		{Group: "admissionregistration.k8s.io", Version: "v1beta1", Kind: "ValidatingWebhookConfiguration"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"},
		{Group: "authentication.istio.io", Version: "v1alpha1", Kind: "MeshPolicy"},
		// Cannot currently prune CRDs because this will also wipe out user config.
		// {Group: "apiextensions.k8s.io", Version: "v1beta1", Kind: "CustomResourceDefinition"},
	}
)

// NewPruningDetails creates a new PruningDetails object specific to the instance.
func NewIstioPruningDetails(instance *v1alpha1.IstioOperator) PruningDetails {
	name := instance.GetName()
	return &SimplePruningDetails{
		OwnerLabels: map[string]string{
			OwnerNameKey:  name,
			OwnerGroupKey: v1alpha1.IstioOperatorGVK.Group,
			OwnerKindKey:  v1alpha1.IstioOperatorGVK.Kind,
		},
		NamespacedResources:    namespacedResources,
		NonNamespacedResources: nonNamespacedResources,
	}
}

// Prune removes any resources not specified in manifests generated by HelmReconciler h. If all is set to true, this
// function prunes all resources.
func (h *HelmReconciler) Prune(excluded map[string]bool, all bool) error {
	allErrors := []error{}
	namespacedResources, clusterResources := h.pruningDetails.GetResourceTypes()
	targetNamespace := h.iop.Namespace
	err := h.PruneUnlistedResources(append(namespacedResources, clusterResources...), excluded, all, targetNamespace)
	if all {
		FlushObjectCaches()
	}
	if err != nil {
		allErrors = append(allErrors, err)
	}
	return utilerrors.NewAggregate(allErrors)
}

func (h *HelmReconciler) PruneUnlistedResources(gvks []schema.GroupVersionKind, excluded map[string]bool, all bool, namespace string) error {
	allErrors := []error{}
	ownerLabels := h.pruningDetails.GetOwnerLabels()
	for _, gvk := range gvks {
		objects := &unstructured.UnstructuredList{}
		objects.SetGroupVersionKind(gvk)
		err := h.client.List(context.TODO(), objects, client.MatchingLabels(ownerLabels), client.InNamespace(namespace))
		if err != nil {
			// we only want to retrieve resources clusters
			log.Warnf("retrieving resources to prune type %s: %s not found", gvk.String(), err)
			continue
		}
		for _, o := range objects.Items {
			oh := object.NewK8sObject(&o, nil, nil).Hash()
			if excluded[oh] && !all {
				continue
			}
			if h.opts.DryRun {
				log.Infof("Not pruning object %s because of dry run.", oh)
				continue
			}

			err = h.client.Delete(context.TODO(), &o, client.PropagationPolicy(metav1.DeletePropagationBackground))
			if err != nil {
				allErrors = append(allErrors, err)
			}
			log.Infof("Pruned object %s.", oh)

		}
	}
	return utilerrors.NewAggregate(allErrors)
}
