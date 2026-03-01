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

package gatewaycommon

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gateway "sigs.k8s.io/gateway-api/apis/v1"
)

func TestGetClassStatus(t *testing.T) {
	status := GetClassStatus(nil, 1)
	if status == nil {
		t.Fatal("GetClassStatus returned nil")
	}
	if len(status.Conditions) == 0 {
		t.Fatal("GetClassStatus returned no conditions")
	}

	// Check that the condition is set correctly
	found := false
	for _, c := range status.Conditions {
		if c.Type == string(gateway.GatewayClassConditionStatusAccepted) {
			found = true
			if c.Status != metav1.ConditionTrue {
				t.Errorf("expected condition status True, got %v", c.Status)
			}
			if c.ObservedGeneration != 1 {
				t.Errorf("expected observed generation 1, got %v", c.ObservedGeneration)
			}
		}
	}
	if !found {
		t.Error("Accepted condition not found")
	}
}
