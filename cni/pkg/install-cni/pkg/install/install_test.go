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

package install

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"istio.io/istio/cni/pkg/install-cni/pkg/config"
	"istio.io/istio/cni/pkg/install-cni/pkg/util"
)

func TestCheckInstall(t *testing.T) {
	cases := []struct {
		name              string
		expectedFailure   bool
		cniConfigFilename string
		cniConfName       string
		chainedCNIPlugin  bool
		existingConfFiles []string
	}{
		{
			name:              "preempted config",
			expectedFailure:   true,
			cniConfigFilename: "list.conflist.golden",
			chainedCNIPlugin:  true,
			existingConfFiles: []string{"bridge.conf", "list.conflist.golden"},
		},
		{
			name:              "intentional preempted config invalid",
			expectedFailure:   true,
			cniConfigFilename: "invalid-arr.conflist",
			cniConfName:       "invalid-arr.conflist",
			chainedCNIPlugin:  true,
			existingConfFiles: []string{"bridge.conf", "invalid-arr.conflist"},
		},
		{
			name:              "intentional preempted config",
			cniConfigFilename: "list.conflist.golden",
			cniConfName:       "list.conflist.golden",
			chainedCNIPlugin:  true,
			existingConfFiles: []string{"bridge.conf", "list.conflist.golden"},
		},
		{
			name:              "CNI config file removed",
			expectedFailure:   true,
			cniConfigFilename: "file-removed.conflist",
		},
		{
			name:              "istio-cni config removed from CNI config file",
			expectedFailure:   true,
			cniConfigFilename: "list.conflist",
			chainedCNIPlugin:  true,
			existingConfFiles: []string{"list.conflist"},
		},
		{
			name:              "standalone istio-cni config not in CNI config file",
			expectedFailure:   true,
			cniConfigFilename: "bridge.conf",
			existingConfFiles: []string{"bridge.conf"},
		},
		{
			name:              "standalone istio-cni config",
			cniConfigFilename: "istio-cni.conf",
			existingConfFiles: []string{"istio-cni.conf"},
		},
	}

	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Create temp directory for files
			tempDir, err := ioutil.TempDir("", fmt.Sprintf("test-case-%d-", i))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.RemoveAll(tempDir); err != nil {
					t.Fatal(err)
				}
			}()

			// Create existing config files if specified in test case
			util.CopyExistingConfFiles(t, tempDir, c.existingConfFiles...)

			cfg := &config.Config{
				MountedCNINetDir: tempDir,
				CNIConfName:      c.cniConfName,
				ChainedCNIPlugin: c.chainedCNIPlugin,
			}
			err = checkInstall(cfg, filepath.Join(tempDir, c.cniConfigFilename))
			if (c.expectedFailure && err == nil) || (!c.expectedFailure && err != nil) {
				t.Fatalf("expected failure: %t, got %v", c.expectedFailure, err)
			}
		})
	}
}
