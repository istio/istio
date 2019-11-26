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
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/proto"
	"github.com/onsi/gomega"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/constants"
)

func TestNoPilotSanIfAuthenticationNone(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role = &model.Proxy{}
	role.DNSDomain = ""
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_NONE.String()

	setSpiffeTrustDomain("", role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, pilotIdentity)

	g.Expect(pilotSAN).To(gomega.BeNil())
}

func TestPilotSanIfAuthenticationMutualDomainEmptyKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role = &model.Proxy{}
	role.DNSDomain = ""
	registryID = serviceregistry.Kubernetes
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain("", role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, pilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://cluster.local/ns/anything/sa/istio-pilot-service-account"}))
}

func TestPilotSanIfAuthenticationMutualDomainNotEmptyKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role = &model.Proxy{}
	role.DNSDomain = "my.domain"
	trustDomain = ""
	registryID = serviceregistry.Kubernetes
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain("", role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, pilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://my.domain/ns/anything/sa/istio-pilot-service-account"}))
}

// This test is used to ensure that the former behavior is unchanged
// When pilot is started without a trust domain, the SPIFFE URI doesn't contain a host and is not valid
func TestPilotSanIfAuthenticationMutualDomainEmptyConsul(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role.DNSDomain = ""
	registryID = serviceregistry.Consul
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain("", role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, pilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe:///ns/anything/sa/istio-pilot-service-account"}))
}

func TestPilotSanIfAuthenticationMutualTrustDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role = &model.Proxy{}
	role.DNSDomain = ""
	trustDomain = "secured"
	defer func() {
		trustDomain = ""
	}()
	registryID = serviceregistry.Kubernetes
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain("", role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, pilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://secured/ns/anything/sa/istio-pilot-service-account"}))
}

func TestPilotSanIfAuthenticationMutualTrustDomainAndDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role = &model.Proxy{}
	role.DNSDomain = "my.domain"
	trustDomain = "secured"
	defer func() {
		trustDomain = ""
	}()
	registryID = serviceregistry.Kubernetes
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain("", role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, pilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://secured/ns/anything/sa/istio-pilot-service-account"}))
}

func TestPilotDefaultDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role = &model.Proxy{}
	role.DNSDomain = ""
	registryID = serviceregistry.Kubernetes

	domain := getDNSDomain("default", role.DNSDomain)

	g.Expect(domain).To(gomega.Equal("default.svc.cluster.local"))
}

func TestDetectSds(t *testing.T) {
	sdsUdsWaitTimeout = 100 * time.Millisecond
	os.Setenv("SDS_ENABLED", "true")
	defer func() {
		sdsUdsWaitTimeout = time.Minute
		os.Unsetenv("SDS_ENABLED")
	}()

	g := gomega.NewGomegaWithT(t)
	tests := []struct {
		controlPlaneBootstrap bool
		sdsAddress            string
		tokenPath             string
		expectedSdsEnabled    bool
		expectedSdsTokenPath  string
	}{
		{
			controlPlaneBootstrap: true,
			expectedSdsEnabled:    false,
			expectedSdsTokenPath:  "",
		},
		{
			controlPlaneBootstrap: true,
			sdsAddress:            "/tmp/testtmpuds1.log",
			tokenPath:             "/tmp/testtmptoken1.log",
			expectedSdsEnabled:    true,
			expectedSdsTokenPath:  "/tmp/testtmptoken1.log",
		},
		{
			controlPlaneBootstrap: true,
			sdsAddress:            "unix:/tmp/testtmpuds1.log",
			tokenPath:             "/tmp/testtmptoken1.log",
			expectedSdsEnabled:    true,
			expectedSdsTokenPath:  "/tmp/testtmptoken1.log",
		},
		{
			controlPlaneBootstrap: true,
			sdsAddress:            "/tmp/testtmpuds1.log",
			tokenPath:             "/tmp/testtmptoken1.log",
			expectedSdsEnabled:    true,
			expectedSdsTokenPath:  "/tmp/testtmptoken1.log",
		},
		{
			controlPlaneBootstrap: true,
			tokenPath:             "/tmp/testtmptoken1.log",
			expectedSdsEnabled:    false,
		},
		{
			controlPlaneBootstrap: true,
			sdsAddress:            "/tmp/testtmpuds1.log",
		},
		{
			controlPlaneBootstrap: false,
			sdsAddress:            "/tmp/test_tmp_uds2",
			tokenPath:             "/tmp/test_tmp_token2",
			expectedSdsEnabled:    true,
			expectedSdsTokenPath:  "/tmp/test_tmp_token2",
		},
		{
			controlPlaneBootstrap: false,
			sdsAddress:            "/tmp/test_tmp_uds4",
		},
	}
	for _, tt := range tests {
		if tt.sdsAddress != "" {
			addr := strings.TrimPrefix(tt.sdsAddress, "unix:")
			if _, err := os.Stat(addr); err != nil {
				os.Create(addr)
				defer os.Remove(addr)
			}
		}
		if tt.tokenPath != "" {
			if _, err := os.Stat(tt.tokenPath); err != nil {
				os.Create(tt.tokenPath)
				defer os.Remove(tt.tokenPath)
			}
		}

		enabled, path := detectSds(tt.controlPlaneBootstrap, tt.sdsAddress, tt.tokenPath)
		g.Expect(enabled).To(gomega.Equal(tt.expectedSdsEnabled))
		g.Expect(path).To(gomega.Equal(tt.expectedSdsTokenPath))
	}
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

func TestPilotSanIfAuthenticationMutualStdDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role = &model.Proxy{}
	role.DNSDomain = ".svc.cluster.local"
	registryID = serviceregistry.Kubernetes
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain("", role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, pilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://cluster.local/ns/anything/sa/istio-pilot-service-account"}))
}

// This test is used to ensure that the former behavior is unchanged
// When pilot is started without a trust domain, the SPIFFE URI doesn't contain a host and is not valid
func TestPilotSanIfAuthenticationMutualStdDomainConsul(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role = &model.Proxy{}
	role.DNSDomain = "service.consul"
	trustDomain = ""
	registryID = serviceregistry.Consul
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain("", role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, pilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe:///ns/anything/sa/istio-pilot-service-account"}))
}

func TestCustomPilotSanIfAuthenticationMutualDomainKubernetesNoTrustDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role = &model.Proxy{}
	role.DNSDomain = ""
	pilotIdentity = "pilot-identity"
	registryID = serviceregistry.Kubernetes
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain("", role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, pilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://cluster.local/pilot-identity"}))
}

func TestCustomPilotSanIfAuthenticationMutualDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role = &model.Proxy{}
	role.DNSDomain = ""
	trustDomain = "mesh.com"
	pilotIdentity = "pilot-identity"
	registryID = serviceregistry.Kubernetes
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain("", role.DNSDomain)
	pilotSAN := getSAN("anything", envoy.PilotSvcAccName, pilotIdentity)

	g.Expect(pilotSAN).To(gomega.Equal([]string{"spiffe://mesh.com/pilot-identity"}))
}

func TestCustomMixerSanIfAuthenticationMutualDomainKubernetes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	role = &model.Proxy{}
	role.DNSDomain = ""
	trustDomain = "mesh.com"
	mixerIdentity = "mixer-identity"
	registryID = serviceregistry.Kubernetes
	controlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS.String()

	setSpiffeTrustDomain("", role.DNSDomain)
	mixerSAN := envoy.GetSAN("", mixerIdentity)

	g.Expect(mixerSAN).To(gomega.Equal("spiffe://mesh.com/mixer-identity"))
}

func TestDedupeStrings(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	in := []string{
		constants.DefaultCertChain, constants.DefaultKey, constants.DefaultRootCert,
		constants.DefaultCertChain, constants.DefaultKey, constants.DefaultRootCert,
	}
	expected := []string{constants.DefaultCertChain, constants.DefaultKey, constants.DefaultRootCert}

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

func Test_fromJSON(t *testing.T) {
	val := `
{"address":"oap.istio-system:11800",
"tlsSettings":{"mode": "DISABLE", "subjectAltNames": [], "caCertificates": null},
"tcpKeepalive":{"interval":"10s","probes":3,"time":"10s"}
}
`

	type args struct {
		j string
	}
	tests := []struct {
		name string
		args args
		want *meshconfig.RemoteService
	}{
		{
			name: "foo",
			args: struct{ j string }{j: val},
			want: &meshconfig.RemoteService{
				Address:     "oap.istio-system:11800",
				TlsSettings: &v1alpha3.TLSSettings{},
				TcpKeepalive: &v1alpha3.ConnectionPoolSettings_TCPSettings_TcpKeepalive{
					Probes:   3,
					Time:     &types.Duration{Seconds: 10},
					Interval: &types.Duration{Seconds: 10},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fromJSON(tt.args.j); !proto.Equal(got, tt.want) {
				t.Errorf("got = %v \n want %v", got, tt.want)
			}
		})
	}
}
