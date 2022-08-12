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

package taint

import (
	"context"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

// can have nodes, pods  and configMap in a fake clientset unit testing
func fakeClientset(pods []v1.Pod, nodes []v1.Node, configMaps []v1.ConfigMap) (cs kubernetes.Interface) {
	var csObjs []runtime.Object
	for _, node := range nodes {
		csObjs = append(csObjs, node.DeepCopy())
	}
	for _, pod := range pods {
		csObjs = append(csObjs, pod.DeepCopy())
	}
	for _, configMap := range configMaps {
		csObjs = append(csObjs, configMap.DeepCopy())
	}
	cs = fake.NewSimpleClientset(csObjs...)
	return cs
}

// test on whether critical labels and namespace are successfully loaded
func TestTaintSetter_LoadConfig(t *testing.T) {
	tests := []struct {
		name   string
		client kubernetes.Interface
		config v1.ConfigMap
		wants  []ConfigSettings
	}{
		{
			name: "istio-cni config",

			client: fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{istiocniConfig}),

			config: istiocniConfig,
			wants: []ConfigSettings{
				{Name: "istio-cni", Namespace: "kube-system", LabelSelector: "app=istio"},
			},
		},
		{
			name:   "general config",
			client: fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{istiocniConfig}),
			config: combinedConfig,
			wants: []ConfigSettings{
				{Name: "istio-cni", Namespace: "kube-system", LabelSelector: "app=istio"},
				{Name: "others", Namespace: "blah", LabelSelector: "app=others"},
			},
		},
		{
			name:   "list config",
			client: fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{istiocniConfig}),
			config: listConfig,
			wants: []ConfigSettings{
				{Name: "critical-test1", Namespace: "test1", LabelSelector: "critical=test1"},
				{Name: "addon=test2", Namespace: "test2", LabelSelector: "addon=test2"},
			},
		},
		{
			name:   "multi config",
			client: fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{istiocniConfig}),
			config: multiLabelConfig,
			wants: []ConfigSettings{
				{Name: "critical-test1", Namespace: "test1", LabelSelector: "critical=test1"},
				{Name: "critical-test1", Namespace: "test1", LabelSelector: "app=istio"},
				{Name: "addon=test2", Namespace: "test2", LabelSelector: "addon=test2"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := Setter{
				Client: fake.NewSimpleClientset(),
			}
			ts.LoadConfig(tt.config)
			gotItem := make(map[string]bool)
			for _, config := range ts.Configs() {
				gotItem[config.String()] = true
			}
			for _, want := range tt.wants {
				if _, ok := gotItem[want.String()]; !ok {
					t.Fatalf("expected config: %v, actually %v", tt.wants, ts.configs)
				}
			}
		})
	}
}

func TestTaintSetter_AddReadinessTaint(t *testing.T) {
	tests := []struct {
		name     string
		client   kubernetes.Interface
		node     v1.Node
		wantList []v1.Taint
	}{
		{
			name: "working node already get taint",

			client: fakeClientset([]v1.Pod{workingPod}, []v1.Node{testingNode}, []v1.ConfigMap{}),

			node: testingNode,

			wantList: []v1.Taint{{Key: TaintName, Effect: v1.TaintEffectNoSchedule}},
		},
		{
			name: "plain node add readiness taint",

			client: fakeClientset([]v1.Pod{workingPod}, []v1.Node{plainNode}, []v1.ConfigMap{}),
			node:   plainNode,

			wantList: []v1.Taint{{Key: TaintName, Effect: v1.TaintEffectNoSchedule}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := Setter{
				Client: tt.client,
			}
			err := ts.AddReadinessTaint(&tt.node)
			if err != nil {
				t.Fatalf("error happened in readiness %v", err.Error())
			}
			updatedNode, err := ts.Client.CoreV1().Nodes().Get(context.TODO(), tt.node.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("error happened in readiness %v", err.Error())
			}
			if !reflect.DeepEqual(updatedNode.Spec.Taints, tt.wantList) {
				t.Errorf("AddReadinessTaint() gotList = %v, want %v", updatedNode.Spec.Taints, tt.wantList)
			}
		})
	}
}

func TestTaintSetter_HasReadinessTaint(t *testing.T) {
	tests := []struct {
		name   string
		client kubernetes.Interface
		node   v1.Node
		want   bool
	}{
		{
			name:   "working node already get taint",
			client: fakeClientset([]v1.Pod{workingPod}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			node:   testingNode,
			want:   true,
		},
		{
			name:   "plain node add readiness taint",
			client: fakeClientset([]v1.Pod{workingPod}, []v1.Node{plainNode}, []v1.ConfigMap{}),
			node:   plainNode,
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := Setter{
				Client: tt.client,
			}
			hastaint := ts.HasReadinessTaint(&tt.node)
			if hastaint != tt.want {
				t.Errorf("AddReadinessTaint() gotList = %v, want %v", hastaint, tt.want)
			}
		})
	}
}

func TestTaintSetter_RemoveReadinessTaint(t *testing.T) {
	tests := []struct {
		name     string
		client   kubernetes.Interface
		node     v1.Node
		wantList []v1.Taint
	}{
		{
			name:     "working node already get taint",
			client:   fakeClientset([]v1.Pod{workingPod}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			node:     testingNode,
			wantList: []v1.Taint{},
		},
		{
			name:     "plain node add readiness taint",
			client:   fakeClientset([]v1.Pod{workingPod}, []v1.Node{plainNode}, []v1.ConfigMap{}),
			node:     plainNode,
			wantList: []v1.Taint{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := Setter{
				Client: tt.client,
			}
			err := ts.RemoveReadinessTaint(&tt.node)
			if err != nil {
				t.Errorf("error happened in readiness %v", err.Error())
			}
			gotNode, _ := ts.Client.CoreV1().Nodes().Get(context.TODO(), tt.node.Name, metav1.GetOptions{})
			if !reflect.DeepEqual(gotNode.Spec.Taints, tt.wantList) {
				t.Errorf("AddReadinessTaint() gotList = %v, want %v", gotNode.Spec.Taints, tt.wantList)
			}
		})
	}
}

func TestGetNodeLatestReadiness(t *testing.T) {
	type args struct {
		node v1.Node
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "working node is ready",
			args: args{
				testingNode,
			},
			want: true,
		},
		{
			name: "not ready node example",
			args: args{
				unreadyNode,
			},
			want: false,
		},
		{
			name: "empty case",
			args: args{
				plainNode,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetNodeLatestReadiness(tt.args.node)
			if got != tt.want {
				t.Fatalf("want %v, get %v", got, tt.want)
			}
		})
	}
}
