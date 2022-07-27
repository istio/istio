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
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

type makeConfigMapArgs struct {
	ConfigName  string
	Namespace   string
	Labels      map[string]string
	Annotations map[string]string
	Data        map[string]string
}

func makeConfigMap(args makeConfigMapArgs) v1.ConfigMap {
	configmap := v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        args.ConfigName,
			Namespace:   args.Namespace,
			Labels:      args.Labels,
			Annotations: args.Annotations,
		},
		Data: args.Data,
	}
	return configmap
}

type makePodArgs struct {
	PodName             string
	Namespace           string
	Labels              map[string]string
	Annotations         map[string]string
	InitContainerName   string
	InitContainerStatus *v1.ContainerStatus
	Tolerations         []v1.Toleration
	NodeName            string
	Conditions          []v1.PodCondition
}

func makePodWithTolerance(args makePodArgs) *v1.Pod {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        args.PodName,
			Namespace:   args.Namespace,
			Labels:      args.Labels,
			Annotations: args.Annotations,
		},
		Spec: v1.PodSpec{
			NodeName: args.NodeName,
			InitContainers: []v1.Container{
				{
					Name: args.InitContainerName,
				},
			},
			Containers: []v1.Container{
				{
					Name: "payload-container",
				},
			},
			Tolerations: args.Tolerations,
		},
		Status: v1.PodStatus{
			Conditions: args.Conditions,
			InitContainerStatuses: []v1.ContainerStatus{
				*args.InitContainerStatus,
			},
			ContainerStatuses: []v1.ContainerStatus{
				{
					Name: "payload-container",
					State: v1.ContainerState{
						Waiting: &v1.ContainerStateWaiting{
							Reason: "PodInitializing",
						},
					},
				},
			},
		},
	}
	return pod
}

type makeNodeArgs struct {
	NodeName      string
	Taints        []v1.Taint
	NodeCondition []v1.NodeCondition
}

func makeNodeWithTaint(args makeNodeArgs) v1.Node {
	node := v1.Node{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Node",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: args.NodeName,
		},
		Spec: v1.NodeSpec{
			Taints: args.Taints,
		},
		Status: v1.NodeStatus{
			Conditions: args.NodeCondition,
		},
	}
	return node
}

var (
	// Data for configMaps
	istiocniConfig = makeConfigMap(makeConfigMapArgs{
		ConfigName: "node.readiness",
		Namespace:  "kube-system",
		Data: map[string]string{
			"istio-cni": `- name: istio-cni
  selector: app=istio 
  namespace: kube-system`,
		},
	})
	combinedConfig = makeConfigMap(makeConfigMapArgs{
		ConfigName: "node.readiness",
		Namespace:  "kube-system",
		Data: map[string]string{
			"istio-cni": `- name: istio-cni
  selector: app=istio
  namespace: kube-system`,
			"others": `- name: others
  selector: app=others	
  namespace: blah`,
		},
	})
	listConfig = makeConfigMap(makeConfigMapArgs{
		ConfigName: "node.readiness",
		Namespace:  "kube-system",
		Data: map[string]string{
			"istio-cni": `- name: critical-test1
  selector: critical=test1
  namespace: test1
- name: addon=test2
  selector: addon=test2
  namespace: test2
`,
		},
	})
	multiLabelConfig = makeConfigMap(makeConfigMapArgs{
		ConfigName: "node.readiness",
		Namespace:  "kube-system",
		Data: map[string]string{
			"istio-cni": `- name: critical-test1
  selector: critical=test1, app=istio
  namespace: test1
- name: addon=test2
  selector: addon=test2
  namespace: test2
`,
		},
	})
)

// Container specs
var (
	workingInitContainer = v1.ContainerStatus{
		Name: constants.ValidationContainerName,
		State: v1.ContainerState{
			Terminated: &v1.ContainerStateTerminated{
				ExitCode: 0,
				Reason:   "Completed",
			},
		},
	}
)

// pods with specified taints for testing
var workingPod = *makePodWithTolerance(makePodArgs{
	PodName:   "WorkingPod",
	Namespace: "kube-system",
	Annotations: map[string]string{
		"sidecar.istio.io/status": "something",
	},
	Labels: map[string]string{
		// specified by config map
		"app": "istio",
	},
	InitContainerStatus: &workingInitContainer,
	Tolerations: []v1.Toleration{
		{Key: TaintName, Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoSchedule},
	},
	NodeName: "foo",
	Conditions: []v1.PodCondition{
		{
			Type:   v1.PodReady,
			Status: v1.ConditionTrue,
		},
	},
})

var (
	testingNode = makeNodeWithTaint(makeNodeArgs{
		NodeName: "foo",
		Taints:   []v1.Taint{{Key: TaintName, Effect: v1.TaintEffectNoSchedule}},
		NodeCondition: []v1.NodeCondition{
			{
				Type:              v1.NodeReady,
				Status:            v1.ConditionTrue,
				LastHeartbeatTime: metav1.Time{Time: time.Unix(1, 1)},
			},
		},
	})
	plainNode = makeNodeWithTaint(makeNodeArgs{
		NodeName:      "bar",
		Taints:        []v1.Taint{},
		NodeCondition: []v1.NodeCondition{},
	})
	unreadyNode = makeNodeWithTaint(makeNodeArgs{
		NodeName: "unready",
		Taints:   []v1.Taint{},
		NodeCondition: []v1.NodeCondition{
			{
				Type:               v1.NodeReady,
				Status:             v1.ConditionTrue,
				LastHeartbeatTime:  metav1.Time{Time: time.Unix(1, 1)},
				LastTransitionTime: metav1.Time{Time: time.Unix(1, 0)},
			},
			{
				Type:               v1.NodeReady,
				Status:             v1.ConditionFalse,
				LastHeartbeatTime:  metav1.Time{Time: time.Unix(2, 1)},
				LastTransitionTime: metav1.Time{Time: time.Unix(2, 0)},
			},
		},
	})
)
