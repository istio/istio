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
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/cni/pkg/util"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/monitoring/monitortest"
	"istio.io/istio/pkg/test/util/assert"
)

func TestInformerExistingPodAddedWhenNsLabeled(t *testing.T) {
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
	fs := &fakeServer{}

	fs.On("AddPodToMesh",
		ctx,
		mock.IsType(pod),
		util.GetPodIPsIfPresent(pod),
		"",
	).Once().Return(nil)

	_, mt := populateClientAndWaitForInformer(ctx, t, client, fs, 2, 1)

	// label the namespace
	labelsPatch := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`,
		label.IoIstioDataplaneMode.Name, constants.DataplaneModeAmbient))
	_, err := client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, labelsPatch, metav1.PatchOptions{})
	assert.NoError(t, err)

	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.Exactly(4))

	assertPodAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)
}

func TestInformerExistingPodAddErrorRetriesIfRetryable(t *testing.T) {
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
	fs := &fakeServer{}

	fs.On("AddPodToMesh",
		ctx,
		mock.IsType(pod),
		util.GetPodIPsIfPresent(pod),
		"",
	).Once().Return(errors.New("something failed"))

	fs.On("AddPodToMesh",
		ctx,
		mock.IsType(pod),
		util.GetPodIPsIfPresent(pod),
		"",
	).Once().Return(errors.New("something failed"))

	fs.On("AddPodToMesh",
		ctx,
		mock.IsType(pod),
		util.GetPodIPsIfPresent(pod),
		"",
	).Return(nil)

	_, mt := populateClientAndWaitForInformer(ctx, t, client, fs, 2, 1)

	// label the namespace
	labelsPatch := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`,
		label.IoIstioDataplaneMode.Name, constants.DataplaneModeAmbient))
	_, err := client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, labelsPatch, metav1.PatchOptions{})
	assert.NoError(t, err)

	mt.Assert(EventTotals.Name(), map[string]string{"type": "add"}, monitortest.Exactly(2))
	// Unfortunately the scenario tested here is inherently racy - once the first AddPodToMesh
	// fails, we annotate with a partial status, and the original event is queued by the informer for retry.
	//
	// However the partial status *also* triggers its own new update event, and given that the informer
	// doesn't "diff" old and new events, we can't tell them apart. This is fine tho, since `Adds` are
	// idempotent, one or both of them will succeed (we don't care which) and will ultimately annotate
	// + enroll the pod.
	//
	// This does make event-counting tricky tho, since the event count varies depending on which event
	// happens to win the race (or if they *both* win and AddPodToMesh gets called 2x),
	// so we have to rely on AtLeast here (sometimes it's 9, sometimes it's 8)
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.AtLeast(8))

	assertPodAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)
}

func TestInformerExistingPodAddErrorAnnotatesWithPartialStatusOnRetry(t *testing.T) {
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
	fs := &fakeServer{}

	fs.On("AddPodToMesh",
		ctx,
		mock.IsType(pod),
		util.GetPodIPsIfPresent(pod),
		"",
	).Return(errors.New("something failed"))

	_, mt := populateClientAndWaitForInformer(ctx, t, client, fs, 2, 1)

	// label the namespace
	labelsPatch := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`,
		label.IoIstioDataplaneMode.Name, constants.DataplaneModeAmbient))
	_, err := client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, labelsPatch, metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait for all update events to settle
	// 1. init ns reconcile 2. ns label reconcile 3. pod reconcile 4. pod partial anno
	// 5. retry 6. retry
	// This must be AtLeast because informer will keep retrying. We just need to wait for enough events
	// to know the pod got a partial annotation
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.AtLeast(6))

	assertPodAnnotatedPending(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)
}

func TestInformerExistingPodAddErrorDoesNotRetryIfNotRetryable(t *testing.T) {
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
	fs := &fakeServer{}

	fs.On("AddPodToMesh",
		ctx,
		mock.IsType(pod),
		util.GetPodIPsIfPresent(pod),
		"",
	).Once().Return(ErrNonRetryableAdd)

	_, mt := populateClientAndWaitForInformer(ctx, t, client, fs, 2, 1)

	// label the namespace
	labelsPatch := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`,
		label.IoIstioDataplaneMode.Name, constants.DataplaneModeAmbient))
	_, err := client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, labelsPatch, metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait for all update events to settle
	// total 3:
	// 1. init ns reconcile 2. ns label reconcile 3. pod reconcile (fail)
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.Exactly(3))

	assertPodNotAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)
}

func TestInformerExistingPodAddedWhenDualStack(t *testing.T) {
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

	fs := &fakeServer{}

	fs.On("AddPodToMesh",
		ctx,
		mock.IsType(pod),
		util.GetPodIPsIfPresent(pod),
		"",
	).Once().Return(nil)

	_, mt := populateClientAndWaitForInformer(ctx, t, client, fs, 2, 1)

	// label the namespace
	labelsPatch := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`,
		label.IoIstioDataplaneMode.Name, constants.DataplaneModeAmbient))
	_, err := client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, labelsPatch, metav1.PatchOptions{})
	assert.NoError(t, err)

	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.Exactly(4))

	assertPodAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)
}

func TestInformerExistingPodNotAddedIfNoIPInAnyStatusField(t *testing.T) {
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
			PodIPs: []corev1.PodIP{},
			PodIP:  "",
		},
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}

	client := kube.NewFakeClient(ns, pod)

	fs := &fakeServer{}

	_, mt := populateClientAndWaitForInformer(ctx, t, client, fs, 2, 1)

	// label the namespace
	labelsPatch := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`,
		label.IoIstioDataplaneMode.Name, constants.DataplaneModeAmbient))
	_, err := client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, labelsPatch, metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait for all update events to settle
	// total 3: 1. init ns reconcile 2. ns label reconcile 3. pod reconcile
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.Exactly(3))

	assertPodNotAnnotated(t, client, pod)

	// Assert expected calls (not) actually made
	fs.AssertExpectations(t)
}

func TestInformerExistingPodRemovedWhenNsUnlabeled(t *testing.T) {
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
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		// TODO: once we if the add pod bug, re-enable this and remove the patch below
		//		Labels: map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient},

	}

	client := kube.NewFakeClient(ns, pod)

	fs := &fakeServer{}

	fs.On("AddPodToMesh",
		ctx,
		mock.IsType(pod),
		util.GetPodIPsIfPresent(pod),
		"",
	).Once().Return(nil)

	_, mt := populateClientAndWaitForInformer(ctx, t, client, fs, 2, 1)

	log.Debug("labeling namespace")
	_, err := client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`,
			label.IoIstioDataplaneMode.Name, constants.DataplaneModeAmbient)), metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait for all update events to settle
	// total 3: 1. init ns reconcile 2. ns label reconcile 3. pod reconcile 4. pod annotate
	// for all that tho, we should only get 1 ADD, as enforced by mock
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.Exactly(4))

	// wait for the pod to be annotated
	// after Pod annotated, another update event will be triggered.
	assertPodAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)

	// unlabelling the namespace should cause only one RemovePodFromMesh to happen
	fs.On("RemovePodFromMesh",
		ctx,
		mock.Anything,
		false,
	).Once().Return(nil)

	// unlabel the namespace
	labelsPatch := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":null}}}`,
		label.IoIstioDataplaneMode.Name))
	_, err = client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, labelsPatch, metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait for another 3 update events for unlabel, total of 7
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.Exactly(7))

	assertPodNotAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)
}

func TestInformerExistingPodRemovalRetriesOnFailure(t *testing.T) {
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
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		// TODO: once we if the add pod bug, re-enable this and remove the patch below
		//		Labels: map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient},

	}

	client := kube.NewFakeClient(ns, pod)

	fs := &fakeServer{}

	fs.On("AddPodToMesh",
		ctx,
		mock.IsType(pod),
		util.GetPodIPsIfPresent(pod),
		"",
	).Once().Return(nil)

	_, mt := populateClientAndWaitForInformer(ctx, t, client, fs, 2, 1)

	log.Debug("labeling namespace")
	_, err := client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`,
			label.IoIstioDataplaneMode.Name, constants.DataplaneModeAmbient)), metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait for all update events to settle
	// total 3: 1. init ns reconcile 2. ns label reconcile 3. pod reconcile 4. pod annotate
	// for all that tho, we should only get 1 ADD, as enforced by mock
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.Exactly(4))

	// wait for the pod to be annotated
	// after Pod annotated, another update event will be triggered.
	assertPodAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)

	// Failure should cause a retry
	fs.On("RemovePodFromMesh",
		ctx,
		mock.Anything,
		false,
	).Once().Return(errors.New("SOME ERR"))

	fs.On("RemovePodFromMesh",
		ctx,
		mock.Anything,
		false,
	).Once().Return(nil)

	// unlabel the namespace
	labelsPatch := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":null}}}`,
		label.IoIstioDataplaneMode.Name))
	_, err = client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, labelsPatch, metav1.PatchOptions{})
	assert.NoError(t, err)

	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.Exactly(8))

	assertPodNotAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)
}

func TestInformerExistingPodRemovedWhenPodLabelRemoved(t *testing.T) {
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
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
	}

	client := kube.NewFakeClient(ns, pod)

	fs := &fakeServer{}

	fs.On("AddPodToMesh",
		ctx,
		mock.IsType(pod),
		util.GetPodIPsIfPresent(pod),
		"",
	).Once().Return(nil)

	_, mt := populateClientAndWaitForInformer(ctx, t, client, fs, 2, 1)

	log.Debug("labeling namespace")
	_, err := client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`,
			label.IoIstioDataplaneMode.Name, constants.DataplaneModeAmbient)), metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait for all update events to settle
	// total 4: 1. init ns reconcile 2. ns label reconcile 3. pod reconcile 4. pod annotate
	// for all that tho, we should only get 1 ADD, as enforced by mock
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.Exactly(4))

	// wait for the pod to be annotated
	// after Pod annotated, another update event will be triggered.
	assertPodAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)

	// annotate Pod as disabled should cause only one RemovePodFromMesh to happen
	fs.On("RemovePodFromMesh",
		ctx,
		mock.Anything,
		false,
	).Once().Return(nil)

	// label the pod for exclusion
	labelsPatch := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`,
		label.IoIstioDataplaneMode.Name, constants.DataplaneModeNone))
	_, err = client.Kube().CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name,
		types.MergePatchType, labelsPatch, metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait for update events
	// Expecting 2 - 1. pod unlabel (us) 2. pod un-annotate (informer)
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.Exactly(6))

	assertPodNotAnnotated(t, client, pod)

	// patch a test label to emulate a POD update event
	_, err = client.Kube().CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name,
		types.MergePatchType, []byte(`{"metadata":{"labels":{"test":"update"}}}`), metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait for an update event
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.Exactly(7))

	assertPodNotAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)
}

func TestInformerJobPodRemovalRetriesOnErrorEvenIfPodTerminal(t *testing.T) {
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
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		// TODO: once we if the add pod bug, re-enable this and remove the patch below
		//		Labels: map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient},

	}

	client := kube.NewFakeClient(ns, pod)

	fs := &fakeServer{}

	fs.On("AddPodToMesh",
		ctx,
		mock.IsType(pod),
		util.GetPodIPsIfPresent(pod),
		"",
	).Once().Return(nil)

	_, mt := populateClientAndWaitForInformer(ctx, t, client, fs, 2, 1)

	log.Debug("labeling namespace")
	_, err := client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`,
			label.IoIstioDataplaneMode.Name, constants.DataplaneModeAmbient)), metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait for all update events to settle
	// total 3: 1. init ns reconcile 2. ns label reconcile 3. pod reconcile 4. Pod annotate.
	// for all that tho, we should only get 1 ADD, as enforced by mock
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.Exactly(4))

	// wait for the pod to be annotated
	// after Pod annotated, another update event will be triggered.
	assertPodAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)

	fs.On("RemovePodFromMesh",
		ctx,
		mock.Anything,
		true,
	).Once().Return(errors.New("SOME REMOVE ERR"))

	fs.On("RemovePodFromMesh",
		ctx,
		mock.Anything,
		true,
	).Once().Return(nil)

	// Patch the pod to a succeeded status
	phasePatch := []byte(`{"status":{"phase":"Succeeded"}}`)
	_, err = client.Kube().CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name,
		types.MergePatchType, phasePatch, metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait for 2 more update events (status change + un-annotate)
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.Exactly(7))

	assertPodNotAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)
}

func TestInformerJobPodRemovedWhenPodTerminates(t *testing.T) {
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
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		// TODO: once we if the add pod bug, re-enable this and remove the patch below
		//		Labels: map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient},

	}

	client := kube.NewFakeClient(ns, pod)

	fs := &fakeServer{}

	fs.On("AddPodToMesh",
		ctx,
		mock.IsType(pod),
		util.GetPodIPsIfPresent(pod),
		"",
	).Once().Return(nil)

	_, mt := populateClientAndWaitForInformer(ctx, t, client, fs, 2, 1)

	log.Debug("labeling namespace")
	_, err := client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`,
			label.IoIstioDataplaneMode.Name, constants.DataplaneModeAmbient)), metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait for all update events to settle
	// total 3: 1. init ns reconcile 2. ns label reconcile 3. pod reconcile 4. Pod annotate.
	// for all that tho, we should only get 1 ADD, as enforced by mock
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.Exactly(4))

	// wait for the pod to be annotated
	// after Pod annotated, another update event will be triggered.
	assertPodAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)

	// annotate Pod as disabled should cause only one RemovePodFromMesh to happen
	fs.On("RemovePodFromMesh",
		ctx,
		mock.Anything,
		true,
	).Once().Return(nil)

	// Patch the pod to a succeeded status
	phasePatch := []byte(`{"status":{"phase":"Succeeded"}}`)
	_, err = client.Kube().CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name,
		types.MergePatchType, phasePatch, metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait for 2 more update events (status change + un-annotate)
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.Exactly(6))

	assertPodNotAnnotated(t, client, pod)

	fs.On("AddPodToMesh",
		ctx,
		mock.Anything,
		util.GetPodIPsIfPresent(pod),
		"",
	).Once().Return(nil)

	// Now bring it back
	// Patch the pod back to a running status
	phaseRunPatch := []byte(`{"status":{"phase":"Running"}}`)
	_, err = client.Kube().CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name,
		types.MergePatchType, phaseRunPatch, metav1.PatchOptions{})
	assert.NoError(t, err)

	// wait for 2 more update events (status change (again) + re-annotate)
	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.Exactly(8))

	assertPodAnnotated(t, client, pod)

	// Assert expected calls actually made
	fs.AssertExpectations(t)
}

func TestInformerGetActiveAmbientPodSnapshotOnlyReturnsActivePods(t *testing.T) {
	setupLogging()
	NodeName = "testnode"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	enrolledNotRedirected := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "enrolled-not-redirected",
			Namespace: "test",
			UID:       "12345",
			Labels:    map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient},
		},
		Spec: corev1.PodSpec{
			NodeName: NodeName,
		},
		Status: corev1.PodStatus{
			PodIP: "11.1.1.12",
		},
	}
	redirectedNotEnrolled := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "redirected-not-enrolled",
			Namespace:   "test",
			UID:         "12346",
			Annotations: map[string]string{annotation.AmbientRedirection.Name: constants.AmbientRedirectionEnabled},
		},
		Spec: corev1.PodSpec{
			NodeName: NodeName,
		},
		Status: corev1.PodStatus{
			PodIP: "11.1.1.13",
		},
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test",
			Labels: map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient},
		},
	}

	client := kube.NewFakeClient(ns, enrolledNotRedirected, redirectedNotEnrolled)
	fs := &fakeServer{}
	fs.Start(ctx)

	server := getFakeDP(fs, client.Kube())

	handlers := setupHandlers(ctx, client, server, "istio-system", defaultAmbientSelector)
	client.RunAndWait(ctx.Done())
	pods := handlers.GetActiveAmbientPodSnapshot()

	// Should only return pods with the annotation indicating they are actually redirected at this time,
	// not pods that are just scheduled to be enrolled.
	assert.Equal(t, len(pods), 1)
	assert.Equal(t, pods[0], redirectedNotEnrolled)
}

func TestInformerGetActiveAmbientPodSnapshotSkipsTerminatedJobPods(t *testing.T) {
	setupLogging()
	NodeName = "testnode"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	enrolledNotRedirected := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "enrolled-not-redirected",
			Namespace: "test",
			UID:       "12345",
			Labels:    map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient},
		},
		Spec: corev1.PodSpec{
			NodeName: NodeName,
		},
		Status: corev1.PodStatus{
			PodIP: "11.1.1.12",
		},
	}
	enrolledButTerminated := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "enrolled-but-terminated",
			Namespace:   "test",
			UID:         "12345",
			Labels:      map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient},
			Annotations: map[string]string{annotation.AmbientRedirection.Name: constants.AmbientRedirectionEnabled},
		},
		Spec: corev1.PodSpec{
			NodeName: NodeName,
		},
		Status: corev1.PodStatus{
			PodIP: "11.1.1.12",
			Phase: corev1.PodFailed,
		},
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test",
			Labels: map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient},
		},
	}

	client := kube.NewFakeClient(ns, enrolledNotRedirected, enrolledButTerminated)
	fs := &fakeServer{}
	fs.Start(ctx)

	server := getFakeDP(fs, client.Kube())

	handlers := setupHandlers(ctx, client, server, "istio-system", defaultAmbientSelector)
	client.RunAndWait(ctx.Done())
	pods := handlers.GetActiveAmbientPodSnapshot()

	// Should skip both pods - the one that's labeled but not annotated, and the one that's annotated but terminated.
	assert.Equal(t, len(pods), 0)
}

func TestInformerAmbientEnabledReturnsPodIfEnabled(t *testing.T) {
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
			Labels: map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient},
		},
	}

	client := kube.NewFakeClient(ns, pod)
	fs := &fakeServer{}
	fs.Start(ctx)

	server := getFakeDP(fs, client.Kube())

	handlers := setupHandlers(ctx, client, server, "istio-system", defaultAmbientSelector)
	client.RunAndWait(ctx.Done())
	_, err := handlers.GetPodIfAmbientEnabled(pod.Name, ns.Name)

	assert.NoError(t, err)
}

func TestInformerAmbientEnabledReturnsNoPodIfNotEnabled(t *testing.T) {
	setupLogging()
	NodeName = "testnode"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
			UID:       "1234",
			Labels:    map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeNone},
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
			Labels: map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient},
		},
	}

	client := kube.NewFakeClient(ns, pod)
	fs := &fakeServer{}

	handlers, _ := populateClientAndWaitForInformer(ctx, t, client, fs, 2, 1)

	disabledPod, err := handlers.GetPodIfAmbientEnabled(pod.Name, ns.Name)

	assert.NoError(t, err)
	assert.Equal(t, disabledPod, nil)
}

func TestInformerAmbientEnabledReturnsErrorIfBogusNS(t *testing.T) {
	setupLogging()
	NodeName = "testnode"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
			UID:       "1234",
			Labels:    map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeNone},
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
			Labels: map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient},
		},
	}

	client := kube.NewFakeClient(ns, pod)
	fs := &fakeServer{}
	fs.Start(ctx)

	server := getFakeDP(fs, client.Kube())

	handlers := setupHandlers(ctx, client, server, "istio-system", defaultAmbientSelector)
	client.RunAndWait(ctx.Done())
	disabledPod, err := handlers.GetPodIfAmbientEnabled(pod.Name, "what")

	assert.Error(t, err)
	assert.Equal(t, disabledPod, nil)
}

func TestInformerExistingPodAddedWhenItPreExists(t *testing.T) {
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
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test",
			Labels: map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient},
		},
	}

	client := kube.NewFakeClient(ns, pod)

	fs := &fakeServer{}

	fs.On("AddPodToMesh",
		ctx,
		mock.IsType(pod),
		util.GetPodIPsIfPresent(pod),
		"",
	).Once().Return(nil)

	_, _ = populateClientAndWaitForInformer(ctx, t, client, fs, 2, 2)

	assertPodAnnotated(t, client, pod)

	// check expectations on mocked calls
	fs.AssertExpectations(t)
}

// Double-adds are something we want to guard against - this is because unlike
// Remove operations, there are 2 sources of Adds (potentially) - the CNI plugin
// and the informer. This test is designed to simulate the case where the informer
// gets a stale event for a pod that has already been Added by the CNI plugin.
func TestInformerPendingPodSkippedIfAlreadyLabeledAndEventStale(t *testing.T) {
	setupLogging()
	NodeName = "testnode"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test",
			Namespace:   "test",
			Annotations: map[string]string{annotation.AmbientRedirection.Name: constants.AmbientRedirectionEnabled},
		},
		Spec: corev1.PodSpec{
			NodeName: NodeName,
		},
		Status: corev1.PodStatus{
			PodIP: "11.1.1.12",
			Phase: corev1.PodPending,
		},
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test",
			Labels: map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient},
		},
	}

	client := kube.NewFakeClient(ns, pod)

	fs := &fakeServer{}

	handlers, mt := populateClientAndWaitForInformer(ctx, t, client, fs, 2, 1)

	// We've started the informer with a Pending pod that has an
	// annotation indicating it was already enrolled

	// Now, force thru a stale pod event that lacks that annotation
	fakePod := pod.DeepCopy()
	fakePod.ObjectMeta.Annotations = map[string]string{}

	fakeEvent := controllers.Event{
		Event: controllers.EventUpdate,
		Old:   fakePod,
		New:   fakePod,
	}
	handlers.reconcile(fakeEvent)

	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.Exactly(2))

	// Pod should still be annotated
	assertPodAnnotated(t, client, pod)

	// None of our remove or add mocks should have been called
	fs.AssertExpectations(t)
}

func TestInformerSkipsUpdateEventIfPodNotActuallyPresentAnymore(t *testing.T) {
	setupLogging()
	NodeName = "testnode"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fakePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test",
			Namespace:   "test",
			Annotations: map[string]string{annotation.AmbientRedirection.Name: constants.AmbientRedirectionEnabled},
		},
		Spec: corev1.PodSpec{
			NodeName: NodeName,
		},
		Status: corev1.PodStatus{
			PodIP: "11.1.1.12",
			Phase: corev1.PodPending,
		},
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test",
			Labels: map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient},
		},
	}

	client := kube.NewFakeClient(ns)

	fs := &fakeServer{}

	handlers, mt := populateClientAndWaitForInformer(ctx, t, client, fs, 1, 0)

	// Now, force thru a stale pod update event that would normally trigger add/remove
	// in the informer if the pod existed
	fakePodNew := fakePod.DeepCopy()
	fakePodNew.ObjectMeta.Annotations = map[string]string{}
	// We've started the informer, but there is no pod in the cluster.
	// Now force thru a "stale" event for an enrolled pod no longer in the cluster.
	fakeEvent := controllers.Event{
		Event: controllers.EventUpdate,
		Old:   fakePod,
		New:   fakePodNew,
	}
	handlers.reconcile(fakeEvent)

	mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.Exactly(1))

	// None of our remove or add mocks should have been called
	fs.AssertExpectations(t)
}

func TestInformerStillHandlesDeleteEventIfPodNotActuallyPresentAnymore(t *testing.T) {
	setupLogging()
	NodeName = "testnode"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fakePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test",
			Namespace:   "test",
			Annotations: map[string]string{annotation.AmbientRedirection.Name: constants.AmbientRedirectionEnabled},
		},
		Spec: corev1.PodSpec{
			NodeName: NodeName,
		},
		Status: corev1.PodStatus{
			PodIP: "11.1.1.12",
			Phase: corev1.PodPending,
		},
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test",
			Labels: map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient},
		},
	}

	client := kube.NewFakeClient(ns)

	fs := &fakeServer{}

	// Pod deletion event should trigger one RemovePodFromMesh, even if the pod doesn't exist anymore
	fs.On("RemovePodFromMesh",
		ctx,
		mock.Anything,
		true,
	).Once().Return(nil)

	handlers, mt := populateClientAndWaitForInformer(ctx, t, client, fs, 1, 0)

	// Now, force thru a pod delete
	fakePodNew := fakePod.DeepCopy()
	fakePodNew.ObjectMeta.Annotations = map[string]string{}
	// We've started the informer, but there is no pod in the cluster.
	// Now force thru a "stale" event for an enrolled pod no longer in the cluster.
	fakeEvent := controllers.Event{
		Event: controllers.EventDelete,
		Old:   fakePod,
		New:   nil,
	}
	handlers.reconcile(fakeEvent)

	mt.Assert(EventTotals.Name(), map[string]string{"type": "delete"}, monitortest.Exactly(1))

	fs.AssertExpectations(t)
}

func assertPodAnnotated(t *testing.T, client kube.Client, pod *corev1.Pod) {
	for i := 0; i < 5; i++ {
		p, err := client.Kube().CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if p.Annotations[annotation.AmbientRedirection.Name] == constants.AmbientRedirectionEnabled {
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatal("Pod not annotated")
}

func assertPodAnnotatedPending(t *testing.T, client kube.Client, pod *corev1.Pod) {
	for i := 0; i < 5; i++ {
		p, err := client.Kube().CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if p.Annotations[annotation.AmbientRedirection.Name] == constants.AmbientRedirectionPending {
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatal("Pod not annotated with pending status")
}

func assertPodNotAnnotated(t *testing.T, client kube.Client, pod *corev1.Pod) {
	for i := 0; i < 5; i++ {
		p, err := client.Kube().CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if p.Annotations[annotation.AmbientRedirection.Name] != constants.AmbientRedirectionEnabled {
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatal("Pod annotated")
}

// nolint: lll
func populateClientAndWaitForInformer(ctx context.Context, t *testing.T, client kube.Client, fs *fakeServer, expectAddEvents, expectUpdateEvents int) (*InformerHandlers, *monitortest.MetricsTest) {
	mt := monitortest.New(t)

	server := getFakeDP(fs, client.Kube())

	handlers := setupHandlers(ctx, client, server, "istio-system", defaultAmbientSelector)
	client.RunAndWait(ctx.Done())
	handlers.Start()

	// Unfortunately mt asserts cannot assert on 0 events (which makes a certain amount of sense)
	if expectAddEvents > 0 {
		mt.Assert(EventTotals.Name(), map[string]string{"type": "add"}, monitortest.Exactly(float64(expectAddEvents)))
	}
	if expectUpdateEvents > 0 {
		mt.Assert(EventTotals.Name(), map[string]string{"type": "update"}, monitortest.Exactly(float64(expectUpdateEvents)))
	}

	return handlers, mt
}
