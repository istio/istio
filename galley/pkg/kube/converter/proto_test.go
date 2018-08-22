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

package converter

import (
	"reflect"
	"testing"

	"github.com/gogo/protobuf/types"

	"istio.io/istio/galley/pkg/runtime/resource"
)

func TestToProto(t *testing.T) {
	spec := map[string]interface{}{}

	s := resource.NewSchema()
	s.Register("type.googleapis.com/google.protobuf.Empty", true)
	i, _ := s.Lookup("type.googleapis.com/google.protobuf.Empty")

	p, err := toProto(i, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var expected = &types.Empty{}
	if !reflect.DeepEqual(p, expected) {
		t.Fatalf("Mismatch\nExpected:\n%+v\nActual:\n%+v\n", expected, p)
	}
}
