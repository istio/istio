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

package ambient

import (
	"context"
	"sync"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	testNamespace = "test-ns"
)

func TestServerReconcilePod(t *testing.T) {
	ctx := context.Background()

	setup := func(ctx context.Context) (kube.CLIClient, *fakePodReconcileHandler, error) {
		kubeClient := kube.NewFakeClient()
		server := &Server{
			ctx:        ctx,
			kubeClient: kubeClient,
		}
		fakePodHandler := &fakePodReconcileHandler{}
		server.podReconcileHandler = fakePodHandler
		server.setupHandlers()
		server.Start()

		_, err := kubeClient.Kube().CoreV1().Namespaces().Create(ctx, &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   testNamespace,
				Labels: map[string]string{"istio.io/dataplane-mode": "ambient"},
			},
		}, metav1.CreateOptions{})

		return kubeClient, fakePodHandler, err
	}

	type expectedEvent struct {
		event     string
		podName   string
		assertPod func(pod *v1.Pod) bool
		// couldSkip is used to indicate that the event could be skipped in assertion.
		couldSkip bool
	}

	cases := []struct {
		name           string
		podOperations  func(ctx context.Context, kubeClient kube.CLIClient) error
		expectedEvents []expectedEvent
	}{
		{
			name: "add a pod to mesh",
			podOperations: func(ctx context.Context, kubeClient kube.CLIClient) error {
				_, err := kubeClient.Kube().CoreV1().Pods(testNamespace).Create(ctx, &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "app-1",
						Namespace: testNamespace,
					},
				}, metav1.CreateOptions{})
				return err
			},
			expectedEvents: []expectedEvent{
				{
					event:   "add",
					podName: "app-1",
				},
				{
					event:     "add",
					podName:   "app-1",
					couldSkip: true,
				},
			},
		},
		{
			name: "add a pod to mesh, then delete it",
			podOperations: func(ctx context.Context, kubeClient kube.CLIClient) error {
				_, err := kubeClient.Kube().CoreV1().Pods(testNamespace).Create(ctx, &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "app-2",
						Namespace: testNamespace,
					},
				}, metav1.CreateOptions{})
				if err != nil {
					return err
				}

				return kubeClient.Kube().CoreV1().Pods(testNamespace).Delete(ctx, "app-2", metav1.DeleteOptions{})
			},
			expectedEvents: []expectedEvent{
				{
					event:   "add",
					podName: "app-2",
				},
				{
					event:     "add",
					podName:   "app-2",
					couldSkip: true,
					assertPod: func(pod *v1.Pod) bool { // means pod is deleting, but it is not received sometimes
						return pod.DeletionTimestamp != nil
					},
				},
				{
					event:   "del",
					podName: "app-2",
				},
			},
		},
		{
			name: "add a pod to mesh, then update it(AmbientRedirectionDisabled)",
			podOperations: func(ctx context.Context, kubeClient kube.CLIClient) error {
				_, err := kubeClient.Kube().CoreV1().Pods(testNamespace).Create(ctx, &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "app-3",
						Namespace: testNamespace,
						Annotations: map[string]string{
							constants.AmbientRedirection: constants.AmbientRedirectionEnabled,
						},
					},
				}, metav1.CreateOptions{})
				if err != nil {
					return err
				}

				_, err = kubeClient.Kube().CoreV1().Pods(testNamespace).Update(ctx, &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "app-3",
						Namespace: testNamespace,
						Annotations: map[string]string{
							constants.AmbientRedirection: constants.AmbientRedirectionDisabled,
						},
					},
				}, metav1.UpdateOptions{})
				if err != nil {
					return err
				}
				return nil
			},
			expectedEvents: []expectedEvent{
				{
					event:   "add",
					podName: "app-3",
				},
				{
					event:     "add",
					podName:   "app-3",
					couldSkip: true,
				},
				{
					event:   "del",
					podName: "app-3",
					assertPod: func(pod *v1.Pod) bool { // means pod is not deleting
						return pod.DeletionTimestamp == nil
					},
				},
			},
		},
	}

	for _, c := range cases {
		c := c

		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			kubeClient, fakePodHandler, err := setup(ctx)
			assert.NoError(t, err)
			defer func() {
				if t.Failed() {
					for _, event := range fakePodHandler.getEvents() {
						println(event.event, event.pod.Name)
					}
				}
			}()

			err = c.podOperations(ctx, kubeClient)
			assert.NoError(t, err)

			assert.EventuallyEqual(t, func() bool {
				skippedEvents := 0
				for i, event := range c.expectedEvents {
					ok := fakePodHandler.eventIs(i-skippedEvents, event.event, event.podName)
					if ok {
						if event.assertPod != nil {
							assert.Equal(t, true, event.assertPod(fakePodHandler.getEventPod(i-skippedEvents)))
						}

						continue
					}
					if event.couldSkip {
						skippedEvents++
						continue
					}
				}
				return true
			}, true, retry.Timeout(3*time.Second))
		})
	}
}

type fakePodReconcileHandler struct {
	sync.Mutex

	events []podEvent
}

var _ podReconcileHandler = &fakePodReconcileHandler{}

type podEvent struct {
	event string // "add" or "del"
	pod   *v1.Pod
}

func (h *fakePodReconcileHandler) getEvents() []podEvent {
	h.Lock()
	defer h.Unlock()

	return h.events
}

func (h *fakePodReconcileHandler) getEventPod(i int) *v1.Pod {
	h.Lock()
	defer h.Unlock()

	return h.events[i].pod
}

func (h *fakePodReconcileHandler) eventIs(index int, event, podName string) bool {
	h.Lock()
	defer h.Unlock()

	if index >= len(h.events) {
		return false
	}
	e := h.events[index]
	if e.event != event || e.pod.Name != podName {
		return false
	}
	return true
}

func (h *fakePodReconcileHandler) delPodFromMesh(pod *v1.Pod, event controllers.Event) {
	h.Lock()
	defer h.Unlock()

	h.events = append(h.events, podEvent{
		event: "del",
		pod:   pod,
	})
}

func (h *fakePodReconcileHandler) addPodToMesh(pod *v1.Pod) {
	h.Lock()
	defer h.Unlock()

	h.events = append(h.events, podEvent{
		event: "add",
		pod:   pod,
	})
}
