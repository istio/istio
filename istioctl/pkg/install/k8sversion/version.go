// Copyright Istio Authors.
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

package k8sversion

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/version"
)

const (
	// MinK8SVersion is the minimum k8s version required to run this version of Istio
	// https://istio.io/docs/setup/platform-setup/
	MinK8SVersion = "1.16"
)

// CheckKubernetesVersion checks if this Istio version is supported in the k8s version
func CheckKubernetesVersion(versionInfo *version.Info) (bool, error) {
	v, err := extractKubernetesVersion(versionInfo)
	if err != nil {
		return false, err
	}
	return parseVersion(MinK8SVersion, 4) <= parseVersion(v, 4), nil
}
func extractKubernetesVersion(versionInfo *version.Info) (string, error) {
	versionMatchRE := regexp.MustCompile(`^\s*v?([0-9]+(?:\.[0-9]+)*)(.*)*$`)
	parts := versionMatchRE.FindStringSubmatch(versionInfo.GitVersion)
	if parts == nil {
		return "", fmt.Errorf("could not parse %q as version", versionInfo.GitVersion)
	}
	numbers := parts[1]
	components := strings.Split(numbers, ".")
	if len(components) <= 1 {
		return "", fmt.Errorf("the version %q is invalid", versionInfo.GitVersion)
	}
	v := strings.Join([]string{components[0], components[1]}, ".")
	return v, nil
}
func parseVersion(s string, width int) int64 {
	strList := strings.Split(s, ".")
	format := fmt.Sprintf("%%s%%0%ds", width)
	v := ""
	for _, value := range strList {
		v = fmt.Sprintf(format, v, value)
	}
	var result int64
	var err error
	if result, err = strconv.ParseInt(v, 10, 64); err != nil {
		return 0
	}
	return result
}
