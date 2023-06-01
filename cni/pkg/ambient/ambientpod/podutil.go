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

package ambientpod

import (
	corev1 "k8s.io/api/core/v1"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/config/constants"
)

// PodZtunnelEnabled determines if a pod is eligible for ztunnel redirection
func PodZtunnelEnabled(namespace *corev1.Namespace, pod *corev1.Pod) bool {
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
