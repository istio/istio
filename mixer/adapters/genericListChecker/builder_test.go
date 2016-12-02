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

package genericListChecker

import (
	"testing"

	"istio.io/mixer/adapters"
	"istio.io/mixer/adapters/testutil"
)

func TestBuilderInvariants(t *testing.T) {
	b := NewBuilder()
	testutil.TestBuilderInvariants(b, t)
}

func TestWhitelistWithOverrides(t *testing.T) {
	b := NewBuilder()
	builderEntries := []string{"Four", "Five"}
	b.Configure(&BuilderConfig{ListEntries: builderEntries, WhitelistMode: true})

	entries := []string{"One", "Two", "Three"}
	config := AdapterConfig{ListEntries: entries}

	aa, err := b.NewAdapter(&config)
	if err != nil {
		t.Error("unable to create adapter " + err.Error())
	}
	a := aa.(adapters.ListChecker)

	goodValues := []string{"One", "Two", "Three"}
	for _, value := range goodValues {
		var ok bool
		ok, err = a.CheckList(value)
		if !ok {
			t.Error("Expecting check to pass")
		}
	}

	badValues := []string{"O", "OneOne", "ne", "ree"}
	for _, value := range badValues {
		var ok bool
		ok, err = a.CheckList(value)
		if ok {
			t.Error("Expecting check to fail")
		}
	}
}

func TestBlacklistWithOverrides(t *testing.T) {
	b := NewBuilder()
	builderEntries := []string{"Four", "Five"}
	b.Configure(&BuilderConfig{ListEntries: builderEntries, WhitelistMode: false})

	entries := []string{"One", "Two", "Three"}
	config := AdapterConfig{ListEntries: entries}

	aa, err := b.NewAdapter(&config)
	if err != nil {
		t.Error("unable to create adapter " + err.Error())
	}
	a := aa.(adapters.ListChecker)

	badValues := []string{"One", "Two", "Three"}
	for _, value := range badValues {
		var ok bool
		ok, err = a.CheckList(value)
		if ok {
			t.Error("Expecting check to fail")
		}
	}

	goodValues := []string{"O", "OneOne", "ne", "ree"}
	for _, value := range goodValues {
		var ok bool
		ok, err = a.CheckList(value)
		if !ok {
			t.Error("Expecting check to pass")
		}
	}
}

func TestWhitelistNoOverride(t *testing.T) {
	b := NewBuilder()
	builderEntries := []string{"One", "Two", "Three"}
	b.Configure(&BuilderConfig{ListEntries: builderEntries, WhitelistMode: true})

	config := AdapterConfig{}

	aa, err := b.NewAdapter(&config)
	if err != nil {
		t.Error("unable to create adapter " + err.Error())
	}
	a := aa.(adapters.ListChecker)

	goodValues := []string{"One", "Two", "Three"}
	for _, value := range goodValues {
		var ok bool
		ok, err = a.CheckList(value)
		if !ok {
			t.Error("Expecting check to pass")
		}
	}

	badValues := []string{"O", "OneOne", "ne", "ree"}
	for _, value := range badValues {
		var ok bool
		ok, err = a.CheckList(value)
		if ok {
			t.Error("Expecting check to fail")
		}
	}
}

func TestEmptyList(t *testing.T) {
	b := NewBuilder()
	b.Configure(&BuilderConfig{WhitelistMode: true})

	config := AdapterConfig{}

	aa, err := b.NewAdapter(&config)
	if err != nil {
		t.Error("unable to create adapter " + err.Error())
	}
	a := aa.(adapters.ListChecker)

	var ok bool
	ok, err = a.CheckList("Lasagna")
	if ok {
		t.Error("Expecting check to fail")
	}
}
