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

	"istio.io/istio/pilot/pkg/model"

	"github.com/onsi/gomega"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/proxy/envoy"
	"istio.io/istio/pilot/pkg/serviceregistry"
)

func TestNoPilotSanIfAuthenticationNone(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	DNSDomain = ""
	role.TrustDomain = ""
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_NONE.String()

	setSpiffeTrustDomain(DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.BeNil())
}

func TestPilotSanIfAuthenticationMutualDomainEmptyKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	DNSDomain = ""
	role.TrustDomain = ""
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://cluster.local/ns/anything/sa/istio-pilot-service-account"}))
}

func TestPilotSanIfAuthenticationMutualDomainNotEmptyKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	DNSDomain = "my.domain"
	role.TrustDomain = ""
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://my.domain/ns/anything/sa/istio-pilot-service-account"}))
}

// This test is used to ensure that the former behavior is unchanged
// When pilot is started without a trust domain, the SPIFFE URI doesn't contain a host and is not valid
func TestPilotSanIfAuthenticationMutualDomainEmptyConsul(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	DNSDomain = ""
	role.TrustDomain = ""
	registry = serviceregistry.ConsulRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe:///ns/anything/sa/istio-pilot-service-account"}))
}

func TestPilotSanIfAuthenticationMutualTrustDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	DNSDomain = ""
	role.TrustDomain = "secured"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://secured/ns/anything/sa/istio-pilot-service-account"}))
}

func TestPilotSanIfAuthenticationMutualTrustDomainAndDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	DNSDomain = "my.domain"
	role.TrustDomain = "secured"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://secured/ns/anything/sa/istio-pilot-service-account"}))
}

func TestPilotDefaultDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	DNSDomain = ""
	registry = serviceregistry.KubernetesRegistry
	os.Setenv("POD_NAMESPACE", "default")

	domain := getDNSDomain(DNSDomain)

	g.Expect(domain).To(gomega.Equal("default.svc.cluster.local"))
	os.Unsetenv("POD_NAMESPACE")
}

func TestPilotDefaultDomainConsul(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	registry = serviceregistry.ConsulRegistry
	DNSDomain = ""

	domain := getDNSDomain(DNSDomain)

	g.Expect(domain).To(gomega.Equal("service.consul"))
}

func TestPilotDefaultDomainOthers(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	DNSDomain = ""
	registry = serviceregistry.MockRegistry

	domain := getDNSDomain(DNSDomain)

	g.Expect(domain).To(gomega.Equal(""))
}

func TestPilotDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	DNSDomain = "my.domain"
	registry = serviceregistry.MockRegistry

	domain := getDNSDomain(DNSDomain)

	g.Expect(domain).To(gomega.Equal("my.domain"))
}

func TestPilotSanIfAuthenticationMutualStdDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	DNSDomain = ".svc.cluster.local"
	role.TrustDomain = ""
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://cluster.local/ns/anything/sa/istio-pilot-service-account"}))
}

// This test is used to ensure that the former behavior is unchanged
// When pilot is started without a trust domain, the SPIFFE URI doesn't contain a host and is not valid
func TestPilotSanIfAuthenticationMutualStdDomainConsul(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	DNSDomain = "service.consul"
	role.TrustDomain = ""
	registry = serviceregistry.ConsulRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe:///ns/anything/sa/istio-pilot-service-account"}))
}

func TestCustomPilotSanIfAuthenticationMutualDomainKubernetesNoTrustDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	DNSDomain = ""
	role.PilotIdentity = "pilot-identity"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://cluster.local/pilot-identity"}))
}

func TestCustomPilotSanIfAuthenticationMutualDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	DNSDomain = ""
	role.TrustDomain = "mesh.com"
	role.PilotIdentity = "pilot-identity"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, role.PilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://mesh.com/pilot-identity"}))
}

func TestCustomMixerSanIfAuthenticationMutualDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	DNSDomain = ""
	role.TrustDomain = "mesh.com"
	role.MixerIdentity = "mixer-identity"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain(DNSDomain)
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
