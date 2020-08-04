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
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/golang/protobuf/ptypes/wrappers"

	"istio.io/istio/pkg/proto"
)

func BoolToProtoBool(gogo *types.BoolValue) *wrappers.BoolValue {
	if gogo == nil {
		return nil
	}
	if gogo.Value {
		return proto.BoolTrue
	}
	return proto.BoolFalse
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
