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

package repair

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/workqueue"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	"istio.io/pkg/monitoring"
)

func TestBrokenPodReconciler_detectPod(t *testing.T) {
	makeDetectPod := func(name string, terminationMessage string, exitCode int) *v1.Pod {
		return makePod(makePodArgs{
			PodName:     name,
			Annotations: map[string]string{"sidecar.istio.io/status": "something"},
			InitContainerStatus: &v1.ContainerStatus{
				Name: constants.ValidationContainerName,
				State: v1.ContainerState{
					Waiting: &v1.ContainerStateWaiting{
						Reason:  "CrashLoopBackOff",
						Message: "Back-off 5m0s restarting failed blah blah blah",
					},
				},
				LastTerminationState: v1.ContainerState{
					Terminated: &v1.ContainerStateTerminated{
						Message:  terminationMessage,
						ExitCode: int32(exitCode),
					},
				},
			},
		})
	}

	type args struct {
		pod v1.Pod
	}
	tests := []struct {
		name   string
		config config.RepairConfig
		args   args
		want   bool
	}{
		{
			"Testing OK pod with only ExitCode check",
			config.RepairConfig{
				SidecarAnnotation: "sidecar.istio.io/status",
				InitContainerName: constants.ValidationContainerName,
				InitExitCode:      126,
			},
			args{pod: workingPod},
			false,
		},
		{
			"Testing working pod (that previously died) with only ExitCode check",
			config.RepairConfig{
				SidecarAnnotation: "sidecar.istio.io/status",
				InitContainerName: constants.ValidationContainerName,
				InitExitCode:      126,
			},
			args{pod: workingPodDiedPreviously},
			false,
		},
		{
			"Testing broken pod (in waiting state) with only ExitCode check",
			config.RepairConfig{
				SidecarAnnotation: "sidecar.istio.io/status",
				InitContainerName: constants.ValidationContainerName,
				InitExitCode:      126,
			},
			args{pod: brokenPodWaiting},
			true,
		},
		{
			"Testing broken pod (in terminating state) with only ExitCode check",
			config.RepairConfig{
				SidecarAnnotation: "sidecar.istio.io/status",
				InitContainerName: constants.ValidationContainerName,
				InitExitCode:      126,
			},
			args{pod: brokenPodTerminating},
			true,
		},
		{
			"Testing broken pod with wrong ExitCode",
			config.RepairConfig{
				SidecarAnnotation: "sidecar.istio.io/status",
				InitContainerName: constants.ValidationContainerName,
				InitExitCode:      55,
			},
			args{pod: brokenPodWaiting},
			false,
		},
		{
			"Testing broken pod with no annotation (should be ignored)",
			config.RepairConfig{
				SidecarAnnotation: "sidecar.istio.io/status",
				InitContainerName: constants.ValidationContainerName,
				InitExitCode:      126,
			},
			args{pod: brokenPodNoAnnotation},
			false,
		},
		{
			"Check termination message match false",
			config.RepairConfig{
				SidecarAnnotation:  "sidecar.istio.io/status",
				InitContainerName:  constants.ValidationContainerName,
				InitTerminationMsg: "Termination Message",
			},
			args{
				pod: *makeDetectPod(
					"TerminationMessageMatchFalse",
					"This Does Not Match",
					0),
			},
			false,
		},
		{
			"Check termination message match true",
			config.RepairConfig{
				SidecarAnnotation:  "sidecar.istio.io/status",
				InitContainerName:  constants.ValidationContainerName,
				InitTerminationMsg: "Termination Message",
			},
			args{
				pod: *makeDetectPod(
					"TerminationMessageMatchTrue",
					"Termination Message",
					0),
			},
			true,
		},
		{
			"Check termination message match true for trailing and leading space",
			config.RepairConfig{
				SidecarAnnotation:  "sidecar.istio.io/status",
				InitContainerName:  constants.ValidationContainerName,
				InitTerminationMsg: "            Termination Message",
			},
			args{
				pod: *makeDetectPod(
					"TerminationMessageMatchTrueLeadingSpace",
					"Termination Message              ",
					0),
			},
			true,
		},
		{
			"Check termination code match false",
			config.RepairConfig{
				SidecarAnnotation: "sidecar.istio.io/status",
				InitContainerName: constants.ValidationContainerName,
				InitExitCode:      126,
			},
			args{
				pod: *makeDetectPod(
					"TerminationCodeMatchFalse",
					"",
					121),
			},
			false,
		},
		{
			"Check termination code match true",
			config.RepairConfig{
				SidecarAnnotation: "sidecar.istio.io/status",
				InitContainerName: constants.ValidationContainerName,
				InitExitCode:      126,
			},
			args{
				pod: *makeDetectPod(
					"TerminationCodeMatchTrue",
					"",
					126),
			},
			true,
		},
		{
			"Check badly formatted pod",
			config.RepairConfig{
				SidecarAnnotation: "sidecar.istio.io/status",
				InitContainerName: constants.ValidationContainerName,
				InitExitCode:      126,
			},
			args{
				pod: *makePod(makePodArgs{
					PodName:             "Test",
					Annotations:         map[string]string{"sidecar.istio.io/status": "something"},
					InitContainerStatus: &v1.ContainerStatus{},
				}),
			},
			false,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			bpr := brokenPodReconciler{
				client: fake.NewSimpleClientset(),
				cfg:    &tt.config,
			}
			if got := bpr.detectPod(tt.args.pod); got != tt.want {
				t.Errorf("detectPod() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Test the ListBrokenPods function
// TODO:(stewartbutler) Add some simple field selector filter test logic to the client-go
// fake client. The fake client does NOT support filtering by field selector,
// so we need to add that ourselves to complete the test.
func TestBrokenPodReconciler_listBrokenPods(t *testing.T) {
	type fields struct {
		client kubernetes.Interface
		config config.RepairConfig
	}
	tests := []struct {
		name     string
		fields   fields
		wantList v1.PodList
	}{
		{
			name: "No broken pods",
			fields: fields{
				client: fake.NewSimpleClientset(&workingPodDiedPreviously, &workingPod),
				config: config.RepairConfig{
					SidecarAnnotation:  "sidecar.istio.io/status",
					InitContainerName:  constants.ValidationContainerName,
					InitTerminationMsg: "Died for some reason",
					InitExitCode:       126,
				},
			},
			wantList: v1.PodList{Items: []v1.Pod{}},
		},
		{
			name: "With broken pods (including one with bad annotation)",
			fields: fields{
				client: fake.NewSimpleClientset(&workingPodDiedPreviously, &workingPod, &brokenPodWaiting, &brokenPodNoAnnotation, &brokenPodTerminating),
				config: config.RepairConfig{
					SidecarAnnotation:  "sidecar.istio.io/status",
					InitContainerName:  constants.ValidationContainerName,
					InitTerminationMsg: "Died for some reason",
					InitExitCode:       126,
				},
			},
			wantList: v1.PodList{Items: []v1.Pod{brokenPodTerminating, brokenPodWaiting}},
		},
		{
			name: "With Label Selector",
			fields: fields{
				client: fake.NewSimpleClientset(&workingPodDiedPreviously, &workingPod, &brokenPodWaiting, &brokenPodNoAnnotation, &brokenPodTerminating),
				config: config.RepairConfig{
					SidecarAnnotation:  "sidecar.istio.io/status",
					InitContainerName:  constants.ValidationContainerName,
					InitTerminationMsg: "Died for some reason",
					InitExitCode:       126,
					LabelSelectors:     "testlabel=true",
				},
			},
			wantList: v1.PodList{Items: []v1.Pod{brokenPodTerminating}},
		},
		{
			name: "With alternate sidecar annotation",
			fields: fields{
				client: fake.NewSimpleClientset(&workingPodDiedPreviously, &workingPod, &brokenPodWaiting, &brokenPodNoAnnotation, &brokenPodTerminating),
				config: config.RepairConfig{
					SidecarAnnotation:  "some.other.sidecar/annotation",
					InitContainerName:  constants.ValidationContainerName,
					InitTerminationMsg: "Died for some reason",
					InitExitCode:       126,
					LabelSelectors:     "testlabel=true",
				},
			},
			wantList: v1.PodList{Items: []v1.Pod{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bpr := brokenPodReconciler{
				client: tt.fields.client,
				cfg:    &tt.fields.config,
			}
			gotList, err := bpr.ListBrokenPods()
			if err != nil {
				t.Errorf("ListBrokenPods() got error listing pods: %v", err)
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

// Testing constructor
func TestNewBrokenPodReconciler(t *testing.T) {
	var (
		client = fake.NewSimpleClientset()
		cfg    = config.RepairConfig{}
	)

	type args struct {
		client kubernetes.Interface
		config config.RepairConfig
	}
	tests := []struct {
		name    string
		args    args
		wantBpr brokenPodReconciler
	}{
		{
			name: "Constructor test",
			args: args{
				client: client,
				config: cfg,
			},
			wantBpr: brokenPodReconciler{
				client: client,
				cfg:    &cfg,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBpr := newBrokenPodReconciler(tt.args.client, &tt.args.config)
			if !reflect.DeepEqual(gotBpr, tt.wantBpr) {
				t.Errorf("newBrokenPodReconciler() = %v, want %v", gotBpr, tt.wantBpr)
			}
		})
	}
}

func labelBrokenPodsClientset(pods ...v1.Pod) (cs kubernetes.Interface) {
	var csPods []runtime.Object

	for _, pod := range pods {
		csPods = append(csPods, pod.DeepCopy())
	}
	cs = fake.NewSimpleClientset(csPods...)
	return
}

func makePodLabelMap(pods []v1.Pod) (podmap map[string]string) {
	podmap = map[string]string{}
	for _, pod := range pods {
		podmap[pod.Name] = ""
		for key, value := range pod.Labels {
			podmap[pod.Name] = strings.Join([]string{podmap[pod.Name], fmt.Sprintf("%s=%s", key, value)}, ",")
		}
		podmap[pod.Name] = strings.Trim(podmap[pod.Name], " ,")
	}
	return
}

func TestBrokenPodReconciler_labelBrokenPods(t *testing.T) {
	type fields struct {
		client kubernetes.Interface
		config config.RepairConfig
	}
	tests := []struct {
		name       string
		fields     fields
		wantLabels map[string]string
		wantErr    bool
		wantCount  float64
		wantTags   []tag.Tag
	}{
		{
			name: "No broken pods",
			fields: fields{
				client: labelBrokenPodsClientset(workingPod, workingPodDiedPreviously),
				config: config.RepairConfig{
					InitContainerName:  constants.ValidationContainerName,
					InitExitCode:       126,
					InitTerminationMsg: "Died for some reason",
					LabelKey:           "testkey",
					LabelValue:         "testval",
				},
			},
			wantLabels: map[string]string{"WorkingPod": "", "WorkingPodDiedPreviously": ""},
			wantErr:    false,
			wantCount:  0,
		},
		{
			name: "With broken pods",
			fields: fields{
				client: labelBrokenPodsClientset(workingPod, workingPodDiedPreviously, brokenPodWaiting),
				config: config.RepairConfig{
					InitContainerName:  constants.ValidationContainerName,
					InitExitCode:       126,
					InitTerminationMsg: "Died for some reason",
					LabelKey:           "testkey",
					LabelValue:         "testval",
				},
			},
			wantLabels: map[string]string{"WorkingPod": "", "WorkingPodDiedPreviously": "", "BrokenPodWaiting": "testkey=testval"},
			wantErr:    false,
			wantCount:  1,
			wantTags:   []tag.Tag{{Key: tag.Key(resultLabel), Value: resultSuccess}, {Key: tag.Key(typeLabel), Value: labelType}},
		},
		{
			name: "With already labeled pod",
			fields: fields{
				client: labelBrokenPodsClientset(workingPod, workingPodDiedPreviously, brokenPodTerminating),
				config: config.RepairConfig{
					InitContainerName:  constants.ValidationContainerName,
					InitExitCode:       126,
					InitTerminationMsg: "Died for some reason",
					LabelKey:           "testlabel",
					LabelValue:         "true",
				},
			},
			wantLabels: map[string]string{"WorkingPod": "", "WorkingPodDiedPreviously": "", "BrokenPodTerminating": "testlabel=true"},
			wantErr:    false,
			wantCount:  1,
			wantTags:   []tag.Tag{{Key: tag.Key(resultLabel), Value: resultSkip}, {Key: tag.Key(typeLabel), Value: labelType}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exp := initStats(tt.name)
			bpr := brokenPodReconciler{
				client: tt.fields.client,
				cfg:    &tt.fields.config,
			}
			if err := bpr.LabelBrokenPods(); (err != nil) != tt.wantErr {
				t.Errorf("LabelBrokenPods() error = %v, wantErr %v", err, tt.wantErr)
			} else {
				havePods, err := bpr.client.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
				if err != nil {
					t.Errorf("LabelBrokenPods() error = %v when listing pods", err)
				}
				plm := makePodLabelMap(havePods.Items)
				if !reflect.DeepEqual(plm, tt.wantLabels) {
					t.Errorf("LabelBrokenPods() haveLabels = %v, wantLabels = %v", plm, tt.wantLabels)
				}
			}
			if err := checkStats(tt.wantCount, tt.wantTags, exp); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestBrokenPodReconciler_deleteBrokenPods(t *testing.T) {
	type fields struct {
		client kubernetes.Interface
		config config.RepairConfig
	}
	tests := []struct {
		name      string
		fields    fields
		wantErr   bool
		wantPods  []v1.Pod
		wantCount float64
		wantTags  []tag.Tag
	}{
		{
			name: "No broken pods",
			fields: fields{
				client: labelBrokenPodsClientset(workingPod, workingPodDiedPreviously),
				config: config.RepairConfig{
					InitContainerName:  constants.ValidationContainerName,
					InitExitCode:       126,
					InitTerminationMsg: "Died for some reason",
				},
			},
			wantPods:  []v1.Pod{workingPod, workingPodDiedPreviously},
			wantErr:   false,
			wantCount: 0,
		},
		{
			name: "With broken pods",
			fields: fields{
				client: labelBrokenPodsClientset(workingPod, workingPodDiedPreviously, brokenPodWaiting),
				config: config.RepairConfig{
					InitContainerName:  constants.ValidationContainerName,
					InitExitCode:       126,
					InitTerminationMsg: "Died for some reason",
				},
			},
			wantPods:  []v1.Pod{workingPod, workingPodDiedPreviously},
			wantErr:   false,
			wantCount: 1,
			wantTags:  []tag.Tag{{Key: tag.Key(resultLabel), Value: resultSuccess}, {Key: tag.Key(typeLabel), Value: deleteType}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exp := initStats(tt.name)
			bpr := brokenPodReconciler{
				client: tt.fields.client,
				cfg:    &tt.fields.config,
			}
			if err := bpr.DeleteBrokenPods(); (err != nil) != tt.wantErr {
				t.Errorf("DeleteBrokenPods() error = %v, wantErr %v", err, tt.wantErr)
			}
			havePods, err := bpr.client.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				t.Errorf("DeleteBrokenPods() error listing pods: %v", err)
			}
			if !reflect.DeepEqual(havePods.Items, tt.wantPods) {
				t.Errorf("DeleteBrokenPods() error havePods = %v, wantPods = %v", havePods.Items, tt.wantPods)
			}
			if err := checkStats(tt.wantCount, tt.wantTags, exp); err != nil {
				t.Error(err)
			}
		})
	}
}

type testExporter struct {
	sync.Mutex

	rows        map[string][]*view.Row
	invalidTags bool
}

func (t *testExporter) ExportView(d *view.Data) {
	t.Lock()
	for _, tk := range d.View.TagKeys {
		if len(tk.Name()) < 1 {
			t.invalidTags = true
		}
	}
	t.rows[d.View.Name] = append(t.rows[d.View.Name], d.Rows...)
	t.Unlock()
}

// because OC uses goroutines to async export, validating proper export
// can introduce timing problems. this helper just trys validation over
// and over until the supplied method either succeeds or it times out.
func retry(fn func() error) error {
	var lasterr error
	to := time.After(1 * time.Second)
	var i int
	for {
		select {
		case <-to:
			return fmt.Errorf("timeout while waiting after %d iterations (last error: %v)", i, lasterr)
		default:
		}
		i++
		if err := fn(); err != nil {
			lasterr = err
		} else {
			return nil
		}
		<-time.After(10 * time.Millisecond)
	}
}

// returns 0 when the metric has not been incremented.
func readFloat64(exp *testExporter, metric monitoring.Metric, tags []tag.Tag) float64 {
	exp.Lock()
	defer exp.Unlock()
	for _, r := range exp.rows[metric.Name()] {
		if !reflect.DeepEqual(r.Tags, tags) {
			continue
		}
		if sd, ok := r.Data.(*view.SumData); ok {
			return sd.Value
		}
	}
	return 0
}

func initStats(name string) *testExporter {
	podsRepaired = monitoring.NewSum(name, "", monitoring.WithLabels(typeLabel, resultLabel))
	monitoring.MustRegister(podsRepaired)
	exp := &testExporter{rows: make(map[string][]*view.Row)}
	view.RegisterExporter(exp)
	view.SetReportingPeriod(1 * time.Millisecond)
	return exp
}

func checkStats(wantCount float64, wantTags []tag.Tag, exp *testExporter) error {
	if wantCount > 0 {
		if err := retry(func() error {
			haveCount := readFloat64(exp, podsRepaired, wantTags)
			if haveCount != wantCount {
				return fmt.Errorf("counter error in ReconcilePod(): haveCount = %v, wantCount = %v", haveCount, wantCount)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func TestAddToWorkerQueue(t *testing.T) {
	tests := []struct {
		name           string
		pod            v1.Pod
		repairConfig   *config.RepairConfig
		expectQueueLen int
	}{
		{
			name:           "broken pod",
			pod:            brokenPodWaiting,
			expectQueueLen: 1,
		},
		{
			name:           "normal pod",
			pod:            workingPod,
			expectQueueLen: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Controller{
				workQueue: workqueue.NewRateLimitingQueue(workqueue.NewItemExponentialFailureRateLimiter(0, 0)),
				reconciler: brokenPodReconciler{
					cfg: &config.RepairConfig{
						SidecarAnnotation: "sidecar.istio.io/status",
						InitContainerName: constants.ValidationContainerName,
						InitExitCode:      126,
					},
				},
			}
			c.mayAddToWorkQueue(&tt.pod)
			if tt.expectQueueLen != c.workQueue.Len() {
				t.Errorf("work queue length got %v want %v", c.workQueue.Len(), tt.expectQueueLen)
			}
		})
	}
}
