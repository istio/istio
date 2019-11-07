// Copyright 2019 Istio Authors
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

func TestNewName(t *testing.T) {
	n := NewName("ns1", "l1")
	if n.string != "ns1/l1" {
		t.Fatalf("unexpected name: %v", n.string)
	}
}

func TestNewName_NoNamespace(t *testing.T) {
	n := NewName("", "l1")
	if n.string != "l1" {
		t.Fatalf("unexpected name: %v", n.string)
	}
}

func TestNewFullName_Empty(t *testing.T) {
	n, err := NewFullName("")
	if n.String() != "" {
		t.Fatalf("unexpected name: %v", n)
	}
	if err == nil {
		t.Fatalf("expected err but got: %v", err)
	}
	errMsg := "invalid name: can not be empty"
	if err.Error() != errMsg {
		t.Fatalf("expected err \"%s\" but got: %v", errMsg, err.Error())
	}
}

func TestNewFullName_Segments(t *testing.T) {
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
			err:         "invalid name testNamespace/: name must not be empty",
			valid:       false,
		},
		{
			description: "name with empty namespace",
			name:        "/testName",
			want:        "",
			err:         "invalid name /testName: namespace must not be empty",
			valid:       false,
		},
		{
			description: "all empty",
			name:        "/",
			want:        "",
			err:         "invalid name /: namespace must not be empty",
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
			n, err := NewFullName(s.name)
			if n.String() != s.want {
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

func TestName_String(t *testing.T) {
	n := NewName("ns1", "l1")
	if n.String() != "ns1/l1" {
		t.Fatalf("unexpected string: %v", n.string)
	}
}

func TestInterpretAsNamespaceAndName(t *testing.T) {
	n := NewName("ns1", "l1")
	ns, l := n.InterpretAsNamespaceAndName()
	if ns != "ns1" || l != "l1" {
		t.Fatalf("unexpected %q, %q", ns, l)
	}
}

func TestInterpretAsNamespaceAndName_NoNamespace(t *testing.T) {
	n := NewName("", "l1")
	ns, l := n.InterpretAsNamespaceAndName()
	if ns != "" || l != "l1" {
		t.Fatalf("unexpected %q, %q", ns, l)
	}
}

func TestNewShortOrFullName_HasNamespace(t *testing.T) {
	actual := NewShortOrFullName("ns1", "ns2/l1")
	expected := NewName("ns2", "l1")
	if actual != expected {
		t.Fatalf("Expected %v, got %v", expected, actual)
	}
}

func TestNewShortOrFullName_NoNamespace(t *testing.T) {
	actual := NewShortOrFullName("ns1", "l1")
	expected := NewName("ns1", "l1")
	if actual != expected {
		t.Fatalf("Expected %v, got %v", expected, actual)
	}
}
