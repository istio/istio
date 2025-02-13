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

package protoconv

import (
	"fmt"

	udpa "github.com/cncf/xds/go/udpa/type/v1"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/log"
)

// MessageToAnyWithError converts from proto message to proto Any
func MessageToAnyWithError(msg proto.Message) (*anypb.Any, error) {
	b, err := marshal(msg)
	if err != nil {
		return nil, err
	}
	return &anypb.Any{
		// nolint: staticcheck
		TypeUrl: "type.googleapis.com/" + string(msg.ProtoReflect().Descriptor().FullName()),
		Value:   b,
	}, nil
}

func marshal(msg proto.Message) ([]byte, error) {
	if features.EnableVtprotobuf {
		if vt, ok := msg.(vtStrictMarshal); ok {
			// Attempt to use more efficient implementation
			// "Strict" is the equivalent to Deterministic=true below
			return vt.MarshalVTStrict()
		}
	}
	// If not available, fallback to normal implementation
	return proto.MarshalOptions{Deterministic: true}.Marshal(msg)
}

// MessageToAny converts from proto message to proto Any
func MessageToAny(msg proto.Message) *anypb.Any {
	out, err := MessageToAnyWithError(msg)
	if err != nil {
		log.Error(fmt.Sprintf("error marshaling Any %s: %v", prototext.Format(msg), err))
		return nil
	}
	return out
}

func TypedStructWithFields(typeURL string, fields map[string]interface{}) *anypb.Any {
	value, err := structpb.NewStruct(fields)
	if err != nil {
		log.Error(fmt.Sprintf("error marshaling struct %s: %v", typeURL, err))
	}
	return MessageToAny(&udpa.TypedStruct{
		TypeUrl: typeURL,
		Value:   value,
	})
}

func SilentlyUnmarshalAny[T any](a *anypb.Any) *T {
	res, err := UnmarshalAny[T](a)
	if err != nil {
		return nil
	}
	return res
}

func UnmarshalAny[T any](a *anypb.Any) (*T, error) {
	dst := any(new(T)).(proto.Message)
	if err := a.UnmarshalTo(dst); err != nil {
		return nil, fmt.Errorf("failed to unmarshal to %T: %v", dst, err)
	}
	return any(dst).(*T), nil
}

// https://github.com/planetscale/vtprotobuf#available-features
type vtStrictMarshal interface {
	MarshalVTStrict() ([]byte, error)
}

type vtEquals[T proto.Message] interface {
	EqualVT(T) bool
}

// Equals checks if two message are equal.
// This is preferred to proto.Equals since it can use vtprotobuf implementations and is more type safe
func Equals[T proto.Message](a T, b T) bool {
	if features.EnableVtprotobuf {
		if vt, ok := any(a).(vtEquals[T]); ok {
			return vt.EqualVT(b)
		}
	}
	// If not available, fallback to normal implementation
	return proto.Equal(a, b)
}
