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

	// WaypointResources List the waypoint resources that should be deleted.
	WaypointResources = schema.GroupVersionKind{
		Group: "gateway.networking.k8s.io", Version: "v1", Kind: "Gateway",
	}
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
				// TODO remove this after 2 releases
				// nolint: staticcheck
				string(component.IstiodRemoteComponentName),
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

	waypointSelector := klabels.Set(labels).AsSelectorPreValidated()
	waypointResult, err := getWaypointResources(clt, waypointSelector.String())
	if err != nil {
		return nil, err
	}

	usList = append(usList, waypointResult)
	return usList, nil
}

func getWaypointResources(clt kube.CLIClient, waypointSelector string) (*unstructured.UnstructuredList, error) {
	c, err := clt.DynamicClientFor(WaypointResources, nil, "")
	if err != nil {
		return nil, err
	}
	allWaypoint, err := c.List(context.Background(), metav1.ListOptions{
		LabelSelector: waypointSelector,
	},
	)
	if err != nil {
		return nil, err
	}
	waypointResult := unstructured.UnstructuredList{
		Object: allWaypoint.Object,
	}
	for _, item := range allWaypoint.Items {
		specMap, ok := item.Object["spec"].(map[string]interface{})
		if !ok {
			continue
		}

		if specMap["gatewayClassName"] == "istio-waypoint" {
			if err != nil {
				continue
			}

			waypointResult.Items = append(waypointResult.Items, item)
		}
	}
	return &waypointResult, nil
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
