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
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"istio.io/istio/cni/pkg/install-cni/pkg/config"
	"istio.io/istio/cni/pkg/install-cni/pkg/util"
	testutils "istio.io/istio/pilot/test/util"
)

func TestGetDefaultCNINetwork(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Fatal(err)
		}
	}()

	cases := []struct {
		name            string
		dir             string
		inFilename      string
		outFilename     string
		fileContents    string
		expectedFailure bool
	}{
		{
			name:            "inexistent directory",
			dir:             "/inexistent/directory",
			expectedFailure: true,
		},
		{
			name:            "empty directory",
			dir:             tempDir,
			expectedFailure: true,
		},
		{
			name:            "empty file",
			dir:             tempDir,
			expectedFailure: true,
			inFilename:      "empty.conf",
		},
		{
			name:            "regular file",
			dir:             tempDir,
			expectedFailure: false,
			inFilename:      "regular.conf",
			outFilename:     "regular.conf",
			fileContents: `
{
	"cniVersion": "0.3.1",
	"name": "istio-cni",
	"type": "istio-cni"
}`,
		},
		{
			name:            "another regular file",
			dir:             tempDir,
			expectedFailure: false,
			inFilename:      "regular2.conf",
			outFilename:     "regular.conf",
			fileContents: `
{
	"cniVersion": "0.3.1",
	"name": "istio-cni",
	"type": "istio-cni"
}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.fileContents != "" {
				err = ioutil.WriteFile(filepath.Join(c.dir, c.inFilename), []byte(c.fileContents), 0644)
				if err != nil {
					t.Fatal(err)
				}
			}

			result, err := getDefaultCNINetwork(c.dir)
			if (c.expectedFailure && err == nil) || (!c.expectedFailure && err != nil) {
				t.Fatalf("expected failure: %t, got %v", c.expectedFailure, err)
			}

			if c.fileContents != "" {
				if c.outFilename != result {
					t.Fatalf("expected %s, got %s", c.outFilename, result)
				}
			}
		})
	}
}

func TestGetCNIConfigFilepath(t *testing.T) {
	cases := []struct {
		name              string
		chainedCNIPlugin  bool
		specifiedConfName string
		delayedConfName   string
		expectedConfName  string
		existingConfFiles []string
	}{
		{
			name:              "unspecified existing CNI config file",
			chainedCNIPlugin:  true,
			expectedConfName:  "bridge.conf",
			existingConfFiles: []string{"bridge.conf", "list.conflist"},
		},
		{
			name:             "unspecified delayed CNI config file",
			chainedCNIPlugin: true,
			delayedConfName:  "bridge.conf",
			expectedConfName: "bridge.conf",
		},
		{
			name:             "unspecified CNI config file never created",
			chainedCNIPlugin: true,
		},
		{
			name:              "specified existing CNI config file",
			chainedCNIPlugin:  true,
			specifiedConfName: "list.conflist",
			expectedConfName:  "list.conflist",
			existingConfFiles: []string{"bridge.conf", "list.conflist"},
		},
		{
			name:              "specified existing CNI config file (.conf to .conflist)",
			chainedCNIPlugin:  true,
			specifiedConfName: "list.conf",
			expectedConfName:  "list.conflist",
			existingConfFiles: []string{"bridge.conf", "list.conflist"},
		},
		{
			name:              "specified existing CNI config file (.conflist to .conf)",
			chainedCNIPlugin:  true,
			specifiedConfName: "bridge.conflist",
			expectedConfName:  "bridge.conf",
			existingConfFiles: []string{"bridge.conf", "list.conflist"},
		},
		{
			name:              "specified delayed CNI config file",
			chainedCNIPlugin:  true,
			specifiedConfName: "bridge.conf",
			delayedConfName:   "bridge.conf",
			expectedConfName:  "bridge.conf",
		},
		{
			name:              "specified CNI config file never created",
			chainedCNIPlugin:  true,
			specifiedConfName: "never-created.conf",
			existingConfFiles: []string{"bridge.conf", "list.conflist"},
		},
		{
			name:             "standalone CNI plugin unspecified CNI config file",
			expectedConfName: "YYY-istio-cni.conf",
		},
		{
			name:              "standalone CNI plugin specified CNI config file",
			specifiedConfName: "specific-name.conf",
			expectedConfName:  "specific-name.conf",
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

			cfg := pluginConfig{
				mountedCNINetDir: tempDir,
				cniConfName:      c.specifiedConfName,
				chainedCNIPlugin: c.chainedCNIPlugin,
			}
			var expectedFilepath string
			if len(c.expectedConfName) > 0 {
				expectedFilepath = filepath.Join(tempDir, c.expectedConfName)
			}

			if !c.chainedCNIPlugin {
				// Standalone CNI plugin
				parent := context.Background()
				ctx1, cancel := context.WithTimeout(parent, 5*time.Second)
				defer cancel()
				result, err := getCNIConfigFilepath(ctx1, cfg)
				if err != nil {
					assert.Empty(t, result)
					if err == context.DeadlineExceeded {
						t.Fatalf("timed out waiting for expected %s", expectedFilepath)
					}
					t.Fatal(err)
				}
				if result != expectedFilepath {
					t.Fatalf("expected %s, got %s", expectedFilepath, result)
				}
				// Successful test case
				return
			}

			// Handle chained CNI plugin cases
			// Call with goroutine to test fsnotify watcher
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			resultChan, errChan := make(chan string), make(chan error)
			go func(resultChan chan string, errChan chan error, ctx context.Context, cfg pluginConfig) {
				result, err := getCNIConfigFilepath(ctx, cfg)
				if err != nil {
					errChan <- err
					return
				}
				resultChan <- result
			}(resultChan, errChan, parent, cfg)

			select {
			case result := <-resultChan:
				assert.NotEmpty(t, result)
				if len(c.delayedConfName) > 0 {
					// Delayed case
					t.Fatalf("did not expect to retrieve a CNI config file %s", result)
				} else if result != expectedFilepath {
					if len(expectedFilepath) > 0 {
						t.Fatalf("expected %s, got %s", expectedFilepath, result)
					}
					t.Fatalf("did not expect to retrieve a CNI config file %s", result)
				}
				// Successful test for non-delayed cases
				return
			case err := <-errChan:
				t.Fatal(err)
			case <-time.After(5 * time.Second):
				if len(c.delayedConfName) > 0 {
					// Delayed case
					// Write delayed CNI config file
					data, err := ioutil.ReadFile(filepath.Join("testdata", c.delayedConfName))
					if err != nil {
						t.Fatal(err)
					}
					err = ioutil.WriteFile(filepath.Join(tempDir, c.delayedConfName), data, 0644)
					if err != nil {
						t.Fatal(err)
					}
				} else if len(c.expectedConfName) > 0 {
					t.Fatalf("timed out waiting for expected %s", expectedFilepath)
				} else {
					// Successful test for test cases where CNI config file is never created
					return
				}
			}

			// Only for delayed cases
			select {
			case result := <-resultChan:
				assert.NotEmpty(t, result)
				if result != expectedFilepath {
					if len(expectedFilepath) > 0 {
						t.Fatalf("expected %s, got %s", expectedFilepath, result)
					}
					t.Fatalf("did not expect to retrieve a CNI config file %s", result)
				}
			case err := <-errChan:
				t.Fatal(err)
			case <-time.After(5 * time.Second):
				t.Fatalf("timed out waiting for expected %s", expectedFilepath)
			}
		})
	}
}

func TestInsertCNIConfig(t *testing.T) {
	cases := []struct {
		name                 string
		expectedFailure      bool
		existingConfFilename string
		newConfFilename      string
	}{
		{
			name:                 "invalid existing config format (map)",
			expectedFailure:      true,
			existingConfFilename: "invalid-map.conflist",
			newConfFilename:      "istio-cni.conf",
		},
		{
			name:                 "invalid new config format (arr)",
			expectedFailure:      true,
			existingConfFilename: "list.conflist",
			newConfFilename:      "invalid-arr.conflist",
		},
		{
			name:                 "invalid existing config format (arr)",
			expectedFailure:      true,
			existingConfFilename: "invalid-arr.conflist",
			newConfFilename:      "istio-cni.conf",
		},
		{
			name:                 "regular network file",
			existingConfFilename: "bridge.conf",
			newConfFilename:      "istio-cni.conf",
		},
		{
			name:                 "list network file",
			existingConfFilename: "list.conflist",
			newConfFilename:      "istio-cni.conf",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			istioConf := testutils.ReadFile(filepath.Join("testdata", c.newConfFilename), t)
			existingConfFilepath := "testdata/" + c.existingConfFilename
			existingConf := testutils.ReadFile(existingConfFilepath, t)

			output, err := insertCNIConfig(istioConf, existingConf)
			if err != nil {
				if !c.expectedFailure {
					t.Fatal(err)
				}
				return
			}

			goldenFilepath := existingConfFilepath + ".golden"
			goldenConfig := testutils.ReadFile(goldenFilepath, t)
			testutils.CompareBytes(output, goldenConfig, goldenFilepath, t)
		})
	}
}

const (
	// For testing purposes, set kubeconfigFilename equivalent to the path in the test files and use __KUBECONFIG_FILENAME__
	// CreateCNIConfigFile joins the MountedCNINetDir and KubeconfigFilename if __KUBECONFIG_FILEPATH__ was used
	kubeconfigFilename   = "/path/to/kubeconfig"
	cniNetworkConfigFile = "testdata/istio-cni.conf.template"
	cniNetworkConfig     = `{
  "cniVersion": "0.3.1",
  "name": "istio-cni",
  "type": "istio-cni",
  "log_level": "__LOG_LEVEL__",
  "kubernetes": {
      "kubeconfig": "__KUBECONFIG_FILENAME__",
      "cni_bin_dir": "/path/cni/bin"
  }
}
`
)

func TestCreateCNIConfigFile(t *testing.T) {
	cases := []struct {
		name              string
		chainedCNIPlugin  bool
		specifiedConfName string
		expectedConfName  string
		goldenConfName    string
		existingConfFiles []string
	}{
		{
			name:              "unspecified existing CNI config file (existing .conf to conflist)",
			chainedCNIPlugin:  true,
			expectedConfName:  "bridge.conflist",
			goldenConfName:    "bridge.conf.golden",
			existingConfFiles: []string{"bridge.conf", "list.conflist"},
		},
		{
			name:             "unspecified CNI config file never created",
			chainedCNIPlugin: true,
		},
		{
			name:              "specified existing CNI config file",
			chainedCNIPlugin:  true,
			specifiedConfName: "list.conflist",
			expectedConfName:  "list.conflist",
			goldenConfName:    "list.conflist.golden",
			existingConfFiles: []string{"bridge.conf", "list.conflist"},
		},
		{
			name:              "specified existing CNI config file (specified .conf to .conflist)",
			chainedCNIPlugin:  true,
			specifiedConfName: "list.conf",
			expectedConfName:  "list.conflist",
			goldenConfName:    "list.conflist.golden",
			existingConfFiles: []string{"bridge.conf", "list.conflist"},
		},
		{
			name:              "specified existing CNI config file (existing .conf to .conflist)",
			chainedCNIPlugin:  true,
			specifiedConfName: "bridge.conflist",
			expectedConfName:  "bridge.conflist",
			goldenConfName:    "bridge.conf.golden",
			existingConfFiles: []string{"bridge.conf", "list.conflist"},
		},
		{
			name:              "specified CNI config file never created",
			chainedCNIPlugin:  true,
			specifiedConfName: "never-created.conf",
			existingConfFiles: []string{"bridge.conf", "list.conflist"},
		},
		{
			name:             "standalone CNI plugin unspecified CNI config file",
			expectedConfName: "YYY-istio-cni.conf",
			goldenConfName:   "istio-cni.conf",
		},
		{
			name:              "standalone CNI plugin specified CNI config file",
			specifiedConfName: "specific-name.conf",
			expectedConfName:  "specific-name.conf",
			goldenConfName:    "istio-cni.conf",
		},
	}

	for i, c := range cases {
		cfgFile := config.Config{
			CNIConfName:          c.specifiedConfName,
			ChainedCNIPlugin:     c.chainedCNIPlugin,
			CNINetworkConfigFile: cniNetworkConfigFile,
			LogLevel:             "debug",
			KubeconfigFilename:   kubeconfigFilename,
		}

		cfg := config.Config{
			CNIConfName:        c.specifiedConfName,
			ChainedCNIPlugin:   c.chainedCNIPlugin,
			CNINetworkConfig:   cniNetworkConfig,
			LogLevel:           "debug",
			KubeconfigFilename: kubeconfigFilename,
		}
		test := func(cfg config.Config) func(t *testing.T) {
			return func(t *testing.T) {
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

				cfg.MountedCNINetDir = tempDir

				var expectedFilepath string
				if len(c.expectedConfName) > 0 {
					expectedFilepath = filepath.Join(tempDir, c.expectedConfName)
				}

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				resultFilepath, err := createCNIConfigFile(ctx, &cfg, "")
				if err != nil {
					assert.Empty(t, resultFilepath)
					if err == context.DeadlineExceeded {
						if len(c.expectedConfName) > 0 {
							t.Fatalf("timed out waiting for expected %s", expectedFilepath)
						}
						// Successful test for never-created config file
						return
					}
					t.Fatal(err)
				}

				assert.NotEmpty(t, resultFilepath)

				if resultFilepath != expectedFilepath {
					if len(expectedFilepath) > 0 {
						t.Fatalf("expected %s, got %s", expectedFilepath, resultFilepath)
					}
					t.Fatalf("did not expect to retrieve a CNI config file %s", resultFilepath)
				}

				resultConfig := testutils.ReadFile(resultFilepath, t)

				goldenFilepath := filepath.Join("testdata", c.goldenConfName)
				goldenConfig := testutils.ReadFile(goldenFilepath, t)
				testutils.CompareBytes(resultConfig, goldenConfig, goldenFilepath, t)
			}
		}
		t.Run("network-config-file "+c.name, test(cfgFile))
		t.Run(c.name, test(cfg))
	}
}
