package taint

import (
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

//simplified taintsetter controller build upon fake sourcer, return controller and namespace, label based sourcer created by configmap
func newMockTaintSetterController(ts *TaintSetter, nodeSource *fcache.FakeControllerSource) (c *Controller, sourcer map[string]map[string]*fcache.FakeControllerSource, e error) {
	c = &Controller{
		clientset:       ts.Client,
		podWorkQueue:    workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		nodeWorkQueue:   workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		taintsetter:     ts,
		cachedPodsStore: make(map[string]map[string]cache.Store),
	}
	//construct a series of pod controller according to the configmaps' namespace and labelselector
	c.podController = []cache.Controller{}
	sourcer = make(map[string]map[string]*fcache.FakeControllerSource)
	for _, config := range ts.configs {
		podSource := fcache.NewFakeControllerSource()
		if _, ok := sourcer[config.Namespace]; !ok {
			sourcer[config.Namespace] = make(map[string]*fcache.FakeControllerSource)
		}
		sourcer[config.Namespace][config.LabelSelector] = podSource
		tempcontroller := buildMockPodController(c, config, podSource)
		c.podController = append(c.podController, tempcontroller)
	}
	c.nodeStore, c.nodeController = buildNodeControler(c, nodeSource)
	return c, sourcer, nil
}

//mocking api for podsController
func buildMockPodController(c *Controller, config ConfigSettings, source cache.ListerWatcher) cache.Controller {
	tempstore, tempcontroller := cache.NewInformer(source, &v1.Pod{}, time.Millisecond*100, cache.ResourceEventHandlerFuncs{
		AddFunc: func(newObj interface{}) {
			//remove filter condition will introduce a lot of error handling in workqueue
			err := validTaintByPod(newObj, c)
			if err != nil {
				return
			}
			c.podWorkQueue.AddRateLimited(newObj)
		},
		UpdateFunc: func(_, newObj interface{}) {
			c.podWorkQueue.AddRateLimited(newObj)
		},
		DeleteFunc: func(newObj interface{}) {
			err := reTaintNodeByPod(newObj, c)
			if err != nil {
				return
			}
		},
	})
	if _, ok := c.cachedPodsStore[config.Namespace]; !ok {
		c.cachedPodsStore[config.Namespace] = make(map[string]cache.Store)
	}
	c.cachedPodsStore[config.Namespace][config.LabelSelector] = tempstore
	return tempcontroller
}

type podInfo struct {
	podName   string
	nodeName  string
	labels    []string
	namespace string
	readiness bool
}

//generate a pod based on its nodename, label, namespace, and its readiness
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

func mockNodeGenerator(nodeArgs nodeInfo) *v1.Node {
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
	type fields struct {
		client kubernetes.Interface
	}
	type args struct {
		nodeNames []string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   map[string]bool
	}{
		{
			name: "add a node",
			fields: fields{
				client: fakeClientset([]v1.Pod{}, []v1.Node{testingNode}, []v1.ConfigMap{})},
			args: args{nodeNames: []string{"a"}},
			want: map[string]bool{"a": true},
		},
		{
			name: "add several node",
			fields: fields{
				client: fakeClientset([]v1.Pod{}, []v1.Node{testingNode}, []v1.ConfigMap{})},
			args: args{nodeNames: []string{"a", "b", "c"}},
			want: map[string]bool{"a": true, "b": true, "c": true},
		},
	}
	for _, tt := range tests {
		ts := TaintSetter{configs: []ConfigSettings{}, Client: tt.fields.client}
		source := fcache.NewFakeControllerSource()
		tc, _, err := newMockTaintSetterController(&ts, source)
		if err != nil {
			t.Fatalf("cannot constrict taint controller")
		}
		stop := make(chan struct{})
		go tc.Run(stop)
		for _, name := range tt.args.nodeNames {
			source.Add(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}})
		}
		// Let's wait for the controller to finish processing the things we just added.
		time.Sleep(100 * time.Millisecond)
		if len(tc.ListAllNode()) != len(tt.args.nodeNames) {
			t.Fatalf("found %v nodes, expected to have %v nodes", len(tt.args.nodeNames), len(tc.ListAllNode()))
		}
		gotItem := make(map[string]bool)
		for _, node := range tc.ListAllNode() {
			gotItem[node.Name] = true
		}
		if !reflect.DeepEqual(tt.want, gotItem) {
			t.Fatalf("expected to have %v , found %v", tt.want, gotItem)
		}
		close(stop)
	}
}
func TestController_RegistTaints(t *testing.T) {
	type fields struct {
		client kubernetes.Interface
	}
	type args struct {
		nodeNames []string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   map[string]bool
	}{
		{
			name: "add a node",
			fields: fields{
				client: fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{})},
			args: args{nodeNames: []string{"a"}},
			want: map[string]bool{"a": true},
		},
		{
			name: "add several node",
			fields: fields{
				client: fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{})},
			args: args{nodeNames: []string{"a", "b", "c"}},
			want: map[string]bool{"a": true, "b": true, "c": true},
		},
	}
	for _, tt := range tests {
		ts := TaintSetter{configs: []ConfigSettings{}, Client: tt.fields.client}
		source := fcache.NewFakeControllerSource()
		tc, _, err := newMockTaintSetterController(&ts, source)
		if err != nil {
			t.Fatalf("cannot constrict taint controller")
		}
		stop := make(chan struct{})
		go tc.Run(stop)
		for _, name := range tt.args.nodeNames {
			source.Add(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}})
		}
		tc.RegistTaints()
		// Let's wait for the controller to finish processing the things we just added.
		time.Sleep(100 * time.Millisecond)
		if len(tc.ListAllNode()) != len(tt.args.nodeNames) {
			t.Fatalf("found %v nodes, expected to have %v nodes", len(tt.args.nodeNames), len(tc.ListAllNode()))
		}
		gotItem := make(map[string]bool)
		for _, node := range tc.ListAllNode() {
			if !tc.taintsetter.HasReadinessTaint(node) {
				t.Fatalf("expected all nodes have readiness taint")
			}
			gotItem[node.Name] = true
		}
		if !reflect.DeepEqual(tt.want, gotItem) {
			t.Fatalf("expected to have %v , found %v", tt.want, gotItem)
		}
		close(stop)
	}
}

func TestController_CheckNodeReadiness(t *testing.T) {
	type fields struct {
		client kubernetes.Interface
	}
	type args struct {
		podInfos []podInfo
		configs  []ConfigSettings
		node     nodeInfo
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "node with at least a pod satisfies critical label",
			fields: fields{
				client: fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{})},
			args: args{
				podInfos: []podInfo{{
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
			name: "node with one critical label not satisfied",
			fields: fields{
				client: fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{})},
			args: args{
				podInfos: []podInfo{{
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
			name: "empty configuration should not be tainted",
			fields: fields{
				client: fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{})},
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
			name: "satisfy some labels but not all labels",
			fields: fields{
				client: fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{})},
			args: args{
				podInfos: []podInfo{{
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
			name: "satisfy all labels",
			fields: fields{
				client: fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{})},
			args: args{
				podInfos: []podInfo{{
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
		ts := TaintSetter{configs: tt.args.configs, Client: tt.fields.client}
		nodeSource := fcache.NewFakeControllerSource()
		tc, podSources, err := newMockTaintSetterController(&ts, nodeSource)
		if err != nil {
			t.Fatalf("cannot constrict taint controller")
		}
		stop := make(chan struct{})
		go tc.Run(stop)
		currNode := mockNodeGenerator(tt.args.node)
		nodeSource.Add(currNode)
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
		err = retry.UntilSuccess(func() error {
			if tc.CheckNodeReadiness(*currNode) != tt.want {
				return fmt.Errorf("want readiness %v, actually: %v", tt.want, tc.CheckNodeReadiness(*currNode))
			}
			return nil
		}, retry.Converge(5), retry.Timeout(10*time.Second), retry.Delay(10*time.Millisecond))
		if err != nil {
			t.Errorf(err.Error())
		}
		close(stop)
	}
}
