//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package tests

import (
	"fmt"
	"istio.io/istio/prow/asm/tester/pkg/exec"
	"istio.io/istio/prow/asm/tester/pkg/resource"
	"os"
	"strings"
)

// generateTestFlags returns an array containing options to be passed
// when running the test target.
func generateTestFlags(settings *resource.Settings) []string {
	testFlags := []string{"--istio.test.kube.deploy=false"}
	if settings.ControlPlane == string(resource.Unmanaged) {
		if settings.ClusterType != string(resource.GKEOnGCP) {
			testFlags = append(testFlags,
				fmt.Sprintf("--istio.test.revision=%s", revisionLabel()))
			testFlags = append(testFlags, "--istio.test.echo.callTimeout=5s")
			testFlags = append(testFlags,
				fmt.Sprintf("--istio.test.imagePullSecret=%s/%s",
					os.Getenv("ARTIFACTS"), imagePullSecretFile))
		}
		if !settings.UseVMs {
			testFlags = append(testFlags, "--istio.test.skipVM")
		}
		if settings.UseVMs || settings.UseGCEVMs {
			// TODO these are the only security tests that excercise VMs. The other tests are written in a way
			// they panic with StaticVMs.
			if settings.TestTarget == "test.integration.asm.security" {
				testFlags = append(testFlags,
					"-run=TestReachability\\|TestMtlsStrictK8sCA\\|TestPassThroughFilterChain")
			}
		}
	} else {
		testFlags = append(testFlags,
			"--istio.test.revision=asm-managed",
			"--istio.test.skipVM=true")
	}
	return testFlags
}

// generateTestSelect returns an array containing options to be passed
// when running the test target.
func generateTestSelect(settings *resource.Settings) string {
	if settings.ControlPlane == string(resource.Unmanaged) {
		// TODO(nmittler): remove this once we no longer run the multicluster tests.
		if settings.TestTarget == "test.integration.multicluster.kube.presubmit" {
			return "+multicluster"
		}
		if settings.TestTarget == "test.integration.asm.security" {
			return "-customsetup"
		}
		return ""
	}
	return "-customsetup"
}

// revisionLabel generates a revision label name from the istioctl version.
func revisionLabel() string {
	istioVersion, _ := exec.RunWithOutput(
		"bash -c \"istioctl version --remote=false -o json | jq -r '.clientVersion.tag'\"")
	versionParts := strings.Split(istioVersion, "-")
	version := fmt.Sprintf("asm-%s-%s", versionParts[0], versionParts[1])
	return strings.ReplaceAll(version, ".", "")
}
