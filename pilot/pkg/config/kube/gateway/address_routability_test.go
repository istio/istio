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

package gateway

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pkg/test/util/assert"
)

func TestManualAddressesAssigned(t *testing.T) {
	hostType := k8s.HostnameAddressType
	hostEntry := k8s.GatewaySpecAddress{Type: &hostType, Value: "svc"}
	cases := []struct {
		name       string
		addrs      []k8s.GatewaySpecAddress
		internal   []string
		pending    []string
		warnings   []string
		reason     string // empty: condition omitted
		status     metav1.ConditionStatus
		programmed string
	}{
		{name: "pending", addrs: []k8s.GatewaySpecAddress{hostEntry}, internal: []string{"svc:80"}, pending: []string{"svc"}, warnings: []string{"address pending"}},
		{
			name: "hostname with routability is unsatisfied",
			addrs: []k8s.GatewaySpecAddress{
				{Type: &hostType, Value: "svc", Routability: k8s.GatewayAddressRoutabilityCluster},
			},
			reason: "NotAssigned", status: metav1.ConditionFalse, programmed: "AddressNotAssigned",
		},
		{
			name: "one of two hostnames has routability",
			addrs: []k8s.GatewaySpecAddress{
				hostEntry, {Type: &hostType, Value: "other", Routability: k8s.GatewayAddressRoutabilityCluster},
			},
			internal: []string{"svc:80"}, reason: "PartiallyAssigned", status: metav1.ConditionFalse,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, progErr := manualAddressesAssigned(tt.addrs, tt.internal, tt.pending, tt.warnings)
			if tt.reason == "" {
				assert.Equal(t, got == nil, true)
			} else {
				assert.Equal(t, got.Reason, tt.reason)
				assert.Equal(t, got.Status, tt.status)
			}
			if tt.programmed == "" {
				assert.Equal(t, progErr == nil, true)
			} else {
				assert.Equal(t, progErr.Reason, tt.programmed)
			}
		})
	}
}
