// Copyright 2017 Istio Authors
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

package cmd

import (
	"testing"
	"bytes"
	"io/ioutil"
	"github.com/spf13/cobra"
)

func executeCommand(root *cobra.Command, args ...string) (output string, err error) {
	_, output, err = executeCommandC(root, args...)
	return output, err
}

func executeCommandC(root *cobra.Command, args ...string) (c *cobra.Command, output string, err error) {
	buf := new(bytes.Buffer)
	root.SetOutput(buf)
	root.SetArgs(args)
	c, err = root.ExecuteC()
	return c, buf.String(), err
}

func TestNewProbeCmd(t *testing.T) {
	cmd := NewProbeCmd()

	if cmd.Name() != "probe" {
		t.Errorf("Command name does not match")
	}

	flags := cmd.PersistentFlags()
	strVal, err := flags.GetString("probe-path")
	if err != nil {
		t.Errorf("Failed to get probe-path flag")
	}
	if strVal != "" {
		t.Errorf("probe-path value should be empty")
	}

	durVal, err := flags.GetDuration("interval")
	if err != nil {
		t.Errorf("Failed to get interval flag")
	}
	if durVal != 0 {
		t.Errorf("interval var should be 0")
	}
}

func TestProbeCommand(t *testing.T) {
	cmd := NewProbeCmd()

	testFile, err := ioutil.TempFile("/tmp", "test")
	if err != nil {
		t.Errorf("failed to create a test file")
	}

	rootCmd := &cobra.Command{}
	rootCmd.AddCommand(cmd)

	output, err := executeCommand(rootCmd, "probe", "--probe-path", testFile.Name(), "--interval", "1s")
	if output != "" {
		t.Errorf("Unexpected output: %v", output)
	}
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}
