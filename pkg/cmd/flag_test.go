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
	"flag"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	defaultInt = 100

	defaultString = "my default string"

	defaultBool = true

	defaultDuration = 24 * time.Hour
)

func TestInitializeIntFlag(t *testing.T) {
	cmd := &cobra.Command{}
	var testInt int
	flag.IntVar(&testInt, "testint", defaultInt, "test int flag")
	AddFlags(cmd)

	testName := "Initialize int Flag"
	if !flag.Parsed() {
		t.Errorf("%s: flag.Parsed() returns false, should be true", testName)
	}

	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name != "testint" {
			t.Errorf("%s: pflag name error. Actual %s, Expected %s", testName, f.Name, "testint")
		}
	})

	_ = cmd.Flags().Parse([]string{})
	if testInt != defaultInt {
		t.Errorf("%s: pflag parse error. Actual %d, Expected %d", testName, testInt, defaultInt)
	}
}

func TestInitializeStringFlag(t *testing.T) {
	cmd := &cobra.Command{}
	var testString string
	flag.StringVar(&testString, "teststring", defaultString, "test string flag")
	AddFlags(cmd)

	testName := "Initialize String Flag"
	if !flag.Parsed() {
		t.Errorf("%s: flag.Parsed() returns false, should be true", testName)
	}

	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name != "teststring" {
			t.Errorf("%s: pflag name error. Actual %s, Expected %s", testName, f.Name, "teststring")
		}
	})

	_ = cmd.Flags().Parse([]string{})
	if testString != defaultString {
		t.Errorf("%s: pflag parse error. Actual %s, Expected %s", testName, testString, defaultString)
	}
}

func TestInitializeBoolFlag(t *testing.T) {
	cmd := &cobra.Command{}
	var testBool bool
	flag.BoolVar(&testBool, "testbool", defaultBool, "test bool flag")
	AddFlags(cmd)

	testName := "Initialize bool Flag"
	if !flag.Parsed() {
		t.Errorf("%s: flag.Parsed() returns false, should be true", testName)
	}

	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name != "testbool" {
			t.Errorf("%s: pflag name error. Actual %s, Expected %s", testName, f.Name, "testbool")
		}
	})

	_ = cmd.Flags().Parse([]string{})
	if testBool != defaultBool { // nolint: megacheck
		t.Errorf("%s: pflag parse error. Actual %t, Expected %t", testName, testBool, defaultBool)
	}
}

func TestInitializeDurationFlag(t *testing.T) {
	cmd := &cobra.Command{}
	var testDuration time.Duration
	flag.DurationVar(&testDuration, "testduration", defaultDuration, "test duration flag")
	AddFlags(cmd)

	testName := "Initialize duration flag"
	if !flag.Parsed() {
		t.Errorf("%s: flag.Parsed() returns false, should be true", testName)
	}

	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name != "testduration" {
			t.Errorf("%s: pflag name error. Actual %s, Expected %s", testName, f.Name, "testduration")
		}
	})

	_ = cmd.Flags().Parse([]string{})
	if testDuration != defaultDuration {
		t.Errorf("%s: pflag parse error. Actual %d, Expected %d", testName, testDuration, defaultDuration)
	}
}
