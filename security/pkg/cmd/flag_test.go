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
	"flag"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestInitializeFlags(t *testing.T) {
	cmd := &cobra.Command{}
	var testInt int
	flag.IntVar(&testInt, "test", 137, "test int flag")
	InitializeFlags(cmd)

	testName := "Initialize Flags"
	if !flag.Parsed() {
		t.Errorf("%s: flag.Parsed() returns false, should be true", testName)
	}

	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name != "test" {
			t.Errorf("%s: pflag name error. Actual %s, Expected %s", testName, f.Name, "test")
		}
	})

	_ = cmd.Flags().Parse([]string{})
	if testInt != 137 {
		t.Errorf("%s: pflag parse error. Actual %d, Expected %d", testName, testInt, 137)
	}
}
