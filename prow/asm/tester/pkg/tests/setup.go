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
	"log"
	"os"
	"path/filepath"
	"strings"

	"istio.io/istio/prow/asm/tester/pkg/kube"
	"istio.io/istio/prow/asm/tester/pkg/resource"
)

const (
	testSkipConfigFile  = "tests/skip.yaml"
	imagePullSecretFile = "test_image_pull_secret.yaml"
)

func Setup(settings *resource.Settings) error {
	skipConfigPath := filepath.Join(resource.ConfigDirPath, testSkipConfigFile)
	skipConfig, err := parseSkipConfig(skipConfigPath)
	if err != nil {
		return err
	}
	testSkipFlags, err := testSkipFlags(skipConfig.Tests, settings.DisabledTests, skipLabels(settings))
	if err != nil {
		return err
	}
	packageSkipEnvvar, err := packageSkipEnvvar(skipConfig.Packages, skipLabels(settings))
	if err != nil {
		return err
	}
	testFlags, err := generateTestFlags(settings)
	if err != nil {
		return err
	}
	integrationTestFlags := append(testFlags, testSkipFlags...)
	integrationTestFlagsEnvvar := strings.Join(integrationTestFlags, " ")

	gcrProjectID1, gcrProjectID2 := gcrProjectIDs(settings)

	// environment variables required when running the test make target
	envVars := map[string]string{
		"INTEGRATION_TEST_FLAGS":         integrationTestFlagsEnvvar,
		"DISABLED_PACKAGES":              packageSkipEnvvar,
		"TEST_SELECT":                    generateTestSelect(settings),
		"INTEGRATION_TEST_TOPOLOGY_FILE": fmt.Sprintf("%s/integration_test_topology.yaml", os.Getenv("ARTIFACTS")),
		"JUNIT_OUT":                      fmt.Sprintf("%s/junit1.xml", os.Getenv("ARTIFACTS")),
		// exported GCR_PROJECT_ID_1 and GCR_PROJECT_ID_2
		// for security and telemetry test.
		"GCR_PROJECT_ID_1": gcrProjectID1,
		"GCR_PROJECT_ID_2": gcrProjectID2,
		// required for bare metal and multicloud environments
		"HTTP_PROXY":  os.Getenv("MC_HTTP_PROXY"),
		"HTTPS_PROXY": os.Getenv("MC_HTTP_PROXY"),
	}
	for k, v := range envVars {
		log.Printf("Set env %s=%s", k, v)
		os.Setenv(k, v)
	}
	return nil
}

func gcrProjectIDs(settings *resource.Settings) (gcrProjectID1, gcrProjectID2 string) {
	gcrProjectID1 = settings.GCRProject
	if len(settings.GCPProjects) == 2 {
		// If it's using multiple gke clusters, set gcrProjectID2 as the project
		// for the second cluster.
		gcrProjectID2 = settings.GCPProjects[1]
	} else {
		gcrProjectID2 = gcrProjectID1
	}
	// When HUB Workload Identity Pool is used in the case of multi projects setup, clusters in different projects
	// will use the same WIP and P4SA of the Hub host project.
	if settings.WIP == resource.HUBWorkloadIdentityPool && strings.Contains(settings.TestTarget, "security") {
		gcrProjectID2 = gcrProjectID1
	}
	// For onprem with Hub CI jobs, clusters are registered into the environ project
	if settings.WIP == resource.HUBWorkloadIdentityPool && settings.ClusterType == resource.OnPrem {
		gcrProjectID1, _ = kube.GetEnvironProjectID(settings.Kubeconfig)
		gcrProjectID2 = gcrProjectID1
	}
	return
}
