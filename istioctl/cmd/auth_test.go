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

func runCommandAndCheckGolden(name, command, golden string, t *testing.T) {
	t.Helper()
	var out bytes.Buffer
	rootCmd := GetRootCmd(strings.Split(command, " "))
	rootCmd.SetOutput(&out)

	err := rootCmd.Execute()
	if err != nil {
		t.Errorf("%s: unexpected error: %s", name, err)
	} else {
		util.CompareContent(out.Bytes(), golden, t)
	}
}

func TestAuthCheck(t *testing.T) {
	testCases := []struct {
		name   string
		in     string
		golden string
	}{
		{
			name:   "listeners and clusters",
			in:     "testdata/auth/productpage_config_dump.json",
			golden: "testdata/auth/productpage.golden",
		},
	}

	for _, c := range testCases {
		command := fmt.Sprintf("experimental auth check -f %s", c.in)
		runCommandAndCheckGolden(c.name, command, c.golden, t)
	}
}

func TestAuthUpgrade(t *testing.T) {
	testCases := []struct {
		name     string
		in       string
		services []string
		golden   string
	}{
		{
			name:     "v1 policies",
			in:       "testdata/auth/authz-policy.yaml",
			services: []string{"testdata/auth/svc-other.yaml", "testdata/auth/svc-bookinfo.yaml"},
			golden:   "testdata/auth/authz-policy.golden",
		},
	}

	for _, c := range testCases {
		command := fmt.Sprintf("experimental auth upgrade -f %s --service %s", c.in, strings.Join(c.services, ","))
		runCommandAndCheckGolden(c.name, command, c.golden, t)
	}
}
