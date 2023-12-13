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

	cases := []struct {
		name              string
		podOperations     func(ctx context.Context, kubeClient kube.CLIClient) error
		mustReceivedEvent expectedEvent
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
			mustReceivedEvent: expectedEvent{
				podName: "app-1",
				event:   "add",
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
			mustReceivedEvent: expectedEvent{
				podName: "app-2",
				event:   "del",
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
			mustReceivedEvent: expectedEvent{
				podName: "app-3",
				event:   "del",
			},
		},
		{
			name: "add a none-ambient pod to mesh, then update it(AmbientRedirectionDisabled)",
			podOperations: func(ctx context.Context, kubeClient kube.CLIClient) error {
				_, err := kubeClient.Kube().CoreV1().Pods(testNamespace).Create(ctx, &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "app-4",
						Namespace: testNamespace,
						Annotations: map[string]string{
							constants.AmbientRedirection: constants.AmbientRedirectionDisabled,
						},
					},
				}, metav1.CreateOptions{})
				if err != nil {
					return err
				}

				_, err = kubeClient.Kube().CoreV1().Pods(testNamespace).Update(ctx, &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "app-4",
						Namespace: testNamespace,
						Annotations: map[string]string{
							constants.AmbientRedirection: constants.AmbientRedirectionEnabled,
						},
					},
				}, metav1.UpdateOptions{})
				if err != nil {
					return err
				}
				return nil
			},
			mustReceivedEvent: expectedEvent{
				podName: "app-4",
				event:   "add",
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
				return fakePodHandler.receivedEvent(c.mustReceivedEvent.event, c.mustReceivedEvent.podName)
			}, true, retry.Timeout(3*time.Second))
		})
	}
}

type expectedEvent struct {
	event   string
	podName string
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

func (h *fakePodReconcileHandler) receivedEvent(event, podName string) bool {
	h.Lock()
	defer h.Unlock()

	for _, e := range h.events {
		if e.event == event && e.pod.Name == podName {
			return true
		}
	}
	return false
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
