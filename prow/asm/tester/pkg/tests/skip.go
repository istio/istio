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
	"io/ioutil"
	"strings"

	"github.com/ghodss/yaml"
	"istio.io/istio/prow/asm/tester/pkg/resource"
)

const (
	controlPlaneSkipLabel    = "control_plane"
	clusterTypeSkipLabel     = "cluster_type"
	clusterTopologySkipLabel = "cluster_topology"
	wipSkipLabel             = "wip"
	gceVmSkipLabel           = "gce_vms"
)

// TargetSkipConfig defines the schema for our skipped test configuration.
type TargetSkipConfig struct {
	Tests    []TargetGroup `json:"tests,omitempty"`
	Packages []TargetGroup `json:"packages,omitempty"`
}

type TargetGroup struct {
	// Selectors are expressions in the form "key=value" where key is one of
	// "control_plane", "cluster_type", "cluster_topology", "wip", or "gce_vms"
	// The selectors field is required and the config will fail to parse if
	// there exist TargetGroup's without Selectors
	Selectors map[string]string `json:"selectors,omitempty"`
	Targets   []Target          `json:"targets,omitempty"`
}

type Target struct {
	// Name are the names or regexes for the tests or packages to skip.
	Names []string `json:"names,omitempty"`
	// Reason is a brief description of the reason this test is skipped.
	Reason string `json:"reason,omitempty"`
	// BuganizerID is a link to the BuganizerID issue tracking this test skip.
	BuganizerID string `json:"buganizer,omitempty"`
}

type SkipLabels map[string]string

func parseSkipConfig(path string) (*TargetSkipConfig, error) {
	yamlContents, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read test skip config file %q: %w",
			path, err)
	}
	config := new(TargetSkipConfig)
	err = yaml.Unmarshal(yamlContents, config)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal test skip config from file %q: %w",
			path, err)
	}
	// Verify required fields are present
	for _, t := range config.Tests {
		if t.Selectors == nil {
			return nil, fmt.Errorf("cannot have test group with empty expressions")
		}
	}
	for _, t := range config.Packages {
		if t.Selectors == nil {
			return nil, fmt.Errorf("cannot have package group with empty expressions")
		}
	}

	return config, nil
}

func testSkipFlags(testTargetGroup []TargetGroup, skippedTests string, skipLabels SkipLabels) ([]string, error) {
	var testFlags []string
	for _, targetGroup := range testTargetGroup {
		matched, err := matches(targetGroup.Selectors, skipLabels)
		if err != nil {
			return nil, err
		}
		if matched {
			for _, target := range targetGroup.Targets {
				for _, name := range target.Names {
					testFlags = append(testFlags, fmt.Sprintf("--istio.test.skip=\"%s\"", name))
				}
			}
		}
	}
	if skippedTests != "" {
		for _, test := range strings.Split(skippedTests, "|") {
			testFlags = append(testFlags, fmt.Sprintf("--istio.test.skip=\"%s\"", test))
		}
	}

	return testFlags, nil
}

func packageSkipEnvvar(packageTargetGroup []TargetGroup, skipLabels SkipLabels) (string, error) {
	var skipped []string
	for _, targetGroup := range packageTargetGroup {
		matched, err := matches(targetGroup.Selectors, skipLabels)
		if err != nil {
			return "", err
		}
		if matched {
			for _, target := range targetGroup.Targets {
				for _, name := range target.Names {
					skipped = append(skipped, name)
				}
			}
		}
	}
	skipEnvvar := strings.Join(skipped, "\\|")
	return skipEnvvar, nil
}

func skipLabels(settings *resource.Settings) SkipLabels {
	labelMap := make(map[string]string)
	labelMap[controlPlaneSkipLabel] = strings.ToLower(settings.ControlPlane.String())
	labelMap[clusterTypeSkipLabel] = strings.ToLower(settings.ClusterType.String())
	labelMap[clusterTopologySkipLabel] = strings.ToLower(settings.ClusterTopology.String())
	labelMap[wipSkipLabel] = strings.ToLower(settings.WIP.String())
	labelMap[gceVmSkipLabel] = fmt.Sprintf("%t", settings.UseGCEVMs || settings.VMStaticConfigDir != "")
	return labelMap
}

// matches takes a labelExpr in the form "control_plane=unmanaged,cluster_type=gke|cluster_type=sc"
// and determines whether it matches the values in skipLabels
func matches(selectors map[string]string, skipLabels map[string]string) (bool, error) {
	for k, v := range selectors {
		if _, ok := skipLabels[k]; !ok {
			return false, fmt.Errorf("unknown match key %s", k)
		}
		if strings.ToLower(v) != skipLabels[k] {
			return false, nil
		}
	}
	return true, nil
}
