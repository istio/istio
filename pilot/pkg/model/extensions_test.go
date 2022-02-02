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

package model

import (
	"testing"

	"istio.io/istio/pilot/pkg/model/credentials"
)

func TestToSecretName(t *testing.T) {
	cases := []struct {
		name      string
		namespace string
		want      string
	}{
		{
			name:      "sec",
			namespace: "nm",
			want:      credentials.KubernetesSecretTypeURI + "nm/sec",
		},
		{
			name:      "nm/sec",
			namespace: "nm",
			want:      credentials.KubernetesSecretTypeURI + "nm/sec",
		},
		{
			name:      "nm2/sec",
			namespace: "nm",
			// This is invalid secret resource, which should result in secret not found and distributed.
			want: credentials.KubernetesSecretTypeURI + "nm/nm2/sec",
		},
		{
			name:      credentials.KubernetesSecretTypeURI + "nm/sec",
			namespace: "nm",
			want:      credentials.KubernetesSecretTypeURI + "nm/sec",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := toSecretResourceName(tt.name, tt.namespace)
			if got != tt.want {
				t.Errorf("got secret name %q, want %q", got, tt.want)
			}
		})
	}
}
