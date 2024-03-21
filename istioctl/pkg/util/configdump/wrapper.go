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
	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	emptypb "github.com/golang/protobuf/ptypes/empty"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"istio.io/istio/pkg/util/protomarshal"
)

type resolver struct {
	*protoregistry.Types
}

var nonStrictResolver = &resolver{protoregistry.GlobalTypes}

func (r *resolver) FindMessageByURL(url string) (protoreflect.MessageType, error) {
	typ, err := r.Types.FindMessageByURL(url)
	if err != nil {
		// Here we ignore the error since we want istioctl to ignore unknown types due to the Envoy version change
		msg := exprpb.Type{TypeKind: &exprpb.Type_Dyn{Dyn: &emptypb.Empty{}}}
		return msg.ProtoReflect().Type(), nil
	}
	return typ, nil
}

// Wrapper is a wrapper around the Envoy ConfigDump
// It has extra helper functions for handling any/struct/marshal protobuf pain
type Wrapper struct {
	*admin.ConfigDump
}

// UnmarshalJSON is a custom unmarshaller to handle protobuf pain
func (w *Wrapper) UnmarshalJSON(b []byte) error {
	cd := &admin.ConfigDump{}
	err := protomarshal.UnmarshalAllowUnknownWithAnyResolver(nonStrictResolver, b, cd)
	*w = Wrapper{cd}
	return err
}
