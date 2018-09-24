package main

import (
	"github.com/onsi/gomega"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"os"
	"testing"
	meshconfig "istio.io/api/mesh/v1alpha1"
)


func TestDefaultDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	registry = serviceregistry.KubernetesRegistry
	os.Setenv("POD_NAMESPACE", "default")

	setIdentityDomainAndDomain()

	g.Expect(role.Domain).To(gomega.Equal("default.svc.cluster.local"))
}

func TestDefaultDomainConsul(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	registry = serviceregistry.ConsulRegistry

	setIdentityDomainAndDomain()

	g.Expect(role.Domain).To(gomega.Equal("service.consul"))
}


func TestDefaultDomainOthers(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	registry = serviceregistry.MockRegistry

	setIdentityDomainAndDomain()

	g.Expect(role.Domain).To(gomega.Equal(""))
}

func TestDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = "my.domain"
	registry = serviceregistry.MockRegistry

	setIdentityDomainAndDomain()

	g.Expect(role.Domain).To(gomega.Equal("my.domain"))
}

func TestIdentityDomainMututalTLS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.IdentityDomain = "secured"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()
	os.Setenv("POD_NAMESPACE", "default")

	setIdentityDomainAndDomain()

	g.Expect(role.IdentityDomain).To(gomega.Equal("secured"))
}

func TestIdentityDomainMututalTLSDefault(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.IdentityDomain = ""
	role.Domain = "my.Domain"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()
	os.Setenv("POD_NAMESPACE", "default")

	setIdentityDomainAndDomain()

	g.Expect(role.IdentityDomain).To(gomega.Equal("my.Domain"))
}

func TestIdentityDomainNoMututalTLS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.IdentityDomain = "secured"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_NONE.String()
	os.Setenv("POD_NAMESPACE", "default")

	setIdentityDomainAndDomain()

	g.Expect(role.IdentityDomain).To(gomega.Equal(""))
}
