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

package driver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"testing"

	fv "istio.io/istio/security/pkg/flexvolume"

)

var (
	// The original standard out for printouts
	oldOut *os.File
	// The standard out used for tests.
	pipeOut *os.File
	// The channel for sending stdout back to test code.
	outC chan string
)

// Will block waiting for data on the channel.
func readStdOut() string {
	pipeOut.Close() //nolint: errcheck
	os.Stdout = oldOut
	fmt.Println("Waiting for output")
	return <-outC
}

// Redirect stdout to a pipe
func testInitStdIo(t *testing.T) {
	var r *os.File
	oldOut = os.Stdout
	r, pipeOut, _ = os.Pipe()
	os.Stdout = pipeOut

	outC = make(chan string)
	go func() {
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, r); err != nil {
			t.Errorf("failed to copy with error %v", err)
		}
		outC <- buf.String()
	}()
}

func cmpStdOutput(expected, actual interface{}) error {
	output := readStdOut()
	err := json.Unmarshal([]byte(output), actual)
	if err != nil {
		return fmt.Errorf("for output %s error (%s)", output, err.Error())
	}

	if !reflect.DeepEqual(expected, actual) {
		return fmt.Errorf("actual: %#v, expected: %#v", actual, expected)
	}

	return nil
}

func TestInitCommandVer1_8(t *testing.T) {
	var gotResp InitResponse
	expResp := InitResponse{Status: "Success",
		Message:      "Init ok.",
		Capabilities: &Capabilities{Attach: false}}


	testInitStdIo(t)
	if err := Init("1.8"); err != nil {
		t.Errorf("Failed to init. (%s)", err.Error())
	}

	if err := cmpStdOutput(&expectedResp, &resp); err != nil {
		t.Errorf("Failed to init. (%s)", err.Error())
	}
}

func TestInitDefault(t *testing.T) {
	var resp Resp
	expectedResp := Resp{Status: "Success", Message: "Init ok."}

	testInitStdIo()
	if err := GenericUnsupported("", "", "foo"); err != nil {
		t.Errorf("Failed (%s)", err.Error())
	}

	if err := cmpStdOutput(&expResp, &gotResp); err != nil {
		t.Errorf("Failed %s", err.Error())
	}
}

func getMountInputs(opts *FlexVolumeInputs) (string, error) {
	var opBytes []byte
	opBytes, err := json.Marshal(&opts)
	if err != nil {
		return "", err
	}
	return string(opBytes), nil
}

func TestMountBasic(t *testing.T) {
	var err error
	var mountInputs string
	opts := FlexVolumeInputs{
		UID:            "1111-1111-1111",
		Name:           "foo",
		Namespace:      "default",
		ServiceAccount: "sa",
	}

	mountInputs, err = getMountInputs(&opts)
	if err != nil {
		t.Errorf("Failed %s", err.Error())
	}

	destinationDir := filepath.Join(testDir, "testMounts", "dir")
	expectedBindToDir := filepath.Join(destinationDir, "nodeagent")
	expectedBindFromDir := filepath.Join(configuration.NodeAgentWorkloadHomeDir, opts.UID)

	// Setup the environment variables.
	envMountDir := strings.Join([]string{"mountDir=", destinationDir}, "")
	envBindFromDir := strings.Join([]string{"BIND_FROM_DIR=", expectedBindFromDir}, "")
	envBindToDir := strings.Join([]string{"BIND_TO_DIR=", expectedBindToDir}, "")
	envExec = []string{envMountDir, envBindFromDir, envBindToDir}
	defer func() { envExec = []string{} }()

	testInitStdIo()
	if err = Mount(destinationDir, mountInputs); err != nil {
		t.Errorf("Failed %s", err.Error())
	}

	checkDirs := []string{expectedBindToDir, expectedBindFromDir}
	for _, dir := range checkDirs {
		if _, err = os.Stat(dir); err != nil {
			t.Errorf("Failed directory %s not created", dir)
		}
		defer os.RemoveAll(dir)
	}

	// Check if credential file created & has the correct content.
	credsFile := filepath.Join(configuration.NodeAgentCredentialsHomeDir, opts.UID+fv.CredentialFileExtension)
	if _, err = os.Stat(credsFile); err != nil {
		t.Errorf("Credentail file %s not created", credsFile)
	}

	var credBytes []byte
	credBytes, err = ioutil.ReadFile(credsFile)
	if err != nil {
		t.Errorf("Failed to read credentials file %s", credsFile)
	}

	var wlCred fv.Credential
	err = json.Unmarshal(credBytes, &wlCred)
	if err != nil {
		t.Errorf("Failed to read credentials from %s into attributes", credsFile)
	}

	gotAttrs := FlexVolumeInputs{
		UID:            wlCred.UID,
		Name:           wlCred.Workload,
		Namespace:      wlCred.Namespace,
		ServiceAccount: wlCred.ServiceAccount,
	}
	if !reflect.DeepEqual(&opts, &gotAttrs) {
		t.Errorf("got: %+v, expected: %+v", gotAttrs, opts)
	}

	if err := cmpStdOutput(&expectedResp, &resp); err != nil {
		t.Errorf("Failed to init. (%s)", err.Error())
	}
}

func TestMount(t *testing.T) {
	err := Mount("device", "opts")
	if err != nil {
		t.Errorf("Mount function failed.")
	}

	opts := `{"Uid": "myuid", "Nme": "myname", "Namespace": "mynamespace", "ServiceAccount": "myaccount"}`
	err = Mount("/tmp", opts)
	if err != nil {
		t.Errorf("Failed %s", err.Error())
	}

	// setup to a invalid file path
	oldManagementHomeDir := configuration.NodeAgentManagementHomeDir
	configuration.NodeAgentManagementHomeDir = filepath.Join(testDir, "fail")
	defer func() { configuration.NodeAgentManagementHomeDir = oldManagementHomeDir }()

	destDir := filepath.Join(testDir, "foo/bar")
	testInitStdIo()
	err = Mount(destDir, mountInputs)
	if err == nil {
		t.Errorf("Failed. Expected error and mount command to fail")
	}

	// check all the removals
	checkWorkloadDir := filepath.Join(configuration.NodeAgentWorkloadHomeDir, opts.UID)
	checkPaths := []string{destDir, checkWorkloadDir}
	for _, path := range checkPaths {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("Mount failed but path %s still there", path)
		}
	}

	errPath := filepath.Join(testDir, "fail", opts.UID+fv.CredentialFileExtension)
	var gotResp Response
	expResp := getFailure("", "", fmt.Sprintf("Failure to create credentials: open %s: no such file or directory", errPath))
	if err := cmpStdOutput(&expResp, &gotResp); err != nil {
		t.Errorf("Failed %s", err.Error())
	}
}

func TestUnmount(t *testing.T) {
	err := Unmount("/tmp")
	if err != nil {
		t.Error(err.Error())
	}

	delDir := filepath.Join(configuration.NodeAgentWorkloadHomeDir, testUID)
	credsDir := configuration.NodeAgentCredentialsHomeDir
	credsFile := filepath.Join(credsDir, testUID+fv.CredentialFileExtension)

	for _, dir := range []string{delDir, credsDir} {
		if err := os.MkdirAll(dir, 0777); err != nil {
			t.Errorf("Failed to create dir %s: %s", dir, err.Error())
		}
	}
	defer os.RemoveAll(credsDir)

	if _, err := os.Create(credsFile); err != nil {
		t.Errorf("Failed to create credential file %s: %s", credsFile, err.Error())
	}

	testInitStdIo()
	if err := Unmount(unmountInputDir); err != nil {
		t.Errorf("Unmount function failed.")
	}

	//Check credential file removed.
	checkRemoved := []string{delDir, credsFile}
	for _, file := range checkRemoved {
		_, err := os.Stat(file)
		if err == nil {
			t.Errorf("Failed %s still there", file)
		}
	}

	var gotResp Response
	expResp := getGenericResp("", "", "Unmount Ok")
	if err := cmpStdOutput(&expResp, &gotResp); err != nil {
		t.Errorf("Failed %s", err.Error())
	}
}

func TestUnmountInvalidDir(t *testing.T) {
	unmountInputDir := filepath.Join(testDir, "1111-1111-1111")
	testInitStdIo()
	err := Unmount(unmountInputDir)
	if err == nil {
		t.Errorf("Failed. Expected error and un-mount command to fail.")
	}

	var gotResp Response
	expResp := getFailure("", "", fmt.Sprintf("Failure to notify nodeagent dir %s", unmountInputDir))
	if err := cmpStdOutput(&expResp, &gotResp); err != nil {
		t.Errorf("Failed %s", err.Error())
	}
}

func TestUnmountCmdCredFailure(t *testing.T) {
	testUID := "1111-1111-1111"
	unmountInputDir, err := getUnmountInputDir(testUID)
	if err != nil {
		t.Error(err.Error())
	}

	delDir := filepath.Join(configuration.NodeAgentWorkloadHomeDir, testUID)
	credsDir := configuration.NodeAgentCredentialsHomeDir
	for _, dir := range []string{delDir, credsDir} {
		if err := os.MkdirAll(dir, 0777); err != nil {
			t.Errorf("Failed to create dir %s: %s", dir, err.Error())
		}
	}
	defer os.RemoveAll(credsDir)

	testInitStdIo()
	if err := Unmount(unmountInputDir); err != nil {
		t.Errorf("Failed. Expected error and un-mount command to NOT fail.")
	}

	//"Failure to delete credentials file: remove /tmp/testFlexvolumeDriver410006374/creds/1111-1111-1111.json
	expectedErrMessage := fmt.Sprintf("Failure to delete credentials file: remove %s: no such file or directory",
		filepath.Join(credsDir, testUID+fv.CredentialFileExtension))
	var gotResp Response
	expResp := getGenericResp("", "", expectedErrMessage)
	if err := cmpStdOutput(&expResp, &gotResp); err != nil {
		t.Errorf("Failed %s", err.Error())
	}
}

func TestInitConfigurationDefault(t *testing.T) {
	InitConfiguration()
	if configuration != &defaultConfiguration {
		t.Errorf("The configuration is not set to default.")
	}
}

func writeConfigFile(fileName string, options *ConfigurationOptions) error {
	var confBytes []byte
	var err error
	confBytes, err = json.Marshal(*options)
	if err != nil {
		return fmt.Errorf("failed to setup the configuration file")
	}

	err = ioutil.WriteFile(fileName, confBytes, 0644)
	if err != nil {
		return fmt.Errorf("failed to write the configuration file")
	}

	return nil
}

func TestInitConfigurationBasic(t *testing.T) {
	configFile = filepath.Join(testDir, "nodeagent.json")
	expectedConfiguration := &ConfigurationOptions{
		K8sVersion:                  "1",
		NodeAgentManagementHomeDir:  "/test",
		NodeAgentWorkloadHomeDir:    "/workload",
		NodeAgentCredentialsHomeDir: "/creds",
		LogLevel:                    "INFO",
	}

	if err := writeConfigFile(configFile, expectedConfiguration); err != nil {
		t.Errorf("%s", err.Error())
	}
	defer os.Remove(configFile)

	oldConfiguration := configuration
	InitConfiguration()
	defer func() { configuration = oldConfiguration }()

	if configuration == oldConfiguration {
		t.Errorf("Configuration not changed to given config")
	}

	mkAbsolutePaths(expectedConfiguration)
	if !reflect.DeepEqual(expectedConfiguration, configuration) {
		t.Errorf("Expected configuration %+v not same as got %+v", *expectedConfiguration, *configuration)
	}
}

func TestInitConfigurationEmptyPaths(t *testing.T) {
	configFile = filepath.Join(testDir, "nodeagent.json")
	inputConfiguration := &ConfigurationOptions{
		K8sVersion: "2",
	}

	if err := writeConfigFile(configFile, inputConfiguration); err != nil {
		t.Errorf("%s", err.Error())
	}
	defer os.Remove(configFile)

	oldConfiguration := configuration
	InitConfiguration()
	defer func() { configuration = oldConfiguration }()

	if configuration.NodeAgentManagementHomeDir != nodeAgentHome ||
		configuration.NodeAgentCredentialsHomeDir != nodeAgentHome+credentialDirHome {
		t.Errorf("Failed to fill up empty configurations (%+v)", configuration)
	}

}

func TestMain(m *testing.M) {
	var err error
	logWriter = nil
	getExecCmd = testGetExecCmd
	testDir, err = ioutil.TempDir("", "testFlexvolumeDriver")
	if err != nil {
		return
	}

	// Setup the configuration to use the testDir
	defaultConfiguration.NodeAgentManagementHomeDir = testDir
	configuration = &defaultConfiguration
	mkAbsolutePaths(configuration)

	r := m.Run()

	os.RemoveAll(testDir) // nolint: errcheck
	os.Exit(r)
}
