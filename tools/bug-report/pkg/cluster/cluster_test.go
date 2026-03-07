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

package cluster

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	config2 "istio.io/istio/tools/bug-report/pkg/config"
)

func TestShouldSkipPod(t *testing.T) {
	cases := []struct {
		name     string
		pod      *v1.Pod
		config   *config2.BugReportConfig
		expected bool
	}{
		{
			"tested namespace not skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "in-namespace1",
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Namespaces: []string{"in-"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Namespaces: []string{"ex-"},
					},
				},
			},
			false,
		},
		{
			"tested namespace not skip 2",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "in-namespace1",
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Namespaces: []string{"in*"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Namespaces: []string{"ex*"},
					},
				},
			},
			false,
		},
		{
			"tested namespace not skip 3",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "in-namespace1",
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Namespaces: []string{"*name*"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Namespaces: []string{"ex*"},
					},
				},
			},
			false,
		},
		{
			"tested namespace not skip 4",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "in-namespace1",
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Namespaces: []string{"*space1"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Namespaces: []string{"ex*"},
					},
				},
			},
			false,
		},
		{
			"tested namespace skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ex-namespace1",
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Namespaces: []string{"in-"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Namespaces: []string{"ex-"},
					},
				},
			},
			true,
		},
		{
			"tested pod name not skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "in-pod1",
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Pods: []string{"in-"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Pods: []string{"ex-"},
					},
				},
			},
			false,
		},
		{
			"tested pod name skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ex-pod1",
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Pods: []string{"in-"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Pods: []string{"ex-"},
					},
				},
			},
			true,
		},
		{
			"tested container not skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "in-test1",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "in-con1",
						},
					},
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Pods:       []string{"in-"},
						Containers: []string{"in-"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Pods:       []string{"ex-"},
						Containers: []string{"ex-"},
					},
				},
			},
			false,
		},
		{
			"tested container skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "in-test1",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "ex-con1",
						},
					},
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Pods:       []string{"in-"},
						Containers: []string{"in-"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Pods:       []string{"ex-"},
						Containers: []string{"ex-"},
					},
				},
			},
			true,
		},
		{
			"tested label not skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "in-test1",
					Labels: map[string]string{
						"l1": "lv1",
					},
					Annotations: map[string]string{
						"a1": "av1",
						"a2": "av1",
					},
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Pods: []string{"in-"},
						Labels: map[string]string{
							"l1": "lv1",
							"l2": "lv2",
						},
					},
				},
			},
			false,
		},
		{
			"tested label skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "in-test1",
					Labels: map[string]string{
						"l1": "lv1",
					},
					Annotations: map[string]string{
						"a1": "av1",
						"a2": "av1",
					},
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Pods: []string{"in-"},
						Labels: map[string]string{
							"l2": "lv2",
						},
					},
				},
			},
			true,
		},
		{
			"tested annotation not skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "in-test1",
					Labels: map[string]string{
						"l3": "lv3",
						"l4": "lv4",
					},
					Annotations: map[string]string{
						"a3": "av3",
						"a4": "av4",
					},
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Pods: []string{"in-"},
						Annotations: map[string]string{
							"a3": "av3",
							"a4": "av4",
						},
					},
				},
			},
			false,
		},
		{
			"tested annotation skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "in-test1",
					Labels: map[string]string{
						"l3": "lv3",
						"l4": "lv4",
					},
					Annotations: map[string]string{
						"a3": "av3",
					},
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Pods: []string{"in-"},
						Annotations: map[string]string{
							"a4": "av4",
						},
					},
				},
			},
			true,
		},
		{
			"tested difference namespace skip exclude",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "in-test1",
					Namespace: "test",
					Labels: map[string]string{
						"l3": "lv3",
						"l4": "lv4",
					},
					Annotations: map[string]string{
						"a3": "av3",
					},
				},
			},
			&config2.BugReportConfig{
				Exclude: []*config2.SelectionSpec{
					{
						Namespaces: []string{
							"fake",
						},
					},
					{
						Namespaces: []string{
							"test",
						},
					},
				},
			},
			true,
		},
		{
			"tested include difference namespace not skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "in-test1",
					Namespace: "test",
					Labels: map[string]string{
						"l3": "lv3",
						"l4": "lv4",
					},
					Annotations: map[string]string{
						"a3": "av3",
					},
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Namespaces: []string{
							"fake",
						},
					},
					{
						Namespaces: []string{
							"test",
						},
					},
				},
			},
			false,
		},
		{
			"tested include difference namespace not skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "in-test1",
					Namespace: "test",
					Labels: map[string]string{
						"l3": "lv3",
						"l4": "lv4",
					},
					Annotations: map[string]string{
						"a3": "av3",
					},
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Namespaces: []string{
							"fake",
						},
					},
					{
						Namespaces: []string{
							"test",
						},
					},
				},
			},
			false,
		},
		{
			"tested include difference namespace/pod... not skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "in-test1",
					Namespace: "test",
					Labels: map[string]string{
						"l3": "lv3",
						"l4": "lv4",
					},
					Annotations: map[string]string{
						"a3": "av3",
					},
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Namespaces: []string{
							"fake",
						},
						Pods: []string{
							"in-test1",
						},
						Labels: map[string]string{
							"l3": "lv3",
						},
						Annotations: map[string]string{
							"a3": "av3",
						},
					},
					{
						Namespaces: []string{
							"test",
						},
						Pods: []string{
							"in-test1",
						},
						Labels: map[string]string{
							"l3": "lv3",
						},
						Annotations: map[string]string{
							"a3": "av3",
						},
					},
				},
			},
			false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			skip := shouldSkipPod(c.pod, c.config)
			if skip != c.expected {
				t.Errorf("shouldSkipPod() for test case name [%s] return= %v, want %v", c.name, skip, c.expected)
			}
		})
	}
}

func TestShouldSkipDeployment(t *testing.T) {
	cases := []struct {
		name       string
		config     *config2.BugReportConfig
		deployment string
		expected   bool
	}{
		{
			"tested deployment not skip",
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Deployments: []string{"in-"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Deployments: []string{"ex-"},
					},
				},
			},
			"in-dep1",
			false,
		},
		{
			"tested deployment skip",
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Deployments: []string{"in-"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Deployments: []string{"ex-"},
					},
				},
			},
			"ex-dep1",
			true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			skip := shouldSkipDeployment(c.deployment, c.config)
			if skip != c.expected {
				t.Errorf("shouldSkip() for test case name [%s] return= %v, want %v", c.name, skip, c.expected)
			}
		})
	}
}

func TestShouldSkipDaemonSet(t *testing.T) {
	cases := []struct {
		name      string
		config    *config2.BugReportConfig
		daemonset string
		expected  bool
	}{
		{
			"tested daemonset not skip",
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Daemonsets: []string{"in-"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Daemonsets: []string{"ex-"},
					},
				},
			},
			"in-dep1",
			false,
		},
		{
			"tested daemonset skip",
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Daemonsets: []string{"in-"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Daemonsets: []string{"ex-"},
					},
				},
			},
			"ex-dep1",
			true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			skip := shouldSkipDaemonSet(c.daemonset, c.config)
			if skip != c.expected {
				t.Errorf("shouldSkip() for test case name [%s] return= %v, want %v", c.name, skip, c.expected)
			}
		})
	}
}

func TestExtractIncludedNamespaces(t *testing.T) {
	cases := []struct {
		name     string
		config   *config2.BugReportConfig
		expected []string
	}{
		{
			name:     "no includes returns nil",
			config:   &config2.BugReportConfig{},
			expected: nil,
		},
		{
			name: "include with empty namespaces returns nil",
			config: &config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{Pods: []string{"my-pod"}},
				},
			},
			expected: nil,
		},
		{
			name: "include with wildcard namespace returns nil",
			config: &config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{Namespaces: []string{"istio-*"}},
				},
			},
			expected: nil,
		},
		{
			name: "concrete namespaces are extracted",
			config: &config2.BugReportConfig{
				IstioNamespace: "istio-system",
				Include: []*config2.SelectionSpec{
					{Namespaces: []string{"ns1"}},
					{Namespaces: []string{"ns2"}},
				},
			},
			// Should contain ns1, ns2, and istio-system
			expected: nil, // checked below
		},
		{
			name: "duplicate namespaces are deduplicated",
			config: &config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{Namespaces: []string{"ns1"}},
					{Namespaces: []string{"ns1"}},
				},
			},
			expected: nil, // checked below
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ExtractIncludedNamespaces(c.config)
			switch c.name {
			case "concrete namespaces are extracted":
				if result == nil {
					t.Fatal("expected non-nil result")
				}
				nsSet := make(map[string]bool)
				for _, ns := range result {
					nsSet[ns] = true
				}
				for _, want := range []string{"ns1", "ns2", "istio-system"} {
					if !nsSet[want] {
						t.Errorf("expected namespace %q in result, got %v", want, result)
					}
				}
				if len(result) != 3 {
					t.Errorf("expected 3 namespaces, got %d: %v", len(result), result)
				}
			case "duplicate namespaces are deduplicated":
				if result == nil {
					t.Fatal("expected non-nil result")
				}
				if len(result) != 1 {
					t.Errorf("expected 1 namespace after dedup, got %d: %v", len(result), result)
				}
			default:
				if c.expected == nil && result != nil {
					t.Errorf("expected nil, got %v", result)
				}
			}
		})
	}
}

func TestBuildReplicaSetOwnerMap(t *testing.T) {
	replicasets := []appsv1.ReplicaSet{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-deploy-abc123",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{Kind: "Deployment", Name: "my-deploy"},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-rs",
				Namespace: "default",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ns2-rs",
				Namespace: "ns2",
				OwnerReferences: []metav1.OwnerReference{
					{Kind: "Deployment", Name: "ns2-deploy"},
				},
			},
		},
	}

	m := buildReplicaSetOwnerMap(replicasets)

	if got, ok := m["default/my-deploy-abc123"]; !ok || got != "my-deploy" {
		t.Errorf("expected default/my-deploy-abc123 -> my-deploy, got %q (ok=%v)", got, ok)
	}
	if _, ok := m["default/other-rs"]; ok {
		t.Error("expected other-rs without owner to not be in map")
	}
	if got, ok := m["ns2/ns2-rs"]; !ok || got != "ns2-deploy" {
		t.Errorf("expected ns2/ns2-rs -> ns2-deploy, got %q (ok=%v)", got, ok)
	}
}

func TestBuildDaemonSetNameMap(t *testing.T) {
	daemonsets := []appsv1.DaemonSet{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-ds",
				Namespace: "default",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-ds",
				Namespace: "kube-system",
			},
		},
	}

	m := buildDaemonSetNameMap(daemonsets)

	if got, ok := m["default/my-ds"]; !ok || got != "my-ds" {
		t.Errorf("expected default/my-ds -> my-ds, got %q (ok=%v)", got, ok)
	}
	if got, ok := m["kube-system/other-ds"]; !ok || got != "other-ds" {
		t.Errorf("expected kube-system/other-ds -> other-ds, got %q (ok=%v)", got, ok)
	}
	if len(m) != 2 {
		t.Errorf("expected 2 entries, got %d", len(m))
	}
}

func TestGetOwnerDeployment(t *testing.T) {
	rsOwnerMap := map[string]string{
		"default/my-deploy-abc123": "my-deploy",
		"ns2/other-rs":             "other-deploy",
	}

	cases := []struct {
		name     string
		pod      *v1.Pod
		expected string
	}{
		{
			name: "pod owned by replicaset with deployment",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "ReplicaSet", Name: "my-deploy-abc123"},
					},
				},
			},
			expected: "my-deploy",
		},
		{
			name: "pod owned by replicaset without deployment",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "ReplicaSet", Name: "standalone-rs"},
					},
				},
			},
			expected: "",
		},
		{
			name: "pod with no owner references",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
			},
			expected: "",
		},
		{
			name: "pod owned by non-replicaset",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "StatefulSet", Name: "my-sts"},
					},
				},
			},
			expected: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := getOwnerDeployment(c.pod, rsOwnerMap)
			if got != c.expected {
				t.Errorf("getOwnerDeployment() = %q, want %q", got, c.expected)
			}
		})
	}
}

func TestGetOwnerDaemonSet(t *testing.T) {
	dsNameMap := map[string]string{
		"default/my-ds":      "my-ds",
		"kube-system/cni-ds": "cni-ds",
	}

	cases := []struct {
		name     string
		pod      *v1.Pod
		expected string
	}{
		{
			name: "pod owned by daemonset",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "DaemonSet", Name: "my-ds"},
					},
				},
			},
			expected: "my-ds",
		},
		{
			name: "pod not owned by daemonset",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "ReplicaSet", Name: "some-rs"},
					},
				},
			},
			expected: "",
		},
		{
			name: "pod with no owner references",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
			},
			expected: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := getOwnerDaemonSet(c.pod, dsNameMap)
			if got != c.expected {
				t.Errorf("getOwnerDaemonSet() = %q, want %q", got, c.expected)
			}
		})
	}
}

func TestGetClusterResources(t *testing.T) {
	// Set up a fake cluster with:
	// - A deployment-owned pod in "app-ns" (via ReplicaSet -> Deployment ownership chain)
	// - A daemonset-owned pod in "infra-ns"
	// - A CNI pod in "kube-system" (should be captured in CniPod map)
	// - A pod in "kube-system" without CNI label (should be skipped via IgnoredNamespaces)
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-deploy-abc123",
			Namespace: "app-ns",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Deployment", Name: "my-deploy", APIVersion: "apps/v1"},
			},
		},
	}
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-daemonset",
			Namespace: "infra-ns",
		},
	}
	appPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-deploy-abc123-pod1",
			Namespace: "app-ns",
			Labels:    map[string]string{"app": "myapp"},
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "ReplicaSet", Name: "my-deploy-abc123", APIVersion: "apps/v1"},
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{Name: "main"},
				{Name: "istio-proxy"},
			},
		},
	}
	dsPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-daemonset-node1",
			Namespace: "infra-ns",
			Labels:    map[string]string{"app": "daemon"},
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "DaemonSet", Name: "my-daemonset", APIVersion: "apps/v1"},
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{Name: "agent"},
			},
		},
	}
	cniPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-cni-node-xyz",
			Namespace: "kube-system",
			Labels:    map[string]string{"k8s-app": "istio-cni-node"},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{Name: "install-cni"},
			},
		},
	}
	systemPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "coredns-abc",
			Namespace: "kube-system",
			Labels:    map[string]string{"k8s-app": "kube-dns"},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{Name: "coredns"},
			},
		},
	}

	objects := []runtime.Object{rs, ds, appPod, dsPod, cniPod, systemPod}
	clientset := fake.NewClientset(objects...)

	config := &config2.BugReportConfig{
		IstioNamespace: "istio-system",
		Include: []*config2.SelectionSpec{
			{Namespaces: []string{"app-ns", "infra-ns", "kube-system"}},
		},
	}

	resources, err := GetClusterResources(context.Background(), clientset, config)
	if err != nil {
		t.Fatalf("GetClusterResources() returned error: %v", err)
	}

	// Verify the deployment-owned pod is in the resource tree under app-ns/my-deploy.
	if resources.Root == nil {
		t.Fatal("expected non-nil Root")
	}
	appNsNode, ok := resources.Root["app-ns"]
	if !ok {
		t.Fatal("expected app-ns in Root")
	}
	deployNode, ok := appNsNode.(map[string]any)["my-deploy"]
	if !ok {
		t.Fatalf("expected my-deploy under app-ns, got keys: %v", appNsNode)
	}
	podNode, ok := deployNode.(map[string]any)["my-deploy-abc123-pod1"]
	if !ok {
		t.Fatalf("expected pod under my-deploy, got keys: %v", deployNode)
	}
	containers := podNode.(map[string]any)
	if _, ok := containers["main"]; !ok {
		t.Error("expected 'main' container in resource tree")
	}
	if _, ok := containers["istio-proxy"]; !ok {
		t.Error("expected 'istio-proxy' container in resource tree")
	}

	// Verify the daemonset-owned pod is in the resource tree under infra-ns/my-daemonset.
	infraNsNode, ok := resources.Root["infra-ns"]
	if !ok {
		t.Fatal("expected infra-ns in Root")
	}
	dsNode, ok := infraNsNode.(map[string]any)["my-daemonset"]
	if !ok {
		t.Fatalf("expected my-daemonset under infra-ns, got keys: %v", infraNsNode)
	}
	dsPodNode, ok := dsNode.(map[string]any)["my-daemonset-node1"]
	if !ok {
		t.Fatalf("expected pod under my-daemonset, got keys: %v", dsNode)
	}
	dsContainers := dsPodNode.(map[string]any)
	if _, ok := dsContainers["agent"]; !ok {
		t.Error("expected 'agent' container in daemonset pod")
	}

	// Verify CNI pod is tracked separately.
	cniKey := PodKey("kube-system", "istio-cni-node-xyz")
	if _, ok := resources.CniPod[cniKey]; !ok {
		t.Errorf("expected CNI pod in CniPod map with key %q", cniKey)
	}

	// Verify kube-system pods (non-CNI) are NOT in the main Pod map (IgnoredNamespaces).
	systemKey := PodKey("kube-system", "coredns-abc")
	if _, ok := resources.Pod[systemKey]; ok {
		t.Error("expected kube-system pod (coredns) to be excluded from Pod map")
	}

	// Verify labels and annotations are populated for included pods.
	appKey := PodKey("app-ns", "my-deploy-abc123-pod1")
	if labels, ok := resources.Labels[appKey]; !ok {
		t.Error("expected labels for app pod")
	} else if labels["app"] != "myapp" {
		t.Errorf("expected label app=myapp, got %v", labels)
	}

	dsKey := PodKey("infra-ns", "my-daemonset-node1")
	if _, ok := resources.Pod[dsKey]; !ok {
		t.Error("expected daemonset pod in Pod map")
	}
}
