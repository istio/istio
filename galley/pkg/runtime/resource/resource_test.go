// Copyright 2018 Istio Authors
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
	"fmt"
	"reflect"
	"testing"

	"github.com/gogo/protobuf/types"
)

func TestCollection_Equality_True(t *testing.T) {
	k1 := Collection{"a"}
	k2 := Collection{"a"}

	if k1 != k2 {
		t.Fatalf("Expected to be equal: %v == %v", k1, k2)
	}
}

func TestCollection_Equality_False(t *testing.T) {
	k1 := Collection{"a"}
	k2 := Collection{"v"}

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
	k1 := Key{Collection: Collection{"a"}, FullName: FullName{"ks"}}
	k2 := Key{Collection: Collection{"a"}, FullName: FullName{"ks"}}

	if k1 != k2 {
		t.Fatalf("Expected to be equal: %v == %v", k1, k2)
	}
}

func TestKey_Equality_False_DifferentCollection(t *testing.T) {
	k1 := Key{Collection: Collection{"a"}, FullName: FullName{"ks"}}
	k2 := Key{Collection: Collection{"b"}, FullName: FullName{"ks"}}

	if k1 == k2 {
		t.Fatalf("Expected to be not equal: %v == %v", k1, k2)
	}
}

func TestKey_Equality_False_DifferentName(t *testing.T) {
	k1 := Key{Collection: Collection{"a"}, FullName: FullName{"ks"}}
	k2 := Key{Collection: Collection{"a"}, FullName: FullName{"otherks"}}

	if k1 == k2 {
		t.Fatalf("Expected to be not equal: %v == %v", k1, k2)
	}
}

func TestKey_String(t *testing.T) {
	k1 := Key{Collection: Collection{"a"}, FullName: FullName{"ks"}}
	// Ensure that it doesn't crash
	_ = k1.String()
}

func TestVersionedKey_Equality_True(t *testing.T) {
	k1 := VersionedKey{
		Key: Key{Collection: Collection{"a"}, FullName: FullName{"ks"}}, Version: Version("v1")}
	k2 := VersionedKey{
		Key: Key{Collection: Collection{"a"}, FullName: FullName{"ks"}}, Version: Version("v1")}

	if k1 != k2 {
		t.Fatalf("Expected to be equal: %v == %v", k1, k2)
	}
}

func TestVersionedKey_Equality_False_DifferentCollection(t *testing.T) {
	k1 := VersionedKey{
		Key: Key{Collection: Collection{"a"}, FullName: FullName{"ks"}}, Version: Version("v1")}
	k2 := VersionedKey{
		Key: Key{Collection: Collection{"b"}, FullName: FullName{"ks"}}, Version: Version("v1")}

	if k1 == k2 {
		t.Fatalf("Expected to be not equal: %v == %v", k1, k2)
	}
}

func TestVersionedKey_Equality_False_DifferentName(t *testing.T) {
	k1 := VersionedKey{
		Key: Key{Collection: Collection{"a"}, FullName: FullName{"ks"}}, Version: Version("v1")}
	k2 := VersionedKey{
		Key: Key{Collection: Collection{"a"}, FullName: FullName{"otherks"}}, Version: Version("v1")}

	if k1 == k2 {
		t.Fatalf("Expected to be not equal: %v == %v", k1, k2)
	}
}

func TestVersionedKey_Equality_False_DifferentVersion(t *testing.T) {
	k1 := VersionedKey{
		Key: Key{Collection: Collection{"a"}, FullName: FullName{"ks"}}, Version: Version("v1")}
	k2 := VersionedKey{
		Key: Key{Collection: Collection{"a"}, FullName: FullName{"ks"}}, Version: Version("v2")}

	if k1 == k2 {
		t.Fatalf("Expected to be not equal: %v == %v", k1, k2)
	}
}

func TestVersionedKey_String(t *testing.T) {
	k1 := VersionedKey{
		Key: Key{Collection: Collection{"a"}, FullName: FullName{"ks"}}, Version: Version("v1")}
	// Ensure that it doesn't crash
	_ = k1.String()
}

func TestResource_IsEmpty(t *testing.T) {
	r := Entry{}
	if !r.IsEmpty() {
		t.Fatal("should have been empty")
	}

	r.Item = &types.Empty{}
	if r.IsEmpty() {
		t.Fatal("should have not been empty")
	}
}

func TestInfo_newProtoInstance_Success(t *testing.T) {
	i := Info{
		goType: reflect.TypeOf(types.Empty{}),
	}
	p := i.NewProtoInstance()

	if p == nil || reflect.TypeOf(p) != reflect.PtrTo(reflect.TypeOf(types.Empty{})) {
		t.Fatalf("Unexpected proto type returned: %v", p)
	}
}

func TestInfo_newProtoInstance_PanicAtNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Expected panic not found")
		}
	}()

	i := Info{
		goType: nil,
	}
	_ = i.NewProtoInstance()
}

func TestInfo_newProtoInstance_PanicAtNonProto(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Expected panic not found")
		}
	}()

	i := Info{
		goType: reflect.TypeOf(""),
	}
	_ = i.NewProtoInstance()
}

func TestInfo_String(t *testing.T) {
	i := Info{
		Collection: Collection{"a"},
	}
	// Ensure that it doesn't crash
	_ = i.String()
}

func TestFullNameFromNamespaceAndName(t *testing.T) {
	cases := []struct {
		namespace string
		name      string
		want      FullName
	}{
		{
			namespace: "default",
			name:      "foo",
			want:      FullName{string: "default/foo"},
		},
		{
			namespace: "",
			name:      "foo",
			want:      FullName{string: "foo"},
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v]%s", i, c.want), func(tt *testing.T) {
			if got := FullNameFromNamespaceAndName(c.namespace, c.name); got != c.want {
				tt.Errorf("wrong FullName: got: %v want %v", got, c.want)
			}
			gotNamespace, gotName := c.want.InterpretAsNamespaceAndName()
			if gotNamespace != c.namespace {
				tt.Errorf("wrong namespace: got %v want %v", gotNamespace, c.namespace)
			}
			if gotName != c.name {
				tt.Errorf("wrong name: got %v want %v", gotName, c.name)
			}
		})
	}
}

func TestNewTypeURL(t *testing.T) {
	goodurls := []string{
		"type.googleapis.com/a.b.c",
		"type.googleapis.com/a",
		"type.googleapis.com/foo/a.b.c",
		"zoo.com/a.b.c",
		"zoo.com/bar/a.b.c",
		"http://type.googleapis.com/foo/a.b.c",
		"https://type.googleapis.com/foo/a.b.c",
	}

	for _, g := range goodurls {
		t.Run(g, func(t *testing.T) {
			_, err := newTypeURL(g)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
		})
	}

	badurls := []string{
		"ftp://type.googleapis.com/a.b.c",
		"type.googleapis.com/a.b.c/",
		"type.googleapis.com/",
		"type.googleapis.com",
		":zoo:bar/doo",
	}

	for _, g := range badurls {
		t.Run(g, func(t *testing.T) {
			_, err := newTypeURL(g)
			if err == nil {
				t.Fatal("expected error not found")
			}
		})
	}
}
