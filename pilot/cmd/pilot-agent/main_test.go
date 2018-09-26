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
	"istio.io/istio/pilot/pkg/serviceregistry"
)

func TestDefaultDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	registry = serviceregistry.KubernetesRegistry
	os.Setenv("POD_NAMESPACE", "default")

	setIdentityDomainAndDomainForMTls()

	g.Expect(role.Domain).To(gomega.Equal("default.svc.cluster.local"))
}

func TestDefaultDomainConsul(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	registry = serviceregistry.ConsulRegistry

	setIdentityDomainAndDomainForMTls()

	g.Expect(role.Domain).To(gomega.Equal("service.consul"))
}

func TestDefaultDomainOthers(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	registry = serviceregistry.MockRegistry

	setIdentityDomainAndDomainForMTls()

	g.Expect(role.Domain).To(gomega.Equal(""))
}

func TestDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = "my.domain"
	registry = serviceregistry.MockRegistry

	setIdentityDomainAndDomainForMTls()

	g.Expect(role.Domain).To(gomega.Equal("my.domain"))
}

func TestIdentityDomainMututalTLS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.IdentityDomain = "secured"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()
	os.Setenv("POD_NAMESPACE", "default")

	setIdentityDomainAndDomainForMTls()

	g.Expect(role.IdentityDomain).To(gomega.Equal("secured"))
}

func TestIdentityDomainMututalTLSDefault(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.IdentityDomain = ""
	role.Domain = "my.Domain"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()
	os.Setenv("POD_NAMESPACE", "default")

	setIdentityDomainAndDomainForMTls()

	g.Expect(role.IdentityDomain).To(gomega.Equal("my.Domain"))
}

func TestIdentityDomainNoMututalTLS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.IdentityDomain = "secured"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_NONE.String()
	os.Setenv("POD_NAMESPACE", "default")

	setIdentityDomainAndDomainForMTls()

	g.Expect(role.IdentityDomain).To(gomega.Equal(""))
}
