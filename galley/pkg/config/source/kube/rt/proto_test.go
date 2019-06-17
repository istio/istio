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

package rt

import (
	"reflect"
	"testing"

	gogoTypes "github.com/gogo/protobuf/types"

	"istio.io/istio/galley/pkg/config/collection"
)

func TestToProto_Success(t *testing.T) {
	spec := map[string]interface{}{}

	s, err := collection.NewSpec("foo", "", "google.protobuf.Empty")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	p, err := toProto(s, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var expected = &gogoTypes.Empty{}
	if !reflect.DeepEqual(p, expected) {
		t.Fatalf("Mismatch\nExpected:\n%+v\nActual:\n%+v\n", expected, p)
	}
}

func TestToProto_Error(t *testing.T) {
	spec := map[string]interface{}{
		"value": 23,
	}

	s, _ := collection.NewSpec("foo", "", "google.protobuf.Any")

	_, err := toProto(s, spec)
	if err == nil {
		t.Fatalf("expected error not found")
	}
}
