// Copyright 2016 Google Inc.
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

package factMapper

import (
	"testing"
)

func TestNoRules(t *testing.T) {
	rules := make(map[string]string)
	var inst *instance
	var err error
	if inst, err = newInstance(&InstanceConfig{Rules: rules}); err != nil {
		t.Error("Expected to successfully create a mapper")
	}

	tracker := inst.NewTracker()

	labels := tracker.GetLabels()
	if len(labels) != 0 {
		t.Error("Got labels when expecting none")
	}

	// pretend to add some facts and try again
	facts := make(map[string]string)
	tracker.UpdateFacts(facts)

	labels = tracker.GetLabels()
	if len(labels) != 0 {
		t.Error("Got labels when expecting none")
	}

	// add some actual facts and try again
	facts = make(map[string]string)
	facts["Fact1"] = "One"
	facts["Fact2"] = "Two"
	facts["Fact4"] = "Four"
	tracker.UpdateFacts(facts)

	labels = tracker.GetLabels()
	if len(labels) != 0 {
		t.Error("Got labels when expecting none")
	}
}

func TestOddballRules(t *testing.T) {
	rules := make(map[string]string)
	rules["Lab1"] = "|A|B|C"
	if _, err := newInstance(&InstanceConfig{Rules: rules}); err == nil {
		t.Error("Expecting to not be able to create a mapper")
	}

	rules["Lab1"] = "A|B|"
	if _, err := newInstance(&InstanceConfig{Rules: rules}); err == nil {
		t.Error("Expecting to not be able to create a mapper")
	}

	rules["Lab1"] = "A| |C"
	if _, err := newInstance(&InstanceConfig{Rules: rules}); err == nil {
		t.Error("Expecting to not be able to create a mapper")
	}

	rules["Lab1"] = "A||C"
	if _, err := newInstance(&InstanceConfig{Rules: rules}); err == nil {
		t.Error("Expecting to not be able to create a mapper")
	}

	rules["Lab1"] = "A | B | C"
	if _, err := newInstance(&InstanceConfig{Rules: rules}); err != nil {
		t.Error("Expecting to be able to create a mapper")
	}
}

func TestNoFacts(t *testing.T) {
	rules := make(map[string]string)
	rules["Lab1"] = "Fact1|Fact2|Fact3"
	rules["Lab2"] = "Fact3|Fact2|Fact1"
	var inst *instance
	var err error
	if inst, err = newInstance(&InstanceConfig{Rules: rules}); err != nil {
		t.Error("Expected to be able to create a mapper")
	}

	tracker := inst.NewTracker()

	labels := tracker.GetLabels()
	if len(labels) != 0 {
		t.Error("Got labels when expecting none")
	}

	// pretend to add some facts and try again
	facts := make(map[string]string)
	tracker.UpdateFacts(facts)

	labels = tracker.GetLabels()
	if len(labels) != 0 {
		t.Error("Got labels when expecting none")
	}

	// add some actual facts and try again
	facts["Fact1"] = "One"
	facts["Fact2"] = "Two"
	facts["Fact4"] = "Four"
	tracker.UpdateFacts(facts)

	labels = tracker.GetLabels()
	if len(labels) != 2 {
		t.Error("Got no labels, expecting 2")
	}

	if labels["Lab1"] != "One" || labels["Lab2"] != "Two" {
		t.Error("Didn't get the expected label values")
	}
}

func TestAddRemoveFacts(t *testing.T) {
	rules := make(map[string]string)
	rules["Lab1"] = "Fact1|Fact2|Fact3"
	rules["Lab2"] = "Fact3|Fact2|Fact1"
	var inst *instance
	var err error
	if inst, err = newInstance(&InstanceConfig{Rules: rules}); err != nil {
		t.Error("Expected to be able to create a mapper")
	}

	tracker := inst.NewTracker()

	// add some facts and try again
	facts := make(map[string]string)
	facts["Fact1"] = "One"
	tracker.UpdateFacts(facts)

	labels := tracker.GetLabels()
	if len(labels) != 2 || labels["Lab1"] != "One" || labels["Lab2"] != "One" {
		t.Error("Got unexpected labels")
	}

	facts["Fact2"] = "Two"
	tracker.UpdateFacts(facts)

	labels = tracker.GetLabels()
	if len(labels) != 2 || labels["Lab1"] != "One" || labels["Lab2"] != "Two" {
		t.Error("Got unexpected labels")
	}

	facts["Fact2"] = ""
	tracker.UpdateFacts(facts)

	labels = tracker.GetLabels()
	if len(labels) != 2 || labels["Lab1"] != "One" || labels["Lab2"] != "" {
		t.Error("Got unexpected labels")
	}

	facts["Fact2"] = ""
	tracker.PurgeFacts([]string{"Fact2"})

	labels = tracker.GetLabels()
	if len(labels) != 2 || labels["Lab1"] != "One" || labels["Lab2"] != "One" {
		t.Error("Got unexpected labels")
	}

	// purge a fact that doesn't exist
	tracker.PurgeFacts([]string{"Fact42"})
}

func TestConfigUpdates(t *testing.T) {
	rules := make(map[string]string)
	rules["Lab1"] = "Fact1|Fact2|Fact3"
	rules["Lab2"] = "Fact3|Fact2|Fact1"
	var inst *instance
	var err error
	if inst, err = newInstance(&InstanceConfig{Rules: rules}); err != nil {
		t.Error("Expected to be able to create a mapper")
	}

	tracker := inst.NewTracker()

	// add some actual facts and try again
	facts := make(map[string]string)
	facts["Fact1"] = "One"
	facts["Fact2"] = "Two"
	facts["Fact4"] = "Four"
	tracker.UpdateFacts(facts)

	labels := tracker.GetLabels()
	if len(labels) != 2 || labels["Lab1"] != "One" || labels["Lab2"] != "Two" {
		t.Error("Got unexpected labels")
	}

	rules["Lab1"] = "Fact3|Fact2|Fact1"
	rules["Lab2"] = "Fact1|Fact2|Fact3"

	if err = inst.UpdateConfig(InstanceConfig{Rules: rules}); err != nil {
		t.Error("Expected to be able to update the config")
	}

	labels = tracker.GetLabels()
	if len(labels) != 2 || labels["Lab1"] != "Two" || labels["Lab2"] != "One" {
		t.Error("Got unexpected labels")
	}
}

func TestReset(t *testing.T) {
	rules := make(map[string]string)
	rules["Lab1"] = "Fact1|Fact2|Fact3"
	rules["Lab2"] = "Fact3|Fact2|Fact1"
	var inst *instance
	var err error
	if inst, err = newInstance(&InstanceConfig{Rules: rules}); err != nil {
		t.Error("Expected to be able to create a mapper")
	}

	tracker := inst.NewTracker()

	// add some actual facts and try again
	facts := make(map[string]string)
	facts["Fact1"] = "One"
	facts["Fact2"] = "Two"
	facts["Fact4"] = "Four"
	tracker.UpdateFacts(facts)

	labels := tracker.GetLabels()
	if len(labels) != 2 || labels["Lab1"] != "One" || labels["Lab2"] != "Two" {
		t.Error("Got unexpected labels")
	}

	tracker.Reset()

	labels = tracker.GetLabels()
	if len(labels) != 0 {
		t.Error("Got unexpected labels")
	}

	numFacts, numLabels := tracker.Stats()
	if numFacts != 0 || numLabels != 0 {
		t.Error("Expected no facts and no labels")
	}
}
