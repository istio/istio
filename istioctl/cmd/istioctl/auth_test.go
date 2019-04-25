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

package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/pilot/test/util"
)

func TestAuthCheck(t *testing.T) {
	testCases := []struct {
		name           string
		in             string
		goldenFilename string
	}{
		{
			name:           "listeners and clusters",
			in:             "testdata/auth/productpage_config_dump.json",
			goldenFilename: "testdata/auth/productpage.golden"},
	}

	for _, c := range testCases {
		var out bytes.Buffer
		rootCmd.SetOutput(&out)

		command := fmt.Sprintf("experimental auth check -f %s", c.in)
		rootCmd.SetArgs(strings.Split(command, " "))

		err := rootCmd.Execute()
		if err != nil {
			t.Errorf("%s: unexpected error: %s", c.name, err)
		} else {
			util.CompareContent(out.Bytes(), c.goldenFilename, t)
		}
	}
}
