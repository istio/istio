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
	"reflect"
	"testing"

	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/golang/protobuf/ptypes/wrappers"

	"istio.io/istio/pkg/proto"
)

func TestBoolToProtoBool(t *testing.T) {
	tests := []struct {
		desc          string
		gogo          *types.BoolValue
		expectedValue *wrappers.BoolValue
	}{
		{
			desc:          "BoolToProtoBool with nil gogo",
			gogo:          nil,
			expectedValue: nil,
		},
		{
			desc: "BoolToProtoBool with true gogo.Value",
			gogo: &types.BoolValue{
				Value: true,
			},
			expectedValue: proto.BoolTrue,
		},
		{
			desc: "BoolToProtoBool with false gogo.Value",
			gogo: &types.BoolValue{
				Value: false,
			},
			expectedValue: proto.BoolFalse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := BoolToProtoBool(tt.gogo); got != tt.expectedValue {
				t.Errorf("%s: got: %v, expected: %v", tt.desc, got, tt.expectedValue)
			}
		})
	}
}

func TestDurationToProtoDuration(t *testing.T) {
	tests := []struct {
		desc          string
		gogo          *types.Duration
		expectedValue *duration.Duration
	}{
		{
			desc:          "DurationToProtoDuration with nil gogo",
			gogo:          nil,
			expectedValue: nil,
		},
		{
			desc: "DurationToProtoDuration with valid Seconds and Nanos",
			gogo: &types.Duration{
				Seconds: 1000000,
				Nanos:   100000,
			},
			expectedValue: &duration.Duration{
				Seconds: 1000000,
				Nanos:   100000,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := DurationToProtoDuration(tt.gogo); !reflect.DeepEqual(got, tt.expectedValue) {
				t.Errorf("%s: got: %v, expected: %v", tt.desc, got, tt.expectedValue)
			}
		})
	}
}
