// Copyright Istio Authors
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

package john

import (
	"context"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"

	"istio.io/api/label"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/ptr"
)

const (
	// MetadataNamespace is the namespace for mesh metadata (labels, annotations)
	MetadataNamespace = "install.operator.istio.io"
	// OwningResourceName represents the name of the owner to which the resource relates
	OwningResourceName = MetadataNamespace + "/owning-resource"
	// OwningResourceNamespace represents the namespace of the owner to which the resource relates
	OwningResourceNamespace = MetadataNamespace + "/owning-resource-namespace"
	// OwningResourceNotPruned indicates that the resource should not be pruned during reconciliation cycles,
	// note this will not prevent the resource from being deleted if the owning resource is deleted.
	OwningResourceNotPruned = MetadataNamespace + "/owning-resource-not-pruned"
	// operatorLabelStr indicates Istio operator is managing this resource.
	operatorLabelStr = name.OperatorAPINamespace + "/managed"
	// operatorReconcileStr indicates that the operator will reconcile the resource.
	operatorReconcileStr = "Reconcile"
	// IstioComponentLabelStr indicates which Istio component a resource belongs to.
	IstioComponentLabelStr = name.OperatorAPINamespace + "/component"
	// istioVersionLabelStr indicates the Istio version of the installation.
	istioVersionLabelStr = name.OperatorAPINamespace + "/version"
)

// TestMode sets the controller into test mode. Used for unit tests to bypass things like waiting on resources.
var TestMode = false

var (
	// ClusterResources are resource types the operator prunes, ordered by which types should be deleted, first to last.
	ClusterResources = []schema.GroupVersionKind{
		{Group: "admissionregistration.k8s.io", Version: "v1", Kind: name.MutatingWebhookConfigurationStr},
		{Group: "admissionregistration.k8s.io", Version: "v1", Kind: name.ValidatingWebhookConfigurationStr},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: name.ClusterRoleStr},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: name.ClusterRoleBindingStr},
		// Cannot currently prune CRDs because this will also wipe out user config.
		// {Group: "apiextensions.k8s.io", Version: "v1beta1", Kind: name.CRDStr},
	}
	// ClusterCPResources lists cluster scope resources types which should be deleted during uninstall command.
	ClusterCPResources = []schema.GroupVersionKind{
		{Group: "admissionregistration.k8s.io", Version: "v1", Kind: name.MutatingWebhookConfigurationStr},
		{Group: "admissionregistration.k8s.io", Version: "v1", Kind: name.ValidatingWebhookConfigurationStr},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: name.ClusterRoleStr},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: name.ClusterRoleBindingStr},
	}
	// AllClusterResources lists all cluster scope resources types which should be deleted in purge case, including CRD.
	AllClusterResources = append(ClusterResources,
		schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: name.CRDStr},
		schema.GroupVersionKind{Group: "k8s.cni.cncf.io", Version: "v1", Kind: name.NetworkAttachmentDefinitionStr},
	)
)

// NamespacedResources gets specific pruning resources based on the k8s version
func NamespacedResources() []schema.GroupVersionKind {
	res := []schema.GroupVersionKind{
		{Group: "apps", Version: "v1", Kind: name.DeploymentStr},
		{Group: "apps", Version: "v1", Kind: name.DaemonSetStr},
		{Group: "", Version: "v1", Kind: name.ServiceStr},
		{Group: "", Version: "v1", Kind: name.CMStr},
		{Group: "", Version: "v1", Kind: name.PodStr},
		{Group: "", Version: "v1", Kind: name.SecretStr},
		{Group: "", Version: "v1", Kind: name.SAStr},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: name.RoleBindingStr},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: name.RoleStr},
		{Group: "policy", Version: "v1", Kind: name.PDBStr},
		{Group: "autoscaling", Version: "v2", Kind: name.HPAStr},
		gvk.EnvoyFilter.Kubernetes(),
	}
	return res
}

// DeleteObjectsList removed resources that are in the slice of UnstructuredList.
func DeleteObjectsList(c kube.CLIClient, dryRun bool, log clog.Logger, objectsList []*unstructured.UnstructuredList) error {
	var errs util.Errors
	deletedObjects := make(map[string]bool)
	for _, ul := range objectsList {
		for _, o := range ul.Items {
			obj := object.NewK8sObject(&o, nil, nil)
			oh := obj.Hash()

			// kube client does not differentiate API version when listing, added this check to deduplicate.
			if deletedObjects[oh] {
				continue
			}
			if err := deleteResource(c, dryRun, log, obj, oh); err != nil {
				errs = append(errs, err)
			}
			deletedObjects[oh] = true
		}
	}

	return errs.ToError()
}

// GetPrunedResources get the list of resources to be removed
// 1. if includeClusterResources is false, we list the namespaced resources by matching revision and component labels.
// 2. if includeClusterResources is true, we list the namespaced and cluster resources by component labels only.
// If componentName is not empty, only resources associated with specific components would be returned
// UnstructuredList of objects and corresponding list of name kind hash of k8sObjects would be returned
func GetPrunedResources(clt kube.CLIClient, iopName, iopNamespace, revision string, includeClusterResources bool) (
	[]*unstructured.UnstructuredList, error,
) {
	var usList []*unstructured.UnstructuredList
	labels := make(map[string]string)
	if revision != "" {
		labels[label.IoIstioRev.Name] = revision
	}
	if iopName != "" {
		labels[OwningResourceName] = iopName
	}
	if iopNamespace != "" {
		labels[OwningResourceNamespace] = iopNamespace
	}
	selector := klabels.Set(labels).AsSelectorPreValidated()
	resources := NamespacedResources()
	gvkList := append(resources, ClusterCPResources...)
	if includeClusterResources {
		gvkList = append(resources, AllClusterResources...)
	}
	for _, gvk := range gvkList {
		var result *unstructured.UnstructuredList
		componentRequirement, err := klabels.NewRequirement(IstioComponentLabelStr, selection.Exists, nil)
		if err != nil {
			return nil, err
		}
		c, err := clt.DynamicClientFor(gvk, nil, "")
		if err != nil {
			return nil, err
		}
		if includeClusterResources {
			s := klabels.NewSelector()
			result, err = c.List(context.Background(), metav1.ListOptions{LabelSelector: s.Add(*componentRequirement).String()})
		} else {
			// do not prune base components or unknown components
			includeCN := []string{
				string(name.PilotComponentName),
				string(name.IngressComponentName),
				string(name.EgressComponentName),
				string(name.CNIComponentName),
				string(name.IstiodRemoteComponentName),
				string(name.ZtunnelComponentName),
			}
			includeRequirement, err := klabels.NewRequirement(IstioComponentLabelStr, selection.In, includeCN)
			if err != nil {
				return nil, err
			}
			result, err = c.List(context.Background(), metav1.ListOptions{LabelSelector: selector.Add(*includeRequirement, *componentRequirement).String()})
		}
		if err != nil {
			return nil, err
		}
		if len(result.Items) == 0 {
			continue
		}
		usList = append(usList, result)
	}

	return usList, nil
}

func PrunedResourcesSchemas() []schema.GroupVersionKind {
	return append(NamespacedResources(), ClusterResources...)
}

func deleteResource(clt kube.CLIClient, dryRun bool, log clog.Logger, obj *object.K8sObject, oh string) error {
	if dryRun {
		log.LogAndPrintf("Not pruning object %s because of dry run.", oh)
		return nil
	}
	u := obj.UnstructuredObject()
	c, err := clt.DynamicClientFor(obj.GroupVersionKind(), obj.UnstructuredObject(), "")
	if err != nil {
		return err
	}

	if err := c.Delete(context.TODO(), u.GetName(), metav1.DeleteOptions{PropagationPolicy: ptr.Of(metav1.DeletePropagationForeground)}); err != nil {
		if !kerrors.IsNotFound(err) {
			return err
		}
		// do not return error if resources are not found
		log.LogAndPrintf("object: %s is not being deleted because it no longer exists", obj.Hash())
		return nil
	}

	log.LogAndPrintf("  Removed %s.", oh)
	return nil
}
