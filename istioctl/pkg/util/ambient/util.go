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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube"
)

func IsZtunnelPod(client kube.Client, podName, podNamespace string) bool {
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
