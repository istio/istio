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
