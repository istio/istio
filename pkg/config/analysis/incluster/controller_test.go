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
)

func TestLogAnalysisMessages(t *testing.T) {
	origin := diag.MockResource("DestinationRule/default/test-dr")

	tests := []struct {
		name string
		msgs diag.Messages
	}{
		{
			name: "empty messages",
			msgs: diag.Messages{},
		},
		{
			name: "error level message",
			msgs: diag.Messages{
				diag.NewMessage(msg.SchemaValidationError, origin, "field is invalid"),
			},
		},
		{
			name: "warning level message",
			msgs: diag.Messages{
				diag.NewMessage(msg.NoMatchingWorkloadsFound, origin, "app=test"),
			},
		},
		{
			name: "info level message",
			msgs: diag.Messages{
				diag.NewMessage(msg.NamespaceNotInjected, origin, "default", "default"),
			},
		},
		{
			name: "mixed levels",
			msgs: diag.Messages{
				diag.NewMessage(msg.SchemaValidationError, origin, "field is invalid"),
				diag.NewMessage(msg.NoMatchingWorkloadsFound, origin, "app=test"),
				diag.NewMessage(msg.NamespaceNotInjected, origin, "default", "default"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// logAnalysisMessages should not panic for any input
			logAnalysisMessages(tt.msgs)
		})
	}
}

func TestLogAnalysisMessagesNil(t *testing.T) {
	// Passing nil should not panic
	logAnalysisMessages(nil)
}
