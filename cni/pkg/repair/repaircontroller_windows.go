//go:build windows
// +build windows

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

import corev1 "k8s.io/api/core/v1"

// redirectRunningPod dynamically enters the provided pod, that is already running, and programs it's networking configuration.
func redirectRunningPod(pod *corev1.Pod, namespaceID string) error {
	panic("not implemented")
}
