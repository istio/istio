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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/config/constants"
)

var annotationPatch = []byte(fmt.Sprintf(
	`{"metadata":{"annotations":{"%s":"%s"}}}`,
	constants.AmbientRedirection,
	constants.AmbientRedirectionEnabled,
))

var annotationRemovePatch = []byte(fmt.Sprintf(
	`{"metadata":{"annotations":{"%s":null}}}`,
	constants.AmbientRedirection,
))

// PodRedirectionEnabled determines if a pod should or should not be configured
// to have traffic redirected thru the node proxy.
func PodRedirectionEnabled(namespace *corev1.Namespace, pod *corev1.Pod) bool {
	if namespace.GetLabels()[constants.DataplaneMode] != constants.DataplaneModeAmbient {
		// Namespace does not have ambient mode enabled
		return false
	}
	if podHasSidecar(pod) {
		// Ztunnel and sidecar for a single pod is currently not supported; opt out.
		return false
	}
	if pod.Annotations[constants.AmbientRedirection] == constants.AmbientRedirectionDisabled {
		// Pod explicitly asked to not have redirection enabled
		return false
	}
	return true
}

func podHasSidecar(pod *corev1.Pod) bool {
	if _, f := pod.Annotations[annotation.SidecarStatus.Name]; f {
		return true
	}
	return false
}

func ztunnelPod(pod *corev1.Pod) bool {
	return pod.GetLabels()["app"] == "ztunnel"
}

func AnnotateEnrolledPod(client kubernetes.Interface, pod *corev1.Pod) error {
	_, err := client.CoreV1().
		Pods(pod.Namespace).
		Patch(
			context.Background(),
			pod.Name,
			types.MergePatchType,
			annotationPatch,
			metav1.PatchOptions{},
		)
	return err
}

func AnnotateUnenrollPod(client kubernetes.Interface, pod *corev1.Pod) error {
	if pod.Annotations[constants.AmbientRedirection] != constants.AmbientRedirectionEnabled {
		return nil
	}
	// TODO: do not overwrite if already none
	_, err := client.CoreV1().
		Pods(pod.Namespace).
		Patch(
			context.Background(),
			pod.Name,
			types.MergePatchType,
			annotationRemovePatch,
			metav1.PatchOptions{},
		)
	if errors.IsNotFound(err) {
		return nil
	}
	return err
}

func getEnvFromPod(pod *corev1.Pod, envName string) string {
	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			if env.Name == envName {
				return env.Value
			}
		}
	}
	return ""
}

// getUID is a nil safe UID accessor
func getUID(o *corev1.Pod) types.UID {
	if o == nil {
		return ""
	}
	return o.GetUID()
}
