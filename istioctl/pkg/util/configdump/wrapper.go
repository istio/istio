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
	"strings"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	legacyproto "github.com/golang/protobuf/proto" // nolint: staticcheck
	emptypb "github.com/golang/protobuf/ptypes/empty"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"istio.io/istio/pkg/util/protomarshal"
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
	mt, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(mname))
	if err != nil {
		// istioctl should keep going if it encounters new Envoy versions; ignore unknown types
		return &exprpb.Type{TypeKind: &exprpb.Type_Dyn{Dyn: &emptypb.Empty{}}}, nil
	}
	return legacyproto.MessageV1(mt.New().Interface()), nil
}

// Wrapper is a wrapper around the Envoy ConfigDump
// It has extra helper functions for handling any/struct/marshal protobuf pain
type Wrapper struct {
	*admin.ConfigDump
}

// MarshalJSON is a custom marshaler to handle protobuf pain
func (w *Wrapper) MarshalJSON() ([]byte, error) {
	return protomarshal.Marshal(w)
}

// UnmarshalJSON is a custom unmarshaler to handle protobuf pain
func (w *Wrapper) UnmarshalJSON(b []byte) error {
	cd := &admin.ConfigDump{}
	err := protomarshal.UnmarshalAllowUnknownWithAnyResolver(&envoyResolver, b, cd)
	*w = Wrapper{cd}
	return err
}
