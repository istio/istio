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
	"io/ioutil"
	pb "istio.io/istio/security/proto"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var (
	// The original standard out for printouts
	oldOut *os.File
	// The standard out used for tests.
	pipeOut *os.File
	// The channel for sending stdout back to test code.
	outC chan string
	// testDir is where all the artifacts are created.
	testDir string
	// environment vars to attach to exec command
	envExec []string
)

// Mock for the various OS commands used by the driver.
func testGetExecCmd(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperExecProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = append([]string{"GO_WANT_HELPER_PROCESS=1"}, envExec...)
	return cmd
}

// TestHelperExecProcess is used to simulate calls to os.ExecCmd()
// inspired by: https://github.com/golang/go/blob/master/src/os/exec/exec_test.go
func TestHelperExecProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	// We now have the original args.
	if len(args) == 0 {
		fmt.Printf("no commands")
		os.Exit(2)
	}

	cmd, args := args[0], args[1:]
	switch cmd {
	case "/bin/mount":
		if args[0] == "-t" {
			if len(args) < 6 {
				fmt.Printf("Not sufficient args %d", len(args))
				os.Exit(2)
			}

			tmpmount := os.Getenv("MOUNT_DIR")
			if len(tmpmount) > 0 && tmpmount != args[5] {
				fmt.Println("expected %s != got %s", tmpmount, args[5])
				os.Exit(2)
			}
			os.Exit(0)
		}
		//bind mount
		if args[0] == "--bind" {
			if len(args) < 3 {
				fmt.Printf("Not sufficient args to bind mount %d", len(args))
			}

			fromBindDir := os.Getenv("BIND_FROM_DIR")
			if len(fromBindDir) > 0 && fromBindDir != args[1] {
				fmt.Println("expected %s != got %s", fromBindDir, args[1])
				os.Exit(2)
			}
			toBindDir := os.Getenv("BIND_TO_DIR")
			if len(toBindDir) > 0 && toBindDir != args[2] {
				fmt.Println("expected %s != got %s", toBindDir, args[2])
				os.Exit(2)
			}
		}
	case "/bin/unmount":
		if len(args) < 1 {
			fmt.Printf("unmount without a dir")
			os.Exit(2)
		}
	}
	os.Exit(0)
}

// Redirect stdout to a pipe
func testInitStdIo() {
	var r *os.File
	oldOut = os.Stdout
	r, pipeOut, _ = os.Pipe()
	os.Stdout = pipeOut

	outC = make(chan string)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		data := buf.String()
		oldOut.WriteString(data)
		outC <- data
	}()
}

// Will block waiting for data on the channel.
func readStdOut() string {
	pipeOut.Close()
	os.Stdout = oldOut
	fmt.Println("Waiting for output")
	return <-outC
}

func getGenericResp(_, _, msg string) Response {
	return Response{Status: "Success", Message: msg}
}

func getGenericUnsupported(_, _, msg string) Response {
	return Response{Status: "Not supported", Message: msg}
}

func getFailure(_, _, msg string) Response {
	return Response{Status: "Failure", Message: msg}
}

func cmpStdOutput(expected, got interface{}) error {
	output := readStdOut()
	err := json.Unmarshal([]byte(output), got)
	if err != nil {
		return fmt.Errorf("For output %s error (%s)", output, err.Error())
	}

	if !reflect.DeepEqual(expected, got) {
		return fmt.Errorf("got: %#v, expected: %#v", got, expected)
	}

	return nil
}

func TestInitCommandVer1_8(t *testing.T) {
	var gotResp InitResponse
	expResp := InitResponse{Status: "Success",
		Message: "Init ok.",
		Attach:  false}

	testInitStdIo()
	if err := InitCommand(); err != nil {
		t.Errorf("Failed to init. (%s)", err.Error())
	}

	if err := cmpStdOutput(&expResp, &gotResp); err != nil {
		t.Errorf("Failed to init. (%s)", err.Error())
	}
}

func TestInitCommandDefault(t *testing.T) {
	var gotResp Response
	configuration.K8sVersion = "1.9"
	expResp := getGenericResp("", "", "Init ok.")

	testInitStdIo()
	if err := InitCommand(); err != nil {
		t.Errorf("Failed to init.")
	}

	if err := cmpStdOutput(&expResp, &gotResp); err != nil {
		t.Errorf("Failed to init (%s)", err.Error())
	}
}

func TestGenericUnsupported(t *testing.T) {
	var gotResp Response
	expResp := getGenericUnsupported("", "", "foo")

	testInitStdIo()
	if err := GenericUnsupported("", "", "foo"); err != nil {
		t.Errorf("Failed (%s)", err.Error())
	}

	if err := cmpStdOutput(&expResp, &gotResp); err != nil {
		t.Errorf("Failed %s", err.Error())
	}
}

func TestMountBasic(t *testing.T) {
	var err error
	opts := FlexVolumeInputs{
		Uid:            "1111-1111-1111",
		Name:           "foo",
		Namespace:      "default",
		ServiceAccount: "sa",
	}

	var opBytes []byte
	opBytes, err = json.Marshal(&opts)
	if err != nil {
		t.Errorf("Failed %s", err.Error())
	}

	destinationDir := filepath.Join(testDir, "testMounts", "dir")
	expectedBindToDir := filepath.Join(destinationDir, "nodeagent")
	expectedBindFromDir := filepath.Join(configuration.NodeAgentWorkloadHomeDir, opts.Uid)

	// Setup the environment variables.
	envMountDir := strings.Join([]string{"MOUNT_DIR=", destinationDir}, "")
	envBindFromDir := strings.Join([]string{"BIND_FROM_DIR=", expectedBindFromDir}, "")
	envBindToDir := strings.Join([]string{"BIND_TO_DIR=", expectedBindToDir}, "")
	envExec = []string{envMountDir, envBindFromDir, envBindToDir}
	defer func() { envExec = []string{} }()

	testInitStdIo()
	if err := Mount(destinationDir, string(opBytes)); err != nil {
		t.Errorf("Failed %s", err.Error())
	}

	checkDirs := []string{expectedBindToDir, expectedBindFromDir}
	for _, dir := range checkDirs {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("Failed directory %s not created", dir)
		}
		defer os.RemoveAll(dir)
	}

	// Check if credential file created & has the correct content.
	credsFile := filepath.Join(configuration.NodeAgentCredentialsHomeDir, opts.Uid+".json")
	if _, err := os.Stat(credsFile); err != nil {
		t.Errorf("Credentail file %s not created", credsFile)
	}

	var bytes []byte
	bytes, err = ioutil.ReadFile(credsFile)
	if err != nil {
		t.Errorf("Failed to read credentials file %s", credsFile)
	}

	var wlInfo pb.WorkloadInfo
	err = json.Unmarshal(bytes, &wlInfo.Attrs)
	if err != nil {
		t.Errorf("Failed to read credentials from %s into attributes", credsFile)
	}

	gotAttrs := FlexVolumeInputs{
		Uid:            wlInfo.Attrs.Uid,
		Name:           wlInfo.Attrs.Workload,
		Namespace:      wlInfo.Attrs.Namespace,
		ServiceAccount: wlInfo.Attrs.Serviceaccount,
	}
	if !reflect.DeepEqual(&opts, &gotAttrs) {
		t.Errorf("got: %+v, expected: %+v", gotAttrs, opts)
	}

	// Check the response output
	var gotResp Response
	expResp := getGenericResp("", "", "Mount ok.")
	if err := cmpStdOutput(&expResp, &gotResp); err != nil {
		t.Errorf("Failed %s", err.Error())
	}
}

func TestUnmount(t *testing.T) {

	testUid := "1111-1111-1111"

	// Find out how many prefix to add s.t the testUid is correctly placed.
	prefixCount := 5
	prefixPath := strings.Split(testDir, "/")
	if len(prefixPath) > 5 {
		t.Errorf("Cannot create the correct test dir path temp dir prefix %d too long.", len(prefixPath))
	}

	var prefix string
	for i := 0; i < prefixCount-len(prefixPath); i++ {
		prefix = filepath.Join(prefix, fmt.Sprintf("test%d", i))
	}

	// /tmp/testFlexvolumeDriver686209052/test0/test1/1111-1111-1111/volumes/nodeagent~uds/test-volume/nodeagent
	// /var/lib/kubelet/pods/20154c76-bf4e-11e7-8a7e-080027631ab3/volumes/nodeagent~uds/test-volume/
	unmountInputDir := filepath.Join(testDir, prefix, testUid, "volumes/nodeagent~uds/test-volume/nodeagent")
	delDir := filepath.Join(configuration.NodeAgentWorkloadHomeDir, testUid)
	credsDir := configuration.NodeAgentCredentialsHomeDir
	credsFile := filepath.Join(credsDir, testUid+".json")

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
	err := Unmount(unmountInputDir)
	if err != nil {
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

func TestInitConfiguration(t *testing.T) {
	// Write config to the configFile;
	// create the expectedConfigFile struct
	// change the pointer of configFile....
	// call InitConfiguration
	// cmp the expected with what was got.
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

	os.RemoveAll(testDir)
	os.Exit(r)
}
