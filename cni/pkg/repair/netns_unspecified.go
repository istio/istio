//go:build !linux && !windows
// +build !linux,!windows

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

func runInHost[T any](f func() (T, error)) (T, error) {
	panic("not implemented")
	// return f()
}

// getPodNetNs returns the network namespace of the pod.
// On windows, we can look at the pod ip address and enumerate all of the
// networks to find the one that corresponds with this pod. From there,
// we return the network namespace guid.
func getPodNetNs(pod *corev1.Pod) (string, error) {
	panic("not implemented")
}
