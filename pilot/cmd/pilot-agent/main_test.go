// Copyright 2018 Istio Authors
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
	"os"
	"testing"

	"github.com/onsi/gomega"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy"
	"istio.io/istio/pilot/pkg/serviceregistry"
)

func TestNoPilotSanIfAuthenticationNone(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.DNSDomain = ""
	role.TrustDomain = ""
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_NONE.String()

	setSpiffeTrustDomain(role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.BeNil())
}

func TestPilotSanIfAuthenticationMutualDomainEmptyKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.DNSDomain = ""
	role.TrustDomain = ""
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://cluster.local/ns/anything/sa/istio-pilot-service-account"}))
}

func TestPilotSanIfAuthenticationMutualDomainNotEmptyKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.DNSDomain = "my.domain"
	role.TrustDomain = ""
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://my.domain/ns/anything/sa/istio-pilot-service-account"}))
}

// This test is used to ensure that the former behavior is unchanged
// When pilot is started without a trust domain, the SPIFFE URI doesn't contain a host and is not valid
func TestPilotSanIfAuthenticationMutualDomainEmptyConsul(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.DNSDomain = ""
	role.TrustDomain = ""
	registry = serviceregistry.ConsulRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe:///ns/anything/sa/istio-pilot-service-account"}))
}

func TestPilotSanIfAuthenticationMutualTrustDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.DNSDomain = ""
	role.TrustDomain = "secured"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://secured/ns/anything/sa/istio-pilot-service-account"}))
}

func TestPilotSanIfAuthenticationMutualTrustDomainAndDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.DNSDomain = "my.domain"
	role.TrustDomain = "secured"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://secured/ns/anything/sa/istio-pilot-service-account"}))
}

func TestPilotDefaultDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.DNSDomain = ""
	registry = serviceregistry.KubernetesRegistry
	os.Setenv("POD_NAMESPACE", "default")

	domain := getDNSDomain(role.DNSDomain)

	g.Expect(domain).To(gomega.Equal("default.svc.cluster.local"))
	os.Unsetenv("POD_NAMESPACE")
}

func TestPilotDefaultDomainConsul(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.DNSDomain = ""
	registry = serviceregistry.ConsulRegistry

	domain := getDNSDomain(role.DNSDomain)

	g.Expect(domain).To(gomega.Equal("service.consul"))
}

func TestPilotDefaultDomainOthers(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.DNSDomain = ""
	registry = serviceregistry.MockRegistry

	domain := getDNSDomain(role.DNSDomain)

	g.Expect(domain).To(gomega.Equal(""))
}

func TestPilotDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.DNSDomain = "my.domain"
	registry = serviceregistry.MockRegistry

	domain := getDNSDomain(role.DNSDomain)

	g.Expect(domain).To(gomega.Equal("my.domain"))
}

func TestPilotSanIfAuthenticationMutualStdDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.DNSDomain = ".svc.cluster.local"
	role.TrustDomain = ""
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://cluster.local/ns/anything/sa/istio-pilot-service-account"}))
}

// This test is used to ensure that the former behavior is unchanged
// When pilot is started without a trust domain, the SPIFFE URI doesn't contain a host and is not valid
func TestPilotSanIfAuthenticationMutualStdDomainConsul(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.DNSDomain = "service.consul"
	role.TrustDomain = ""
	registry = serviceregistry.ConsulRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe:///ns/anything/sa/istio-pilot-service-account"}))
}

func TestCustomPilotSanIfAuthenticationMutualDomainKubernetesNoTrustDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.DNSDomain = ""
	role.PilotIdentity = "pilot-identity"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://cluster.local/pilot-identity"}))
}

func TestCustomPilotSanIfAuthenticationMutualDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.DNSDomain = ""
	role.TrustDomain = "mesh.com"
	role.PilotIdentity = "pilot-identity"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://mesh.com/pilot-identity"}))
}

func TestCustomMixerSanIfAuthenticationMutualDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.DNSDomain = ""
	role.TrustDomain = "mesh.com"
	role.MixerIdentity = "mixer-identity"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(role.DNSDomain)
	mixerSAN := envoy.GetSAN("", role.MixerIdentity)

	g.Expect(mixerSAN).To(gomega.Equal("spiffe://mesh.com/mixer-identity"))
}

func TestDedupeStrings(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	in := []string{
		model.DefaultCertChain, model.DefaultKey, model.DefaultRootCert,
		model.DefaultCertChain, model.DefaultKey, model.DefaultRootCert,
	}
	expected := []string{model.DefaultCertChain, model.DefaultKey, model.DefaultRootCert}

	actual := dedupeStrings(in)

	g.Expect(actual).To(gomega.ConsistOf(expected))
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
