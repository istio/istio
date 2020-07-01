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

package main

import (
	"testing"

	"github.com/onsi/gomega"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/security/authn/utils"
	"istio.io/istio/pilot/pkg/serviceregistry"
)

func TestPilotDefaultDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role = &model.Proxy{}
	role.DNSDomain = ""
	registryID = serviceregistry.Kubernetes

	domain := getDNSDomain("default", role.DNSDomain)

	g.Expect(domain).To(gomega.Equal("default.svc.cluster.local"))
}

func TestPilotDefaultDomainConsul(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role := &model.Proxy{}
	role.DNSDomain = ""
	registryID = serviceregistry.Consul

	domain := getDNSDomain("", role.DNSDomain)

	g.Expect(domain).To(gomega.Equal("service.consul"))
}

func TestPilotDefaultDomainOthers(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role = &model.Proxy{}
	role.DNSDomain = ""
	registryID = serviceregistry.Mock

	domain := getDNSDomain("", role.DNSDomain)

	g.Expect(domain).To(gomega.Equal(""))
}

func TestPilotDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.DNSDomain = "my.domain"
	registryID = serviceregistry.Mock

	domain := getDNSDomain("", role.DNSDomain)

	g.Expect(domain).To(gomega.Equal("my.domain"))
}

func TestCustomMixerSanIfAuthenticationMutualDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role = &model.Proxy{}
	role.DNSDomain = ""
	trustDomain = "mesh.com"
	mixerIdentity = "mixer-identity"
	registryID = serviceregistry.Kubernetes

	setSpiffeTrustDomain("", role.DNSDomain)
	mixerSAN := utils.GetSAN("", mixerIdentity)

	g.Expect(mixerSAN).To(gomega.Equal("spiffe://mesh.com/mixer-identity"))
}

func TestIsIPv6Proxy(t *testing.T) {
	tests := []struct {
		name     string
		addrs    []string
		expected bool
	}{
		{
			name:     "ipv4 only",
			addrs:    []string{"1.1.1.1", "127.0.0.1", "2.2.2.2"},
			expected: false,
		},
		{
			name:     "ipv6 only",
			addrs:    []string{"1111:2222::1", "::1", "2222:3333::1"},
			expected: true,
		},
		{
			name:     "mixed ipv4 and ipv6",
			addrs:    []string{"1111:2222::1", "::1", "127.0.0.1", "2.2.2.2", "2222:3333::1"},
			expected: false,
		},
	}
	for _, tt := range tests {
		result := isIPv6Proxy(tt.addrs)
		if result != tt.expected {
			t.Errorf("Test %s failed, expected: %t got: %t", tt.name, tt.expected, result)
		}
	}
}
