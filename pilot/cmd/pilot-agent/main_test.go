package main

import (
	"github.com/onsi/gomega"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"testing"
)

func TestNoPilotSanIfAuthentificationNone(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_NONE.String()

	pilotSAN := determinePilotSAN("anything")

	g.Expect(pilotSAN).To(gomega.BeNil())
}

func TestNoPilotSanIfAuthentificationMutualDomainEmptyKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN := determinePilotSAN("anything")

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://cluster.local/ns/anything/sa/istio-pilot-service-account"} ))
}

func TestNoPilotSanIfAuthentificationMutualDomainNotEmptyKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = "my.domain"
	registry = serviceregistry.KubernetesRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN := determinePilotSAN("anything")

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://my.domain/ns/anything/sa/istio-pilot-service-account"} ))
}

//TODO Is this really correct?
func TestNoPilotSanIfAuthentificationMutualDomainEmptyConsul(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.Domain = ""
	registry = serviceregistry.ConsulRegistry
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	pilotSAN := determinePilotSAN("anything")

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe:///ns/anything/sa/istio-pilot-service-account"} ))
}