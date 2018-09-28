package main

import (
	"github.com/onsi/gomega"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"os"
	"testing"
)

func TestNoPilotSanIfAuthenticationNone(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	role.IdentityDomain = ""
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_NONE.String()

	pilotSAN := getPilotSAN(role.Domain, "anything")

	g.Expect(pilotSAN).To(gomega.BeNil())
}

func TestPilotSanIfAuthenticationMutualDomainEmptyKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	role.IdentityDomain = ""
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN:= getPilotSAN(role.Domain,"anything")

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://cluster.local/ns/anything/sa/istio-pilot-service-account"} ))
}

func TestPilotSanIfAuthenticationMutualDomainNotEmptyKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = "my.domain"
	role.IdentityDomain = ""
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN:= getPilotSAN(role.Domain,"anything")

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://my.domain/ns/anything/sa/istio-pilot-service-account"} ))
}

//TODO Is this really correct?
func TestPilotSanIfAuthenticationMutualDomainEmptyConsul(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	role.IdentityDomain = ""
	registry = serviceregistry.ConsulRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN:= getPilotSAN(role.Domain,"anything")

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe:///ns/anything/sa/istio-pilot-service-account"} ))
}

func TestPilotSanIfAuthenticationMutualIdentityDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	role.IdentityDomain = "secured"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN:= getPilotSAN(role.Domain,"anything")

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://secured/ns/anything/sa/istio-pilot-service-account"} ))
}

func TestPilotSanIfAuthenticationMutualIdentityDomainAndDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = "my.domain"
	role.IdentityDomain = "secured"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN:= getPilotSAN(role.Domain,"anything")

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://secured/ns/anything/sa/istio-pilot-service-account"} ))
}

func TestPilotDefaultDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	registry = serviceregistry.KubernetesRegistry
	os.Setenv("POD_NAMESPACE", "default")

	domain := getDomain(role.Domain)

	g.Expect(domain).To(gomega.Equal("default.svc.cluster.local"))
	os.Unsetenv("POD_NAMESPACE")
}

func TestPilotDefaultDomainConsul(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	registry = serviceregistry.ConsulRegistry

	domain := getDomain(role.Domain)

	g.Expect(domain).To(gomega.Equal("service.consul"))
}


func TestPilotDefaultDomainOthers(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	registry = serviceregistry.MockRegistry

	domain := getDomain(role.Domain)

	g.Expect(domain).To(gomega.Equal(""))
}

func TestPilotDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = "my.domain"
	registry = serviceregistry.MockRegistry

	domain := getDomain(role.Domain)

	g.Expect(domain).To(gomega.Equal("my.domain"))
}

func TestPilotSanIfAuthenticationMutualStdDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ".svc.cluster.local"
	role.IdentityDomain = ""
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN:= getPilotSAN(role.Domain,"anything")

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://cluster.local/ns/anything/sa/istio-pilot-service-account"} ))
}

func TestPilotSanIfAuthenticationMutualStdDomainConsul(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = "service.consul"
	role.IdentityDomain = ""
	registry = serviceregistry.ConsulRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN:= getPilotSAN(role.Domain,"anything")

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe:///ns/anything/sa/istio-pilot-service-account"} ))
}
