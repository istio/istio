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

func TestInitVer1_8(t *testing.T) {
	var resp Resp
	expectedResp := Resp{Status: "Success", Message: "Init ok.", Capabilities: &Capabilities{Attach: false}}

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

	testInitStdIo(t)
	if err := Init("1.9"); err != nil {
		t.Errorf("Failed to init. (%s)", err.Error())
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
		t.Errorf("Mount function failed: %s.", err.Error())
	}
}

func TestUnmount(t *testing.T) {
	err := Unmount("/tmp")
	if err != nil {
		t.Errorf("Unmount function failed.")
	}
}
