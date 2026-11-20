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

	k8s "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pkg/test/util/assert"
)

func TestBackendTLSSubjectAltNames(t *testing.T) {
	cases := []struct {
		name       string
		validation k8s.BackendTLSPolicyValidation
		want       []string
	}{
		{
			name:       "hostname authenticates the backend",
			validation: k8s.BackendTLSPolicyValidation{Hostname: "auth.example.com"},
			want:       []string{"auth.example.com"},
		},
		{
			name: "explicit names replace the hostname",
			validation: k8s.BackendTLSPolicyValidation{
				Hostname: "auth.example.com",
				SubjectAltNames: []k8s.SubjectAltName{
					{Type: k8s.HostnameSubjectAltNameType, Hostname: "other.example.com"},
					{Type: k8s.URISubjectAltNameType, URI: "spiffe://example.com/backend"},
				},
			},
			want: []string{"other.example.com", "spiffe://example.com/backend"},
		},
		{
			// XBackend may omit the hostname.
			name:       "no hostname",
			validation: k8s.BackendTLSPolicyValidation{},
			want:       nil,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, backendTLSSubjectAltNames(tt.validation), tt.want)
		})
	}
}
