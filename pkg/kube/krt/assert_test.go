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

package krt

import (
	"testing"

	"istio.io/istio/pkg/kube/controllers"
)

// TestAssertState logs the state of KRT test assertions. In the future we may wish to make this fail if tests are being run without strict assertions
func TestAssertState(t *testing.T) {
	state := "disabled"
	if EnableAssertions {
		state = "enabled"
	}
	t.Logf("krt test assertions are %s.", state)
}

func TestAssertMergeJoinRefreshesStaleEventAsAdd(t *testing.T) {
	const key = "key"
	source := NewStaticCollection[string](nil, []string{key}, WithStop(t.Context().Done()))
	joined := &mergejoin[string]{
		collectionName: "test",
		collections:    collections[string]{source},
		log:            log.WithLabels("owner", "test"),
		outputs:        map[Key[string]]string{},
		merge:          func(values []string) *string { return &values[0] },
	}

	// Model an event that became stale in the queue: the source contains the
	// object now, while the joined output does not contain it yet.
	old := key
	current := key
	events := joined.refreshEventsLocked([]Event[string]{
		{Event: controllers.EventUpdate, Old: &old, New: &current},
	})

	if len(events) != 1 || events[0].Event != controllers.EventAdd || events[0].Old != nil ||
		events[0].New == nil || *events[0].New != current {
		t.Fatalf("expected stale event to become an add, got %+v", events)
	}
}
