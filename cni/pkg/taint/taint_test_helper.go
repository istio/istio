package taint

import (
	"time"

	v1 "k8s.io/api/core/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type makeConfigMapArgs struct {
	ConfigName  string
	Namespace   string
	Labels      map[string]string
	Annotations map[string]string
	Data        map[string]string
}

const (
	ValidationContainerName = "istio-validation"
)

func makeConfigMap(args makeConfigMapArgs) *v1.ConfigMap {
	configmap := &v1.ConfigMap{
		TypeMeta: v12.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: v12.ObjectMeta{
			Name:        args.ConfigName,
			Namespace:   args.Namespace,
			Labels:      args.Labels,
			Annotations: args.Annotations,
		},
		Immutable:  nil,
		Data:       args.Data,
		BinaryData: nil,
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
		TypeMeta: v12.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: v12.ObjectMeta{
			Name:        args.PodName,
			Namespace:   args.Namespace,
			Labels:      args.Labels,
			Annotations: args.Annotations,
		},
		Spec: v1.PodSpec{
			NodeName: args.NodeName,
			Volumes:  nil,
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
	Labels        map[string]string
	Annotations   map[string]string
	Taints        []v1.Taint
	NodeCondition []v1.NodeCondition
}

func makeNodeWithTaint(args makeNodeArgs) *v1.Node {
	node := &v1.Node{
		TypeMeta: v12.TypeMeta{
			Kind:       "Node",
			APIVersion: "v1",
		},
		ObjectMeta: v12.ObjectMeta{
			Name:        args.NodeName,
			Labels:      args.Labels,
			Annotations: args.Annotations,
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
	//Data for configMaps
	istiocniConfig = *makeConfigMap(makeConfigMapArgs{
		ConfigName: "node.readiness",
		Namespace:  "kube-system",
		Data: map[string]string{
			"istio-cni.properties": `
									name=istio-cni
									selector=app=istio
									namespace=kube-system
									`,
		},
	})
	combinedConfig = *makeConfigMap(makeConfigMapArgs{
		ConfigName: "node.readiness",
		Namespace:  "kube-system",
		Data: map[string]string{
			"istio-cni.properties": `
									name=istio-cni
									selector=app=istio
									namespace=kube-system
									`,
			"others.properties": `
									name=others
									selector=app=others
									namespace=blah
									`,
		},
	})
	multiLabelInOneConfig = *makeConfigMap(makeConfigMapArgs{
		ConfigName: "node.readiness",
		Namespace:  "kube-system",
		Data: map[string]string{
			"istio-cni.properties": `
									name=istio-cni
									selector=app=istio, critical=others
									namespace=kube-system
									`,
		},
	})
	multiLabelsConfig = *makeConfigMap(makeConfigMapArgs{
		ConfigName: "node.readiness",
		Namespace:  "kube-system",
		Data: map[string]string{
			"istio-cni.properties": `
									name=istio-cni
									selector=app=istio
									namespace=kube-system
									`,
			"critical.properties": `name=critical-test1
									selector=critical=others
									namespace=kube-system`,
		},
	})
	emptyConfig = *makeConfigMap(makeConfigMapArgs{
		ConfigName: "node.readiness",
		Namespace:  "kube-system",
		Data:       map[string]string{},
	})
)

// Container specs
var (
	workingInitContainer = v1.ContainerStatus{
		Name: ValidationContainerName,
		State: v1.ContainerState{
			Terminated: &v1.ContainerStateTerminated{
				ExitCode: 0,
				Reason:   "Completed",
			},
		},
	}
)
var (
	//pods with specified taints for testing
	workingPod = *makePodWithTolerance(makePodArgs{
		PodName:   "WorkingPod",
		Namespace: "kube-system",
		Annotations: map[string]string{
			"sidecar.istio.io/status": "something",
		},
		Labels: map[string]string{
			//specified by config map
			"app": "istio",
		},
		InitContainerStatus: &workingInitContainer,
		Tolerations: []v1.Toleration{
			{Key: "NodeReadiness", Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoSchedule},
		},
		NodeName: "foo",
		Conditions: []v1.PodCondition{
			{
				Type:   v1.PodReady,
				Status: v1.ConditionTrue,
			},
		},
	})
	notolerantPod = *makePodWithTolerance(makePodArgs{
		PodName:   "notolerantPod",
		Namespace: "kube-system",
		Annotations: map[string]string{
			"sidecar.istio.io/status": "something",
		},
		Labels: map[string]string{
			//specified by config map
			"app": "istio",
		},
		InitContainerStatus: &workingInitContainer,
		NodeName:            "foo",
	})
	irelevantPod = *makePodWithTolerance(makePodArgs{
		PodName:   "irrelevantPod",
		Namespace: "kube-system",
		Annotations: map[string]string{
			"sidecar.istio.io/status": "something",
		},
		Labels: map[string]string{
			//specified by config map
			"app": "irrelevant",
		},
		InitContainerStatus: &workingInitContainer,
		Tolerations: []v1.Toleration{
			{Key: "NodeReadiness", Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoSchedule},
		},
		NodeName: "foo",
	})
	othersPodInKube = *makePodWithTolerance(makePodArgs{
		PodName:   "othersPodInKube",
		Namespace: "kube-system",
		Annotations: map[string]string{
			"sidecar.istio.io/status": "something",
		},
		Labels: map[string]string{
			//specified by config map
			"app":      "others",
			"critical": "others",
		},
		InitContainerStatus: &workingInitContainer,
		Tolerations: []v1.Toleration{
			{Key: "NodeReadiness", Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoSchedule},
		},
		Conditions: []v1.PodCondition{
			{
				Type:   v1.PodReady,
				Status: v1.ConditionTrue,
			},
		},
		NodeName: "foo",
	})
	othersPod = *makePodWithTolerance(makePodArgs{
		PodName:   "othersPod",
		Namespace: "blah",
		Annotations: map[string]string{
			"sidecar.istio.io/status": "something",
		},
		Labels: map[string]string{
			//specified by config map
			"app":      "others",
			"critical": "others",
		},
		InitContainerStatus: &workingInitContainer,
		Tolerations: []v1.Toleration{
			{Key: "NodeReadiness", Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoSchedule},
		},
		Conditions: []v1.PodCondition{
			{
				Type:   v1.PodReady,
				Status: v1.ConditionTrue,
			},
		},
		NodeName: "foo",
	})
	workingPodWithNoTaintNode = *makePodWithTolerance(makePodArgs{
		PodName:   "WorkingPodWithNoTaintNode",
		Namespace: "kube-system",
		Annotations: map[string]string{
			"sidecar.istio.io/status": "something",
		},
		Labels: map[string]string{
			//specified by config map
			"app": "istio",
		},
		InitContainerStatus: &workingInitContainer,
		Tolerations: []v1.Toleration{
			{Key: "NodeReadiness", Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoSchedule},
		},
		NodeName: "bar",
	})
	notReadyPod = *makePodWithTolerance(makePodArgs{
		PodName:   "NotReadyPod",
		Namespace: "kube-system",
		Annotations: map[string]string{
			"sidecar.istio.io/status": "something",
		},
		Labels: map[string]string{
			//specified by config map
			"app": "istio",
		},
		InitContainerStatus: &workingInitContainer,
		Tolerations: []v1.Toleration{
			{Key: "NodeReadiness", Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoSchedule},
		},
		NodeName: "foo",
		Conditions: []v1.PodCondition{
			{
				Type:   v1.PodReasonUnschedulable,
				Status: v1.ConditionTrue,
			},
		},
	})
	notReadyPodWithNoTaint = *makePodWithTolerance(makePodArgs{
		PodName:   "NotReadyPodWithNoTaint",
		Namespace: "kube-system",
		Annotations: map[string]string{
			"sidecar.istio.io/status": "something",
		},
		Labels: map[string]string{
			//specified by config map
			"app": "istio",
		},
		InitContainerStatus: &workingInitContainer,
		Tolerations: []v1.Toleration{
			{Key: "NodeReadiness", Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoSchedule},
		},
		NodeName: "bar",
		Conditions: []v1.PodCondition{
			{
				Type:   v1.PodReasonUnschedulable,
				Status: v1.ConditionTrue,
			},
		},
	})
	workingPodWithMultipleLabels = *makePodWithTolerance(makePodArgs{
		PodName:   "WorkingPodithMultipleLabels",
		Namespace: "kube-system",
		Annotations: map[string]string{
			"sidecar.istio.io/status": "something",
		},
		Labels: map[string]string{
			//specified by config map
			"app":      "istio",
			"critical": "others",
		},
		InitContainerStatus: &workingInitContainer,
		Tolerations: []v1.Toleration{
			{Key: "NodeReadiness", Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoSchedule},
		},
		NodeName: "foo",
		Conditions: []v1.PodCondition{
			{
				Type:   v1.PodReady,
				Status: v1.ConditionTrue,
			},
		},
	})
)
var (
	testingNode = *makeNodeWithTaint(makeNodeArgs{
		NodeName:    "foo",
		Labels:      nil,
		Annotations: nil,
		Taints:      []v1.Taint{{Key: TaintName, Effect: v1.TaintEffectNoSchedule}},
		NodeCondition: []v1.NodeCondition{
			{
				Type:              v1.NodeReady,
				Status:            v1.ConditionTrue,
				LastHeartbeatTime: v12.Time{Time: time.Unix(1, 1)},
			},
		},
	})
	plainNode = *makeNodeWithTaint(makeNodeArgs{
		NodeName:      "bar",
		Labels:        nil,
		Annotations:   nil,
		Taints:        []v1.Taint{},
		NodeCondition: []v1.NodeCondition{},
	})
	unreadyNode = *makeNodeWithTaint(makeNodeArgs{
		NodeName:    "unready",
		Labels:      nil,
		Annotations: nil,
		Taints:      []v1.Taint{},
		NodeCondition: []v1.NodeCondition{
			{
				Type:               v1.NodeReady,
				Status:             v1.ConditionTrue,
				LastHeartbeatTime:  v12.Time{Time: time.Unix(1, 1)},
				LastTransitionTime: v12.Time{Time: time.Unix(1, 0)},
			},
			{
				Type:               v1.NodeReady,
				Status:             v1.ConditionFalse,
				LastHeartbeatTime:  v12.Time{Time: time.Unix(2, 1)},
				LastTransitionTime: v12.Time{Time: time.Unix(2, 0)},
			},
		},
	})
)
