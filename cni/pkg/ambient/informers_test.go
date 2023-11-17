package ambient

import (
	"context"
	"testing"
	"time"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		name          string
		podOperations func(ctx context.Context, kubeClient kube.CLIClient) error
		// the number of events is not guaranteed.
		expectedLeastEvents []podEvent
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
			expectedLeastEvents: []podEvent{
				// at least receive one add event
				{"add", "app-1"},
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
			expectedLeastEvents: []podEvent{
				{"add", "app-2"},
				{"del", "app-2"},
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
			expectedLeastEvents: []podEvent{
				{"add", "app-3"},
				{"del", "app-3"}, // should be deleted from mesh because of AmbientRedirectionDisabled
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
					for _, event := range fakePodHandler.events {
						println(event.event, event.podName)
					}
				}
			}()

			err = c.podOperations(ctx, kubeClient)
			assert.NoError(t, err)

			assert.EventuallyEqual(t, func() bool {
				for i, event := range c.expectedLeastEvents {
					if !fakePodHandler.eventIs(i, event.event, event.podName) {
						return false
					}
				}
				return len(fakePodHandler.events) >= len(c.expectedLeastEvents)
			}, true, retry.Timeout(3*time.Second))
		})
	}
}

type fakePodReconcileHandler struct {
	events []podEvent
}

var _ podReconcileHandler = &fakePodReconcileHandler{}

type podEvent struct {
	event   string // "add" or "del"
	podName string
}

func (h *fakePodReconcileHandler) eventIs(index int, event, podName string) bool {
	if index >= len(h.events) {
		return false
	}
	e := h.events[index]
	if e.event != event || e.podName != podName {
		return false
	}
	return true
}

func (h *fakePodReconcileHandler) delPodFromMesh(pod *v1.Pod, event controllers.Event) {
	h.events = append(h.events, podEvent{
		event:   "del",
		podName: pod.Name,
	})
}

func (h *fakePodReconcileHandler) addPodToMesh(pod *v1.Pod) {
	h.events = append(h.events, podEvent{
		event:   "add",
		podName: pod.Name,
	})
}
