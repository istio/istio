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
	"os"
	"path/filepath"
	"testing"
	"time"

	"istio.io/istio/cni/pkg/config"
	testutils "istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/file"
	"istio.io/istio/pkg/test/util/assert"
)

func TestGetConfigFilenames(t *testing.T) {
	tempDir := t.TempDir()

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
			// Only .conf and .conflist files are detectable
			name:            "undetectable file",
			dir:             tempDir,
			expectedFailure: true,
			inFilename:      "undetectable.file",
			fileContents: `
{
	"cniVersion": "0.3.1",
	"name": "istio-cni",
	"type": "istio-cni"
}`,
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
				err := os.WriteFile(filepath.Join(c.dir, c.inFilename), []byte(c.fileContents), 0o644)
				if err != nil {
					t.Fatal(err)
				}
			}

			result, err := getConfigFilenames(c.dir)
			if (c.expectedFailure && err == nil) || (!c.expectedFailure && err != nil) {
				t.Fatalf("expected failure: %t, got %v", c.expectedFailure, err)
			}

			if c.fileContents != "" {
				if len(result) > 0 && c.outFilename != result[0] {
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

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Create temp directory for files
			tempDir := t.TempDir()

			// Create existing config files if specified in test case
			for _, filename := range c.existingConfFiles {
				if err := file.AtomicCopy(filepath.Join("testdata", filepath.Base(filename)), tempDir, filepath.Base(filename)); err != nil {
					t.Fatal(err)
				}
			}

			var expectedFilepath string
			if len(c.expectedConfName) > 0 {
				expectedFilepath = filepath.Join(tempDir, c.expectedConfName)
			}

			if !c.chainedCNIPlugin {
				// Standalone CNI plugin
				parent := context.Background()
				ctx1, cancel := context.WithTimeout(parent, 100*time.Millisecond)
				defer cancel()
				result, err := getCNIConfigFilepath(ctx1, c.specifiedConfName, tempDir, c.chainedCNIPlugin)
				if err != nil {
					assert.Equal(t, result, "")
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
			parent := t.Context()
			resultChan, errChan := make(chan string, 1), make(chan error, 1)
			go func(resultChan chan string, errChan chan error, ctx context.Context, cniConfName, mountedCNINetDir string, chained bool) {
				result, err := getCNIConfigFilepath(ctx, cniConfName, mountedCNINetDir, chained)
				if err != nil {
					errChan <- err
					return
				}
				resultChan <- result
			}(resultChan, errChan, parent, c.specifiedConfName, tempDir, c.chainedCNIPlugin)

			select {
			case result := <-resultChan:
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
			case <-time.After(250 * time.Millisecond):
				if len(c.delayedConfName) > 0 {
					// Delayed case
					// Write delayed CNI config file
					data, err := os.ReadFile(filepath.Join("testdata", c.delayedConfName))
					if err != nil {
						t.Fatal(err)
					}
					err = os.WriteFile(filepath.Join(tempDir, c.delayedConfName), data, 0o644)
					if err != nil {
						t.Fatal(err)
					}
					t.Logf("delayed write to %v", filepath.Join(tempDir, c.delayedConfName))
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
				if result != expectedFilepath {
					if len(expectedFilepath) > 0 {
						t.Fatalf("expected %s, got %s", expectedFilepath, result)
					}
					t.Fatalf("did not expect to retrieve a CNI config file %s", result)
				}
			case err := <-errChan:
				t.Fatal(err)
			case <-time.After(250 * time.Millisecond):
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
		{
			name:                 "list network file with existing istio",
			existingConfFilename: "list-with-istio.conflist",
			newConfFilename:      "istio-cni.conf",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			istioConf := testutils.ReadFile(t, filepath.Join("testdata", c.newConfFilename))
			existingConfFilepath := filepath.Join("testdata", c.existingConfFilename)
			existingConf := testutils.ReadFile(t, existingConfFilepath)

			output, err := insertCNIConfig(istioConf, existingConf)
			if err != nil {
				if !c.expectedFailure {
					t.Fatal(err)
				}
				return
			}

			goldenFilepath := existingConfFilepath + ".golden"
			goldenConfig := testutils.ReadFile(t, goldenFilepath)
			testutils.CompareBytes(t, output, goldenConfig, goldenFilepath)
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
  "plugin_log_level": "__LOG_LEVEL__",
  "pod_namespace": "__POD_NAMESPACE__",
  "kubernetes": {
      "kubeconfig": "__KUBECONFIG_FILENAME__",
      "cni_bin_dir": "/path/cni/bin"
  }
}
`
)

func TestCreateCNIConfigFile(t *testing.T) {
	cases := []struct {
		name                    string
		chainedCNIPlugin        bool
		specifiedConfName       string
		expectedConfName        string
		goldenConfName          string
		existingConfFiles       map[string]string // {srcFilename: targetFilename, ...}
		ambientEnabled          bool
		istioOwnedCNIConfig     bool
		istioOwnedCNIConfigFile string
	}{
		{
			name:              "unspecified existing CNI config file (existing .conf to conflist)",
			chainedCNIPlugin:  true,
			expectedConfName:  "bridge.conflist",
			goldenConfName:    "bridge.conf.golden",
			existingConfFiles: map[string]string{"bridge.conf": "bridge.conf", "list.conflist": "list.conflist"},
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
			existingConfFiles: map[string]string{"bridge.conf": "bridge.conf", "list.conflist": "list.conflist"},
		},
		{
			name:              "specified existing CNI config file (specified .conf to .conflist)",
			chainedCNIPlugin:  true,
			specifiedConfName: "list.conf",
			expectedConfName:  "list.conflist",
			goldenConfName:    "list.conflist.golden",
			existingConfFiles: map[string]string{"bridge.conf": "bridge.conf", "list.conflist": "list.conflist"},
		},
		{
			name:              "specified existing CNI config file (existing .conf to .conflist)",
			chainedCNIPlugin:  true,
			specifiedConfName: "bridge.conflist",
			expectedConfName:  "bridge.conflist",
			goldenConfName:    "bridge.conf.golden",
			existingConfFiles: map[string]string{"bridge.conf": "bridge.conf", "list.conflist": "list.conflist"},
		},
		{
			name:              "specified CNI config file never created",
			chainedCNIPlugin:  true,
			specifiedConfName: "never-created.conf",
			existingConfFiles: map[string]string{"bridge.conf": "bridge.conf", "list.conflist": "list.conflist"},
		},
		{
			name:              "specified CNI config file undetectable",
			chainedCNIPlugin:  true,
			specifiedConfName: "undetectable.file",
			expectedConfName:  "undetectable.file",
			goldenConfName:    "list.conflist.golden",
			existingConfFiles: map[string]string{"bridge.conf": "bridge.conf", "list.conflist": "undetectable.file"},
		},
		// TODO(jaellio): Fix unspecified behavior when chainedCNIPlugin is false
		// expected config name should remain YYY-istio-cni.conf
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
		{
			name:                "specified existing .conf primary CNI config file - istio owned",
			chainedCNIPlugin:    true,
			specifiedConfName:   "bridge.conflist",
			expectedConfName:    "02-istio-conf.conflist",
			goldenConfName:      "istio-owned-bridge.conflist.golden",
			existingConfFiles:   map[string]string{"bridge.conf": "bridge.conf", "list.conflist": "list.conflist"},
			ambientEnabled:      true,
			istioOwnedCNIConfig: true,
		},
		{
			name:                "specified existing primary .conflist CNI config file - istio owned",
			chainedCNIPlugin:    true,
			specifiedConfName:   "list.conflist",
			expectedConfName:    "02-istio-conf.conflist",
			goldenConfName:      "istio-owned.conflist.golden",
			existingConfFiles:   map[string]string{"bridge.conf": "bridge.conf", "list.conflist": "list.conflist"},
			ambientEnabled:      true,
			istioOwnedCNIConfig: true,
		},
		{
			name:                "specified existing CNI config file (specified .conf to .conflist) - istio owned",
			chainedCNIPlugin:    true,
			specifiedConfName:   "list.conf",
			expectedConfName:    "02-istio-conf.conflist",
			goldenConfName:      "istio-owned.conflist.golden",
			existingConfFiles:   map[string]string{"bridge.conf": "bridge.conf", "list.conflist": "list.conflist"},
			ambientEnabled:      true,
			istioOwnedCNIConfig: true,
		},
		{
			name:                "specified existing CNI config file (existing .conf to .conflist) - istio owned",
			chainedCNIPlugin:    true,
			specifiedConfName:   "bridge.conflist",
			expectedConfName:    "02-istio-conf.conflist",
			goldenConfName:      "istio-owned-bridge.conflist.golden",
			existingConfFiles:   map[string]string{"bridge.conf": "bridge.conf", "list.conflist": "list.conflist"},
			ambientEnabled:      true,
			istioOwnedCNIConfig: true,
		},
		{
			name:                "specified CNI config file never created - istio owned",
			chainedCNIPlugin:    true,
			specifiedConfName:   "02-istio-conf.conflist",
			existingConfFiles:   map[string]string{"bridge.conf": "bridge.conf", "list.conflist": "list.conflist"},
			ambientEnabled:      true,
			istioOwnedCNIConfig: true,
		},
		{
			name:                "specified CNI config file undetectable - istio owned",
			chainedCNIPlugin:    true,
			specifiedConfName:   "undetectable.file",
			expectedConfName:    "02-istio-conf.conflist",
			goldenConfName:      "istio-owned.conflist.golden",
			existingConfFiles:   map[string]string{"bridge.conf": "bridge.conf", "list.conflist": "undetectable.file"},
			ambientEnabled:      true,
			istioOwnedCNIConfig: true,
		},
	}

	for _, c := range cases {
		if c.istioOwnedCNIConfig && len(c.istioOwnedCNIConfigFile) == 0 {
			c.istioOwnedCNIConfigFile = "02-istio-conf.conflist"
		}
		cfgFile := config.InstallConfig{
			CNIConfName:                 c.specifiedConfName,
			ChainedCNIPlugin:            c.chainedCNIPlugin,
			PluginLogLevel:              "debug",
			CNIAgentRunDir:              kubeconfigFilename,
			PodNamespace:                "my-namespace",
			AmbientEnabled:              c.ambientEnabled,
			IstioOwnedCNIConfig:         c.istioOwnedCNIConfig,
			IstioOwnedCNIConfigFilename: c.istioOwnedCNIConfigFile,
		}

		cfg := config.InstallConfig{
			CNIConfName:                 c.specifiedConfName,
			ChainedCNIPlugin:            c.chainedCNIPlugin,
			PluginLogLevel:              "debug",
			CNIAgentRunDir:              kubeconfigFilename,
			PodNamespace:                "my-namespace",
			AmbientEnabled:              c.ambientEnabled,
			IstioOwnedCNIConfig:         c.istioOwnedCNIConfig,
			IstioOwnedCNIConfigFilename: c.istioOwnedCNIConfigFile,
			NativeNftables:              false,
		}
		test := func(cfg config.InstallConfig) func(t *testing.T) {
			return func(t *testing.T) {
				// Create temp directory for files
				tempDir := t.TempDir()

				// Create existing config files if specified in test case
				for srcFilename, targetFilename := range c.existingConfFiles {
					if err := file.AtomicCopy(filepath.Join("testdata", srcFilename), tempDir, targetFilename); err != nil {
						t.Fatal(err)
					}
				}

				cfg.MountedCNINetDir = tempDir

				var expectedFilepath string
				if len(c.expectedConfName) > 0 {
					expectedFilepath = filepath.Join(tempDir, c.expectedConfName)
				}

				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()
				resultFilepath, err := createCNIConfigFile(ctx, &cfg)
				if err != nil {
					assert.Equal(t, resultFilepath, "")
					if err == context.DeadlineExceeded {
						if len(c.expectedConfName) > 0 {
							t.Fatalf("timed out waiting for expected %s", expectedFilepath)
						}
						// Successful test for never-created config file
						return
					}
					t.Fatal(err)
				}

				if resultFilepath != expectedFilepath {
					if len(expectedFilepath) > 0 {
						t.Fatalf("expected %s, got %s", expectedFilepath, resultFilepath)
					}
					t.Fatalf("did not expect to retrieve a CNI config file %s", resultFilepath)
				}

				resultConfig := testutils.ReadFile(t, resultFilepath)

				goldenFilepath := filepath.Join("testdata", c.goldenConfName)
				goldenConfig := testutils.ReadFile(t, goldenFilepath)
				testutils.CompareBytes(t, resultConfig, goldenConfig, goldenFilepath)
			}
		}
		t.Run("network-config-file "+c.name, test(cfgFile))
		t.Run(c.name, test(cfg))
	}
}
