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

package statusqueue

import (
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/test/util/assert"
)

func TestTranslateToPatch(t *testing.T) {
	typ := model.WaypointBound
	cond := model.Condition{
		ObservedGeneration: 0,
		Reason:             "something",
		Message:            "hello",
		Status:             true,
	}
	tests := []struct {
		name       string
		desired    model.ConditionSet
		cur        map[string]model.Condition
		wantUpdate bool
	}{
		{
			name: "both no status",
			desired: map[model.ConditionType]*model.Condition{
				typ: nil,
			},
			cur:        map[string]model.Condition{},
			wantUpdate: false,
		},
		{
			name: "first status write",
			desired: map[model.ConditionType]*model.Condition{
				typ: &cond,
			},
			cur:        map[string]model.Condition{},
			wantUpdate: true,
		},
		{
			name: "status remove",
			desired: map[model.ConditionType]*model.Condition{
				typ: nil,
			},
			cur:        map[string]model.Condition{string(typ): cond},
			wantUpdate: true,
		},
		{
			name: "status unchanged",
			desired: map[model.ConditionType]*model.Condition{
				typ: &cond,
			},
			cur:        map[string]model.Condition{string(typ): cond},
			wantUpdate: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := model.TypedObject{Kind: kind.Service}
			got := translateToPatch(obj, tt.desired, tt.cur)
			assert.Equal(t, got != nil, tt.wantUpdate)
		})
	}
}
