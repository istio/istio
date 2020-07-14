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

package handler

import (
	"errors"
	"reflect"
	"testing"

	"github.com/gogo/protobuf/proto"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/runtime/config"
)

type fakeProto struct {
	content []byte
}

func (f *fakeProto) Reset()        {}
func (f *fakeProto) ProtoMessage() {}
func (f *fakeProto) String() string {
	return "Fake!"
}
func (f *fakeProto) Marshal() ([]byte, error) {
	if f.content == nil {
		return nil, errors.New("fake marshaling error")
	}
	return f.content, nil
}

var _ proto.Message = &fakeProto{}
var _ proto.Marshaler = &fakeProto{}

func TestSignature_ProtoMarshalFailure(t *testing.T) {
	p := &fakeProto{}

	h := config.HandlerStatic{Name: "h1", Params: p, Adapter: &adapter.Info{Name: "a1"}}
	s := calculateSignature(&h, []interface{}{})

	// The fake proto will fail serialization. It should return zero-signature.
	if !reflect.DeepEqual(s, zeroSignature) {
		t.Fail()
	}
}

func TestSignature_ZeroSignature(t *testing.T) {

	if zeroSignature.equals(zeroSignature) {
		t.Fail()
	}

	otherSignature := signature{} // zero signature as well

	if zeroSignature.equals(otherSignature) || otherSignature.equals(zeroSignature) {
		t.Fail()
	}
	if otherSignature.equals(otherSignature) {
		t.Fail()
	}

	// non-zero signature
	otherSignature[1] = 1

	if !otherSignature.equals(otherSignature) {
		t.Fail()
	}
}
