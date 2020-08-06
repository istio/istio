package taint

import (
	"context"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"reflect"
	"testing"
)

//can have nodes, pods  and configMap in a fake clientset unit testing
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

//test on whether critical labels and namespace are successfully loaded
func TestTaintSetter_LoadConfig(t *testing.T) {
	type fields struct {
		client kubernetes.Interface
	}
	type args struct {
		config v1.ConfigMap
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		wants  map[string][]string
	}{
		{
			name: "istio-cni config",
			fields: fields{
				client: fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{istiocniConfig}),
			},
			args: args{
				config: istiocniConfig,
			},
			wants: map[string][]string{
				"istio-cni": {"istio-cni", "kube-system", "app=istio"},
			},
		},
		{
			name: "general config",
			fields: fields{
				client: fakeClientset([]v1.Pod{}, []v1.Node{}, []v1.ConfigMap{istiocniConfig}),
			},
			args: args{
				config: combinedConfig,
			},
			wants: map[string][]string{
				"istio-cni": {"istio-cni", "kube-system", "app=istio"},
				"others":    {"others", "blah", "app=others"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := TaintSetter{
				Client: fake.NewSimpleClientset(),
			}
			ts.LoadConfig(tt.args.config)
			for _, elem := range ts.configs {
				if tt.wants[elem.name] == nil {
					t.Errorf("wants to load = %v", elem.name)
				}
				if tt.wants[elem.name][0] != elem.name {
					t.Errorf("wants to load name = %v found %v", elem.name, tt.wants[elem.name][0])
				}
				if tt.wants[elem.name][1] != elem.namespace {
					t.Errorf("wants to load namespace = %v found %v", elem.namespace, tt.wants[elem.name][1])
				}
				if tt.wants[elem.name][2] != elem.LabelSelectors {
					t.Errorf("wants to load selector = %v found %v", elem.LabelSelectors, tt.wants[elem.name][2])
				}
			}
		})
	}
}

//test on whether taint controller can detect all pods with critical labels and namespace
func TestTaintSetter_ListCandidatePods(t *testing.T) {
	type fields struct {
		client kubernetes.Interface
	}
	type args struct {
		config v1.ConfigMap
	}
	tests := []struct {
		name     string
		fields   fields
		args     args
		wantList v1.PodList
	}{
		{
			name: "working pod detect",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod}, []v1.Node{testingNode, plainNode}, []v1.ConfigMap{}),
			},
			args: args{
				config: istiocniConfig,
			},
			wantList: v1.PodList{Items: []v1.Pod{workingPod}},
		},
		{
			name: "additional no tolerant pod",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, notolerantPod}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				config: istiocniConfig,
			},
			wantList: v1.PodList{Items: []v1.Pod{workingPod}},
		},
		{
			name: "additional irrelevant pod",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, notolerantPod, irelevantPod}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				config: istiocniConfig,
			},
			wantList: v1.PodList{Items: []v1.Pod{workingPod}},
		},
		{
			name: "additional others pod",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, notolerantPod, irelevantPod, othersPod}, []v1.Node{testingNode, plainNode}, []v1.ConfigMap{}),
			},
			args: args{
				config: combinedConfig,
			},
			wantList: v1.PodList{Items: []v1.Pod{workingPod, othersPod}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := TaintSetter{
				Client: tt.fields.client,
			}
			ts.LoadConfig(tt.args.config)
			gotList, err := ts.ListCandidatePods()
			if err != nil {
				t.Errorf("something wrong in list")
				return
			}
			if gotItems := gotList.Items; gotItems != nil {
				podsSet := make(map[string]bool)
				for _, item := range gotItems {
					podsSet[item.String()] = true
				}
				for _, item := range tt.wantList.Items {
					if _, ok := podsSet[item.String()]; !ok {
						t.Errorf("ListBrokenPods() not have %s", item.String())
					}
				}
			}
		})
	}
}
func TestTaintSetter_AddReadinessTaint(t *testing.T) {
	type fields struct {
		client kubernetes.Interface
	}
	type args struct {
		node v1.Node
	}
	tests := []struct {
		name     string
		fields   fields
		args     args
		wantList []v1.Taint
	}{
		{
			name: "working node already get taint",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				testingNode,
			},
			wantList: []v1.Taint{{Key: TaintName, Effect: v1.TaintEffectNoSchedule}},
		},
		{
			name: "plain node add readiness taint",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod}, []v1.Node{plainNode}, []v1.ConfigMap{}),
			},
			args: args{
				plainNode,
			},
			wantList: []v1.Taint{{Key: TaintName, Effect: v1.TaintEffectNoSchedule}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := TaintSetter{
				Client: tt.fields.client,
			}
			err := ts.AddReadinessTaint(&tt.args.node)
			if err != nil {
				t.Errorf("error happened in readiness %v", err.Error())
				return
			}
			updatedNode, err := ts.Client.CoreV1().Nodes().Get(context.TODO(), tt.args.node.Name, metav1.GetOptions{})
			if err != nil {
				t.Errorf("error happened in readiness %v", err.Error())
				return
			}
			if !reflect.DeepEqual(updatedNode.Spec.Taints, tt.wantList) {
				t.Errorf("AddReadinessTaint() gotList = %v, want %v", updatedNode.Spec.Taints, tt.wantList)
			}
		})
	}
}
func TestTaintSetter_HasReadinessTaint(t *testing.T) {
	type fields struct {
		client kubernetes.Interface
	}
	type args struct {
		node v1.Node
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "working node already get taint",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				testingNode,
			},
			want: true,
		},
		{
			name: "plain node add readiness taint",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod}, []v1.Node{plainNode}, []v1.ConfigMap{}),
			},
			args: args{
				plainNode,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := TaintSetter{
				Client: tt.fields.client,
			}
			hastaint := ts.HasReadinessTaint(&tt.args.node)
			if hastaint != tt.want {
				t.Errorf("AddReadinessTaint() gotList = %v, want %v", hastaint, tt.want)
			}
		})
	}
}
func TestTaintSetter_RemoveReadinessTaint(t *testing.T) {
	type fields struct {
		client kubernetes.Interface
	}
	type args struct {
		node v1.Node
	}
	tests := []struct {
		name     string
		fields   fields
		args     args
		wantList []v1.Taint
	}{
		{
			name: "working node already get taint",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				testingNode,
			},
			wantList: []v1.Taint{},
		},
		{
			name: "plain node add readiness taint",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod}, []v1.Node{plainNode}, []v1.ConfigMap{}),
			},
			args: args{
				plainNode,
			},
			wantList: []v1.Taint{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := TaintSetter{
				Client: tt.fields.client,
			}
			err := ts.RemoveReadinessTaint(&tt.args.node)
			if err != nil {
				t.Errorf("error happened in readiness %v", err.Error())
			}
			gotNode, _ := ts.Client.CoreV1().Nodes().Get(context.TODO(), tt.args.node.Name, metav1.GetOptions{})
			if !reflect.DeepEqual(gotNode.Spec.Taints, tt.wantList) {
				t.Errorf("AddReadinessTaint() gotList = %v, want %v", gotNode.Spec.Taints, tt.wantList)
			}
		})
	}
}
func TestTaintSetter_GetAllCriticalPodsInNode(t *testing.T) {
	type fields struct {
		client kubernetes.Interface
	}
	type args struct {
		config v1.ConfigMap
		node   v1.Node
	}
	tests := []struct {
		name     string
		fields   fields
		args     args
		wantList v1.PodList
	}{
		{
			name: "working pod detect",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, notolerantPod, othersPod}, []v1.Node{testingNode, plainNode}, []v1.ConfigMap{}),
			},
			args: args{
				config: istiocniConfig,
				node:   testingNode,
			},
			wantList: v1.PodList{Items: []v1.Pod{workingPod}},
		},
		{
			name: "no critical pod",
			fields: fields{
				client: fakeClientset([]v1.Pod{irelevantPod, notolerantPod, othersPod}, []v1.Node{testingNode, plainNode}, []v1.ConfigMap{}),
			},
			args: args{
				config: istiocniConfig,
				node:   testingNode,
			},
			wantList: v1.PodList{Items: []v1.Pod{}},
		},
		{
			name: "several critical pod",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, notolerantPod, othersPod}, []v1.Node{testingNode, plainNode}, []v1.ConfigMap{}),
			},
			args: args{
				config: combinedConfig,
				node:   testingNode,
			},
			wantList: v1.PodList{Items: []v1.Pod{workingPod, othersPod}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := TaintSetter{
				Client: tt.fields.client,
			}
			ts.LoadConfig(tt.args.config)
			gotList, err := ts.GetAllCriticalPodsInNode(&tt.args.node)
			if err != nil {
				t.Errorf("something wrong in list")
				return
			}
			if gotItems := gotList.Items; gotItems != nil {
				if !reflect.DeepEqual(gotItems, tt.wantList.Items) {
					t.Errorf("ListBrokenPods() gotList = %v, want %v", gotItems, tt.wantList.Items)
				}
			}

		})
	}
}
func TestTaintSetter_GetNodeByPod(t *testing.T) {
	type fields struct {
		client kubernetes.Interface
	}
	type args struct {
		config v1.ConfigMap
		pod    v1.Pod
	}
	tests := []struct {
		name     string
		fields   fields
		args     args
		wantNode v1.Node
	}{
		{
			name: "working pod detect",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, notolerantPod, othersPod, workingPodWithNoTaintNode}, []v1.Node{testingNode, plainNode}, []v1.ConfigMap{}),
			},
			args: args{
				config: istiocniConfig,
				pod:    workingPodWithNoTaintNode,
			},
			wantNode: plainNode,
		},
		{
			name: "working pod detect",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, notolerantPod, othersPod, workingPodWithNoTaintNode}, []v1.Node{testingNode, plainNode}, []v1.ConfigMap{}),
			},
			args: args{
				config: istiocniConfig,
				pod:    workingPod,
			},
			wantNode: testingNode,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := TaintSetter{
				Client: tt.fields.client,
			}
			ts.LoadConfig(tt.args.config)
			gotNode, err := ts.GetNodeByPod(&tt.args.pod)
			if err != nil {
				t.Errorf("something wrong in list")
				return
			}
			if !reflect.DeepEqual(gotNode, &tt.wantNode) {
				t.Errorf("ListBrokenPods() gotList = %v, want %v", gotNode, &tt.wantNode)
			}
		})
	}
}
func TestTaintSetter_ProcessNode(t *testing.T) {
	type fields struct {
		client kubernetes.Interface
	}
	type args struct {
		node   v1.Node
		config v1.ConfigMap
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "working node already get taint",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, othersPod}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				testingNode,
				istiocniConfig,
			},
			want: false,
		},
		{
			name: "tainted node with not ready pod",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, othersPod, notReadyPod}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				testingNode,
				istiocniConfig,
			},
			want: false,
		},
		{
			name: "tainted node with not ready pod",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, othersPod, notReadyPod}, []v1.Node{unreadyNode}, []v1.ConfigMap{}),
			},
			args: args{
				unreadyNode,
				istiocniConfig,
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := TaintSetter{
				Client: tt.fields.client,
			}
			ts.LoadConfig(tt.args.config)
			err := ts.ProcessNode(&tt.args.node)
			if err != nil {
				t.Errorf("error: %s", err.Error())
			}
			if ts.HasReadinessTaint(&tt.args.node) != tt.want {
				t.Errorf("want to taint: %v, real: %v", ts.HasReadinessTaint(&tt.args.node), tt.want)
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

//when pod is ready, it should check all critical labels in the corresponding node and check their readiness

func TestTaintSetter_ProcessReadyPod(t *testing.T) {
	type fields struct {
		client kubernetes.Interface
	}
	type args struct {
		pod    v1.Pod
		config v1.ConfigMap
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "working node already get taint",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, othersPod}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				workingPod,
				istiocniConfig,
			},
			want: false,
		},
		{
			name: "tainted node with not ready pod, should not be tainted because all labels are satisfied",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, othersPod, notReadyPod}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				notReadyPod,
				istiocniConfig,
			},
			want: false,
		},
		{
			name: "tainted node with not ready pod, check ready pod",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, othersPod, notReadyPod}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				workingPod,
				istiocniConfig,
			},
			want: false,
		},
		{
			name: "tainted node with not ready pod",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, othersPod, notReadyPod, workingPodWithNoTaintNode}, []v1.Node{testingNode, plainNode}, []v1.ConfigMap{}),
			},
			args: args{
				workingPodWithNoTaintNode,
				istiocniConfig,
			},
			want: true,
		},
		{
			name: "tainted node with ready pod but do not satisfy all labels",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, othersPod}, []v1.Node{testingNode, plainNode}, []v1.ConfigMap{}),
			},
			args: args{
				workingPod,
				multiLabelsConfig,
			},
			want: true,
		},
		{
			name: "tainted node with ready pod, satisfy all labels",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, othersPod, workingPodWithMultipleLabels}, []v1.Node{testingNode, plainNode}, []v1.ConfigMap{}),
			},
			args: args{
				workingPod,
				multiLabelsConfig,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := TaintSetter{
				Client: tt.fields.client,
			}
			ts.LoadConfig(tt.args.config)
			err := ts.ProcessReadyPod(&tt.args.pod)
			if err != nil {
				t.Errorf("error: %s", err.Error())
			}
			node, _ := ts.GetNodeByPod(&tt.args.pod)
			if ts.HasReadinessTaint(node) != tt.want {
				t.Errorf("want to taint: %v, real: %v", ts.HasReadinessTaint(node), tt.want)
			}

		})
	}
}
func TestTaintSetter_ProcessUnReadyPod(t *testing.T) {
	type fields struct {
		client kubernetes.Interface
	}
	type args struct {
		pod    v1.Pod
		config v1.ConfigMap
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "tainted node with not ready pod",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, othersPod, notReadyPod}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				notReadyPod,
				istiocniConfig,
			},
			want: true,
		},
		{
			name: "tainted node with not ready pod",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, othersPod, notReadyPod, notReadyPodWithNoTaint}, []v1.Node{testingNode, plainNode}, []v1.ConfigMap{}),
			},
			args: args{
				notReadyPodWithNoTaint,
				istiocniConfig,
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := TaintSetter{
				Client: tt.fields.client,
			}
			ts.LoadConfig(tt.args.config)
			err := ts.ProcessUnReadyPod(&tt.args.pod)
			if err != nil {
				t.Errorf("error: %s", err.Error())
			}
			node, _ := ts.GetNodeByPod(&tt.args.pod)
			if ts.HasReadinessTaint(node) != tt.want {
				t.Errorf("want to taint: %v, real: %v", ts.HasReadinessTaint(node), tt.want)
			}

		})
	}
}
func TestTaintSetter_CheckNodeReadiness(t *testing.T) {
	type fields struct {
		client kubernetes.Interface
	}
	type args struct {
		node   v1.Node
		config v1.ConfigMap
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "not ready because labels are not satisfied",
			fields: fields{
				client: fakeClientset([]v1.Pod{irelevantPod, othersPod}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				testingNode,
				istiocniConfig,
			},
			want: false,
		},
		{
			name: "empty configuration should not be tainted",
			fields: fields{
				client: fakeClientset([]v1.Pod{irelevantPod, othersPod}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				testingNode,
				emptyConfig,
			},
			want: true,
		},
		{
			name: "satisfy some labels but not all labels",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				testingNode,
				multiLabelInOneConfig,
			},
			want: false,
		},
		{
			name: "satisfy all labels, should be ready",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, othersPod, workingPodWithMultipleLabels}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				testingNode,
				multiLabelInOneConfig,
			},
			want: true,
		},
		{
			name: "satisfy all labels in several labels, should be ready",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, othersPod, othersPodInKube}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				testingNode,
				multiLabelInOneConfig,
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := TaintSetter{
				Client: tt.fields.client,
			}
			ts.LoadConfig(tt.args.config)
			got := ts.CheckNodeReadiness(&tt.args.node)
			if got != tt.want {
				t.Errorf("got %v, actually %v", got, tt.want)
			}
		})
	}
}
func TestTaintSetter_GetCriticalLabels(t *testing.T) {
	type fields struct {
		client kubernetes.Interface
	}
	type args struct {
		pod    v1.Pod
		config v1.ConfigMap
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   map[string]bool
	}{
		{
			name: "working pod test",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, othersPod, notReadyPod}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				workingPod,
				istiocniConfig,
			},
			want: map[string]bool{"app=istio": true},
		},
		{
			name: "working pod with one label defined in configmap",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, othersPod, notReadyPod, workingPodWithMultipleLabels}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				workingPod,
				multiLabelInOneConfig,
			},
			want: map[string]bool{"app=istio": true},
		},
		{
			name: "working pod with several labels",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, othersPod, notReadyPod, workingPodWithMultipleLabels}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				workingPodWithMultipleLabels,
				multiLabelInOneConfig,
			},
			want: map[string]bool{"app=istio": true, "critical=others": true},
		},
		{
			name: "working pod with several labels",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, othersPod, notReadyPod, workingPodWithMultipleLabels}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				workingPodWithMultipleLabels,
				multiLabelsConfig,
			},
			want: map[string]bool{"app=istio": true, "critical=others": true},
		},
		{
			name: "working pod with no labels",
			fields: fields{
				client: fakeClientset([]v1.Pod{workingPod, irelevantPod, othersPod, notReadyPod, workingPodWithMultipleLabels}, []v1.Node{testingNode}, []v1.ConfigMap{}),
			},
			args: args{
				irelevantPod,
				multiLabelInOneConfig,
			},
			want: map[string]bool{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := TaintSetter{
				Client: tt.fields.client,
			}
			ts.LoadConfig(tt.args.config)
			labels := ts.GetCriticalLabels(tt.args.pod)
			if len(labels) != len(tt.want) {
				t.Errorf("expected to get %v labels, actually get %v", len(tt.want), len(labels))
			}
			for _, label := range labels {
				if _, ok := tt.want[label]; !ok {
					t.Errorf("got %v, actually %v", labels, tt.want)
				}
			}
		})
	}
}
