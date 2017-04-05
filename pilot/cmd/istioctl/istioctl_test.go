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

package main

import (
	"testing"

	"istio.io/manager/test/mock"
)

func rootSetup(t *testing.T) {
	config = mock.MakeRegistry()
}

func TestCreateInvalidFile(t *testing.T) {
	rootSetup(t)
	file = "does-not-exist.yaml"
	if err := postCmd.RunE(postCmd, []string{}); err == nil {
		t.Fatalf("Did not fail looking for file")
	}
}

func TestInvalidType(t *testing.T) {
	rootSetup(t)
	file = "testdata/invalid-type.yaml"
	if err := postCmd.RunE(postCmd, []string{}); err == nil {
		t.Fatalf("Did not fail when presented with invalid rule type")
	}
}

func TestInvalidRuleStructure(t *testing.T) {
	rootSetup(t)
	file = "testdata/invalid-dest-policy.yaml"
	if err := postCmd.RunE(postCmd, []string{}); err == nil {
		t.Fatalf("Did not fail when presented with invalid rule structure")
	}
}

func TestCreateReplaceDeleteRoutes(t *testing.T) {
	rootSetup(t)
	file = "testdata/four-route-rules.yaml"
	if err := postCmd.RunE(postCmd, []string{}); err != nil {
		t.Fatalf("Could not create routes: %v", err)
	}
	if err := getCmd.RunE(getCmd, []string{"route-rules"}); err != nil {
		t.Fatalf("Could not list destination policies: %v", err)
	}
	if err := putCmd.RunE(putCmd, []string{}); err != nil {
		t.Fatalf("Could not replace routes: %v", err)
	}
	if err := deleteCmd.RunE(deleteCmd, []string{}); err != nil {
		t.Fatalf("Could not delete routes: %v", err)
	}
	// Try to delete again, to verify we fail if we delete a rule that exists
	if err := deleteCmd.RunE(deleteCmd, []string{}); err == nil {
		t.Fatalf("Second attempt to delete route rules did not fail")
	}
}

func TestCreateReplaceDeletePolicy(t *testing.T) {
	rootSetup(t)
	file = "testdata/dest-policy.yaml"
	if err := postCmd.RunE(postCmd, []string{}); err != nil {
		t.Fatalf("Could not create destination policy: %v", err)
	}
	if err := getCmd.RunE(getCmd, []string{"destination-policy"}); err != nil {
		t.Fatalf("Could not list destination policies: %v", err)
	}
	if err := putCmd.RunE(putCmd, []string{}); err != nil {
		t.Fatalf("Could not replace destination policy: %v", err)
	}
	if err := deleteCmd.RunE(deleteCmd, []string{}); err != nil {
		t.Fatalf("Could not delete destination policy: %v", err)
	}
	// Try to delete again, to verify we fail if we delete a policy that exists
	if err := deleteCmd.RunE(deleteCmd, []string{}); err == nil {
		t.Fatalf("Second attempt to delete destination policy did not fail")
	}
}

func TestGet(t *testing.T) {
	rootSetup(t)
	file = "testdata/dest-policy.yaml"
	if err := postCmd.RunE(postCmd, []string{}); err != nil {
		t.Fatalf("Could not create destination policy: %v", err)
	}

	file = ""
	if err := getCmd.RunE(getCmd, []string{"destination-policy", "world-cb"}); err != nil {
		t.Fatalf("Did not find world-cb %v", err)
	}

	if err := deleteCmd.RunE(deleteCmd, []string{"destination-policy", "world-cb"}); err != nil {
		t.Fatalf("Could not delete world-cb: %v", err)
	}

	if err := getCmd.RunE(getCmd, []string{"destination-policy", "world-cb"}); err == nil {
		t.Fatalf("Found world-cb after deletion ")
	}
}

func TestNewGet(t *testing.T) {
	rootSetup(t)

	file = "testdata/dest-policy.yaml"
	if err := postCmd.RunE(postCmd, []string{}); err != nil {
		t.Fatalf("Could not create destination policy: %v", err)
	}

	file = ""
	if err := getCmd.RunE(getCmd, []string{"route-rule"}); err != nil {
		t.Fatalf("Could not list routes: %v", err)
	}

	// Plural form
	if err := getCmd.RunE(getCmd, []string{"route-rules"}); err != nil {
		t.Fatalf("Could not list routes: %v", err)
	}

	// Short output
	outputFormat = "short"
	if err := getCmd.RunE(getCmd, []string{"route-rules"}); err != nil {
		t.Fatalf("Could not list routes: %v", err)
	}

	// YAML output
	outputFormat = "yaml"
	if err := getCmd.RunE(getCmd, []string{"route-rules"}); err != nil {
		t.Fatalf("Could not list routes: %v", err)
	}

	// Singular form
	if err := getCmd.RunE(getCmd, []string{"destination-policy"}); err != nil {
		t.Fatalf("Could not list routes: %v", err)
	}

	// Plural form
	if err := getCmd.RunE(getCmd, []string{"destination-policies"}); err != nil {
		t.Fatalf("Could not list routes: %v", err)
	}

	if err := deleteCmd.RunE(deleteCmd, []string{"destination-policy", "world-cb"}); err != nil {
		t.Fatalf("Could not delete world-cb: %v", err)
	}
}

func TestInvalidRouteRule(t *testing.T) {
	rootSetup(t)
	file = "testdata/invalid-route-rule.yaml"
	if err := postCmd.RunE(postCmd, []string{}); err == nil {
		t.Fatalf("Did not fail when presented with invalid route rule")
	}
}

func TestInvalidDestinationPolicy(t *testing.T) {
	rootSetup(t)
	file = "testdata/invalid-destination-policy2.yaml"
	if err := postCmd.RunE(postCmd, []string{}); err == nil {
		t.Fatalf("Did not fail when presented with invalid destination policy")
	}
}
