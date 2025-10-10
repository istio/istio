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

package kube

import (
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/util/version"
	kubeVersion "k8s.io/apimachinery/pkg/version"
)

// IsAtLeastVersion returns true if the client is at least the specified version.
// For example, on Kubernetes v1.15.2, IsAtLeastVersion(13) == true, IsAtLeastVersion(17) == false
func IsAtLeastVersion(client Client, minorVersion uint) bool {
	clusterVersion, err := client.GetKubernetesVersion()
	if err != nil {
		return true
	}
	return IsKubeAtLeastOrLessThanVersion(clusterVersion, minorVersion, true)
}

// IsLessThanVersion returns true if the client version is less than the specified version.
// For example, on Kubernetes v1.15.2, IsLessThanVersion(13) == false, IsLessThanVersion(17) == true
func IsLessThanVersion(client Client, minorVersion uint) bool {
	clusterVersion, err := client.GetKubernetesVersion()
	if err != nil {
		return true
	}
	return IsKubeAtLeastOrLessThanVersion(clusterVersion, minorVersion, false)
}

// IsKubeAtLeastOrLessThanVersion returns if the kubernetes version is at least or less than the specified version.
func IsKubeAtLeastOrLessThanVersion(clusterVersion *kubeVersion.Info, minorVersion uint, atLeast bool) bool {
	if clusterVersion == nil {
		return true
	}
	cv, err := version.ParseGeneric(fmt.Sprintf("v%s.%s.0", clusterVersion.Major, clusterVersion.Minor))
	if err != nil {
		return true
	}
	ev, err := version.ParseGeneric(fmt.Sprintf("v1.%d.0", minorVersion))
	if err != nil {
		return true
	}
	if atLeast {
		return cv.AtLeast(ev)
	}
	return cv.LessThan(ev)
}

// GetVersionAsInt returns the kubernetes version as an integer.
// For example, on Kubernetes v1.15.2, GetVersionAsInt returns 115
func GetVersionAsInt(client Client) int {
	clusterVersion, err := client.GetKubernetesVersion()
	if err != nil {
		return -1
	}
	v, err := strconv.Atoi(clusterVersion.Major + clusterVersion.Minor)
	if err != nil {
		// Apparently some clusters don't put proper numbers here. Try GitVersion
		vp, err := version.ParseGeneric(clusterVersion.GitVersion)
		if err != nil {
			// no good
			return -1
		}
		np, err := strconv.Atoi(fmt.Sprintf("%d%d", vp.Major(), vp.Minor()))
		if err != nil {
			// no good...
			return -1
		}
		return np
	}
	return v
}
