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
	"bytes"
	"reflect"
	"strings"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	"github.com/golang/protobuf/jsonpb"
	legacyproto "github.com/golang/protobuf/proto" // nolint: staticcheck
	emptypb "github.com/golang/protobuf/ptypes/empty"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// nonstrictResolver is an AnyResolver that ignores unknown proto messages
type nonstrictResolver struct{}

var envoyResolver nonstrictResolver

func (m *nonstrictResolver) Resolve(typeURL string) (legacyproto.Message, error) {
	// See https://github.com/golang/protobuf/issues/747#issuecomment-437463120
	mname := typeURL
	if slash := strings.LastIndex(typeURL, "/"); slash >= 0 {
		mname = mname[slash+1:]
	}
	// nolint: staticcheck
	mt := legacyproto.MessageType(mname)
	if mt == nil {
		// istioctl should keep going if it encounters new Envoy versions; ignore unknown types
		return &exprpb.Type{TypeKind: &exprpb.Type_Dyn{Dyn: &emptypb.Empty{}}}, nil
	}
	return reflect.New(mt.Elem()).Interface().(legacyproto.Message), nil
}

// Wrapper is a wrapper around the Envoy ConfigDump
// It has extra helper functions for handling any/struct/marshal protobuf pain
type Wrapper struct {
	*adminapi.ConfigDump
}

// MarshalJSON is a custom marshaller to handle protobuf pain
func (w *Wrapper) MarshalJSON() ([]byte, error) {
	buffer := &bytes.Buffer{}
	err := (&jsonpb.Marshaler{}).Marshal(buffer, w)
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// UnmarshalJSON is a custom unmarshaller to handle protobuf pain
func (w *Wrapper) UnmarshalJSON(b []byte) error {
	cd := &adminapi.ConfigDump{}
	err := (&jsonpb.Unmarshaler{
		AllowUnknownFields: true,
		AnyResolver:        &envoyResolver,
	}).Unmarshal(bytes.NewReader(b), cd)
	*w = Wrapper{cd}
	return err
}
