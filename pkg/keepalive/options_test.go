// Copyright Istio Authors. All Rights Reserved.
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

package keepalive_test

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"istio.io/istio/pkg/keepalive"
)

// Test default maximum connection age is set to infinite, preserving previous
// unbounded lifetime behavior.
func TestAgeDefaultsToInfinite(t *testing.T) {
	ko := keepalive.DefaultOption()

	if ko.MaxServerConnectionAge != keepalive.Infinity {
		t.Errorf("%s maximum connection age %v", t.Name(), ko.MaxServerConnectionAge)
	}
}

// Confirm maximum connection age parameters can be set from the command line.
func TestSetConnectionAgeCommandlineOptions(t *testing.T) {
	ko := keepalive.DefaultOption()
	cmd := &cobra.Command{}
	ko.AttachCobraFlags(cmd)

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	sec := 1 * time.Second
	cmd.SetArgs([]string{
		fmt.Sprintf("--keepaliveMaxServerConnectionAge=%v", sec),
	})

	if err := cmd.Execute(); err != nil {
		t.Errorf("%s %s", t.Name(), err.Error())
	}
	if ko.MaxServerConnectionAge != sec {
		t.Errorf("%s maximum connection age %v", t.Name(), ko.MaxServerConnectionAge)
	}
}
