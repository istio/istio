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

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adaptertesting"
)

func TestBuilderInvariants(t *testing.T) {
	b := NewBuilder()
	testutil.TestBuilderInvariants(b, t)
}

type testCase struct {
	bc            BuilderConfig
	ac            AspectConfig
	matchValues   []string
	unmatchValues []string
}

func TestAll(t *testing.T) {
	cases := []testCase{
		{
			BuilderConfig{ListEntries: []string{"Four", "Five"}, WhitelistMode: true},
			AspectConfig{ListEntries: []string{"One", "Two", "Three"}},
			[]string{"One", "Two", "Three"},
			[]string{"O", "OneOne", "ne", "ree"},
		},

		{
			BuilderConfig{ListEntries: []string{"Four", "Five"}, WhitelistMode: false},
			AspectConfig{ListEntries: []string{"One", "Two", "Three"}},
			[]string{"O", "OneOne", "ne", "ree"},
			[]string{"One", "Two", "Three"},
		},

		{
			BuilderConfig{ListEntries: []string{"One", "Two", "Three"}, WhitelistMode: true},
			AspectConfig{},
			[]string{"One", "Two", "Three"},
			[]string{"O", "OneOne", "ne", "ree"},
		},

		{
			BuilderConfig{WhitelistMode: true},
			AspectConfig{},
			[]string{},
			[]string{"Lasagna"},
		},
	}

	for _, c := range cases {
		b := NewBuilder()
		b.Configure(&c.bc)

		aa, err := b.NewAspect(&c.ac)
		if err != nil {
			t.Errorf("Unable to create adapter: %v", err)
		}
		a := aa.(adapter.ListChecker)

		for _, value := range c.matchValues {
			ok, err := a.CheckList(value)
			if err != nil {
				t.Errorf("CheckList(%s) failed with %v", value, err)
			}

			if !ok {
				t.Errorf("CheckList(%s): expecting 'true', got 'false'", value)
			}
		}

		for _, value := range c.unmatchValues {
			ok, err := a.CheckList(value)
			if err != nil {
				t.Errorf("CheckList(%s) failed with %v", value, err)
			}

			if ok {
				t.Errorf("CheckList(%s): expecting 'false', got 'true'", value)
			}
		}
	}
}
