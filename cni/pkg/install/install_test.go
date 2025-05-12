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
	"sync/atomic"
	"testing"
	"time"

	"istio.io/istio/cni/pkg/config"
	testutils "istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/file"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

func TestCheckInstall(t *testing.T) {
	cases := []struct {
		name              string
		expectedFailure   bool
		cniConfigFilename string
		cniConfName       string
		chainedCNIPlugin  bool
		existingConfFiles map[string]string // {srcFilename: targetFilename, ...}
	}{
		{
			name:              "preempted config",
			expectedFailure:   true,
			cniConfigFilename: "list.conflist",
			chainedCNIPlugin:  true,
			existingConfFiles: map[string]string{"bridge.conf": "bridge.conf", "list.conflist.golden": "list.conflist"},
		},
		{
			name:              "intentional preempted config invalid",
			expectedFailure:   true,
			cniConfigFilename: "invalid-arr.conflist",
			cniConfName:       "invalid-arr.conflist",
			chainedCNIPlugin:  true,
			existingConfFiles: map[string]string{"bridge.conf": "bridge.conf", "invalid-arr.conflist": "invalid-arr.conflist"},
		},
		{
			name:              "intentional preempted config, missing istio owned config",
			expectedFailure:   true,
			cniConfigFilename: "list.conflist",
			cniConfName:       "list.conflist",
			chainedCNIPlugin:  true,
			existingConfFiles: map[string]string{"bridge.conf": "bridge.conf", "list.conflist.golden": "list.conflist"},
		},
		{
			name:              "CNI config file removed",
			expectedFailure:   true,
			cniConfigFilename: "file-removed.conflist",
		},
		{
			name:              "istio-cni config removed from CNI config file",
			expectedFailure:   true,
			cniConfigFilename: "02-istio-conf.conflist",
			chainedCNIPlugin:  true,
			existingConfFiles: map[string]string{"list.conflist": "02-istio-conf.conflist"},
		},
		{
			name:              "chained CNI plugin",
			cniConfigFilename: "02-istio-conf.conflist",
			chainedCNIPlugin:  true,
			existingConfFiles: map[string]string{"list.conflist.golden": "02-istio-conf.conflist"},
		},
		{
			name:              "standalone CNI plugin istio-cni config not in CNI config file",
			expectedFailure:   true,
			cniConfigFilename: "bridge.conf",
			existingConfFiles: map[string]string{"bridge.conf": "bridge.conf"},
		},
		{
			name:              "standalone CNI plugin",
			cniConfigFilename: "istio-cni.conf",
			existingConfFiles: map[string]string{"istio-cni.conf": "istio-cni.conf"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Create temp directory for files
			tempDir := t.TempDir()

			// Create existing config files if specified in test case
			for srcFilename, targetFilename := range c.existingConfFiles {
				if err := file.AtomicCopy(filepath.Join("testdata", srcFilename), tempDir, targetFilename); err != nil {
					t.Fatal(err)
				}
			}

			cfg := &config.InstallConfig{
				MountedCNINetDir: tempDir,
				CNIConfName:      c.cniConfName,
				ChainedCNIPlugin: c.chainedCNIPlugin,
			}
			ctx := context.Background()
			err := checkValidCNIConfig(ctx, cfg, filepath.Join(tempDir, c.cniConfigFilename))
			if (c.expectedFailure && err == nil) || (!c.expectedFailure && err != nil) {
				t.Fatalf("expected failure: %t, got %v", c.expectedFailure, err)
			}
		})
	}
}

// TODO(jaellio): update to check plugin equality btw Istio owned config and primary config
func TestSleepCheckInstall(t *testing.T) {
	cases := []struct {
		name                  string
		chainedCNIPlugin      bool
		cniConfigFilename     string
		invalidConfigFilename string
		validConfigFilename   string
		saFilename            string
		saNewFilename         string
	}{
		{
			name:                  "chained CNI plugin",
			chainedCNIPlugin:      true,
			cniConfigFilename:     "02-istio-conf.conflist",
			invalidConfigFilename: "list.conflist",
			validConfigFilename:   "list.conflist.golden",
			saFilename:            "token-foo",
		},
		{
			name:                "standalone CNI plugin",
			cniConfigFilename:   "istio-cni.conf",
			validConfigFilename: "istio-cni.conf",
			saFilename:          "token-foo",
			saNewFilename:       "token-bar",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Create temp directory for files
			tempDir := t.TempDir()

			// Initialize parameters
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cfg := &config.InstallConfig{
				MountedCNINetDir: tempDir,
				ChainedCNIPlugin: c.chainedCNIPlugin,
			}
			cniConfigFilepath := filepath.Join(tempDir, c.cniConfigFilename)
			isReady := &atomic.Value{}
			setNotReady(isReady)
			in := NewInstaller(cfg, isReady)
			in.cniConfigFilepath = cniConfigFilepath

			if err := file.AtomicCopy(filepath.Join("testdata", c.saFilename), tempDir, c.saFilename); err != nil {
				t.Fatal(err)
			}

			if len(c.invalidConfigFilename) > 0 {
				// Copy an invalid config file into tempDir
				if err := file.AtomicCopy(filepath.Join("testdata", c.invalidConfigFilename), tempDir, c.cniConfigFilename); err != nil {
					t.Fatal(err)
				}
			}

			t.Log("Expecting an invalid configuration log:")
			err := in.sleepWatchInstall(ctx, sets.String{})
			if err != nil {
				t.Fatalf("error should be nil due to invalid config, got: %v", err)
			}
			assert.Equal(t, isReady.Load(), false)

			if len(c.invalidConfigFilename) > 0 {
				if err := os.Remove(cniConfigFilepath); err != nil {
					t.Fatal(err)
				}
			}

			// Copy a valid config file into tempDir
			if err := file.AtomicCopy(filepath.Join("testdata", c.validConfigFilename), tempDir, c.cniConfigFilename); err != nil {
				t.Fatal(err)
			}

			// Listen for isReady to be set to true
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			readyChan := make(chan bool)
			go func(ctx context.Context, tick <-chan time.Time) {
				for {
					select {
					case <-ctx.Done():
						return
					case <-tick:
						if isReady.Load().(bool) {
							readyChan <- true
						}
					}
				}
			}(ctx, ticker.C)

			// Listen to sleepWatchInstall return value
			// Should detect a valid configuration and wait indefinitely for a file modification
			errChan := make(chan error)
			go func(ctx context.Context) {
				errChan <- in.sleepWatchInstall(ctx, sets.String{})
			}(ctx)

			select {
			case <-readyChan:
				assert.Equal(t, isReady.Load(), true)
			case err := <-errChan:
				if err == nil {
					t.Fatal("invalid configuration detected")
				}
				t.Fatal(err)
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for isReady to be set to true")
			}

			// Change SA token
			if len(c.saNewFilename) > 0 {
				t.Log("Expecting detect changes to the SA token")
				if err := file.AtomicCopy(filepath.Join("testdata", c.saNewFilename), tempDir, c.saFilename); err != nil {
					t.Fatal(err)
				}

				select {
				case err := <-errChan:
					if err != nil {
						// A change in SA token should return nil
						t.Fatal(err)
					}
					assert.Equal(t, isReady.Load(), false)
				case <-time.After(5 * time.Second):
					t.Fatal("timed out waiting for invalid configuration to be detected")
				}

				// Revert valid SA
				if err := file.AtomicCopy(filepath.Join("testdata", c.saFilename), tempDir, c.saFilename); err != nil {
					t.Fatal(err)
				}

				// Run sleepWatchInstall
				go func(ctx context.Context, in *Installer) {
					errChan <- in.sleepWatchInstall(ctx, sets.String{})
				}(ctx, in)
			}

			// Remove Istio CNI's config
			t.Log("Expecting an invalid configuration log:")
			if len(c.invalidConfigFilename) > 0 {
				if err := file.AtomicCopy(filepath.Join("testdata", c.invalidConfigFilename), tempDir, c.cniConfigFilename); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Remove(cniConfigFilepath); err != nil {
					t.Fatal(err)
				}
			}

			select {
			case err := <-errChan:
				if err != nil {
					// An invalid configuration should return nil
					// Either an invalid config did not return nil (which is an issue) or an unexpected error occurred
					t.Fatal(err)
				}
				assert.Equal(t, isReady.Load(), false)
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for invalid configuration to be detected")
			}
		})
	}
}

// TODO(jaellio) Update this test to ensure istio owned config is cleaned up (and other configs that contain istio-cni(?))
func TestCleanup(t *testing.T) {
	cases := []struct {
		name                   string
		expectedFailure        bool
		chainedCNIPlugin       bool
		configFilename         string
		existingConfigFilename string
		expectedConfigFilename string
	}{
		{
			name:                   "chained CNI plugin",
			chainedCNIPlugin:       true,
			configFilename:         "list.conflist",
			existingConfigFilename: "list-with-istio.conflist",
			expectedConfigFilename: "list-no-istio.conflist",
		},
		{
			name:                   "standalone CNI plugin",
			configFilename:         "istio-cni.conf",
			existingConfigFilename: "istio-cni.conf",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Create temp directory for files
			cniNetDir := t.TempDir()
			cniBinDir := t.TempDir()

			// Create existing config file if specified in test case
			cniConfigFilePath := filepath.Join(cniNetDir, c.configFilename)
			if err := file.AtomicCopy(filepath.Join("testdata", c.existingConfigFilename), cniNetDir, c.configFilename); err != nil {
				t.Fatal(err)
			}

			// Create existing binary files
			if err := os.WriteFile(filepath.Join(cniBinDir, "istio-cni"), []byte{1, 2, 3}, 0o755); err != nil {
				t.Fatal(err)
			}

			// Create kubeconfig
			kubeConfigFilePath := filepath.Join(cniNetDir, "kubeconfig")
			if err := os.WriteFile(kubeConfigFilePath, []byte{1, 2, 3}, 0o755); err != nil {
				t.Fatal(err)
			}

			cfg := &config.InstallConfig{
				MountedCNINetDir: cniNetDir,
				ChainedCNIPlugin: c.chainedCNIPlugin,
				CNIBinTargetDirs: []string{cniBinDir},
			}

			isReady := &atomic.Value{}
			isReady.Store(false)
			installer := NewInstaller(cfg, isReady)
			installer.cniConfigFilepath = cniConfigFilePath
			installer.kubeconfigFilepath = kubeConfigFilePath
			err := installer.Cleanup()
			if (c.expectedFailure && err == nil) || (!c.expectedFailure && err != nil) {
				t.Fatalf("expected failure: %t, got %v", c.expectedFailure, err)
			}

			// check if conf file is deleted/conflist file is updated
			if c.chainedCNIPlugin {
				resultConfig := testutils.ReadFile(t, cniConfigFilePath)

				goldenFilepath := filepath.Join("testdata", c.expectedConfigFilename)
				goldenConfig := testutils.ReadFile(t, goldenFilepath)
				testutils.CompareBytes(t, resultConfig, goldenConfig, goldenFilepath)
			} else if file.Exists(cniConfigFilePath) {
				t.Fatalf("file %s was not deleted", c.configFilename)
			}

			// check if kubeconfig is deleted
			if file.Exists(kubeConfigFilePath) {
				t.Fatal("kubeconfig was not deleted")
			}

			// check if binaries are deleted
			if file.Exists(filepath.Join(cniBinDir, "istio-cni")) {
				t.Fatalf("File %s was not deleted", "istio-cni")
			}
		})
	}
}
