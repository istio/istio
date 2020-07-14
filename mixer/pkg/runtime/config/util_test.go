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

package config

import (
	"testing"
)

func TestIsFQN(t *testing.T) {
	tests := map[string]bool{
		"a":       false,
		"b":       false,
		"a.b":     false,
		"a.b.c.d": false,

		"a.b.c": true,
	}

	for k, v := range tests {
		if isFQN(k) != v {
			t.Fatal(k)
		}
	}
}

func TestCanonicalize(t *testing.T) {
	if a, _ := canonicalize("foo", "mykind", "bar"); a != "foo.mykind.bar" {
		t.Fail()
	}

	if a, _ := canonicalize("foo.bar.baz", "mykind", "bar"); a != "foo.bar.baz" {
		t.Fail()
	}

	if a, _ := canonicalize("foo.bar.baz.baz2", "mykind", "bar"); a != "foo.bar.baz.baz2" {
		t.Fail()
	}
}

func TestExtractShortName(t *testing.T) {
	if ExtractShortName("foo") != "foo" {
		t.Fail()
	}

	if ExtractShortName("foo.bar.baz") != "foo" {
		t.Fail()
	}
}
