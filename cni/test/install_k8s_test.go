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

// This tests the k8s installation.  It validates the CNI plugin configuration
// and the existence of the CNI plugin binary locations.
package install_test

import (
	"testing"

	install "istio.io/istio/cni/test"
	"istio.io/istio/pkg/test/env"
)

var (
	Hub = "gcr.io/istio-release"
	Tag = "master-latest-daily"
)

type testCase struct {
	name             string
	chainedCNIPlugin bool
	preConfFile      string
	resultFileName   string
	// Must set chainedCNIPlugin to true if delayedConfFile is specified
	delayedConfFile        string
	expectedOutputFile     string
	expectedPostCleanFile  string
	cniConfDirOrderedFiles []string
}

func TestInstall(t *testing.T) {
	testDataDir := env.IstioSrc + "/cni/test/testdata"
	cases := []testCase{
		{
			name:                   "File with pre-plugins--.conflist",
			chainedCNIPlugin:       true,
			preConfFile:            "00-calico.conflist",
			resultFileName:         "00-calico.conflist",
			expectedOutputFile:     testDataDir + "/expected/10-calico.conflist-istioconfig",
			expectedPostCleanFile:  testDataDir + "/pre/calico.conflist",
			cniConfDirOrderedFiles: []string{"calico.conflist"},
		},
		{
			name:                   "File without pre-plugins--.conf",
			chainedCNIPlugin:       true,
			preConfFile:            "00-minikube_cni.conf",
			resultFileName:         "00-minikube_cni.conflist",
			expectedOutputFile:     testDataDir + "/expected/minikube_cni.conflist.expected",
			expectedPostCleanFile:  testDataDir + "/expected/minikube_cni.conflist.clean",
			cniConfDirOrderedFiles: []string{"minikube_cni.conf"},
		},
		{
			name:                   "First file with pre-plugins--.conflist",
			chainedCNIPlugin:       true,
			resultFileName:         "00-calico.conflist",
			expectedOutputFile:     testDataDir + "/expected/10-calico.conflist-istioconfig",
			expectedPostCleanFile:  testDataDir + "/pre/calico.conflist",
			cniConfDirOrderedFiles: []string{"calico.conflist", "minikube_cni.conf"},
		},
		{
			name:                   "First file without pre-plugins--.conf",
			chainedCNIPlugin:       true,
			resultFileName:         "00-minikube_cni.conflist",
			expectedOutputFile:     testDataDir + "/expected/minikube_cni.conflist.expected",
			expectedPostCleanFile:  testDataDir + "/expected/minikube_cni.conflist.clean",
			cniConfDirOrderedFiles: []string{"minikube_cni.conf", "calico.conflist"},
		},
		{
			name:                   "Skip non-json file for first valid .conf file",
			chainedCNIPlugin:       true,
			resultFileName:         "01-minikube_cni.conflist",
			expectedOutputFile:     testDataDir + "/expected/minikube_cni.conflist.expected",
			expectedPostCleanFile:  testDataDir + "/expected/minikube_cni.conflist.clean",
			cniConfDirOrderedFiles: []string{"non_json.conf", "minikube_cni.conf", "calico.conflist"},
		},
		{
			name:                   "Skip non-json file for first valid .conflist file",
			chainedCNIPlugin:       true,
			resultFileName:         "01-calico.conflist",
			expectedOutputFile:     testDataDir + "/expected/10-calico.conflist-istioconfig",
			expectedPostCleanFile:  testDataDir + "/pre/calico.conflist",
			cniConfDirOrderedFiles: []string{"non_json.conf", "calico.conflist", "minikube_cni.conf"},
		},
		{
			name:                   "Skip invalid .conf file for first valid .conf file",
			chainedCNIPlugin:       true,
			resultFileName:         "01-minikube_cni.conflist",
			expectedOutputFile:     testDataDir + "/expected/minikube_cni.conflist.expected",
			expectedPostCleanFile:  testDataDir + "/expected/minikube_cni.conflist.clean",
			cniConfDirOrderedFiles: []string{"bad_minikube_cni.conf", "minikube_cni.conf", "calico.conflist"},
		},
		{
			name:                   "Skip invalid .conf file for first valid .conflist file",
			chainedCNIPlugin:       true,
			resultFileName:         "01-calico.conflist",
			expectedOutputFile:     testDataDir + "/expected/10-calico.conflist-istioconfig",
			expectedPostCleanFile:  testDataDir + "/pre/calico.conflist",
			cniConfDirOrderedFiles: []string{"bad_minikube_cni.conf", "calico.conflist", "minikube_cni.conf"},
		},
		{
			name:                  "Skip invalid .conflist files for first valid .conf file",
			chainedCNIPlugin:      true,
			resultFileName:        "02-minikube_cni.conflist",
			expectedOutputFile:    testDataDir + "/expected/minikube_cni.conflist.expected",
			expectedPostCleanFile: testDataDir + "/expected/minikube_cni.conflist.clean",
			cniConfDirOrderedFiles: []string{
				"noname_calico.conflist",
				"noplugins_calico.conflist",
				"minikube_cni.conf", "calico.conflist",
			},
		},
		{
			name:                  "Skip invalid .conflist files for first valid .conflist file",
			chainedCNIPlugin:      true,
			resultFileName:        "02-calico.conflist",
			expectedOutputFile:    testDataDir + "/expected/10-calico.conflist-istioconfig",
			expectedPostCleanFile: testDataDir + "/pre/calico.conflist",
			cniConfDirOrderedFiles: []string{
				"noname_calico.conflist",
				"noplugins_calico.conflist",
				"calico.conflist", "minikube_cni.conf",
			},
		},
		{
			name:                   "confFile env var point to missing .conf with valid .conflist file",
			chainedCNIPlugin:       true,
			preConfFile:            "00-calico.conf",
			resultFileName:         "00-calico.conflist",
			expectedOutputFile:     testDataDir + "/expected/10-calico.conflist-istioconfig",
			expectedPostCleanFile:  testDataDir + "/pre/calico.conflist",
			cniConfDirOrderedFiles: []string{"calico.conflist"},
		},
		{
			name:                   "confFile env var point to missing .conflist with valid .conf file",
			chainedCNIPlugin:       true,
			preConfFile:            "00-minikube_cni.conflist",
			resultFileName:         "00-minikube_cni.conflist",
			expectedOutputFile:     testDataDir + "/expected/minikube_cni.conflist.expected",
			expectedPostCleanFile:  testDataDir + "/expected/minikube_cni.conflist.clean",
			cniConfDirOrderedFiles: []string{"minikube_cni.conf"},
		},
		{
			name:                  "confFile env var point to missing file initially and ignore different conf",
			chainedCNIPlugin:      true,
			preConfFile:           "missing_initially.conf",
			resultFileName:        "missing_initially.conflist",
			delayedConfFile:       testDataDir + "/pre/calico.conflist",
			expectedOutputFile:    testDataDir + "/expected/10-calico.conflist-istioconfig",
			expectedPostCleanFile: testDataDir + "/pre/calico.conflist",
		},
		{
			name:                  "confFile env var not specified and no valid conf file initially",
			chainedCNIPlugin:      true,
			resultFileName:        "calico.conflist",
			delayedConfFile:       testDataDir + "/pre/calico.conflist",
			expectedOutputFile:    testDataDir + "/expected/10-calico.conflist-istioconfig",
			expectedPostCleanFile: testDataDir + "/pre/calico.conflist",
		},
		{
			name:               "standalone plugin default name",
			resultFileName:     "YYY-istio-cni.conf",
			expectedOutputFile: testDataDir + "/expected/YYY-istio-cni.conf",
		},
		{
			name:               "standalone plugin user defined name",
			preConfFile:        "user-defined.conf",
			resultFileName:     "user-defined.conf",
			expectedOutputFile: testDataDir + "/expected/YYY-istio-cni.conf",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: Test preconf %s, expected %s", tc.name, tc.preConfFile, tc.expectedOutputFile)
			install.RunInstallCNITest(t, tc.chainedCNIPlugin, tc.preConfFile, tc.resultFileName, tc.delayedConfFile, tc.expectedOutputFile,
				tc.expectedPostCleanFile, tc.cniConfDirOrderedFiles)
		})
	}
}
