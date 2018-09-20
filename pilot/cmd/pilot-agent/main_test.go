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

	pilotSAN, _ := determinePilotSanAndDomain("anything")

	g.Expect(pilotSAN).To(gomega.BeNil())
}

func TestPilotSanIfAuthentificationMutualDomainEmptyKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	role.IdentityDomain = ""
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN, _ := determinePilotSanAndDomain("anything")

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://cluster.local/ns/anything/sa/istio-pilot-service-account"} ))
}

func TestPilotSanIfAuthentificationMutualDomainNotEmptyKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = "my.domain"
	role.IdentityDomain = ""
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN, _ := determinePilotSanAndDomain("anything")

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://my.domain/ns/anything/sa/istio-pilot-service-account"} ))
}

//TODO Is this really correct?
func TestPilotSanIfAuthentificationMutualDomainEmptyConsul(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	role.IdentityDomain = ""
	registry = serviceregistry.ConsulRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN, _ := determinePilotSanAndDomain("anything")

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe:///ns/anything/sa/istio-pilot-service-account"} ))
}

func TestPilotSanIfAuthentificationMutualIdentityDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	role.IdentityDomain = "secured"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN, _ := determinePilotSanAndDomain("anything")

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://secured/ns/anything/sa/istio-pilot-service-account"} ))
}

func TestPilotSanIfAuthentificationMutualIdentityDomainAndDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = "my.domain"
	role.IdentityDomain = "secured"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN, _ := determinePilotSanAndDomain("anything")

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://secured/ns/anything/sa/istio-pilot-service-account"} ))
}

func TestPilotDefaultDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	registry = serviceregistry.KubernetesRegistry
	os.Setenv("POD_NAMESPACE", "default")

	_, domain := determinePilotSanAndDomain("anything")

	g.Expect(domain).To(gomega.Equal("default.svc.cluster.local"))
}

func TestPilotDefaultDomainConsul(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	registry = serviceregistry.ConsulRegistry

	_, domain := determinePilotSanAndDomain("anything")

	g.Expect(domain).To(gomega.Equal("service.consul"))
}


func TestPilotDefaultDomainOthers(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	registry = serviceregistry.MockRegistry

	_, domain := determinePilotSanAndDomain("anything")

	g.Expect(domain).To(gomega.Equal(""))
}

func TestPilotDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = "my.domain"
	registry = serviceregistry.MockRegistry

	_, domain := determinePilotSanAndDomain("anything")

	g.Expect(domain).To(gomega.Equal("my.domain"))
}

