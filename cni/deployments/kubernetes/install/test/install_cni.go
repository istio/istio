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

package test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coreos/etcd/pkg/fileutil"

	"istio.io/istio/cni/pkg/install-cni/pkg/util"
	"istio.io/istio/pkg/test/env"
)

const (
	cniConfSubDir    = "/data/pre/"
	k8sSvcAcctSubDir = "/data/k8s_svcacct/"

	defaultFileMode = 0644

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
	tempDir, err := ioutil.TempDir(dir, prefix)
	if err != nil {
		t.Fatalf("Couldn't get current working directory, err: %v", err)
	}
	t.Logf("Created temporary dir: %v", tempDir)
	return tempDir
}

func ls(dir string, t *testing.T) []string {
	files, err := ioutil.ReadDir(dir)
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
	data, err := ioutil.ReadFile(src)
	if err != nil {
		t.Fatalf("Failed to read file %v, err: %v", src, err)
	}
	if err = ioutil.WriteFile(dest, data, os.FileMode(defaultFileMode)); err != nil {
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

	if err = util.AtomicWrite(cniConfigFilepath, cniConfig, os.FileMode(0644)); err != nil {
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
func startDocker(testNum int, wd, tempCNIConfDir, tempCNIBinDir,
	tempK8sSvcAcctDir, cniConfFileName string, chainedCNIPlugin bool, t *testing.T) string {
	t.Helper()

	dockerImage := getEnv("HUB", "") + "/install-cni:" + getEnv("TAG", "")
	errFileName := path.Dir(tempCNIConfDir) + "/docker_run_stderr"

	// Build arguments list by picking whatever is necessary from the environment.
	args := []string{"run", "-d",
		"--name", "test-istio-cni-install",
		"-v", getEnv("PWD", "") + ":/usr/src/project-config",
		"-v", tempCNIConfDir + ":/host/etc/cni/net.d",
		"-v", tempCNIBinDir + ":/host/opt/cni/bin",
		"-v", tempK8sSvcAcctDir + ":/var/run/secrets/kubernetes.io/serviceaccount",
		"--env-file", wd + "/data/env_vars.sh",
		"-e", cniNetworkConfigName,
	}
	if cniConfFileName != "" {
		args = append(args, "-e", cniConfName+"="+cniConfFileName)
	}
	if !chainedCNIPlugin {
		args = append(args, "-e", chainedCNIPluginName+"=false")
	}
	args = append(args, dockerImage, "/usr/local/bin/install-cni")

	// Create a temporary log file to write docker command error log.
	errFile, err := os.Create(errFileName)
	if err != nil {
		t.Fatalf("Couldn't create docker stderr file, err: %v", err)
	}
	defer func() {
		errClose := errFile.Close()
		if errClose != nil {
			t.Fatalf("Couldn't create docker stderr file, err: %v", errClose)
		}
	}()

	// Run the docker command and write errors to a temporary file.
	cmd := exec.Command("docker", args...)
	cmd.Stderr = errFile

	containerID, err := cmd.Output()
	if err != nil {
		t.Fatalf("Test %v ERROR: failed to start docker container '%v', see %v",
			testNum, dockerImage, errFileName)
	}
	t.Logf("Test %v: Container ID: %s", testNum, containerID)
	return strings.Trim(string(containerID), "\n")
}

// docker runs the given docker command on the given container ID.
func docker(cmd, containerID string, t *testing.T) {
	t.Helper()
	out, err := exec.Command("docker", cmd, containerID).CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to execute 'docker %s %s', err: %v", cmd, containerID, err)
	}
	t.Logf("docker %s %s - out:\n%s", cmd, containerID, out)
}

// checkResult checks if resultFile is equal to expectedFile at each tick until timeout
func checkResult(result, expected string, timeout, tick <-chan time.Time, t *testing.T) bool {
	t.Helper()
	for {
		select {
		case <-timeout:
			return false
		case <-tick:
			resultFile, err := ioutil.ReadFile(result)
			if err != nil {
				break
			}
			expectedFile, err := ioutil.ReadFile(expected)
			if err != nil {
				break
			}
			if bytes.Equal(resultFile, expectedFile) {
				return true
			}
		}
	}
}

// compareConfResult does a string compare of 2 test files.
func compareConfResult(testWorkRootDir, result, expected string, t *testing.T) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	if checkResult(result, expected, timeout, ticker.C, t) {
		t.Logf("PASS: result matches expected: %v v. %v", result, expected)
	} else {
		// Log errors
		_, err := ioutil.ReadFile(result)
		if err != nil {
			t.Fatalf("Failed to read file %v, err: %v", result, err)
		}
		_, err = ioutil.ReadFile(expected)
		if err != nil {
			t.Fatalf("Failed to read file %v, err: %v", expected, err)
		}
		tempFail := mktemp(testWorkRootDir, filepath.Base(result)+"-fail-", t)
		t.Errorf("FAIL: result doesn't match expected: %v v. %v", result, expected)
		cp(result, tempFail+"/"+"failResult", t)
		cmd := exec.Command("diff", result, expected)
		diffOutput, derr := cmd.Output()
		if derr != nil {
			t.Logf("Diff output:\n %s", diffOutput)
		}
		t.Fatalf("Check %v for diff contents", tempFail)
	}
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
	files, err := ioutil.ReadDir(tempCNIConfDir)
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
func doTest(testNum int, chainedCNIPlugin bool, wd, preConfFile, resultFileName, delayedConfFile, expectedOutputFile,
	expectedPostCleanFile, tempCNIConfDir, tempCNIBinDir, tempK8sSvcAcctDir,
	testWorkRootDir string, t *testing.T) {

	t.Logf("Test %v: prior cni-conf='%v', expected result='%v'", testNum, preConfFile, resultFileName)

	// Don't set the CNI conf file env var if preConfFile is not set
	var envPreconf string
	if preConfFile != "" {
		envPreconf = preConfFile
	} else {
		preConfFile = resultFileName
	}
	setEnv(cniNetworkConfigName, cniNetworkConfig, t)

	containerID := startDocker(testNum, wd, tempCNIConfDir, tempCNIBinDir, tempK8sSvcAcctDir, envPreconf, chainedCNIPlugin, t)
	defer func() {
		docker("stop", containerID, t)
		docker("logs", containerID, t)
		docker("rm", containerID, t)
	}()

	resultFile := tempCNIConfDir + "/" + resultFileName
	if chainedCNIPlugin && delayedConfFile != "" {
		timeout := time.After(5 * time.Second)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		if checkResult(resultFile, expectedOutputFile, timeout, ticker.C, t) {
			t.Fatalf("FAIL: Istio CNI did not wait for valid config file")
		}
		var destFilenm string
		if preConfFile != "" {
			destFilenm = preConfFile
		} else {
			destFilenm = delayedConfFile
		}
		cp(delayedConfFile, tempCNIConfDir+"/"+destFilenm, t)
	}

	compareConfResult(testWorkRootDir, resultFile, expectedOutputFile, t)
	checkBinDir(t, tempCNIBinDir, "add", "istio-cni", "istio-iptables")

	// Test script restart by removing configuration
	if chainedCNIPlugin {
		rmCNIConfig(resultFile, t)
	} else if err := os.Remove(resultFile); err != nil {
		t.Fatalf("error removing CNI config file: %s", resultFile)
	}
	// Verify configuration is still valid after removal
	compareConfResult(testWorkRootDir, resultFile, expectedOutputFile, t)
	t.Log("PASS: Istio CNI configuration still valid after removal")

	docker("stop", containerID, t)

	t.Logf("Test %v: Check the cleanup worked", testNum)
	if chainedCNIPlugin {
		if len(expectedPostCleanFile) == 0 {
			compareConfResult(testWorkRootDir, resultFile, wd+cniConfSubDir+preConfFile, t)
		} else {
			compareConfResult(testWorkRootDir, resultFile, expectedPostCleanFile, t)
		}
	} else {
		if fileutil.Exist(resultFile) {
			t.Logf("FAIL: Istio CNI config file was not removed: %s", resultFile)
		}
	}
	checkBinDir(t, tempCNIBinDir, "del", "istio-cni", "istio-iptables")
	checkTempFilesCleaned(tempCNIConfDir, t)
}

// RunInstallCNITest sets up temporary directories and runs the test.
//
// Doing a go test install_cni.go by itself will not execute the test as the
// file doesn't have a _test.go suffix, and this func doesn't start with a Test
// prefix. This func is only meant to be invoked programmatically. A separate
// install_cni_test.go file exists for executing this test.
func RunInstallCNITest(testNum int, chainedCNIPlugin bool, preConfFile, resultFileName, delayedConfFile, expectedOutputFile,
	expectedPostCleanFile string, cniConfDirOrderedFiles []string, t *testing.T) {

	wd := env.IstioSrc + "/cni/deployments/kubernetes/install/test"
	testWorkRootDir := getEnv("TEST_WORK_ROOTDIR", "/tmp")

	tempCNIConfDir := mktemp(testWorkRootDir, "cni-conf-", t)
	defer rmDir(tempCNIConfDir, t)
	tempCNIBinDir := mktemp(testWorkRootDir, "cni-bin-", t)
	defer rmDir(tempCNIBinDir, t)
	tempK8sSvcAcctDir := mktemp(testWorkRootDir, "kube-svcacct-", t)
	defer rmDir(tempK8sSvcAcctDir, t)

	t.Logf("conf-dir=%v; bin-dir=%v; k8s-serviceaccount=%v", tempCNIConfDir,
		tempCNIBinDir, tempK8sSvcAcctDir)

	populateTempDirs(wd, cniConfDirOrderedFiles, tempCNIConfDir, tempK8sSvcAcctDir, t)
	doTest(testNum, chainedCNIPlugin, wd, preConfFile, resultFileName, delayedConfFile, expectedOutputFile,
		expectedPostCleanFile, tempCNIConfDir, tempCNIBinDir, tempK8sSvcAcctDir,
		testWorkRootDir, t)
}
