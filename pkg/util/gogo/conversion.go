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

package gogo

import (
	"fmt"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/golang/protobuf/ptypes/wrappers"

	iproto "istio.io/istio/pkg/proto"
	"istio.io/pkg/log"
)

// MessageToAnyWithError converts from proto message to proto Any
func MessageToAnyWithError(msg proto.Message) (*any.Any, error) {
	b := proto.NewBuffer(nil)
	err := b.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return &any.Any{
		// nolint: staticcheck
		TypeUrl: "type.googleapis.com/" + proto.MessageName(msg),
		Value:   b.Bytes(),
	}, nil
}

// MessageToAny converts from proto message to proto Any
func MessageToAny(msg proto.Message) *any.Any {
	out, err := MessageToAnyWithError(msg)
	if err != nil {
		log.Error(fmt.Sprintf("error marshaling Any %s: %v", msg.String(), err))
		return nil
	}
	return out
}

func ConvertAny(golang *any.Any) *types.Any {
	if golang == nil {
		return nil
	}
	return &types.Any{Value: golang.Value, TypeUrl: golang.TypeUrl}
}

func BoolToProtoBool(gogo *types.BoolValue) *wrappers.BoolValue {
	if gogo == nil {
		return nil
	}
	if gogo.Value {
		return iproto.BoolTrue
	}
	return iproto.BoolFalse
}

func DurationToProtoDuration(gogo *types.Duration) *duration.Duration {
	if gogo == nil {
		return nil
	}
	return &duration.Duration{
		Seconds: gogo.Seconds,
		Nanos:   gogo.Nanos,
	}
}
