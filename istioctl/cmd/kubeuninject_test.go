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

package cmd

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestKubeUninject(t *testing.T) {
	cases := []testCase{
		{ // case 0
			args:           strings.Split("experimental kube-uninject", " "),
			expectedRegexp: regexp.MustCompile(`filename not specified \(see --filename or -f\)`),
			wantException:  true,
		},
		{ // case 1
			args:           strings.Split("experimental kube-uninject -f missing.yaml", " "),
			expectedRegexp: regexp.MustCompile(`open missing.yaml: no such file or directory`),
			wantException:  true,
		},
		{ // case 2
			args: strings.Split(
				"experimental kube-uninject -f testdata/uninject/cronjob.yaml.injected", " "),
			goldenFilename: "testdata/uninject/cronjob.yaml",
		},
		{ // case 3
			args: strings.Split(
				"experimental kube-uninject -f testdata/uninject/cronjob-with-app.yaml.injected", " "),
			goldenFilename: "testdata/uninject/cronjob-with-app.yaml",
		},
		{ // case 4
			args: strings.Split(
				"experimental kube-uninject -f testdata/uninject/daemonset.yaml.injected", " "),
			goldenFilename: "testdata/uninject/daemonset.yaml",
		},
		{ // case 5
			args: strings.Split(
				"experimental kube-uninject -f testdata/uninject/deploymentconfig.yaml.injected", " "),
			goldenFilename: "testdata/uninject/deploymentconfig.yaml",
		},
		{ // case 6
			args: strings.Split(
				"experimental kube-uninject -f testdata/uninject/deploymentconfig-multi.yaml.injected", " "),
			goldenFilename: "testdata/uninject/deploymentconfig-multi.yaml",
		},
		{ // case 7
			args: strings.Split(
				"experimental kube-uninject -f testdata/uninject/job.yaml.injected", " "),
			goldenFilename: "testdata/uninject/job.yaml",
		},
		{ // case 8
			args: strings.Split(
				"experimental kube-uninject -f testdata/uninject/list.yaml.injected", " "),
			goldenFilename: "testdata/uninject/list.yaml",
		},
		{ // case 9
			args: strings.Split(
				"experimental kube-uninject -f testdata/uninject/pod.yaml.injected", " "),
			goldenFilename: "testdata/uninject/pod.yaml",
		},
		{ // case 10
			args: strings.Split(
				"experimental kube-uninject -f testdata/uninject/replicaset.yaml.injected", " "),
			goldenFilename: "testdata/uninject/replicaset.yaml",
		},
		{ // case 11
			args: strings.Split(
				"experimental kube-uninject -f testdata/uninject/replicationcontroller.yaml.injected", " "),
			goldenFilename: "testdata/uninject/replicationcontroller.yaml",
		},
		{ // case 12
			args: strings.Split(
				"experimental kube-uninject -f testdata/uninject/statefulset.yaml.injected", " "),
			goldenFilename: "testdata/uninject/statefulset.yaml",
		},
		{ // case 13: verify the uninjected file
			args: strings.Split(
				"experimental kube-uninject -f testdata/uninject/hello.yaml", " "),
			goldenFilename: "testdata/uninject/hello.yaml",
		},
		{ // case 14: enable-core-dump
			args: strings.Split(
				"experimental kube-uninject -f testdata/uninject/enable-core-dump.yaml.injected", " "),
			goldenFilename: "testdata/uninject/enable-core-dump.yaml",
		},
		{ // case 15: restore rewritten app probes
			args: strings.Split(
				"experimental kube-uninject -f testdata/uninject/deploymentconfig-app-probe.yaml.injected", " "),
			goldenFilename: "testdata/uninject/deploymentconfig-app-probe.yaml",
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}

func TestRemoveDNSConfig(t *testing.T) {
	// create a PodDNSConfig with sample data
	pd := corev1.PodDNSConfig{
		Searches: []string{
			"foo",
			"bar",
			"global",
			"baz",
		},
	}
	removeDNSConfig(&pd)

	// check that the 'global' element was removed
	if len(pd.Searches) != 3 {
		t.Errorf("Expected %d searches, got %d", 3, len(pd.Searches))
	}
	for _, s := range pd.Searches {
		if strings.Contains(s, "global") {
			t.Errorf("Found 'global' element when it should be removed")
		}
	}

	// check that a nil pointer doesn't cause a panic
	var npd *corev1.PodDNSConfig
	removeDNSConfig(npd)
}
