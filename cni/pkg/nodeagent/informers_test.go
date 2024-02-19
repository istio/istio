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

package nodeagent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/cni/pkg/util"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/monitoring/monitortest"
	"istio.io/istio/pkg/test/util/assert"
)

func TestExistingPodAddedWhenNsLabeled(t *testing.T) {
	setupLogging()
	NodeName = "testnode"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			NodeName: NodeName,
		},
		Status: corev1.PodStatus{
			PodIP: "11.1.1.12",
		},
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}

	client := kube.NewFakeClient(ns, pod)

	// We are expecting at most 1 calls to the mock, wait for them
	wg, waitForMockCalls := NewWaitForNCalls(t, 1)
	fs := &fakeServer{testWG: wg}

	fs.On("AddPodToMesh",
		ctx,
		pod,
		util.GetPodIPsIfPresent(pod),
		"",
	).Return(nil)

	server := &meshDataplane{
		kubeClient: client.Kube(),
		netServer:  fs,
	}

	handlers := setupHandlers(ctx, client, server, "istio-system")
	client.RunAndWait(ctx.Done())
	go handlers.Start()

	// label the namespace
	labelsPatch := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`,
		constants.DataplaneMode, constants.DataplaneModeAmbient))
	_, err := client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, labelsPatch, metav1.PatchOptions{})
	assert.NoError(t, err)

	waitForMockCalls()

	assertPodAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)
}

func TestExistingPodAddedWhenDualStack(t *testing.T) {
	setupLogging()
	NodeName = "testnode"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			NodeName: NodeName,
		},
		Status: corev1.PodStatus{
			PodIPs: []corev1.PodIP{
				{
					IP: "11.1.1.12",
				},
			},
		},
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}

	client := kube.NewFakeClient(ns, pod)

	// We are expecting at most 1 calls to the mock, wait for them
	wg, waitForMockCalls := NewWaitForNCalls(t, 1)

	fs := &fakeServer{testWG: wg}

	fs.On("AddPodToMesh",
		ctx,
		pod,
		util.GetPodIPsIfPresent(pod),
		"",
	).Return(nil)

	server := &meshDataplane{
		kubeClient: client.Kube(),
		netServer:  fs,
	}

	fs.Start(ctx)
	handlers := setupHandlers(ctx, client, server, "istio-system")
	client.RunAndWait(ctx.Done())
	go handlers.Start()

	// label the namespace
	labelsPatch := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`,
		constants.DataplaneMode, constants.DataplaneModeAmbient))
	_, err := client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, labelsPatch, metav1.PatchOptions{})
	assert.NoError(t, err)

	waitForMockCalls()

	assertPodAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)
}

func TestExistingPodNotAddedIfNoIPInAnyStatusField(t *testing.T) {
	setupLogging()
	NodeName = "testnode"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mt := monitortest.New(t)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			NodeName: NodeName,
		},
		Status: corev1.PodStatus{
			PodIPs: []corev1.PodIP{},
			PodIP:  "",
		},
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}

	client := kube.NewFakeClient(ns, pod)

	fs := &fakeServer{}

	server := &meshDataplane{
		kubeClient: client.Kube(),
		netServer:  fs,
	}

	handlers := setupHandlers(ctx, client, server, "istio-system")
	client.RunAndWait(ctx.Done())
	go handlers.Start()

	// label the namespace
	labelsPatch := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`,
		constants.DataplaneMode, constants.DataplaneModeAmbient))
	_, err := client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, labelsPatch, metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait until at least one add event happens
	mt.Assert(EventTotals.Name(), map[string]string{"type": "add"}, monitortest.AtLeast(1))

	assertPodNotAnnotated(t, client, pod)

	// Assert expected calls (not) actually made
	fs.AssertExpectations(t)
}

func TestExistingPodRemovedWhenNsUnlabeled(t *testing.T) {
	setupLogging()
	mt := monitortest.New(t)
	NodeName = "testnode"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			NodeName: NodeName,
		},
		Status: corev1.PodStatus{
			PodIP: "11.1.1.12",
		},
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		// TODO: once we if the add pod bug, re-enable this and remove the patch below
		//		Labels: map[string]string{constants.DataplaneMode: constants.DataplaneModeAmbient},

	}

	client := kube.NewFakeClient(ns, pod)

	// We are expecting at most 2 calls to the mock, wait for them
	wg, waitForMockCalls := NewWaitForNCalls(t, 2)
	fs := &fakeServer{testWG: wg}

	fs.On("AddPodToMesh",
		ctx,
		pod,
		util.GetPodIPsIfPresent(pod),
		"",
	).Return(nil)

	server := &meshDataplane{
		kubeClient: client.Kube(),
		netServer:  fs,
	}

	handlers := setupHandlers(ctx, client, server, "istio-system")
	client.RunAndWait(ctx.Done())
	go handlers.Start()
	// wait until pod add was called
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.AtLeast(1))

	log.Debug("labeling namespace")
	_, err := client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`,
			constants.DataplaneMode, constants.DataplaneModeAmbient)), metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait for an update event
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.AtLeast(2))

	// wait for the pod to be annotated
	// after Pod annotated, another update event will be triggered.
	assertPodAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)

	// unlabelling the namespace should cause only one RemovePodFromMesh to happen
	fs.On("RemovePodFromMesh",
		ctx,
		mock.Anything,
	).Once().Return(nil)

	// unlabel the namespace
	labelsPatch := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":null}}}`,
		constants.DataplaneMode))
	_, err = client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, labelsPatch, metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait for another two update events
	// total 3 update at before unlabel point: 1. init ns reconcile 2. ns label reconcile 3. pod annotation update
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.AtLeast(5))

	waitForMockCalls()

	assertPodNotAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)
}

func TestExistingPodRemovedWhenPodAnnotated(t *testing.T) {
	setupLogging()
	mt := monitortest.New(t)
	NodeName = "testnode"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			NodeName: NodeName,
		},
		Status: corev1.PodStatus{
			PodIP: "11.1.1.12",
		},
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		// TODO: once we if the add pod bug, re-enable this and remove the patch below
		//		Labels: map[string]string{constants.DataplaneMode: constants.DataplaneModeAmbient},

	}

	client := kube.NewFakeClient(ns, pod)

	// We are expecting at most 2 calls to the mock, wait for them
	wg, waitForMockCalls := NewWaitForNCalls(t, 2)
	fs := &fakeServer{testWG: wg}

	fs.On("AddPodToMesh",
		ctx,
		pod,
		util.GetPodIPsIfPresent(pod),
		"",
	).Return(nil)

	server := &meshDataplane{
		kubeClient: client.Kube(),
		netServer:  fs,
	}

	handlers := setupHandlers(ctx, client, server, "istio-system")
	client.RunAndWait(ctx.Done())
	go handlers.Start()
	// wait until pod add was called
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.AtLeast(1))

	log.Debug("labeling namespace")
	_, err := client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`,
			constants.DataplaneMode, constants.DataplaneModeAmbient)), metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait for an update event
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.AtLeast(2))

	// wait for the pod to be annotated
	// after Pod annotated, another update event will be triggered.
	assertPodAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)

	// annotate Pod as disabled should cause only one RemovePodFromMesh to happen
	fs.On("RemovePodFromMesh",
		ctx,
		mock.Anything,
	).Once().Return(nil)

	// annotate the pod
	annotationsPatch := []byte(fmt.Sprintf(`{"metadata":{"annotations":{"%s":"%s"}}}`,
		constants.AmbientRedirection, constants.AmbientRedirectionDisabled))
	_, err = client.Kube().CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name,
		types.MergePatchType, annotationsPatch, metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait for an update events
	// total 3 update at before unlabel point: 1. init ns reconcile 2. ns label reconcile 3. pod annotation update
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.AtLeast(4))

	waitForMockCalls()

	assertPodNotAnnotated(t, client, pod)

	// patch a test label to emulate a non-annotation POD update event
	_, err = client.Kube().CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name,
		types.MergePatchType, []byte(`{"metadata":{"labels":{"test":"update"}}}`), metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait for an update events
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.AtLeast(5))

	assertPodNotAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)
}

func TestAmbientEnabledReturnsPodIfEnabled(t *testing.T) {
	setupLogging()
	NodeName = "testnode"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
			UID:       "1234",
		},
		Spec: corev1.PodSpec{
			NodeName: NodeName,
		},
		Status: corev1.PodStatus{
			PodIP: "11.1.1.12",
		},
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test",
			Labels: map[string]string{constants.DataplaneMode: constants.DataplaneModeAmbient},
		},
	}

	client := kube.NewFakeClient(ns, pod)
	fs := &fakeServer{}
	fs.Start(ctx)
	server := &meshDataplane{
		kubeClient: client.Kube(),
		netServer:  fs,
	}

	handlers := setupHandlers(ctx, client, server, "istio-system")
	client.RunAndWait(ctx.Done())
	_, err := handlers.GetPodIfAmbient(pod.Name, ns.Name)

	assert.NoError(t, err)
}

func TestAmbientEnabledReturnsNoPodIfNotEnabled(t *testing.T) {
	setupLogging()
	NodeName = "testnode"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test",
			Namespace:   "test",
			UID:         "1234",
			Annotations: map[string]string{constants.AmbientRedirection: constants.AmbientRedirectionDisabled},
		},
		Spec: corev1.PodSpec{
			NodeName: NodeName,
		},
		Status: corev1.PodStatus{
			PodIP: "11.1.1.12",
		},
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test",
			Labels: map[string]string{constants.DataplaneMode: constants.DataplaneModeAmbient},
		},
	}

	client := kube.NewFakeClient(ns, pod)
	fs := &fakeServer{}
	fs.Start(ctx)
	server := &meshDataplane{
		kubeClient: client.Kube(),
		netServer:  fs,
	}

	handlers := setupHandlers(ctx, client, server, "istio-system")
	client.RunAndWait(ctx.Done())
	disabledPod, err := handlers.GetPodIfAmbient(pod.Name, ns.Name)

	assert.NoError(t, err)
	assert.Equal(t, disabledPod, nil)
}

func TestAmbientEnabledReturnsErrorIfBogusNS(t *testing.T) {
	setupLogging()
	NodeName = "testnode"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test",
			Namespace:   "test",
			UID:         "1234",
			Annotations: map[string]string{constants.AmbientRedirection: constants.AmbientRedirectionDisabled},
		},
		Spec: corev1.PodSpec{
			NodeName: NodeName,
		},
		Status: corev1.PodStatus{
			PodIP: "11.1.1.12",
		},
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test",
			Labels: map[string]string{constants.DataplaneMode: constants.DataplaneModeAmbient},
		},
	}

	client := kube.NewFakeClient(ns, pod)
	fs := &fakeServer{}
	fs.Start(ctx)
	server := &meshDataplane{
		kubeClient: client.Kube(),
		netServer:  fs,
	}

	handlers := setupHandlers(ctx, client, server, "istio-system")
	client.RunAndWait(ctx.Done())
	disabledPod, err := handlers.GetPodIfAmbient(pod.Name, "what")

	assert.Error(t, err)
	assert.Equal(t, disabledPod, nil)
}

func TestExistingPodAddedWhenItPreExists(t *testing.T) {
	setupLogging()
	NodeName = "testnode"

	mt := monitortest.New(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			NodeName: NodeName,
		},
		Status: corev1.PodStatus{
			PodIP: "11.1.1.12",
		},
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test",
			Labels: map[string]string{constants.DataplaneMode: constants.DataplaneModeAmbient},
		},
	}

	client := kube.NewFakeClient(ns, pod)

	// We are expecting at most 1 calls to the mock, wait for them
	wg, waitForMockCalls := NewWaitForNCalls(t, 1)
	fs := &fakeServer{testWG: wg}

	fs.On("AddPodToMesh",
		ctx,
		pod,
		util.GetPodIPsIfPresent(pod),
		"",
	).Return(nil)

	server := &meshDataplane{
		kubeClient: client.Kube(),
		netServer:  fs,
	}

	handlers := setupHandlers(ctx, client, server, "istio-system")
	client.RunAndWait(ctx.Done())
	go handlers.Start()

	waitForMockCalls()
	// wait until pod add was called
	mt.Assert(EventTotals.Name(), map[string]string{"type": "add"}, monitortest.AtLeast(1))

	assertPodAnnotated(t, client, pod)

	// check expectations on mocked calls
	fs.AssertExpectations(t)
}

func assertPodAnnotated(t *testing.T, client kube.Client, pod *corev1.Pod) {
	for i := 0; i < 5; i++ {
		p, err := client.Kube().CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if p.Annotations[constants.AmbientRedirection] == constants.AmbientRedirectionEnabled {
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatal("Pod not annotated")
}

func assertPodNotAnnotated(t *testing.T, client kube.Client, pod *corev1.Pod) {
	for i := 0; i < 5; i++ {
		p, err := client.Kube().CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if p.Annotations[constants.AmbientRedirection] != constants.AmbientRedirectionEnabled {
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatal("Pod annotated")
}
