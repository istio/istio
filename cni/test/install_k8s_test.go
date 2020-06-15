// Copyright 2018 Istio Authors
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
	"fmt"
	"os"
	"testing"

	//"github.com/nsf/jsondiff"
	"istio.io/cni/deployments/kubernetes/install/test"
)

var (
	TestWorkDir, _ = os.Getwd()
	Hub            = "gcr.io/istio-release"
	Tag            = "master-latest-daily"
)

type testCase struct {
	name                   string
	preConfFile            string
	resultFileName         string
	expectedOutputFile     string
	expectedPostCleanFile  string
	cniConfDirOrderedFiles []string
}

func doTest(testNum int, tc testCase, t *testing.T) {
	_ = os.Setenv("HUB", Hub)
	_ = os.Setenv("TAG", Tag)
	t.Logf("Running install CNI test with HUB=%s, TAG=%s", Hub, Tag)
	test.RunInstallCNITest(testNum, tc.preConfFile, tc.resultFileName, tc.expectedOutputFile,
		tc.expectedPostCleanFile, tc.cniConfDirOrderedFiles, t)
}

func TestInstall(t *testing.T) {
	envHub := os.Getenv("HUB")
	if envHub != "" {
		Hub = envHub
	}
	envTag := os.Getenv("TAG")
	if envTag != "" {
		Tag = envTag
	}
	t.Logf("HUB=%s, TAG=%s", Hub, Tag)
	testDataDir := TestWorkDir + "/../deployments/kubernetes/install/test/data"
	cases := []testCase{
		{
			name:                   "File with pre-plugins--.conflist",
			preConfFile:            "00-calico.conflist",
			resultFileName:         "00-calico.conflist",
			expectedOutputFile:     testDataDir + "/expected/10-calico.conflist-istioconfig",
			expectedPostCleanFile:  testDataDir + "/pre/calico.conflist",
			cniConfDirOrderedFiles: []string{"calico.conflist"},
		},
		{
			name:                   "File without pre-plugins--.conf",
			preConfFile:            "00-minikube_cni.conf",
			resultFileName:         "00-minikube_cni.conflist",
			expectedOutputFile:     testDataDir + "/expected/minikube_cni.conflist.expected",
			expectedPostCleanFile:  testDataDir + "/expected/minikube_cni.conflist.clean",
			cniConfDirOrderedFiles: []string{"minikube_cni.conf"},
		},
		{
			name:                   "First file with pre-plugins--.conflist",
			preConfFile:            "NONE",
			resultFileName:         "00-calico.conflist",
			expectedOutputFile:     testDataDir + "/expected/10-calico.conflist-istioconfig",
			expectedPostCleanFile:  testDataDir + "/pre/calico.conflist",
			cniConfDirOrderedFiles: []string{"calico.conflist", "minikube_cni.conf"},
		},
		{
			name:                   "First file without pre-plugins--.conf",
			preConfFile:            "NONE",
			resultFileName:         "00-minikube_cni.conflist",
			expectedOutputFile:     testDataDir + "/expected/minikube_cni.conflist.expected",
			expectedPostCleanFile:  testDataDir + "/expected/minikube_cni.conflist.clean",
			cniConfDirOrderedFiles: []string{"minikube_cni.conf", "calico.conflist"},
		},
		{
			name:                   "Skip non-json file for first valid .conf file",
			preConfFile:            "NONE",
			resultFileName:         "01-minikube_cni.conflist",
			expectedOutputFile:     testDataDir + "/expected/minikube_cni.conflist.expected",
			expectedPostCleanFile:  testDataDir + "/expected/minikube_cni.conflist.clean",
			cniConfDirOrderedFiles: []string{"non_json.conf", "minikube_cni.conf", "calico.conflist"},
		},
		{
			name:                   "Skip non-json file for first valid .conflist file",
			preConfFile:            "NONE",
			resultFileName:         "01-calico.conflist",
			expectedOutputFile:     testDataDir + "/expected/10-calico.conflist-istioconfig",
			expectedPostCleanFile:  testDataDir + "/pre/calico.conflist",
			cniConfDirOrderedFiles: []string{"non_json.conf", "calico.conflist", "minikube_cni.conf"},
		},
		{
			name:                   "Skip invalid .conf file for first valid .conf file",
			preConfFile:            "NONE",
			resultFileName:         "01-minikube_cni.conflist",
			expectedOutputFile:     testDataDir + "/expected/minikube_cni.conflist.expected",
			expectedPostCleanFile:  testDataDir + "/expected/minikube_cni.conflist.clean",
			cniConfDirOrderedFiles: []string{"bad_minikube_cni.conf", "minikube_cni.conf", "calico.conflist"},
		},
		{
			name:                   "Skip invalid .conf file for first valid .conflist file",
			preConfFile:            "NONE",
			resultFileName:         "01-calico.conflist",
			expectedOutputFile:     testDataDir + "/expected/10-calico.conflist-istioconfig",
			expectedPostCleanFile:  testDataDir + "/pre/calico.conflist",
			cniConfDirOrderedFiles: []string{"bad_minikube_cni.conf", "calico.conflist", "minikube_cni.conf"},
		},
		{
			name:                  "Skip invalid .conflist files for first valid .conf file",
			preConfFile:           "NONE",
			resultFileName:        "02-minikube_cni.conflist",
			expectedOutputFile:    testDataDir + "/expected/minikube_cni.conflist.expected",
			expectedPostCleanFile: testDataDir + "/expected/minikube_cni.conflist.clean",
			cniConfDirOrderedFiles: []string{"noname_calico.conflist",
				"noplugins_calico.conflist",
				"minikube_cni.conf", "calico.conflist"},
		},
		{
			name:                  "Skip invalid .conflist files for first valid .conflist file",
			preConfFile:           "NONE",
			resultFileName:        "02-calico.conflist",
			expectedOutputFile:    testDataDir + "/expected/10-calico.conflist-istioconfig",
			expectedPostCleanFile: testDataDir + "/pre/calico.conflist",
			cniConfDirOrderedFiles: []string{"noname_calico.conflist",
				"noplugins_calico.conflist",
				"calico.conflist", "minikube_cni.conf"},
		},
		{
			name:                   "confFile env var point to missing .conf with valid .conflist file",
			preConfFile:            "00-calico.conf",
			resultFileName:         "00-calico.conflist",
			expectedOutputFile:     testDataDir + "/expected/10-calico.conflist-istioconfig",
			expectedPostCleanFile:  testDataDir + "/pre/calico.conflist",
			cniConfDirOrderedFiles: []string{"calico.conflist"},
		},
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, c.name), func(t *testing.T) {
			t.Logf("%s: Test preconf %s, expected %s", c.name, c.preConfFile, c.expectedOutputFile)
			doTest(i, c, t)
		})
	}
}
