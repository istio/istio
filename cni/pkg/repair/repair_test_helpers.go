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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

type makePodArgs struct {
	PodName             string
	Labels              map[string]string
	Annotations         map[string]string
	InitContainerName   string
	InitContainerStatus *corev1.ContainerStatus
	NodeName            string
}

func makePod(args makePodArgs) *corev1.Pod {
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        args.PodName,
			Namespace:   "default",
			Labels:      args.Labels,
			Annotations: args.Annotations,
		},
		Spec: corev1.PodSpec{
			NodeName: args.NodeName,
			Volumes:  nil,
			InitContainers: []corev1.Container{
				{
					Name: args.InitContainerName,
				},
			},
			Containers: []corev1.Container{
				{
					Name: "payload-container",
				},
			},
		},
		Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{
				*args.InitContainerStatus,
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "payload-container",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "PodInitializing",
						},
					},
				},
			},
		},
	}
	return pod
}

// Container specs
var (
	brokenInitContainerWaiting = corev1.ContainerStatus{
		Name: constants.ValidationContainerName,
		State: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{
				Reason:  "CrashLoopBackOff",
				Message: "Back-off 5m0s restarting failed blah blah blah",
			},
		},
		LastTerminationState: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{
				ExitCode: constants.ValidationErrorCode,
				Reason:   "Error",
				Message:  "Died for some reason",
			},
		},
	}

	brokenInitContainerTerminating = corev1.ContainerStatus{
		Name: constants.ValidationContainerName,
		State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{
				ExitCode: constants.ValidationErrorCode,
				Reason:   "Error",
				Message:  "Died for some reason",
			},
		},
		LastTerminationState: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{
				ExitCode: constants.ValidationErrorCode,
				Reason:   "Error",
				Message:  "Died for some reason",
			},
		},
	}

	workingInitContainerDiedPreviously = corev1.ContainerStatus{
		Name: constants.ValidationContainerName,
		State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 0,
				Reason:   "Completed",
			},
		},
		LastTerminationState: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 126,
				Reason:   "Error",
				Message:  "Died for some reason",
			},
		},
	}

	workingInitContainer = corev1.ContainerStatus{
		Name: constants.ValidationContainerName,
		State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 0,
				Reason:   "Completed",
			},
		},
	}
)

// Pod specs
var (
	brokenPodTerminating = makePod(makePodArgs{
		PodName: "broken-pod-terminating",
		Annotations: map[string]string{
			"sidecar.istio.io/status": "something",
		},
		Labels: map[string]string{
			"testlabel": "true",
		},
		NodeName:            "test-node",
		InitContainerStatus: &brokenInitContainerTerminating,
	})

	brokenPodWaiting = makePod(makePodArgs{
		PodName: "broken-pod-waiting",
		Annotations: map[string]string{
			"sidecar.istio.io/status": "something",
		},
		NodeName:            "test-node",
		InitContainerStatus: &brokenInitContainerWaiting,
	})

	brokenPodNoAnnotation = makePod(makePodArgs{
		PodName:             "broken-pod-no-annotation",
		InitContainerStatus: &brokenInitContainerWaiting,
	})

	workingPod = makePod(makePodArgs{
		PodName: "working-pod",
		Annotations: map[string]string{
			"sidecar.istio.io/status": "something",
		},
		InitContainerStatus: &workingInitContainer,
	})

	workingPodDiedPreviously = makePod(makePodArgs{
		PodName: "working-pod-died-previously",
		Annotations: map[string]string{
			"sidecar.istio.io/status": "something",
		},
		InitContainerStatus: &workingInitContainerDiedPreviously,
	})
)
