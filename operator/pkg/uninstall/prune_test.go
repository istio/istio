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
	"bytes"
	"context"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
)

func TestDeleteObjectsListDeletesLeaseAfterPodTerminates(t *testing.T) {
	const namespace = "istio-system"
	pod := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name": "istiod-stable", "namespace": namespace,
			"labels": map[string]any{
				"istio.io/rev":                "stable",
				"operator.istio.io/component": "Pilot",
			},
		},
	}}
	replacementPod := pod.DeepCopy()
	replacementPod.SetName("istiod-stable-replacement")
	lease := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "coordination.k8s.io/v1",
		"kind":       "Lease",
		"metadata": map[string]any{
			"name": "istio-gateway-status-leader-stable", "namespace": namespace,
			"labels": map[string]any{
				"istio.io/rev":                "stable",
				"operator.istio.io/component": "Pilot",
			},
		},
	}}

	client := kube.NewFakeClient()
	dynamicClient := client.Dynamic().(*dynamicfake.FakeDynamicClient)
	for resource, object := range map[schema.GroupVersionResource]*unstructured.Unstructured{
		gvr.Pod:   pod,
		gvr.Lease: lease,
	} {
		if _, err := dynamicClient.Resource(resource).Namespace(namespace).Create(
			context.Background(), object.DeepCopy(), metav1.CreateOptions{},
		); err != nil {
			t.Fatal(err)
		}
	}

	// Model asynchronous Pod deletion and a replacement created while the
	// workload controller is terminating.
	dynamicClient.PrependReactor("delete", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, nil
	})
	podGets := 0
	dynamicClient.PrependReactor("get", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		podGets++
		if podGets < 2 {
			return false, nil, nil
		}
		get := action.(k8stesting.GetAction)
		if podGets == 2 {
			if err := dynamicClient.Tracker().Delete(gvr.Pod, get.GetNamespace(), get.GetName()); err != nil && !apierrors.IsNotFound(err) {
				return true, nil, err
			}
			if err := dynamicClient.Tracker().Create(gvr.Pod, replacementPod.DeepCopy(), namespace); err != nil {
				return true, nil, err
			}
		}
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, get.GetName())
	})
	podLists := 0
	dynamicClient.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		podLists++
		if podLists == 2 {
			if err := dynamicClient.Tracker().Delete(gvr.Pod, namespace, replacementPod.GetName()); err != nil && !apierrors.IsNotFound(err) {
				return true, nil, err
			}
		}
		return false, nil, nil
	})

	// Model leader election: deleting the Lease while its Pod is alive causes
	// the Lease to be recreated.
	dynamicClient.PrependReactor("delete", "leases", func(action k8stesting.Action) (bool, runtime.Object, error) {
		deleted := action.(k8stesting.DeleteAction)
		if err := dynamicClient.Tracker().Delete(gvr.Lease, deleted.GetNamespace(), deleted.GetName()); err != nil {
			return true, nil, err
		}
		originalPodAlive := false
		if _, err := dynamicClient.Tracker().Get(gvr.Pod, namespace, pod.GetName()); err == nil {
			originalPodAlive = true
		}
		replacementPodAlive := false
		if _, err := dynamicClient.Tracker().Get(gvr.Pod, namespace, replacementPod.GetName()); err == nil {
			replacementPodAlive = true
		}
		if originalPodAlive || replacementPodAlive {
			if err := dynamicClient.Tracker().Create(gvr.Lease, lease.DeepCopy(), namespace); err != nil {
				return true, nil, err
			}
		}
		return true, nil, nil
	})

	objects := []*unstructured.UnstructuredList{{Items: []unstructured.Unstructured{*pod, *lease}}}
	logger := clog.NewConsoleLogger(&bytes.Buffer{}, &bytes.Buffer{}, nil)
	if err := DeleteObjectsList(client, false, logger, objects); err != nil {
		t.Fatal(err)
	}

	if _, err := dynamicClient.Resource(gvr.Lease).Namespace(namespace).Get(
		context.Background(), lease.GetName(), metav1.GetOptions{},
	); !apierrors.IsNotFound(err) {
		t.Fatalf("expected lease to be deleted after pod termination, got %v", err)
	}
}

func TestLeaseIsOnlyIncludedInUninstallPruning(t *testing.T) {
	leaseGVK := gvk.Lease.Kubernetes()
	for _, resource := range PrunedResourcesSchemas() {
		if resource == leaseGVK {
			t.Fatal("lease must not participate in install-time pruning")
		}
	}
	for _, resource := range UninstallNamespacedResources() {
		if resource == leaseGVK {
			return
		}
	}
	t.Fatal("lease must participate in uninstall pruning")
}

func TestRevisionUninstallPreservesOtherRevisionLease(t *testing.T) {
	const namespace = "istio-system"
	client := kube.NewFakeClient()
	dynamicClient := client.Dynamic().(*dynamicfake.FakeDynamicClient)
	for _, revision := range []string{"stable", "canary"} {
		lease := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "coordination.k8s.io/v1",
			"kind":       "Lease",
			"metadata": map[string]any{
				"name":      "istio-gateway-status-leader-" + revision,
				"namespace": namespace,
				"labels": map[string]any{
					"istio.io/rev":                revision,
					"operator.istio.io/component": "Pilot",
				},
			},
		}}
		if _, err := dynamicClient.Resource(gvr.Lease).Namespace(namespace).Create(
			context.Background(), lease, metav1.CreateOptions{},
		); err != nil {
			t.Fatal(err)
		}
	}

	objects, err := GetPrunedResources(client, "", "", "stable", false)
	if err != nil {
		t.Fatal(err)
	}
	logger := clog.NewConsoleLogger(&bytes.Buffer{}, &bytes.Buffer{}, nil)
	if err := DeleteObjectsList(client, false, logger, objects); err != nil {
		t.Fatal(err)
	}

	leases := dynamicClient.Resource(gvr.Lease).Namespace(namespace)
	if _, err := leases.Get(context.Background(), "istio-gateway-status-leader-stable", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected stable lease to be deleted, got %v", err)
	}
	if _, err := leases.Get(context.Background(), "istio-gateway-status-leader-canary", metav1.GetOptions{}); err != nil {
		t.Fatalf("expected canary lease to remain, got %v", err)
	}
}
