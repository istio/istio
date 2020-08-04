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

package event_test

import (
	"testing"

	"istio.io/istio/pkg/config/event"
)

func TestEventKind_String(t *testing.T) {
	tests := map[event.Kind]string{
		event.None:     "None",
		event.Added:    "Added",
		event.Updated:  "Updated",
		event.Deleted:  "Deleted",
		event.FullSync: "FullSync",
		event.Reset:    "Reset",
		55:             "<<Unknown Kind 55>>",
	}

	for i, e := range tests {
		t.Run(e, func(t *testing.T) {
			a := i.String()
			if a != e {
				t.Fatalf("Mismatch: Actual=%v, Expected=%v", a, e)
			}
		})
	}
}
