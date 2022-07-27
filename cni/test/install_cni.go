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
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/viper"

	"istio.io/istio/cni/pkg/cmd"
	"istio.io/istio/cni/pkg/constants"
	"istio.io/istio/cni/pkg/util"
	"istio.io/istio/pkg/file"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/pkg/log"
)

const (
	cniConfSubDir    = "/testdata/pre/"
	k8sSvcAcctSubDir = "/testdata/k8s_svcacct/"

	defaultFileMode = 0o644

	cniConfName          = "CNI_CONF_NAME"
	chainedCNIPluginName = "CHAINED_CNI_PLUGIN"
	cniNetworkConfigName = "CNI_NETWORK_CONFIG"
	cniNetworkConfig     = `{
  "cniVersion": "0.3.1",
  "type": "istio-cni",
  "log_level": "info",
  "kubernetes": {
      "kubeconfig": "__KUBECONFIG_FILEPATH__",
      "cni_bin_dir": "/opt/cni/bin",
      "exclude_namespaces": [ "istio-system" ]
  }
}
`
)

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func setEnv(key, value string, t *testing.T) {
	t.Helper()
	err := os.Setenv(key, value)
	if err != nil {
		t.Fatalf("Couldn't set environment variable, err: %v", err)
	}
}

func mktemp(dir, prefix string, t *testing.T) string {
	t.Helper()
	tempDir, err := os.MkdirTemp(dir, prefix)
	if err != nil {
		t.Fatalf("Couldn't get current working directory, err: %v", err)
	}
	t.Logf("Created temporary dir: %v", tempDir)
	return tempDir
}

func ls(dir string, t *testing.T) []string {
	files, err := os.ReadDir(dir)
	t.Helper()
	if err != nil {
		t.Fatalf("Failed to list files, err: %v", err)
	}
	fileNames := make([]string, len(files))
	for i, f := range files {
		fileNames[i] = f.Name()
	}
	return fileNames
}

func cp(src, dest string, t *testing.T) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("Failed to read file %v, err: %v", src, err)
	}
	if err = os.WriteFile(dest, data, os.FileMode(defaultFileMode)); err != nil {
		t.Fatalf("Failed to write file %v, err: %v", dest, err)
	}
}

func rmDir(dir string, t *testing.T) {
	t.Helper()
	err := os.RemoveAll(dir)
	if err != nil {
		t.Fatalf("Failed to remove dir %v, err: %v", dir, err)
	}
}

// Removes Istio CNI's config from the CNI config file
func rmCNIConfig(cniConfigFilepath string, t *testing.T) {
	t.Helper()

	// Read JSON from CNI config file
	cniConfigMap, err := util.ReadCNIConfigMap(cniConfigFilepath)
	if err != nil {
		t.Fatal(err)
	}

	// Find Istio CNI and remove from plugin list
	plugins, err := util.GetPlugins(cniConfigMap)
	if err != nil {
		t.Fatal(err)
	}
	for i, rawPlugin := range plugins {
		plugin, err := util.GetPlugin(rawPlugin)
		if err != nil {
			t.Fatal(err)
		}
		if plugin["type"] == "istio-cni" {
			cniConfigMap["plugins"] = append(plugins[:i], plugins[i+1:]...)
			break
		}
	}

	cniConfig, err := util.MarshalCNIConfig(cniConfigMap)
	if err != nil {
		t.Fatal(err)
	}

	if err = file.AtomicWrite(cniConfigFilepath, cniConfig, os.FileMode(0o644)); err != nil {
		t.Fatal(err)
	}
}

// populateTempDirs populates temporary test directories with golden files and
// other related configuration.
func populateTempDirs(wd string, cniDirOrderedFiles []string, tempCNIConfDir, tempK8sSvcAcctDir string, t *testing.T) {
	t.Helper()
	t.Logf("Pre-populating working dirs")
	for i, f := range cniDirOrderedFiles {
		destFilenm := fmt.Sprintf("0%d-%s", i, f)
		t.Logf("Copying %v into temp config dir %v/%s", f, tempCNIConfDir, destFilenm)
		cp(wd+cniConfSubDir+f, tempCNIConfDir+"/"+destFilenm, t)
	}
	for _, f := range ls(wd+k8sSvcAcctSubDir, t) {
		t.Logf("Copying %v into temp k8s serviceaccount dir %v", f, tempK8sSvcAcctDir)
		cp(wd+k8sSvcAcctSubDir+f, tempK8sSvcAcctDir+"/"+f, t)
	}
	t.Logf("Finished pre-populating working dirs")
}

// startDocker starts a test Docker container and runs the install-cni script.
func runInstall(ctx context.Context, tempCNIConfDir, tempCNIBinDir,
	tempK8sSvcAcctDir, cniConfFileName string, chainedCNIPlugin bool,
) {
	root := cmd.GetCommand()
	constants.ServiceAccountPath = tempK8sSvcAcctDir
	constants.HostCNIBinDir = tempCNIBinDir
	constants.CNIBinDir = filepath.Join(env.IstioSrc, "cni/test/testdata/bindir")
	root.SetArgs([]string{
		"--mounted-cni-net-dir", tempCNIConfDir,
		"--ctrlz_port", "0",
	})
	os.Setenv("KUBERNETES_SERVICE_PORT", "443")
	os.Setenv("KUBERNETES_SERVICE_HOST", "10.110.0.1")
	if cniConfFileName != "" {
		os.Setenv(cniConfName, cniConfFileName)
	} else {
		os.Unsetenv(cniConfName)
	}
	if !chainedCNIPlugin {
		os.Setenv(chainedCNIPluginName, "false")
	} else {
		os.Unsetenv(chainedCNIPluginName)
	}
	if err := root.ExecuteContext(ctx); err != nil {
		log.Errorf("error during install-cni execution")
	}
}

// checkResult checks if resultFile is equal to expectedFile at each tick until timeout
func checkResult(result, expected string) error {
	resultFile, err := os.ReadFile(result)
	if err != nil {
		return fmt.Errorf("couldn't read result: %v", err)
	}
	expectedFile, err := os.ReadFile(expected)
	if err != nil {
		return fmt.Errorf("couldn't read expected: %v", err)
	}
	if !bytes.Equal(resultFile, expectedFile) {
		return fmt.Errorf("expected != result. Diff: %v", cmp.Diff(string(expectedFile), string(resultFile)))
	}
	return nil
}

// compareConfResult does a string compare of 2 test files.
func compareConfResult(result, expected string, t *testing.T) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		return checkResult(result, expected)
	}, retry.Delay(time.Millisecond*10), retry.Timeout(time.Second*3))
}

// checkBinDir verifies the presence/absence of test files.
func checkBinDir(t *testing.T, tempCNIBinDir, op string, files ...string) {
	t.Helper()
	for _, f := range files {
		if _, err := os.Stat(tempCNIBinDir + "/" + f); !os.IsNotExist(err) {
			if op == "add" {
				t.Logf("PASS: File %v was added to %v", f, tempCNIBinDir)
			} else if op == "del" {
				t.Fatalf("FAIL: File %v was not removed from %v", f, tempCNIBinDir)
			}
		} else {
			if op == "add" {
				t.Fatalf("FAIL: File %v was not added to %v", f, tempCNIBinDir)
			} else if op == "del" {
				t.Logf("PASS: File %v was removed from %v", f, tempCNIBinDir)
			}
		}
	}
}

// checkTempFilesCleaned verifies that all temporary files have been cleaned up
func checkTempFilesCleaned(tempCNIConfDir string, t *testing.T) {
	t.Helper()
	files, err := os.ReadDir(tempCNIConfDir)
	if err != nil {
		t.Fatalf("Failed to list files, err: %v", err)
	}
	for _, f := range files {
		if strings.Contains(f.Name(), ".tmp") {
			t.Fatalf("FAIL: Temporary file not cleaned in %v: %v", tempCNIConfDir, f.Name())
		}
	}
	t.Logf("PASS: All temporary files removed from %v", tempCNIConfDir)
}

// doTest sets up necessary environment variables, runs the Docker installation
// container and verifies output file correctness.
func doTest(t *testing.T, chainedCNIPlugin bool, wd, preConfFile, resultFileName, delayedConfFile, expectedOutputFile,
	expectedPostCleanFile, tempCNIConfDir, tempCNIBinDir, tempK8sSvcAcctDir string,
) {
	t.Logf("prior cni-conf='%v', expected result='%v'", preConfFile, resultFileName)

	// Don't set the CNI conf file env var if preConfFile is not set
	var envPreconf string
	if preConfFile != "" {
		envPreconf = preConfFile
	} else {
		preConfFile = resultFileName
	}
	setEnv(cniNetworkConfigName, cniNetworkConfig, t)

	// disable monitoring & uds logging
	viper.Set(constants.MonitoringPort, 0)
	viper.Set(constants.LogUDSAddress, "")

	ctx, cancel := context.WithCancel(context.Background())
	wg := sync.WaitGroup{}
	wg.Add(1)
	defer func() {
		cancel()
		wg.Wait()
	}()
	go func() {
		runInstall(ctx, tempCNIConfDir, tempCNIBinDir, tempK8sSvcAcctDir, envPreconf, chainedCNIPlugin)
		wg.Done()
	}()

	resultFile := tempCNIConfDir + "/" + resultFileName
	if chainedCNIPlugin && delayedConfFile != "" {
		retry.UntilSuccessOrFail(t, func() error {
			if err := checkResult(resultFile, expectedOutputFile); err == nil {
				// We should have waited for the delayed conf
				return fmt.Errorf("did not wait for valid config file")
			}
			return nil
		}, retry.Delay(time.Millisecond), retry.Timeout(time.Millisecond*250))
		var destFilenm string
		if preConfFile != "" {
			destFilenm = preConfFile
		} else {
			destFilenm = delayedConfFile
		}
		cp(delayedConfFile, tempCNIConfDir+"/"+destFilenm, t)
	}

	compareConfResult(resultFile, expectedOutputFile, t)
	checkBinDir(t, tempCNIBinDir, "add", "istio-cni")

	// Test script restart by removing configuration
	if chainedCNIPlugin {
		rmCNIConfig(resultFile, t)
	} else if err := os.Remove(resultFile); err != nil {
		t.Fatalf("error removing CNI config file: %s", resultFile)
	}
	// Verify configuration is still valid after removal
	compareConfResult(resultFile, expectedOutputFile, t)
	t.Log("PASS: Istio CNI configuration still valid after removal")

	// Shutdown the install-cni
	cancel()
	wg.Wait()

	t.Logf("Check the cleanup worked")
	if chainedCNIPlugin {
		if len(expectedPostCleanFile) == 0 {
			compareConfResult(resultFile, wd+cniConfSubDir+preConfFile, t)
		} else {
			compareConfResult(resultFile, expectedPostCleanFile, t)
		}
	} else {
		if file.Exists(resultFile) {
			t.Logf("FAIL: Istio CNI config file was not removed: %s", resultFile)
		}
	}
	checkBinDir(t, tempCNIBinDir, "del", "istio-cni")
	checkTempFilesCleaned(tempCNIConfDir, t)
}

// RunInstallCNITest sets up temporary directories and runs the test.
//
// Doing a go test install_cni.go by itself will not execute the test as the
// file doesn't have a _test.go suffix, and this func doesn't start with a Test
// prefix. This func is only meant to be invoked programmatically. A separate
// install_cni_test.go file exists for executing this test.
func RunInstallCNITest(t *testing.T, chainedCNIPlugin bool, preConfFile, resultFileName, delayedConfFile, expectedOutputFile,
	expectedPostCleanFile string, cniConfDirOrderedFiles []string,
) {
	wd := env.IstioSrc + "/cni/test"
	testWorkRootDir := getEnv("TEST_WORK_ROOTDIR", "/tmp")

	tempCNIConfDir := mktemp(testWorkRootDir, "cni-conf-", t)
	defer rmDir(tempCNIConfDir, t)
	tempCNIBinDir := mktemp(testWorkRootDir, "cni-bin-", t)
	defer rmDir(tempCNIBinDir, t)
	tempK8sSvcAcctDir := mktemp(testWorkRootDir, "kube-svcacct-", t)
	defer rmDir(tempK8sSvcAcctDir, t)

	populateTempDirs(wd, cniConfDirOrderedFiles, tempCNIConfDir, tempK8sSvcAcctDir, t)
	doTest(t, chainedCNIPlugin, wd, preConfFile, resultFileName, delayedConfFile, expectedOutputFile,
		expectedPostCleanFile, tempCNIConfDir, tempCNIBinDir, tempK8sSvcAcctDir)
}
