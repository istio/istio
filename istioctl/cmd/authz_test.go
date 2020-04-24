// Copyright 2019 Istio Authors
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
	"bytes"
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/pilot/test/util"
)

func runCommandWantOutput(command, golden string, t *testing.T) {
	t.Helper()
	out, err := runCommand(command, t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	util.CompareContent(out.Bytes(), golden, t)
}

func runCommand(command string, t *testing.T) (bytes.Buffer, error) {
	t.Helper()
	out := bytes.Buffer{}
	rootCmd := GetRootCmd(strings.Split(command, " "))
	rootCmd.SetOut(&out)
	return out, rootCmd.Execute()
}

func TestAuthZCheck(t *testing.T) {
	testCases := []struct {
		name   string
		in     string
		golden string
	}{
		{
			name:   "listeners and clusters",
			in:     "testdata/authz/productpage_config_dump.json",
			golden: "testdata/authz/productpage.golden",
		},
	}

	for _, c := range testCases {
		command := fmt.Sprintf("experimental authz check -f %s", c.in)
		runCommandWantOutput(command, c.golden, t)
	}
}
