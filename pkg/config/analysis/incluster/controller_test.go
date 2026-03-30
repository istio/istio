/*
 Copyright Istio Authors

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package incluster

import (
	"testing"

	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/util/sets"
)

func TestLogNewAnalysisMessages(t *testing.T) {
	origin := diag.MockResource("DestinationRule/default/test-dr")

	t.Run("empty messages", func(t *testing.T) {
		result := logNewAnalysisMessages(diag.Messages{}, sets.New[string]())
		if result.Len() != 0 {
			t.Errorf("expected empty set, got %d entries", result.Len())
		}
	})

	t.Run("nil messages", func(t *testing.T) {
		result := logNewAnalysisMessages(nil, sets.New[string]())
		if result.Len() != 0 {
			t.Errorf("expected empty set, got %d entries", result.Len())
		}
	})

	t.Run("nil previous set", func(t *testing.T) {
		msgs := diag.Messages{
			diag.NewMessage(msg.SchemaValidationError, origin, "field is invalid"),
		}
		result := logNewAnalysisMessages(msgs, nil)
		if result.Len() != 1 {
			t.Errorf("expected 1 entry, got %d", result.Len())
		}
	})

	t.Run("new messages are logged and returned", func(t *testing.T) {
		msgs := diag.Messages{
			diag.NewMessage(msg.SchemaValidationError, origin, "field is invalid"),
			diag.NewMessage(msg.NoMatchingWorkloadsFound, origin, "app=test"),
		}
		result := logNewAnalysisMessages(msgs, sets.New[string]())
		if result.Len() != 2 {
			t.Errorf("expected 2 entries, got %d", result.Len())
		}
		for _, m := range msgs {
			if !result.Contains(m.String()) {
				t.Errorf("expected set to contain %q", m.String())
			}
		}
	})

	t.Run("duplicate messages are not re-logged", func(t *testing.T) {
		msgs := diag.Messages{
			diag.NewMessage(msg.SchemaValidationError, origin, "field is invalid"),
		}
		// First cycle: message is new
		first := logNewAnalysisMessages(msgs, sets.New[string]())
		// Second cycle: same message should be deduplicated (no panic, returns same set)
		second := logNewAnalysisMessages(msgs, first)
		if second.Len() != 1 {
			t.Errorf("expected 1 entry, got %d", second.Len())
		}
	})

	t.Run("resolved messages are removed from set", func(t *testing.T) {
		msgs := diag.Messages{
			diag.NewMessage(msg.SchemaValidationError, origin, "field is invalid"),
		}
		first := logNewAnalysisMessages(msgs, sets.New[string]())
		// Second cycle: issue resolved (empty messages)
		second := logNewAnalysisMessages(diag.Messages{}, first)
		if second.Len() != 0 {
			t.Errorf("expected empty set after resolution, got %d", second.Len())
		}
	})

	t.Run("mixed new and existing messages", func(t *testing.T) {
		existingMsg := diag.NewMessage(msg.SchemaValidationError, origin, "field is invalid")
		newMsg := diag.NewMessage(msg.NoMatchingWorkloadsFound, origin, "app=test")

		previous := sets.New[string]()
		previous.Insert(existingMsg.String())

		msgs := diag.Messages{existingMsg, newMsg}
		result := logNewAnalysisMessages(msgs, previous)
		if result.Len() != 2 {
			t.Errorf("expected 2 entries, got %d", result.Len())
		}
	})
}
