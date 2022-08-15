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
	"fmt"
	"reflect"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	fcache "k8s.io/client-go/tools/cache/testing"
	"k8s.io/client-go/util/workqueue"

	"istio.io/istio/pkg/test/util/retry"
)

// simplified taintsetter controller build upon fake sourcer, return controller and namespace, label based sourcer created by configmap
func newMockTaintSetterController(ts *Setter,
	nodeSource *fcache.FakeControllerSource) (c *Controller,
	sourcer map[string]map[string]*fcache.FakeControllerSource,
) {
	c = &Controller{
		clientset:       ts.Client,
		podWorkQueue:    workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		nodeWorkQueue:   workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		taintsetter:     ts,
		cachedPodsStore: make(map[string]map[string]cache.Store),
	}
	// construct a series of pod controller according to the configmaps' namespace and labelselector
	c.podController = []cache.Controller{}
	sourcer = make(map[string]map[string]*fcache.FakeControllerSource)
	for _, config := range ts.configs {
		podSource := fcache.NewFakeControllerSource()
		if _, ok := sourcer[config.Namespace]; !ok {
			sourcer[config.Namespace] = make(map[string]*fcache.FakeControllerSource)
		}
		sourcer[config.Namespace][config.LabelSelector] = podSource
		tempcontroller := buildPodController(c, config, podSource)
		c.podController = append(c.podController, tempcontroller)
	}
	c.nodeStore, c.nodeController = buildNodeController(c, nodeSource)
	return c, sourcer
}

type podInfo struct {
	podName   string
	nodeName  string
	labels    []string
	namespace string
	readiness bool
}

// generate a pod based on its nodename, label, namespace, and its readiness
func mockPodGenerator(podArg podInfo) *v1.Pod {
	labelMap := make(map[string]string)
	for _, label := range podArg.labels {
		labelselector, err := metav1.ParseToLabelSelector(label)
		if err != nil {
			return nil
		}
		tempMap, _ := metav1.LabelSelectorAsMap(labelselector)
		for key, val := range tempMap {
			labelMap[key] = val
		}
	}
	var condition v1.PodCondition
	if podArg.readiness {
		condition = v1.PodCondition{
			Type:   v1.PodReady,
			Status: v1.ConditionTrue,
		}
	} else {
		condition = v1.PodCondition{
			Type:   v1.PodReady,
			Status: v1.ConditionFalse,
		}
	}
	return makePodWithTolerance(makePodArgs{
		PodName:             podArg.podName,
		Namespace:           podArg.namespace,
		Labels:              labelMap,
		InitContainerStatus: &workingInitContainer,
		Tolerations: []v1.Toleration{
			{Key: TaintName, Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoSchedule},
		},
		NodeName: podArg.nodeName,
		Conditions: []v1.PodCondition{
			condition,
		},
	})
}

type nodeInfo struct {
	nodeName  string
	hasTaint  bool
	readiness bool
}

func mockNodeGenerator(nodeArgs nodeInfo) v1.Node {
	var taint v1.Taint
	if nodeArgs.hasTaint {
		taint = v1.Taint{Key: TaintName, Effect: v1.TaintEffectNoSchedule}
	} else {
		taint = v1.Taint{}
	}
	var nodeReadiness v1.NodeCondition
	if nodeArgs.readiness {
		nodeReadiness = v1.NodeCondition{
			Type:              v1.NodeReady,
			Status:            v1.ConditionTrue,
			LastHeartbeatTime: metav1.Time{Time: time.Unix(1, 1)},
		}
	} else {
		nodeReadiness = v1.NodeCondition{
			Type:              v1.NodeReady,
			Status:            v1.ConditionFalse,
			LastHeartbeatTime: metav1.Time{Time: time.Unix(1, 1)},
		}
	}
	return makeNodeWithTaint(makeNodeArgs{NodeName: nodeArgs.nodeName, Taints: []v1.Taint{taint}, NodeCondition: []v1.NodeCondition{nodeReadiness}})
}

func TestController_ListAllNode(t *testing.T) {
	tests := []struct {
		name      string
		client    kubernetes.Interface
		nodeNames []string
		want      map[string]bool
	}{
		{
			name:      "add a node",
			client:    fakeClientset([]v1.Pod{}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			nodeNames: []string{"a"},
			want:      map[string]bool{"a": true},
		},
		{
			name:      "add several node",
			client:    fakeClientset([]v1.Pod{}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			nodeNames: []string{"a", "b", "c"},
			want:      map[string]bool{"a": true, "b": true, "c": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := Setter{configs: []ConfigSettings{}, Client: tt.client}
			source := fcache.NewFakeControllerSource()
			tc, _ := newMockTaintSetterController(&ts, source)
			stop := make(chan struct{})
			for _, name := range tt.nodeNames {
				source.Add(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}})
			}
			go tc.Run(stop)
			// Let's wait for the controller to finish processing the things we just added.
			err := retry.UntilSuccess(func() error {
				if len(tc.ListAllNode()) != len(tt.nodeNames) {
					return fmt.Errorf("found %v nodes, expected to have %v nodes", len(tt.nodeNames), len(tc.ListAllNode()))
				}
				return nil
			}, retry.Converge(1), retry.Timeout(1*time.Second), retry.Delay(100*time.Millisecond))
			if err != nil {
				t.Fatalf(err.Error())
			}
			gotItem := make(map[string]bool)
			for _, node := range tc.ListAllNode() {
				gotItem[node.Name] = true
			}
			if !reflect.DeepEqual(tt.want, gotItem) {
				t.Fatalf("expected to have %v , found %v", tt.want, gotItem)
			}
			close(stop)
		})
	}
}

func TestController_RegisterTaints(t *testing.T) {
	tests := []struct {
		name      string
		client    kubernetes.Interface
		nodeNames []string
		want      map[string]bool
	}{
		{
			name:      "add a node",
			client:    fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{}),
			nodeNames: []string{"a"},
			want:      map[string]bool{"a": true},
		},
		{
			name:      "add several node",
			client:    fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{}),
			nodeNames: []string{"a", "b", "c"},
			want:      map[string]bool{"a": true, "b": true, "c": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := Setter{configs: []ConfigSettings{}, Client: tt.client}
			source := fcache.NewFakeControllerSource()
			tc, _ := newMockTaintSetterController(&ts, source)
			stop := make(chan struct{})
			for _, name := range tt.nodeNames {
				node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}}
				source.Add(node)
				tt.client.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
			}
			go tc.Run(stop)
			tc.RegisterTaints()
			// Let's wait for the controller to finish processing the things we just added.
			err := retry.UntilSuccess(func() error {
				if len(tc.ListAllNode()) != len(tt.nodeNames) {
					return fmt.Errorf("found %v nodes, expected to have %v nodes", len(tt.nodeNames), len(tc.ListAllNode()))
				}
				return nil
			}, retry.Converge(1), retry.Timeout(1*time.Second), retry.Delay(100*time.Millisecond))
			if err != nil {
				t.Fatalf(err.Error())
			}
			gotItem := make(map[string]bool)
			err = retry.UntilSuccess(func() error {
				for _, node := range tc.ListAllNode() {
					if !tc.taintsetter.HasReadinessTaint(node) {
						return fmt.Errorf("expected all nodes have readiness taint")
					}
					gotItem[node.Name] = true
				}
				return nil
			}, retry.Converge(1), retry.Timeout(1*time.Second), retry.Delay(100*time.Millisecond))
			if err != nil {
				t.Fatalf(err.Error())
			}
			if !reflect.DeepEqual(tt.want, gotItem) {
				t.Fatalf("expected to have %v , found %v", tt.want, gotItem)
			}
			close(stop)
		})
	}
}

func TestController_CheckNodeReadiness(t *testing.T) {
	type args struct {
		podInfos []podInfo
		configs  []ConfigSettings
		node     nodeInfo
	}
	tests := []struct {
		name   string
		client kubernetes.Interface
		args   args
		want   bool
	}{
		{
			name:   "node with at least a pod satisfies critical label",
			client: fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{}),
			args: args{
				podInfos: []podInfo{
					{
						podName:   "pod1",
						nodeName:  "testing",
						labels:    []string{"app=istio"},
						namespace: "kube-system",
						readiness: true,
					},
				},
				configs: []ConfigSettings{
					{
						Name:          "istio-cni",
						Namespace:     "kube-system",
						LabelSelector: "app=istio",
					},
				},
				node: nodeInfo{
					nodeName:  "testing",
					hasTaint:  true,
					readiness: true,
				},
			},
			want: true,
		},
		{
			name:   "node with one critical label not satisfied",
			client: fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{}),
			args: args{
				podInfos: []podInfo{
					{
						podName:   "pod1",
						nodeName:  "testing",
						labels:    []string{"app=others"},
						namespace: "kube-system",
						readiness: true,
					},
					{
						podName:   "pod2",
						nodeName:  "testing",
						labels:    []string{"app=istio"},
						namespace: "other-namespace",
						readiness: true,
					},
					{
						podName:   "pod3",
						nodeName:  "testing",
						labels:    []string{"app=istio"},
						namespace: "kube-system",
						readiness: false,
					},
				},
				configs: []ConfigSettings{
					{
						Name:          "istio-cni",
						Namespace:     "kube-system",
						LabelSelector: "app=istio",
					},
				},
				node: nodeInfo{
					nodeName:  "testing",
					hasTaint:  true,
					readiness: true,
				},
			},
			want: false,
		},
		{
			name:   "empty configuration should not be tainted",
			client: fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{}),
			args: args{
				podInfos: []podInfo{},
				configs:  []ConfigSettings{},
				node: nodeInfo{
					nodeName:  "testing",
					hasTaint:  true,
					readiness: true,
				},
			},
			want: true,
		},
		{
			name:   "satisfy some labels but not all labels",
			client: fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{}),
			args: args{
				podInfos: []podInfo{
					{
						podName:   "pod1",
						nodeName:  "testing",
						labels:    []string{"app=istio"},
						namespace: "kube-system",
						readiness: true,
					},
					{
						podName:   "pod2",
						nodeName:  "testing",
						labels:    []string{"app=istio"},
						namespace: "other-namespace",
						readiness: true,
					},
					{
						podName:   "pod3",
						nodeName:  "testing",
						labels:    []string{"app=istio"},
						namespace: "kube-system",
						readiness: false,
					},
				},
				configs: []ConfigSettings{
					{
						Name:          "istio-cni",
						Namespace:     "kube-system",
						LabelSelector: "app=istio",
					},
					{
						Name:          "istio-cni",
						Namespace:     "kube-system",
						LabelSelector: "app=others",
					},
				},
				node: nodeInfo{
					nodeName:  "testing",
					hasTaint:  true,
					readiness: true,
				},
			},
			want: false,
		},
		{
			name:   "satisfy all labels",
			client: fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{}),
			args: args{
				podInfos: []podInfo{
					{
						podName:   "pod1",
						nodeName:  "testing",
						labels:    []string{"app=istio"},
						namespace: "kube-system",
						readiness: true,
					},
					{
						podName:   "pod2",
						nodeName:  "testing",
						labels:    []string{"app=istio"},
						namespace: "other-namespace",
						readiness: true,
					},
					{
						podName:   "pod3",
						nodeName:  "testing",
						labels:    []string{"app=istio"},
						namespace: "kube-system",
						readiness: false,
					},
					{
						podName:   "pod3",
						nodeName:  "testing",
						labels:    []string{"app=others"},
						namespace: "kube-system",
						readiness: true,
					},
				},
				configs: []ConfigSettings{
					{
						Name:          "istio-cni",
						Namespace:     "kube-system",
						LabelSelector: "app=istio",
					},
					{
						Name:          "istio-cni",
						Namespace:     "kube-system",
						LabelSelector: "app=others",
					},
				},
				node: nodeInfo{
					nodeName:  "testing",
					hasTaint:  true,
					readiness: true,
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := Setter{configs: tt.args.configs, Client: tt.client}
			nodeSource := fcache.NewFakeControllerSource()
			tc, podSources := newMockTaintSetterController(&ts, nodeSource)
			stop := make(chan struct{})
			go tc.Run(stop)
			currNode := mockNodeGenerator(tt.args.node)
			nodeSource.Add(&currNode)
			for _, podInfo := range tt.args.podInfos {
				tempPod := mockPodGenerator(podInfo)
				for _, label := range podInfo.labels {
					if _, ok := podSources[podInfo.namespace]; ok {
						if _, ok := podSources[podInfo.namespace][label]; ok {
							podSources[podInfo.namespace][label].Add(tempPod)
						}
					}
				}
			}
			err := retry.UntilSuccess(func() error {
				if tc.CheckNodeReadiness(currNode) != tt.want {
					return fmt.Errorf("want readiness %v, actually: %v", tt.want, tc.CheckNodeReadiness(currNode))
				}
				return nil
			}, retry.Converge(5), retry.Timeout(10*time.Second), retry.Delay(10*time.Millisecond))
			if err != nil {
				t.Errorf(err.Error())
			}
			close(stop)
		})
	}
}
