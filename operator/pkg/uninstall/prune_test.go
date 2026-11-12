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
	"errors"
	"testing"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/assert"
)

const (
	istioNamespace = "istio-system"
	// istioctlOwner is the IstioOperator name that istioctl uses when the install did not come from a named file.
	istioctlOwner = "unknown"
)

func createObject(t *testing.T, c kube.CLIClient, kind schema.GroupVersionKind, resource schema.GroupVersionResource,
	namespace, name string, labels map[string]string,
) {
	t.Helper()
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(kind)
	obj.SetNamespace(namespace)
	obj.SetName(name)
	obj.SetLabels(labels)
	if _, err := c.Dynamic().Resource(resource).Namespace(namespace).Create(context.Background(), obj, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
}

// leaseLabels are the labels that istiod sets on per-revision Leases.
func leaseLabels(revision string) map[string]string {
	return map[string]string{
		"istio.io/rev":                revision,
		"operator.istio.io/component": "Pilot",
	}
}

// istioctlLabels are the labels that `istioctl install` sets on a rendered object.
func istioctlLabels(owner, component, revision string) map[string]string {
	return map[string]string{
		"istio.io/rev":                                        revision,
		"operator.istio.io/component":                         component,
		"install.operator.istio.io/owning-resource":           owner,
		"install.operator.istio.io/owning-resource-namespace": istioNamespace,
	}
}

func createIstiod(t *testing.T, c kube.CLIClient, revision string, labels map[string]string) {
	t.Helper()
	createObject(t, c, gvk.Deployment.Kubernetes(), gvr.Deployment, istioNamespace, "istiod-"+revision, labels)
}

// createLeases creates the Leases that two istiod revisions create in istio-system, plus Leases that must never be pruned.
func createLeases(t *testing.T, c kube.CLIClient) {
	t.Helper()
	lease := func(namespace, name string, labels map[string]string) {
		createObject(t, c, gvk.Lease.Kubernetes(), gvr.Lease, namespace, name, labels)
	}
	lease(istioNamespace, "istio-gateway-deployment-stable", leaseLabels("stable"))
	lease(istioNamespace, "istio-gateway-status-leader-stable", leaseLabels("stable"))
	lease(istioNamespace, "istio-gateway-deployment-canary", leaseLabels("canary"))
	lease(istioNamespace, "istio-gateway-status-leader-canary", leaseLabels("canary"))
	// A Lease created by an istiod version that did not label Leases.
	lease(istioNamespace, "istio-gateway-deployment-old", nil)
	// A Lease of a "stable" istiod that runs in a different namespace.
	lease("other-istio", "istio-gateway-deployment-stable", leaseLabels("stable"))
	// A Lease that belongs to something other than Istio.
	lease(istioNamespace, "kube-controller-manager", map[string]string{"app": "other"})
}

func prunedLeases(objects []*unstructured.UnstructuredList) []string {
	var names []string
	for _, list := range objects {
		for _, o := range list.Items {
			if o.GroupVersionKind() == gvk.Lease.Kubernetes() {
				names = append(names, o.GetNamespace()+"/"+o.GetName())
			}
		}
	}
	return slices.Sort(names)
}

func clusterLeases(t *testing.T, c kube.CLIClient) []string {
	t.Helper()
	leases, err := c.Dynamic().Resource(gvr.Lease).Namespace(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return slices.Sort(slices.Map(leases.Items, func(o unstructured.Unstructured) string { return o.GetNamespace() + "/" + o.GetName() }))
}

func TestPrunedResourcesSchemasExcludesLease(t *testing.T) {
	// Install-time pruning iterates over PrunedResourcesSchemas. Leases are created by istiod at runtime and are never
	// pruned on install, so listing them there would only waste a call and require extra RBAC permissions for every install.
	assert.Equal(t, slices.Contains(PrunedResourcesSchemas(), gvk.Lease.Kubernetes()), false)
	assert.Equal(t, slices.Contains(RuntimeResources, gvk.Lease.Kubernetes()), true)
}

func TestGetPrunedResourcesLeases(t *testing.T) {
	stableLeases := []string{
		"istio-system/istio-gateway-deployment-stable",
		"istio-system/istio-gateway-status-leader-stable",
	}
	cases := []struct {
		name string
		// istiods creates the istiod Deployments in the cluster.
		istiods func(t *testing.T, c kube.CLIClient)
		// The arguments that `istioctl uninstall` passes to GetPrunedResources.
		iopName  string
		revision string
		want     []string
	}{
		{
			name: "uninstall --revision stable",
			istiods: func(t *testing.T, c kube.CLIClient) {
				createIstiod(t, c, "stable", istioctlLabels(istioctlOwner, "Pilot", "stable"))
				createIstiod(t, c, "canary", istioctlLabels(istioctlOwner, "Pilot", "canary"))
			},
			revision: "stable",
			want:     stableLeases,
		},
		{
			// The revision is not passed as a flag, so it is taken from the istiod Deployment that is pruned.
			name: "uninstall -f stable.yaml",
			istiods: func(t *testing.T, c kube.CLIClient) {
				createIstiod(t, c, "stable", istioctlLabels("stable-iop", "Pilot", "stable"))
				createIstiod(t, c, "canary", istioctlLabels("canary-iop", "Pilot", "canary"))
			},
			iopName: "stable-iop",
			want:    stableLeases,
		},
		{
			// The file owns only gateways of the canary revision, so istiod-canary keeps running and keeps its Leases.
			name: "uninstall -f gateways.yaml --revision canary",
			istiods: func(t *testing.T, c kube.CLIClient) {
				createIstiod(t, c, "canary", istioctlLabels(istioctlOwner, "Pilot", "canary"))
				createObject(t, c, gvk.Deployment.Kubernetes(), gvr.Deployment, istioNamespace, "istio-ingressgateway-canary",
					istioctlLabels("gateways", "IngressGateways", "canary"))
			},
			iopName:  "gateways",
			revision: "canary",
			want:     nil,
		},
		{
			// istioctl does not uninstall an istiod that Helm installed, so its Leases must stay too.
			name: "uninstall --revision stable of a Helm install",
			istiods: func(t *testing.T, c kube.CLIClient) {
				createIstiod(t, c, "stable", leaseLabels("stable"))
			},
			revision: "stable",
			want:     nil,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := kube.NewFakeClient()
			createLeases(t, c)
			tt.istiods(t, c)
			objects, err := GetPrunedResources(c, tt.iopName, istioNamespace, tt.revision, false)
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, prunedLeases(objects), tt.want)
		})
	}
}

func TestRevisionLeasesForEveryPrunedIstiod(t *testing.T) {
	// An uninstall can prune several istiod Deployments at once, for example with an IstioOperator file that owns two
	// revisions. revisionLeases must return the Leases of each of them exactly once.
	c := kube.NewFakeClient()
	createLeases(t, c)
	deployments := func(items ...*unstructured.Unstructured) *unstructured.UnstructuredList {
		list := &unstructured.UnstructuredList{}
		for _, item := range items {
			list.Items = append(list.Items, *item)
		}
		return list
	}
	deployment := func(namespace, name string, labels map[string]string) *unstructured.Unstructured {
		d := &unstructured.Unstructured{}
		d.SetGroupVersionKind(gvk.Deployment.Kubernetes())
		d.SetNamespace(namespace)
		d.SetName(name)
		d.SetLabels(labels)
		return d
	}

	leases, err := revisionLeases(c, []*unstructured.UnstructuredList{deployments(
		deployment(istioNamespace, "istiod-stable", leaseLabels("stable")),
		deployment(istioNamespace, "istiod-canary", leaseLabels("canary")),
		// A second Deployment of the same revision must not list the Leases twice.
		deployment(istioNamespace, "istiod-stable-copy", leaseLabels("stable")),
		// A gateway carries a revision label but does not own Leases.
		deployment(istioNamespace, "istio-ingressgateway", map[string]string{"istio.io/rev": "other", "operator.istio.io/component": "IngressGateways"}),
	)})
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, prunedLeases(leases), []string{
		"istio-system/istio-gateway-deployment-canary",
		"istio-system/istio-gateway-deployment-stable",
		"istio-system/istio-gateway-status-leader-canary",
		"istio-system/istio-gateway-status-leader-stable",
	})
}

func TestPilotLeasesForPurge(t *testing.T) {
	// The purge path of GetPrunedResources also lists NetworkAttachmentDefinitions, which the fake dynamic client does not
	// register, so this calls pilotLeases directly. A purge removes every Pilot Lease, even without an istiod Deployment,
	// but still leaves unlabeled Leases and Leases that do not belong to Istio.
	c := kube.NewFakeClient()
	createLeases(t, c)

	leases, err := pilotLeases(c)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, prunedLeases(leases), []string{
		"istio-system/istio-gateway-deployment-canary",
		"istio-system/istio-gateway-deployment-stable",
		"istio-system/istio-gateway-status-leader-canary",
		"istio-system/istio-gateway-status-leader-stable",
		"other-istio/istio-gateway-deployment-stable",
	})
}

func TestGetPrunedResourcesLeasesAfterRole(t *testing.T) {
	c := kube.NewFakeClient()
	createLeases(t, c)
	createIstiod(t, c, "stable", istioctlLabels(istioctlOwner, "Pilot", "stable"))
	role := schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"}
	roles := schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}
	createObject(t, c, role, roles, istioNamespace, "istiod-stable", istioctlLabels(istioctlOwner, "Pilot", "stable"))

	objects, err := GetPrunedResources(c, "", istioNamespace, "stable", false)
	if err != nil {
		t.Fatal(err)
	}
	// The Role that lets istiod write Leases must be deleted before the Leases, so that a terminating istiod
	// cannot recreate a Lease after it is removed. DeleteObjectsList deletes in list order.
	roleIndex, leaseIndex := -1, -1
	for i, list := range objects {
		switch list.Items[0].GroupVersionKind() {
		case role:
			roleIndex = i
		case gvk.Lease.Kubernetes():
			leaseIndex = i
		}
	}
	assert.Equal(t, roleIndex >= 0 && leaseIndex == len(objects)-1 && roleIndex < leaseIndex, true)
}

func TestGetPrunedResourcesSkipsLeasesWhenForbidden(t *testing.T) {
	c := kube.NewFakeClient()
	createLeases(t, c)
	createIstiod(t, c, "stable", istioctlLabels(istioctlOwner, "Pilot", "stable"))
	c.Dynamic().(*dynamicfake.FakeDynamicClient).PrependReactor("list", "leases",
		func(action clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, kerrors.NewForbidden(gvr.Lease.GroupResource(), "", errors.New("not allowed"))
		})

	objects, err := GetPrunedResources(c, "", istioNamespace, "stable", false)
	if err != nil {
		t.Fatal(err)
	}
	// The rest of the control plane is still pruned; only the Leases are skipped.
	assert.Equal(t, len(prunedLeases(objects)), 0)
	assert.Equal(t, len(objects) > 0, true)
}

func TestDeleteObjectsListRemovesRevisionLeases(t *testing.T) {
	c := kube.NewFakeClient()
	createLeases(t, c)
	createIstiod(t, c, "stable", istioctlLabels(istioctlOwner, "Pilot", "stable"))

	objects, err := GetPrunedResources(c, "", istioNamespace, "stable", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := DeleteObjectsList(c, false, clog.NewDefaultLogger(), objects); err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, clusterLeases(t, c), []string{
		"istio-system/istio-gateway-deployment-canary",
		"istio-system/istio-gateway-deployment-old",
		"istio-system/istio-gateway-status-leader-canary",
		"istio-system/kube-controller-manager",
		"other-istio/istio-gateway-deployment-stable",
	})
}

func TestDeleteObjectsListDryRunKeepsLeases(t *testing.T) {
	c := kube.NewFakeClient()
	createLeases(t, c)
	createIstiod(t, c, "stable", istioctlLabels(istioctlOwner, "Pilot", "stable"))
	before := clusterLeases(t, c)

	objects, err := GetPrunedResources(c, "", istioNamespace, "stable", false)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, len(prunedLeases(objects)), 2)
	if err := DeleteObjectsList(c, true, clog.NewDefaultLogger(), objects); err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, clusterLeases(t, c), before)
}
