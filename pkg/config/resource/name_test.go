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

package resource

import (
	"testing"
)

func TestFullNameString(t *testing.T) {
	n := NewFullName("ns1", "l1")
	if n.String() != "ns1/l1" {
		t.Fatalf("unexpected name string: %v", n.String())
	}
}

func TestFullNameString_NoName(t *testing.T) {
	n := NewFullName("ns1", "")
	if n.String() != "ns1/" {
		t.Fatalf("unexpected name string: %v", n.String())
	}
}

func TestFullNameString_NoNamespace(t *testing.T) {
	n := NewFullName("", "l1")
	if n.String() != "l1" {
		t.Fatalf("unexpected name string: %v", n.String())
	}
}

func TestParseFullName_Segments(t *testing.T) {
	steps := []struct {
		description string
		name        string
		want        string
		err         string
		valid       bool
	}{
		{
			description: "namespace with emapty name",
			name:        "testNamespace/",
			want:        "",
			err:         "failed parsing name 'testNamespace/': invalid name 'testNamespace/': name must not be empty",
			valid:       false,
		},
		{
			description: "name with empty namespace",
			name:        "/testName",
			want:        "testName",
			err:         "",
			valid:       true,
		},
		{
			description: "all empty",
			name:        "/",
			want:        "",
			err:         "failed parsing name '/': invalid name '': name must not be empty",
			valid:       false,
		},
		{
			description: "multiple segments", // Only the first segment is treated specially
			name:        "testName//someotherStuff",
			want:        "testName//someotherStuff",
			err:         "",
			valid:       true,
		},
		{
			description: "valid name with namespace",
			name:        "testNamespace/testName",
			want:        "testNamespace/testName",
			valid:       true,
		},
	}
	for _, s := range steps {
		t.Run(s.description, func(tt *testing.T) {
			n, err := ParseFullName(s.name)
			if err == nil && n.String() != s.want {
				tt.Fatalf("unexpected name: %v", n)
			}
			if s.valid {
				if err != nil {
					tt.Fatalf("expected no err but got: %v", err)
				}
				return
			}
			if err == nil {
				tt.Fatalf("expected err but got: %v", err)
			}
			if err.Error() != s.err {
				tt.Fatalf("expected err \"%s\" but got: %v", s.err, err.Error())
			}
		})
	}
}

func TestNewShortOrFullName_HasNamespace(t *testing.T) {
	actual := NewShortOrFullName("ns1", "ns2/l1")
	expected := NewFullName("ns2", "l1")
	if actual != expected {
		t.Fatalf("Expected %v, got %v", expected, actual)
	}
}

func TestNewShortOrFullName_NoNamespace(t *testing.T) {
	actual := NewShortOrFullName("ns1", "l1")
	expected := NewFullName("ns1", "l1")
	if actual != expected {
		t.Fatalf("Expected %v, got %v", expected, actual)
	}
}
