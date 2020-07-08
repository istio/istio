/*
 * // Copyright Istio Authors
 * //
 * // Licensed under the Apache License, Version 2.0 (the "License");
 * // you may not use this file except in compliance with the License.
 * // You may obtain a copy of the License at
 * //
 * //     http://www.apache.org/licenses/LICENSE-2.0
 * //
 * // Unless required by applicable law or agreed to in writing, software
 * // distributed under the License is distributed on an "AS IS" BASIS,
 * // WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * // See the License for the specific language governing permissions and
 * // limitations under the License.
 */

package vm

const (
	// TODO do not merge until I revert back to debian
	DefaultVMImage = "app_sidecar_centos_8"
)

func GetSupportedOSVersion() []string {
	return []string{"app_sidecar_ubuntu_xenial", "app_sidecar_ubuntu_focal", "app_sidecar_ubuntu_bionic",
		"app_sidecar_debian_9", "app_sidecar_debian_10", "app_sidecar_centos_8"}
}
