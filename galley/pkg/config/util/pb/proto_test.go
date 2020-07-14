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

package pb

import (
	"testing"

	gogoTypes "github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"
)

func TestToProto_Success(t *testing.T) {
	g := NewGomegaWithT(t)

	data := map[string]interface{}{
		"foo": "bar",
		"boo": "baz",
	}

	p := &gogoTypes.Struct{}
	err := UnmarshalData(p, data)
	g.Expect(err).To(BeNil())
	expected := &gogoTypes.Struct{
		Fields: map[string]*gogoTypes.Value{
			"foo": {
				Kind: &gogoTypes.Value_StringValue{StringValue: "bar"},
			},
			"boo": {
				Kind: &gogoTypes.Value_StringValue{StringValue: "baz"},
			},
		},
	}

	g.Expect(p).To(Equal(expected))
}

func TestToProto_UnknownFields(t *testing.T) {
	g := NewGomegaWithT(t)

	data := map[string]interface{}{
		"foo": "bar",
		"boo": "baz",
	}

	p := &gogoTypes.Empty{}
	err := UnmarshalData(p, data)
	g.Expect(err).To(BeNil())
	expected := &gogoTypes.Empty{}

	g.Expect(p).To(Equal(expected))
}

func TestToProto_Error(t *testing.T) {
	g := NewGomegaWithT(t)

	data := map[string]interface{}{
		"value": 23,
	}

	p := &gogoTypes.Any{}
	err := UnmarshalData(p, data)
	g.Expect(err).NotTo(BeNil())
}
