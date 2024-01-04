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
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
)

func IsZtunnelPod(client kube.CLIClient, podName, podNamespace string) bool {
	isZtunnel := strings.HasPrefix(podName, "ztunnel")
	if client == nil {
		return isZtunnel
	}
	pod, err := client.Kube().CoreV1().Pods(podNamespace).Get(context.Background(), podName, metav1.GetOptions{})
	if err != nil {
		return isZtunnel
	}
	if v, ok := pod.Labels["app"]; ok {
		return v == "ztunnel"
	}
	return isZtunnel
}

// InAmbient returns true if a resource is in ambient data plane mode.
func InAmbient(obj metav1.Object) bool {
	if obj == nil {
		return false
	}
	switch t := obj.(type) {
	case *corev1.Pod:
		return t.GetAnnotations()[constants.AmbientRedirection] == constants.AmbientRedirectionEnabled
	case *corev1.Namespace:
		if t.GetLabels()["istio-injection"] == "enabled" {
			return false
		}
		if v, ok := t.GetLabels()[label.IoIstioRev.Name]; ok && v != "" {
			return false
		}
		return t.GetLabels()[constants.DataplaneMode] == constants.DataplaneModeAmbient
	}
	return false
}
