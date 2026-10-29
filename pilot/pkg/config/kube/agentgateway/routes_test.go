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

package agentgateway

import (
	"testing"

	"github.com/agentgateway/agentgateway/api"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test/util/assert"
)

func TestApplyRetries(t *testing.T) {
	for _, tt := range []struct {
		name         string
		retry        *gatewayv1.HTTPRouteRetry
		wantPolicy   bool
		wantAttempts *int32
	}{
		{name: "absent"},
		{name: "omitted attempts", retry: &gatewayv1.HTTPRouteRetry{}, wantPolicy: true},
		{name: "disabled", retry: &gatewayv1.HTTPRouteRetry{Attempts: ptr.Of(0)}},
		{name: "explicit attempts", retry: &gatewayv1.HTTPRouteRetry{Attempts: ptr.Of(3)}, wantPolicy: true, wantAttempts: ptr.Of[int32](3)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			route := &api.Route{}
			assert.NoError(t, ApplyRetries(&gatewayv1.HTTPRouteRule{Retry: tt.retry}, route))
			if !tt.wantPolicy {
				assert.Equal(t, len(route.TrafficPolicies), 0)
				return
			}
			assert.Equal(t, len(route.TrafficPolicies), 1)
			if len(route.TrafficPolicies) != 1 {
				return
			}
			retry := route.TrafficPolicies[0].GetRetry()
			if retry == nil {
				t.Fatal("expected retry policy")
			}
			assert.Equal(t, retry.Attempts, tt.wantAttempts)
		})
	}
}
