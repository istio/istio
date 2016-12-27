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

package denyChecker

import (
	"testing"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adaptertesting"
)

func TestBuilderInvariants(t *testing.T) {
	b := NewAdapter()
	testutil.TestBuilderInvariants(b, t)
}

func TestAll(t *testing.T) {
	b := NewAdapter()
	if err := b.Configure(b.DefaultAdapterConfig()); err != nil {
		t.Errorf("Unable to configure adapter: %v", err)
	}

	a, err := b.NewAspect(b.DefaultAspectConfig())
	if err != nil {
		t.Errorf("Unable to create adapter: %v", err)
	}
	listChecker := a.(adapter.ListChecker)

	cases := []string{"", "ABC", "ABC/DEF", "AAA", "123"}
	for _, c := range cases {
		ok, err := listChecker.CheckList(c)
		if err != nil {
			t.Errorf("CheckList(%s) failed with error %v", c, err)
		}

		if ok {
			t.Errorf("CheckList(%s) => got 'true'; want 'false'", c)
		}
	}

	if err := listChecker.Close(); err != nil {
		t.Errorf("listChecker.Close failed: %v", err)
	}
}
