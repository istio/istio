// Copyright Istio Authors
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
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func checkHelpForFlag(t *testing.T, gotHelpText, flag string, wantExists bool) {
	t.Helper()

	if strings.Contains(gotHelpText, flag) != wantExists {
		if wantExists {
			t.Errorf("%q flag was expected but not found in help text", flag)
		} else {
			t.Errorf("%q flag was found in help text but not expected", flag)
		}
	}
}

func TestHideInheritedFlags(t *testing.T) {
	const (
		parentFlag0 = "parent-flag0"
		parentFlag1 = "parent-flag1"
		parentFlag2 = "parent-flag2"
		childFlag2  = "child-flag2"
	)
	parent := &cobra.Command{Use: "parent"}
	_ = parent.PersistentFlags().String(parentFlag0, "", parentFlag0)
	_ = parent.PersistentFlags().String(parentFlag1, "", parentFlag1)
	_ = parent.PersistentFlags().String(parentFlag2, "", parentFlag2)
	var out bytes.Buffer
	parent.SetOut(&out)
	parent.SetErr(&out)

	child := &cobra.Command{
		Use: "child",
		Run: func(c *cobra.Command, args []string) {},
	}
	_ = parent.PersistentFlags().String(childFlag2, "", childFlag2)
	parent.AddCommand(child)

	// verify both parent flags and the child flag are visible by default
	parent.SetArgs([]string{"child", "--help"})
	if err := parent.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	out.Reset()
	checkHelpForFlag(t, got, parentFlag0, true)
	checkHelpForFlag(t, got, parentFlag1, true)
	checkHelpForFlag(t, got, parentFlag2, true)
	checkHelpForFlag(t, got, childFlag2, true)

	// verify the hidden parent flag is not visible in help text
	hideInheritedFlags(child, parentFlag1, parentFlag2)
	if err := parent.Execute(); err != nil {
		t.Fatal(err)
	}
	got = out.String()
	out.Reset()
	checkHelpForFlag(t, got, parentFlag0, true)
	checkHelpForFlag(t, got, parentFlag1, false)
	checkHelpForFlag(t, got, parentFlag1, false)
	checkHelpForFlag(t, got, childFlag2, true)
}
