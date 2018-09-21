package main

import (
	"github.com/onsi/gomega"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"os"
	"testing"
)

func TestNoPilotSanIfAuthentificationNone(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	role.IdentityDomain = ""
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_NONE.String()

	pilotSAN, _ := determineIdentityDomainAndDomain()

	g.Expect(pilotSAN).To(gomega.Equal(""))
}

func TestPilotSanIfAuthentificationMutualDomainEmptyKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	role.IdentityDomain = ""
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN, _ := determineIdentityDomainAndDomain()

	g.Expect(pilotSAN).To(gomega.Equal("cluster.local" ))
}

func TestPilotSanIfAuthentificationMutualDomainNotEmptyKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = "my.domain"
	role.IdentityDomain = ""
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN, _ := determineIdentityDomainAndDomain()

	g.Expect(pilotSAN).To(gomega.Equal("my.domain"))
}

//TODO Is this really correct?
func TestPilotSanIfAuthentificationMutualDomainEmptyConsul(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	role.IdentityDomain = ""
	registry = serviceregistry.ConsulRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN, _ := determineIdentityDomainAndDomain()

	g.Expect(pilotSAN).To(gomega.Equal("" ))
}

func TestPilotSanIfAuthentificationMutualIdentityDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	role.IdentityDomain = "secured"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN, _ := determineIdentityDomainAndDomain()

	g.Expect(pilotSAN).To(gomega.Equal("secured" ))
}

func TestPilotSanIfAuthentificationMutualIdentityDomainAndDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = "my.domain"
	role.IdentityDomain = "secured"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN, _ := determineIdentityDomainAndDomain()

	g.Expect(pilotSAN).To(gomega.Equal("secured" ))
}

func TestPilotDefaultDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	registry = serviceregistry.KubernetesRegistry
	os.Setenv("POD_NAMESPACE", "default")

	_, domain := determineIdentityDomainAndDomain()

	g.Expect(domain).To(gomega.Equal("default.svc.cluster.local"))
}

func TestPilotDefaultDomainConsul(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	registry = serviceregistry.ConsulRegistry

	_, domain := determineIdentityDomainAndDomain()

	g.Expect(domain).To(gomega.Equal("service.consul"))
}


func TestPilotDefaultDomainOthers(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	registry = serviceregistry.MockRegistry

	_, domain := determineIdentityDomainAndDomain()

	g.Expect(domain).To(gomega.Equal(""))
}

func TestPilotDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = "my.domain"
	registry = serviceregistry.MockRegistry

	_, domain := determineIdentityDomainAndDomain()

	g.Expect(domain).To(gomega.Equal("my.domain"))
}

