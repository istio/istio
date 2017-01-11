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

	pb "istio.io/mixer/adapter/genericListChecker/config"
	"istio.io/mixer/pkg/adaptertesting"
)

type testCase struct {
	ac            pb.Config
	matchValues   []string
	unmatchValues []string
}

func TestAll(t *testing.T) {
	cases := []testCase{
		{
			pb.Config{ListEntries: []string{"One", "Two", "Three"}},
			[]string{"One", "Two", "Three"},
			[]string{"O", "OneOne", "ne", "ree"},
		},

		{
			pb.Config{},
			[]string{},
			[]string{"One", "Two", "Three"},
		},
	}

	for _, c := range cases {
		b := newAdapter()

		a, err := b.NewAspect(nil, &c.ac)
		if err != nil {
			t.Errorf("Unable to create adapter: %v", err)
		}

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

		if err := a.Close(); err != nil {
			t.Errorf("a.Close failed: %v", err)
		}

		if err := b.Close(); err != nil {
			t.Errorf("b.Close failed: %v", err)
		}
	}
}

func TestInvariants(t *testing.T) {
	adaptertesting.TestAdapterInvariants(newAdapter(), Register, t)
}
