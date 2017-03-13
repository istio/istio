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

	"istio.io/manager/cmd"
)

func rootSetup(t *testing.T) {
	cmd.RootFlags.Kubeconfig = "../../platform/kube/config"
	if err := cmd.RootCmd.PersistentPreRunE(postCmd, []string{}); err != nil { // Set up Client
		t.Fatalf("Could not set up root command: %v", err)
	}
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
	if err := listCmd.RunE(listCmd, []string{"route-rule"}); err != nil {
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
	if err := listCmd.RunE(listCmd, []string{"destination-policy"}); err != nil {
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
