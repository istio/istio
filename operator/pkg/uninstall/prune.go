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

package uninstall

import (
	"context"
	"fmt"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"

	"istio.io/api/label"
	"istio.io/istio/operator/pkg/component"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/util/sets"
)

var (
	// ClusterResources are resource types the operator prunes, ordered by which types should be deleted, first to last.
	ClusterResources = []schema.GroupVersionKind{
		gvk.MutatingWebhookConfiguration.Kubernetes(),
		gvk.ValidatingWebhookConfiguration.Kubernetes(),
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"},
		// Cannot currently prune CRDs because this will also wipe out user config.
		// {Group: "apiextensions.k8s.io", Version: "v1beta1", Kind: name.CRDStr},
	}
	// ClusterCPResources lists cluster scope resources types which should be deleted during uninstall command.
	ClusterCPResources = []schema.GroupVersionKind{
		gvk.MutatingWebhookConfiguration.Kubernetes(),
		gvk.ValidatingWebhookConfiguration.Kubernetes(),
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"},
	}
	// AllClusterResources lists all cluster scope resources types which should be deleted in purge case, including CRD.
	AllClusterResources = append(ClusterResources,
		gvk.CustomResourceDefinition.Kubernetes(),
		schema.GroupVersionKind{Group: "k8s.cni.cncf.io", Version: "v1", Kind: "NetworkAttachmentDefinition"},
	)
)

// NamespacedResources gets specific pruning resources based on the k8s version
func NamespacedResources() []schema.GroupVersionKind {
	res := []schema.GroupVersionKind{
		gvk.Deployment.Kubernetes(),
		gvk.DaemonSet.Kubernetes(),
		gvk.Service.Kubernetes(),
		gvk.ConfigMap.Kubernetes(),
		gvk.Pod.Kubernetes(),
		gvk.Secret.Kubernetes(),
		gvk.ServiceAccount.Kubernetes(),
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"},
		{Group: "policy", Version: "v1", Kind: "PodDisruptionBudget"},
		{Group: "autoscaling", Version: "v2", Kind: "HorizontalPodAutoscaler"},
		gvk.EnvoyFilter.Kubernetes(),
	}
	return res
}

// RuntimeResources are resource types that istiod creates at runtime instead of istioctl rendering them, so they never
// carry the owning-resource labels. GetPrunedResources selects them through the istiod Deployment that it prunes (see
// revisionLeases). They are deliberately not part of NamespacedResources, so that install-time pruning never lists them.
var RuntimeResources = []schema.GroupVersionKind{
	gvk.Lease.Kubernetes(),
}

// DeleteObjectsList removed resources that are in the slice of UnstructuredList.
func DeleteObjectsList(c kube.CLIClient, dryRun bool, log clog.Logger, objectsList []*unstructured.UnstructuredList) error {
	var errs util.Errors
	for _, ul := range objectsList {
		for _, o := range ul.Items {
			if err := DeleteResource(c, dryRun, log, &o); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errs.ToError()
}

// GetPrunedResources get the list of resources to be removed
// 1. if includeClusterResources is false, we list the namespaced resources by matching revision and component labels.
// 2. if includeClusterResources is true, we list the namespaced and cluster resources by component labels only.
// The leader election Leases that istiod creates at runtime are appended last: with includeClusterResources, every
// Pilot Lease (see pilotLeases); otherwise, the Leases of each istiod Deployment being pruned (see revisionLeases).
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
		labels[manifest.OwningResourceName] = iopName
	}
	if iopNamespace != "" {
		labels[manifest.OwningResourceNamespace] = iopNamespace
	}
	selector := klabels.Set(labels).AsSelectorPreValidated()
	resources := NamespacedResources()
	gvkList := append(resources, ClusterCPResources...)
	if includeClusterResources {
		gvkList = append(resources, AllClusterResources...)
	}
	for _, gvk := range gvkList {
		var result *unstructured.UnstructuredList
		componentRequirement, err := klabels.NewRequirement(manifest.IstioComponentLabel, selection.Exists, nil)
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
				string(component.PilotComponentName),
				string(component.IngressComponentName),
				string(component.EgressComponentName),
				string(component.CNIComponentName),
				string(component.ZtunnelComponentName),
			}
			includeRequirement, lerr := klabels.NewRequirement(manifest.IstioComponentLabel, selection.In, includeCN)
			if lerr != nil {
				return nil, lerr
			}
			result, err = c.List(context.Background(), metav1.ListOptions{LabelSelector: selector.Add(*includeRequirement, *componentRequirement).String()})
		}
		if controllers.IgnoreNotFound(err) != nil {
			return nil, err
		}
		if result == nil || len(result.Items) == 0 {
			continue
		}
		usList = append(usList, result)
	}

	var leases []*unstructured.UnstructuredList
	var err error
	if includeClusterResources {
		leases, err = pilotLeases(clt)
	} else {
		leases, err = revisionLeases(clt, usList)
	}
	if err != nil {
		return nil, err
	}
	// The Leases go last. By the time they are deleted, the Role and RoleBinding that grant the terminating istiod access
	// to Leases have already been deleted, so istiod cannot recreate a Lease after it is removed.
	usList = append(usList, leases...)

	return usList, nil
}

// pilotLeases returns every Lease that istiod labeled with the Pilot component, for a purge. A purge removes every
// istiod, so this also cleans up Leases whose istiod Deployment is already gone or lives in another cluster.
func pilotLeases(clt kube.CLIClient) ([]*unstructured.UnstructuredList, error) {
	selector := klabels.Set{manifest.IstioComponentLabel: string(component.PilotComponentName)}.AsSelectorPreValidated()
	result, err := listLeases(clt, "", selector)
	if err != nil || result == nil {
		return nil, err
	}
	return []*unstructured.UnstructuredList{result}, nil
}

// revisionLeases returns the per-revision leader election Leases of every istiod Deployment in objects.
// Istiod creates these Leases at runtime in its own namespace and labels them with its revision and the Pilot component.
// They are pruned only together with the istiod Deployment that owns them. This way an uninstall that keeps a revision's
// istiod running (for example a gateways-only IstioOperator file, or a Helm-installed istiod) never deletes its Leases,
// and an uninstall by revision, by IstioOperator file or by --set revision removes them like the rest of the revision.
// Leases without an istiod Deployment in this cluster, such as those an external control plane writes into its config
// cluster, are only removed by a purge.
func revisionLeases(clt kube.CLIClient, objects []*unstructured.UnstructuredList) ([]*unstructured.UnstructuredList, error) {
	type istiodRevision struct {
		namespace string
		revision  string
	}
	seen := sets.New[istiodRevision]()
	var res []*unstructured.UnstructuredList
	for _, list := range objects {
		for _, o := range list.Items {
			if o.GroupVersionKind() != gvk.Deployment.Kubernetes() {
				continue
			}
			labels := o.GetLabels()
			rev := labels[label.IoIstioRev.Name]
			if labels[manifest.IstioComponentLabel] != string(component.PilotComponentName) || rev == "" {
				continue
			}
			if seen.InsertContains(istiodRevision{namespace: o.GetNamespace(), revision: rev}) {
				continue
			}
			selector := klabels.Set{
				label.IoIstioRev.Name:        rev,
				manifest.IstioComponentLabel: string(component.PilotComponentName),
			}.AsSelectorPreValidated()
			result, err := listLeases(clt, o.GetNamespace(), selector)
			if err != nil {
				return nil, err
			}
			if result != nil {
				res = append(res, result)
			}
		}
	}
	return res, nil
}

// listLeases lists the Leases that match selector in namespace, or in every namespace the client lists when namespace is
// empty. It returns nil when there are none. A leftover Lease is harmless, so if the user may not list Leases, they are
// skipped instead of failing the whole uninstall.
func listLeases(clt kube.CLIClient, namespace string, selector klabels.Selector) (*unstructured.UnstructuredList, error) {
	c, err := clt.DynamicClientFor(gvk.Lease.Kubernetes(), nil, namespace)
	if err != nil {
		return nil, err
	}
	result, err := c.List(context.Background(), metav1.ListOptions{LabelSelector: selector.String()})
	if kerrors.IsNotFound(err) || kerrors.IsForbidden(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(result.Items) == 0 {
		return nil, nil
	}
	return result, nil
}

func PrunedResourcesSchemas() []schema.GroupVersionKind {
	return append(NamespacedResources(), ClusterResources...)
}

func DeleteResource(clt kube.CLIClient, dryRun bool, log clog.Logger, obj *unstructured.Unstructured) error {
	name := fmt.Sprintf("%v/%s.%s", obj.GroupVersionKind(), obj.GetName(), obj.GetNamespace())
	if dryRun {
		log.LogAndPrintf("Not pruning object %s because of dry run.", name)
		return nil
	}
	c, err := clt.DynamicClientFor(obj.GroupVersionKind(), obj, "")
	if err != nil {
		return err
	}

	if err := c.Delete(context.TODO(), obj.GetName(), metav1.DeleteOptions{PropagationPolicy: ptr.Of(metav1.DeletePropagationForeground)}); err != nil {
		if !kerrors.IsNotFound(err) {
			return err
		}
		// do not return error if resources are not found
		log.LogAndPrintf("object: %s is not being deleted because it no longer exists", name)
		return nil
	}

	log.LogAndPrintf("  Removed %s.", name)
	return nil
}
