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
	"strings"
	"testing"
	"time"
)

const (
	cniConfSubDir    = "/data/pre/"
	k8sSvcAcctSubDir = "/data/k8s_svcacct/"

	cniConfName          = "CNI_CONF_NAME"
	cniNetworkConfigName = "CNI_NETWORK_CONFIG"
	cniNetworkConfig     = `{
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

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func setEnv(key, value string, t *testing.T) {
	err := os.Setenv(key, value)
	if err != nil {
		t.Fatalf("Couldn't set environment variable, err: %v", err)
	}
}

func mktemp(dir, prefix string, t *testing.T) string {
	tempDir, err := ioutil.TempDir(dir, prefix)
	if err != nil {
		t.Fatalf("Couldn't get current working directory, err: %v", err)
	}
	t.Logf("Created temporary dir: %v", tempDir)
	return tempDir
}

func pwd(t *testing.T) string {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Couldn't get current working directory, err: %v", err)
	}
	// TODO: ensure that test artifacts are placed at an accessible location
	return wd + "/../deployments/kubernetes/install/test/"
}

func ls(dir string, t *testing.T) []string {
	files, err := ioutil.ReadDir(dir)
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
	data, err := ioutil.ReadFile(src)
	if err != nil {
		t.Fatalf("Failed to read file %v, err: %v", src, err)
	}
	if err = ioutil.WriteFile(dest, data, 0644); err != nil {
		t.Fatalf("Failed to write file %v, err: %v", dest, err)
	}
}

func rm(dir string, t *testing.T) {
	err := os.RemoveAll(dir)
	if err != nil {
		t.Fatalf("Failed to remove dir %v, err: %v", dir, err)
	}
}

// populateTempDirs populates temporary test directories with golden files and
// other related configuration.
func populateTempDirs(wd string, cniDirOrderedFiles []string, tempCNIConfDir, tempK8sSvcAcctDir string, t *testing.T) {
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

// startDocker starts a test Docker container and runs the install-cni.sh script.
func startDocker(testNum int, wd, tempCNIConfDir, tempCNIBinDir,
	tempK8sSvcAcctDir, cniConfFileName string, t *testing.T) string {

	dockerImage := env("HUB", "") + "/install-cni:" + env("TAG", "")
	errFileName := path.Dir(tempCNIConfDir) + "/docker_run_stderr"

	// Build arguments list by picking whatever is necessary from the environment.
	args := []string{"run", "-d",
		"--name", "test-istio-cni-install",
		"-v", env("PWD", "") + ":/usr/src/project-config",
		"-v", tempCNIConfDir + ":/host/etc/cni/net.d",
		"-v", tempCNIBinDir + ":/host/opt/cni/bin",
		"-v", tempK8sSvcAcctDir + ":/var/run/secrets/kubernetes.io/serviceaccount",
		"--env-file", wd + "/data/env_vars.sh",
		"-e", cniNetworkConfigName,
	}
	if cniConfFileName != "" {
		args = append(args, "-e", cniConfName+"="+cniConfFileName)
	}
	args = append(args, dockerImage, "/install-cni.sh")

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
	t.Logf("Container ID: %s", containerID)
	return strings.Trim(string(containerID), "\n")
}

// docker runs the given docker command on the given container ID.
func docker(cmd, containerID string, t *testing.T) {
	out, err := exec.Command("docker", cmd, containerID).CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to execute 'docker %s %s', err: %v", cmd, containerID, err)
	}
	t.Logf("docker %s %s - out:\n%s", cmd, containerID, out)
}

// compareConfResult does a string compare of 2 test files.
func compareConfResult(testWorkRootDir, tempCNIConfDir, result, expected string, t *testing.T) {
	tempResult := tempCNIConfDir + "/" + result
	resultFile, err := ioutil.ReadFile(tempResult)
	if err != nil {
		t.Fatalf("Failed to read file %v, err: %v", tempResult, err)
	}

	expectedFile, err := ioutil.ReadFile(expected)
	if err != nil {
		t.Fatalf("Failed to read file %v, err: %v", expected, err)
	}

	if bytes.Equal(resultFile, expectedFile) {
		t.Logf("PASS: result matches expected: %v v. %v", tempResult, expected)
	} else {
		tempFail := mktemp(testWorkRootDir, result+".fail.XXXX", t)
		t.Errorf("FAIL: result doesn't match expected: %v v. %v", tempResult, expected)
		cp(tempResult, tempFail+"/"+"failResult", t)
		cmd := exec.Command("diff", tempResult, expected)
		diffOutput, derr := cmd.Output()
		if derr != nil {
			t.Logf("Diff output:\n %s", diffOutput)
		}
		t.Fatalf("Check %v for diff contents", tempFail)
	}
}

// checkBinDir verifies the presence/absence of test files.
func checkBinDir(t *testing.T, tempCNIBinDir, op string, files ...string) {
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

// doTest sets up necessary environment variables, runs the Docker installation
// container and verifies output file correctness.
func doTest(testNum int, wd, preConfFile, resultFileName, expectedOutputFile,
	expectedPostCleanFile, tempCNIConfDir, tempCNIBinDir, tempK8sSvcAcctDir,
	testWorkRootDir string, t *testing.T) {

	t.Logf("Test %v: prior cni-conf='%v', expected result='%v'", testNum, preConfFile, resultFileName)

	// Don't set the CNI conf file env var if preConfFile is not set
	var envPreconf string
	if preConfFile != "NONE" {
		envPreconf = preConfFile
	} else {
		preConfFile = resultFileName
	}
	setEnv(cniNetworkConfigName, cniNetworkConfig, t)

	containerID := startDocker(testNum, wd, tempCNIConfDir, tempCNIBinDir, tempK8sSvcAcctDir, envPreconf, t)
	defer func() {
		docker("stop", containerID, t)
		docker("logs", containerID, t)
		docker("rm", containerID, t)
	}()
	time.Sleep(10 * time.Second)

	compareConfResult(testWorkRootDir, tempCNIConfDir, resultFileName, expectedOutputFile, t)
	checkBinDir(t, tempCNIBinDir, "add", "istio-cni", "istio-iptables.sh")

	docker("stop", containerID, t)
	time.Sleep(10 * time.Second)

	t.Logf("Test %v: Check the cleanup worked", testNum)
	if len(expectedPostCleanFile) == 0 {
		compareConfResult(testWorkRootDir, tempCNIConfDir, resultFileName, wd+cniConfSubDir+preConfFile, t)
	} else {
		compareConfResult(testWorkRootDir, tempCNIConfDir, resultFileName, expectedPostCleanFile, t)
	}
	checkBinDir(t, tempCNIBinDir, "del", "istio-cni", "istio-iptables.sh")
}

// RunInstallCNITest sets up temporary directories and runs the test.
//
// Doing a go test install_cni.go by itself will not execute the test as the
// file doesn't have a _test.go suffix, and this func doesn't start with a Test
// prefix. This func is only meant to be invoked programmatically. A separate
// install_cni_test.go file exists for executing this test.
func RunInstallCNITest(testNum int, preConfFile, resultFileName, expectedOutputFile,
	expectedPostCleanFile string, cniConfDirOrderedFiles []string, t *testing.T) {

	wd := pwd(t)
	testWorkRootDir := env("TEST_WORK_ROOTDIR", "/tmp")

	tempCNIConfDir := mktemp(testWorkRootDir, "cni-confXXXXX", t)
	defer rm(tempCNIConfDir, t)
	tempCNIBinDir := mktemp(testWorkRootDir, "cni-binXXXXX", t)
	defer rm(tempCNIBinDir, t)
	tempK8sSvcAcctDir := mktemp(testWorkRootDir, "kube-svcacctXXXXX", t)
	defer rm(tempK8sSvcAcctDir, t)

	t.Logf("conf-dir=%v; bin-dir=%v; k8s-serviceaccount=%v", tempCNIConfDir,
		tempCNIBinDir, tempK8sSvcAcctDir)

	populateTempDirs(wd, cniConfDirOrderedFiles, tempCNIConfDir, tempK8sSvcAcctDir, t)
	doTest(testNum, wd, preConfFile, resultFileName, expectedOutputFile,
		expectedPostCleanFile, tempCNIConfDir, tempCNIBinDir, tempK8sSvcAcctDir,
		testWorkRootDir, t)
}
