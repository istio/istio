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

package configdump

import (
	"reflect"
	"strings"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/runtime/protoimpl"
	"google.golang.org/protobuf/types/known/emptypb"
)

// nonstrictResolver is an AnyResolver that ignores unknown proto messages
type nonstrictResolver struct{}

var envoyResolver nonstrictResolver

func (m nonstrictResolver) Resolve(typeURL string) (proto.Message, error) {
	// See https://github.com/golang/protobuf/issues/747#issuecomment-437463120
	mname := typeURL
	if slash := strings.LastIndex(typeURL, "/"); slash >= 0 {
		mname = mname[slash+1:]
	}
	// nolint: staticcheck
	mt, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(mname))
	if err != nil {
		// istioctl should keep going if it encounters new Envoy versions; ignore unknown types
		return &exprpb.Type{TypeKind: &exprpb.Type_Dyn{Dyn: &emptypb.Empty{}}}, nil
	}
	return reflect.New(reflect.TypeOf(mt.Zero().Interface()).Elem()).Interface().(proto.Message), nil
}

func (m nonstrictResolver) FindMessageByName(message protoreflect.FullName) (protoreflect.MessageType, error) {
	return m.FindMessageByURL(string(message))
}

func (m nonstrictResolver) FindMessageByURL(url string) (protoreflect.MessageType, error) {
	mx, err := m.Resolve(url)
	if err != nil {
		return nil, err
	}
	return protoimpl.X.MessageTypeOf(mx), nil
}

func (m nonstrictResolver) FindExtensionByName(field protoreflect.FullName) (protoreflect.ExtensionType, error) {
	return protoregistry.GlobalTypes.FindExtensionByName(field)
}

func (m nonstrictResolver) FindExtensionByNumber(message protoreflect.FullName, field protoreflect.FieldNumber) (protoreflect.ExtensionType, error) {
	return protoregistry.GlobalTypes.FindExtensionByNumber(message, field)
}

// Wrapper is a wrapper around the Envoy ConfigDump
// It has extra helper functions for handling any/struct/marshal protobuf pain
type Wrapper struct {
	*adminapi.ConfigDump
}

// MarshalJSON is a custom marshaller to handle protobuf pain
func (w *Wrapper) MarshalJSON() ([]byte, error) {
	return protojson.Marshal(w)
}

// UnmarshalJSON is a custom unmarshaller to handle protobuf pain
func (w *Wrapper) UnmarshalJSON(b []byte) error {
	cd := &adminapi.ConfigDump{}
	err := (&protojson.UnmarshalOptions{
		DiscardUnknown: true,
		Resolver:       envoyResolver,
	}).Unmarshal(b, cd)
	*w = Wrapper{cd}
	return err
}
