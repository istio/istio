// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"testing"
	"time"

	"istio.io/istio/pkg/test/util/assert"
)

func TestParseReconcileHostRulesInterval(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		expected time.Duration
	}{
		{"empty falls back to default", "", defaultReconcileHostRulesInterval},
		{"valid interval", "1m30s", 90 * time.Second},
		{"zero disables", "0", 0},
		{"zero seconds disables", "0s", 0},
		{"negative disables", "-5s", 0},
		{"invalid falls back to default", "banana", defaultReconcileHostRulesInterval},
		{"missing unit falls back to default", "30", defaultReconcileHostRulesInterval},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseReconcileHostRulesInterval(tt.raw))
		})
	}
}
