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
	"encoding/json"

	"google.golang.org/protobuf/types/known/timestamppb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/meta/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/slices"
)

type statusConditions struct {
	metav1.TypeMeta
	// TODO(https://github.com/kubernetes/kubernetes/issues/126726) drop this
	metav1.ObjectMeta `json:"metadata"`
	Status            conditions `json:"status"`
}

type conditions struct {
	Conditions []any `json:"conditions"`
}

// translateToPatch converts a ConditionSet to a patch
// The ConditionSet is expected to contain all Types owned by this controller; if they should be unset, they should nil.
// Failure to do so means the status may not properly get pruned.
// currentConditions must be set to the current conditions. This is used to determine if we need a write.
// Note: we do not simply rely on SSA to tell us if a change is needed, because every write does a mutation due to LastTransitionTime.
func translateToPatch(object model.TypedObject, status model.ConditionSet, currentConditions map[string]model.Condition) []byte {
	conds := make([]any, 0, len(status))

	changesNeeded := false

	for t, v := range status {
		nowExists := v != nil
		cc, curExists := currentConditions[string(t)]
		if nowExists != curExists {
			changesNeeded = true
		}
		if !nowExists {
			continue
		}
		if !cc.Equals(v) {
			changesNeeded = true
		}

		s := string(metav1.ConditionFalse)
		if v.Status {
			s = string(metav1.ConditionTrue)
		}
		conds = append(conds, &v1alpha1.IstioCondition{
			Type:               string(t),
			Status:             s,
			LastTransitionTime: timestamppb.Now(),
			Reason:             v.Reason,
			Message:            v.Message,
			ObservedGeneration: v.ObservedGeneration,
		})
	}
	if !changesNeeded {
		return nil
	}
	// TODO: it would be nice to have a more direct kind -> GVK mapping
	s := slices.FindFunc(collections.All.All(), func(schema resource.Schema) bool {
		return schema.Kind() == object.Kind.String()
	})
	res, _ := json.Marshal(statusConditions{
		TypeMeta: metav1.TypeMeta{
			Kind:       object.Kind.String(),
			APIVersion: (*s).APIVersion(),
		},
		ObjectMeta: metav1.ObjectMeta{Name: object.Name},
		Status:     conditions{Conditions: conds},
	})
	return res
}
