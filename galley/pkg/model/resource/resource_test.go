//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package resource

import (
	"testing"
)

func TestKind_Equality_True(t *testing.T) {
	k1 := Kind("a")
	k2 := Kind("a")

	if k1 != k2 {
		t.Fatalf("Expected to be equal: %v == %v", k1, k2)
	}
}

func TestKind_Equality_False(t *testing.T) {
	k1 := Kind("a")
	k2 := Kind("v")

	if k1 == k2 {
		t.Fatalf("Expected to be not equal: %v == %v", k1, k2)
	}
}

func TestVersion_Equality_True(t *testing.T) {
	v1 := Version("a")
	v2 := Version("a")

	if v1 != v2 {
		t.Fatalf("Expected to be equal: %v == %v", v1, v2)
	}
}

func TestVersion_Equality_False(t *testing.T) {
	v1 := Version("a")
	v2 := Version("v")

	if v1 == v2 {
		t.Fatalf("Expected to be not equal: %v == %v", v1, v2)
	}
}
func TestKey_Equality_True(t *testing.T) {
	k1 := Key{Kind: Kind("a"), FullName: "ks"}
	k2 := Key{Kind: Kind("a"), FullName: "ks"}

	if k1 != k2 {
		t.Fatalf("Expected to be equal: %v == %v", k1, k2)
	}
}

func TestKey_Equality_False_DifferentKind(t *testing.T) {
	k1 := Key{Kind: Kind("a"), FullName: "ks"}
	k2 := Key{Kind: Kind("b"), FullName: "ks"}

	if k1 == k2 {
		t.Fatalf("Expected to be not equal: %v == %v", k1, k2)
	}
}

func TestKey_Equality_False_DifferentName(t *testing.T) {
	k1 := Key{Kind: Kind("a"), FullName: "ks"}
	k2 := Key{Kind: Kind("a"), FullName: "otherks"}

	if k1 == k2 {
		t.Fatalf("Expected to be not equal: %v == %v", k1, k2)
	}
}

func TestVersionedKey_Equality_True(t *testing.T) {
	k1 := VersionedKey{
		Key: Key{Kind: Kind("a"), FullName: "ks"}, Version: Version("v1")}
	k2 := VersionedKey{
		Key: Key{Kind: Kind("a"), FullName: "ks"}, Version: Version("v1")}

	if k1 != k2 {
		t.Fatalf("Expected to be equal: %v == %v", k1, k2)
	}
}

func TestVersionedKey_Equality_False_DifferentKind(t *testing.T) {
	k1 := VersionedKey{
		Key: Key{Kind: Kind("a"), FullName: "ks"}, Version: Version("v1")}
	k2 := VersionedKey{
		Key: Key{Kind: Kind("b"), FullName: "ks"}, Version: Version("v1")}

	if k1 == k2 {
		t.Fatalf("Expected to be not equal: %v == %v", k1, k2)
	}
}

func TestVersionedKey_Equality_False_DifferentName(t *testing.T) {
	k1 := VersionedKey{
		Key: Key{Kind: Kind("a"), FullName: "ks"}, Version: Version("v1")}
	k2 := VersionedKey{
		Key: Key{Kind: Kind("a"), FullName: "otherks"}, Version: Version("v1")}

	if k1 == k2 {
		t.Fatalf("Expected to be not equal: %v == %v", k1, k2)
	}
}

func TestVersionedKey_Equality_False_DifferentVersion(t *testing.T) {
	k1 := VersionedKey{
		Key: Key{Kind: Kind("a"), FullName: "ks"}, Version: Version("v1")}
	k2 := VersionedKey{
		Key: Key{Kind: Kind("a"), FullName: "ks"}, Version: Version("v2")}

	if k1 == k2 {
		t.Fatalf("Expected to be not equal: %v == %v", k1, k2)
	}
}
