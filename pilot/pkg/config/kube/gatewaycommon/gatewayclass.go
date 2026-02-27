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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pilot/pkg/model/kstatus"
)

// GetClassStatus returns the status for a GatewayClass
func GetClassStatus(existing *k8sv1.GatewayClassStatus, gen int64) *k8sv1.GatewayClassStatus {
	if existing == nil {
		existing = &k8sv1.GatewayClassStatus{}
	}
	existing.Conditions = kstatus.UpdateConditionIfChanged(existing.Conditions, metav1.Condition{
		Type:               string(k8sv1.GatewayClassConditionStatusAccepted),
		Status:             kstatus.StatusTrue,
		ObservedGeneration: gen,
		LastTransitionTime: metav1.Now(),
		Reason:             string(k8sv1.GatewayClassConditionStatusAccepted),
		Message:            "Handled by Istio controller",
	})
	return existing
}
