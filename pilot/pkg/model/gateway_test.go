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

package model

import (
	"fmt"
	"testing"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/sets"
)

const (
	AllowedNamespace    = "allowed-ns"
	NotAllowedNamespace = "not-allowed-ns"
)

// nolint lll
func TestMergeGateways(t *testing.T) {
	gwHTTPFoo := makeConfig("foo1", "not-default", "foo.bar.com", "name1", "http", 7, "ingressgateway", "", networking.ServerTLSSettings_SIMPLE, []string{}, "sa")
	gwHTTPbar := makeConfig("bar1", "not-default", "bar.foo.com", "bname1", "http", 7, "ingressgateway", "", networking.ServerTLSSettings_SIMPLE, []string{}, "sa")
	gwHTTPlocalbar := makeConfig("lcoalbar1", "not-default", "localbar.foo.com", "bname1", "http", 7, "ingressgateway", "127.0.0.1", networking.ServerTLSSettings_SIMPLE, []string{}, "sa")
	gwHTTP2Wildcard := makeConfig("foo5", "not-default", "*", "name5", "http2", 8, "ingressgateway", "", networking.ServerTLSSettings_SIMPLE, []string{}, "sa")
	gwHTTPWildcard := makeConfig("foo3", "not-default", "*", "name3", "http", 8, "ingressgateway", "", networking.ServerTLSSettings_SIMPLE, []string{}, "sa")
	gwTCPWildcard := makeConfig("foo4", "not-default-2", "*", "name4", "tcp", 8, "ingressgateway", "", networking.ServerTLSSettings_SIMPLE, []string{}, "sa")

	gwHTTPWildcardAlternate := makeConfig("foo2", "not-default", "*", "name2", "http", 7, "ingressgateway2", "", networking.ServerTLSSettings_SIMPLE, []string{}, "sa")

	gwSimple := makeConfig("foo-simple", "not-default-2", "*.example.com", "https", "HTTPS", 443, "ingressgateway", "", networking.ServerTLSSettings_SIMPLE, []string{}, "sa")
	gwPassthrough := makeConfig("foo-passthrough", "not-default-2", "foo.example.com", "tls-foo", "TLS", 443, "ingressgateway", "", networking.ServerTLSSettings_PASSTHROUGH, []string{}, "sa")

	gwSimpleCred := makeConfig("foo1", "ns", "foo.bar.com", "name1", "http", 7, "ingressgateway", "", networking.ServerTLSSettings_SIMPLE, []string{"kubernetes-gateway://ns/foo"}, "sa")
	gwSimpleMultipleCreds := makeConfig("foo1", "ns", "foo.bar.com", "name1", "http", 7, "ingressgateway", "", networking.ServerTLSSettings_SIMPLE, []string{"kubernetes-gateway://ns/foo", "kubernetes-gateway://ns/bar"}, "sa")
	gwMutualCred := makeConfig("foo1", "ns", "foo.bar.com", "name1", "http", 7, "ingressgateway", "", networking.ServerTLSSettings_MUTUAL, []string{"kubernetes-gateway://ns/foo"}, "sa")
	gwSimpleCredInAllowedNS := makeConfig("foo1", "ns", "foo.bar.com", "name1", "http", 7, "ingressgateway", "", networking.ServerTLSSettings_SIMPLE, []string{fmt.Sprintf("kubernetes-gateway://%s/foo", AllowedNamespace)}, "sa")
	// If no SA annotation, then the SA name shouldn't matter
	gwSimpleCredInAllowedNSNoSA := makeConfig("foo1", "ns", "foo.bar.com", "name1", "http", 7, "ingressgateway", "", networking.ServerTLSSettings_SIMPLE, []string{fmt.Sprintf("kubernetes-gateway://%s/foo", AllowedNamespace)}, "")
	gwSimpleCredInNotAllowedNS := makeConfig("foo1", "ns", "foo.bar.com", "name1", "http", 7, "ingressgateway", "", networking.ServerTLSSettings_SIMPLE, []string{fmt.Sprintf("kubernetes-gateway://%s/foo", NotAllowedNamespace)}, "sa")
	gwMutualCredInAllowedNS := makeConfig("foo1", "ns", "foo.bar.com", "name1", "http", 7, "ingressgateway", "", networking.ServerTLSSettings_MUTUAL, []string{fmt.Sprintf("kubernetes-gateway://%s/foo", AllowedNamespace)}, "sa")
	gwMutualCredInNotAllowedNS := makeConfig("foo1", "ns", "foo.bar.com", "name1", "http", 7, "ingressgateway", "", networking.ServerTLSSettings_MUTUAL, []string{fmt.Sprintf("kubernetes-gateway://%s/foo", NotAllowedNamespace)}, "sa")
	// Even if we allow any SA name, we still should do namespace checks
	gwMutualCredInNotAllowedNSNoSA := makeConfig("foo1", "ns", "foo.bar.com", "name1", "http", 7, "ingressgateway", "", networking.ServerTLSSettings_MUTUAL, []string{fmt.Sprintf("kubernetes-gateway://%s/foo", NotAllowedNamespace)}, "")

	proxyNoInput := makeProxy(func() *spiffe.Identity { return nil })
	proxyIdentity := makeProxy(func() *spiffe.Identity {
		identity, _ := spiffe.ParseIdentity("spiffe://td/ns/ns/sa/sa")
		return &identity
	})
	otherProxyIdentity := makeProxy(func() *spiffe.Identity {
		identity, _ := spiffe.ParseIdentity("spiffe://td/ns/ns/sa/other-sa")
		return &identity
	})
	// ListenerSet-derived config with unmanaged parent Gateway: InternalServiceAccount is empty, any SA is accepted.
	makeListenerSetConfig := func(certName string) config.Config {
		return config.Config{
			Meta: config.Meta{
				Name:      "ls1",
				Namespace: "tenant",
				Annotations: map[string]string{
					constants.InternalServiceAccount:  "",
					constants.InternalParentNames:     "ListenerSet/ls1/https.tenant",
					constants.InternalParentNamespace: "ns",
				},
			},
			Spec: &networking.Gateway{
				Servers: []*networking.Server{{
					Hosts: []string{"foo.example.com"},
					Port:  &networking.Port{Name: "https", Number: 443, Protocol: "HTTPS"},
					Tls:   makeTLSSettings(networking.ServerTLSSettings_SIMPLE, []string{certName}),
				}},
			},
		}
	}
	gwListenerSetCredSameNS := makeListenerSetConfig("kubernetes-gateway://tenant/foo")
	gwListenerSetCredCrossNSAllowed := makeListenerSetConfig(fmt.Sprintf("kubernetes-gateway://%s/foo", AllowedNamespace))
	gwListenerSetCredCrossNSNotAllowed := makeListenerSetConfig(fmt.Sprintf("kubernetes-gateway://%s/foo", NotAllowedNamespace))

	// Configs with an explicit frontend mTLS CA credential (CaCertCredentialName already includes -cacert suffix).
	gwMutualWithCaCertCredSameNS := makeConfigWithCaCert("foo1", "ns", "foo.bar.com", "name1", "http", 7, "ingressgateway",
		networking.ServerTLSSettings_MUTUAL, "kubernetes-gateway://ns/server-cert", "kubernetes-gateway://ns/client-ca-cacert", "sa")
	gwMutualWithCaCertCredAllowedNS := makeConfigWithCaCert("foo1", "ns", "foo.bar.com", "name1", "http", 7, "ingressgateway",
		networking.ServerTLSSettings_MUTUAL,
		fmt.Sprintf("kubernetes-gateway://%s/server-cert", AllowedNamespace),
		fmt.Sprintf("kubernetes-gateway://%s/client-ca-cacert", AllowedNamespace), "sa")
	gwMutualWithCaCertCredNotAllowedNS := makeConfigWithCaCert("foo1", "ns", "foo.bar.com", "name1", "http", 7, "ingressgateway",
		networking.ServerTLSSettings_MUTUAL,
		fmt.Sprintf("kubernetes-gateway://%s/server-cert", NotAllowedNamespace),
		fmt.Sprintf("kubernetes-gateway://%s/client-ca-cacert", NotAllowedNamespace), "sa")

	// TODO(ramaraochavali): Add more test cases here.
	tests := []struct {
		name               string
		gwConfig           []config.Config
		proxy              *Proxy
		mergedServersNum   int
		serverNum          int
		serversForRouteNum map[string]int
		gatewaysNum        int
		verifiedCertNum    int
	}{
		{
			"single-server-config",
			[]config.Config{gwHTTPFoo},
			proxyNoInput,
			1,
			1,
			map[string]int{"http.7": 1},
			1,
			0,
		},
		{
			"two servers on the same port",
			[]config.Config{gwHTTPFoo, gwHTTPbar},
			proxyNoInput,
			1,
			2,
			map[string]int{"http.7": 2},
			2,
			0,
		},
		{
			"two servers on the same port with different bind",
			[]config.Config{gwHTTPbar, gwHTTPlocalbar},
			proxyNoInput,
			2,
			2,
			map[string]int{"http.7": 1, "http.7.127.0.0.1": 1},
			2,
			0,
		},
		{
			"same-server-config",
			[]config.Config{gwHTTPFoo, gwHTTPWildcardAlternate},
			proxyNoInput,
			1,
			2,
			map[string]int{"http.7": 2},
			2,
			0,
		},
		{
			"multi-server-config",
			[]config.Config{gwHTTPFoo, gwHTTPWildcardAlternate, gwHTTPWildcard},
			proxyNoInput,
			2,
			3,
			map[string]int{"http.7": 2, "http.8": 1},
			3,
			0,
		},
		{
			"http-tcp-wildcard-server-config",
			[]config.Config{gwHTTPFoo, gwTCPWildcard},
			proxyNoInput,
			2,
			2,
			map[string]int{"http.7": 1},
			2,
			0,
		},
		{
			"tcp-http-server-config",
			[]config.Config{gwTCPWildcard, gwHTTPWildcard},
			proxyNoInput,
			1,
			1,
			map[string]int{},
			2,
			0,
		},
		{
			"tcp-tcp-server-config",
			[]config.Config{gwHTTPWildcard, gwTCPWildcard}, // order matters
			proxyNoInput,
			1,
			1,
			map[string]int{"http.8": 1},
			2,
			0,
		},
		{
			"http-http2-server-config",
			[]config.Config{gwHTTPWildcard, gwHTTP2Wildcard},
			proxyNoInput,
			1,
			1,
			// http and http2 both present
			map[string]int{"http.8": 1},
			2,
			0,
		},
		{
			"simple-passthrough",
			[]config.Config{gwSimple, gwPassthrough},
			proxyNoInput,
			2,
			2,
			map[string]int{"https.443.https.foo-simple.not-default-2": 1},
			2,
			0,
		},
		{
			"simple-cred",
			[]config.Config{gwSimpleCred},
			proxyIdentity,
			1,
			1,
			map[string]int{"http.7": 1},
			1,
			1,
		},
		{
			"mutual-cred",
			[]config.Config{gwMutualCred},
			proxyIdentity,
			1,
			1,
			map[string]int{"http.7": 1},
			1,
			2,
		},
		{
			"simple-cred-in-allowed-ns",
			[]config.Config{gwSimpleCredInAllowedNS},
			proxyIdentity,
			1,
			1,
			map[string]int{"http.7": 1},
			1,
			1,
		},
		{
			"simple-cred-in-allowed-ns-no-sa",
			[]config.Config{gwSimpleCredInAllowedNSNoSA},
			proxyIdentity,
			1,
			1,
			map[string]int{"http.7": 1},
			1,
			1,
		},
		{
			"simple-cred-in-not-allowed-ns",
			[]config.Config{gwSimpleCredInNotAllowedNS},
			proxyIdentity,
			1,
			1,
			map[string]int{"http.7": 1},
			1,
			0,
		},
		{
			"mutual-cred-in-allowed-ns",
			[]config.Config{gwMutualCredInAllowedNS},
			proxyIdentity,
			1,
			1,
			map[string]int{"http.7": 1},
			1,
			2,
		},
		{
			"mutual-cred-in-not-allowed-ns",
			[]config.Config{gwMutualCredInNotAllowedNS},
			proxyIdentity,
			1,
			1,
			map[string]int{"http.7": 1},
			1,
			0,
		},
		{
			"mutual-cred-in-not-allowed-ns-no-sa",
			[]config.Config{gwMutualCredInNotAllowedNSNoSA},
			proxyIdentity,
			1,
			1,
			map[string]int{"http.7": 1},
			1,
			0,
		},
		{
			"invalid-request-same-ns-wrong-sa",
			[]config.Config{gwSimpleCredInAllowedNS},
			otherProxyIdentity,
			1,
			1,
			map[string]int{"http.7": 1},
			1,
			0,
		},
		{
			"multiple-creds",
			[]config.Config{gwSimpleMultipleCreds},
			proxyIdentity,
			1,
			1,
			map[string]int{"http.7": 1},
			1,
			2,
		},
		{
			"listenerset-cert-same-ns",
			[]config.Config{gwListenerSetCredSameNS},
			proxyIdentity,
			1,
			1,
			map[string]int{"https.443.https.ls1.tenant": 1},
			1,
			1,
		},
		{
			"listenerset-cert-cross-ns-allowed",
			[]config.Config{gwListenerSetCredCrossNSAllowed},
			proxyIdentity,
			1,
			1,
			map[string]int{"https.443.https.ls1.tenant": 1},
			1,
			1,
		},
		{
			"listenerset-cert-cross-ns-not-allowed",
			[]config.Config{gwListenerSetCredCrossNSNotAllowed},
			proxyIdentity,
			1,
			1,
			map[string]int{"https.443.https.ls1.tenant": 1},
			1,
			0,
		},
		{
			"mutual-with-explicit-ca-cert-cred-same-ns",
			[]config.Config{gwMutualWithCaCertCredSameNS},
			proxyIdentity,
			1,
			1,
			map[string]int{"http.7": 1},
			1,
			3,
		},
		{
			"mutual-with-explicit-ca-cert-cred-allowed-ns",
			[]config.Config{gwMutualWithCaCertCredAllowedNS},
			proxyIdentity,
			1,
			1,
			map[string]int{"http.7": 1},
			1,
			3,
		},
		{
			"mutual-with-explicit-ca-cert-cred-not-allowed-ns",
			[]config.Config{gwMutualWithCaCertCredNotAllowedNS},
			proxyIdentity,
			1,
			1,
			map[string]int{"http.7": 1},
			1,
			0,
		},
	}

	// Exact-set assertions for the CaCertCredentialName test cases only.
	caCertExpected := map[string]sets.Set[string]{
		"mutual-with-explicit-ca-cert-cred-same-ns": sets.New(
			"kubernetes-gateway://ns/server-cert",
			"kubernetes-gateway://ns/server-cert-cacert",
			"kubernetes-gateway://ns/client-ca-cacert",
		),
		"mutual-with-explicit-ca-cert-cred-allowed-ns": sets.New(
			"kubernetes-gateway://"+AllowedNamespace+"/server-cert",
			"kubernetes-gateway://"+AllowedNamespace+"/server-cert-cacert",
			"kubernetes-gateway://"+AllowedNamespace+"/client-ca-cacert",
		),
		"mutual-with-explicit-ca-cert-cred-not-allowed-ns": sets.New[string](),
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			instances := []gatewayWithInstances{}
			for _, c := range tt.gwConfig {
				instances = append(instances, gatewayWithInstances{c, true, nil})
			}
			mgw := mergeGateways(instances, tt.proxy, makePushContext())
			if len(mgw.MergedServers) != tt.mergedServersNum {
				t.Errorf("Incorrect number of merged servers. Expected: %v Got: %d", tt.mergedServersNum, len(mgw.MergedServers))
			}
			if len(mgw.ServersByRouteName) != len(tt.serversForRouteNum) {
				t.Errorf("Incorrect number of routes. Expected: %v Got: %d", len(tt.serversForRouteNum), len(mgw.ServersByRouteName))
			}
			for k, v := range mgw.ServersByRouteName {
				if tt.serversForRouteNum[k] != len(v) {
					t.Errorf("for route %v expected %v servers got %v", k, tt.serversForRouteNum[k], len(v))
				}
			}
			ns := 0
			for _, ms := range mgw.MergedServers {
				ns += len(ms.Servers)
			}
			if ns != tt.serverNum {
				t.Errorf("Incorrect number of total servers. Expected: %v Got: %d", tt.serverNum, ns)
			}
			if len(mgw.GatewayNameForServer) != tt.gatewaysNum {
				t.Errorf("Incorrect number of gateways. Expected: %v Got: %d", tt.gatewaysNum, len(mgw.GatewayNameForServer))
			}
			if mgw.VerifiedCertificateReferences.Len() != tt.verifiedCertNum {
				t.Errorf("Incorrect number of verified certs. Expected: %v Got: %d", tt.verifiedCertNum, mgw.VerifiedCertificateReferences.Len())
			}
			if expected, ok := caCertExpected[tt.name]; ok && !expected.Equals(mgw.VerifiedCertificateReferences) {
				t.Errorf("VerifiedCertificateReferences mismatch for %q: want %v, got %v",
					tt.name, expected, mgw.VerifiedCertificateReferences)
			}
		})
	}
}

func TestGetAutoPassthroughSNIHosts(t *testing.T) {
	gateway := config.Config{
		Meta: config.Meta{
			Name:      "gateway",
			Namespace: "istio-system",
		},
		Spec: &networking.Gateway{
			Selector: map[string]string{"istio": "ingressgateway"},
			Servers: []*networking.Server{
				{
					Hosts: []string{"static.example.com"},
					Port:  &networking.Port{Name: "http", Number: 80, Protocol: "HTTP"},
				},
				{
					Hosts: []string{"www.example.com"},
					Port:  &networking.Port{Name: "https", Number: 443, Protocol: "HTTPS"},
					Tls:   &networking.ServerTLSSettings{Mode: networking.ServerTLSSettings_SIMPLE},
				},
				{
					Hosts: []string{"a.apps.svc.cluster.local", "b.apps.svc.cluster.local"},
					Port:  &networking.Port{Name: "tls", Number: 15443, Protocol: "TLS"},
					Tls:   &networking.ServerTLSSettings{Mode: networking.ServerTLSSettings_AUTO_PASSTHROUGH},
				},
			},
		},
	}
	svc := &Service{
		Attributes: ServiceAttributes{
			Labels: map[string]string{},
		},
	}
	gatewayServiceTargets := []ServiceTarget{
		{
			Service: svc,
			Port: ServiceInstancePort{
				ServicePort: &Port{Port: 80},
				TargetPort:  80,
			},
		},
		{
			Service: svc,
			Port: ServiceInstancePort{
				ServicePort: &Port{Port: 443},
				TargetPort:  443,
			},
		},
		{
			Service: svc,
			Port: ServiceInstancePort{
				ServicePort: &Port{Port: 15443},
				TargetPort:  15443,
			},
		},
	}
	instances := []gatewayWithInstances{{gateway: gateway, instances: gatewayServiceTargets}}
	mgw := mergeGateways(instances, &Proxy{}, nil)
	hosts := mgw.GetAutoPassthroughGatewaySNIHosts()
	expectedHosts := sets.Set[string]{}
	expectedHosts.InsertAll("a.apps.svc.cluster.local", "b.apps.svc.cluster.local")
	if !hosts.Equals(expectedHosts) {
		t.Errorf("expected to get: [a.apps.svc.cluster.local,b.apps.svc.cluster.local], got: %s", hosts.String())
	}
}

// TestMergeGatewaysHttpsFirstBug tests the bug where creating HTTPS server first
// on a port prevents HTTP server from building correct routes on the same port but different bind.
// This follows the existing test naming conventions: foo-*, not-default namespace, example.com hosts, standard ports.
func TestMergeGatewaysHttpsFirstBug(t *testing.T) {
	// Create HTTPS server first (this was causing the bug)
	gwHTTPS := makeConfig("foo-https", "not-default", "*.example.com", "https-port", "HTTPS", 443, "ingressgateway", "10.0.0.1",
		networking.ServerTLSSettings_SIMPLE, []string{}, "sa")

	// Create HTTP server second (this was failing to create proper routes)
	gwHTTP := makeConfig("foo-http", "not-default", "*.example.com", "http-port", "HTTP", 443, "ingressgateway", "10.0.0.2",
		networking.ServerTLSSettings_SIMPLE, []string{}, "sa")

	// Test case 1: HTTPS first, then HTTP (reproduces the bug)
	t.Run("https-first-then-http", func(t *testing.T) {
		gwWithInstances := []gatewayWithInstances{
			{gateway: gwHTTPS, legacyGatewaySelector: true, instances: nil},
			{gateway: gwHTTP, legacyGatewaySelector: true, instances: nil},
		}

		mgw := mergeGateways(gwWithInstances, &Proxy{}, &PushContext{})
		if mgw == nil {
			t.Fatal("mergeGateways returned nil")
		}

		// Should have 2 merged servers (one per bind)
		if len(mgw.MergedServers) != 2 {
			t.Errorf("Expected 2 merged servers, got %d", len(mgw.MergedServers))
		}

		// Should have routes for both HTTPS and HTTP
		httpsRoute := "https.443.https-port.foo-https.not-default.10.0.0.1"
		httpRoute := "http.443.10.0.0.2"

		if _, exists := mgw.ServersByRouteName[httpsRoute]; !exists {
			t.Errorf("HTTPS route %s not found in ServersByRouteName", httpsRoute)
		}

		if _, exists := mgw.ServersByRouteName[httpRoute]; !exists {
			t.Errorf("HTTP route %s not found in ServersByRouteName", httpRoute)
		}

		// Verify that both servers are properly configured
		httpsServerPort := ServerPort{Number: 443, Protocol: "HTTPS", Bind: "10.0.0.1"}
		httpServerPort := ServerPort{Number: 443, Protocol: "HTTP", Bind: "10.0.0.2"}

		if _, exists := mgw.MergedServers[httpsServerPort]; !exists {
			t.Errorf("HTTPS server port not found in MergedServers")
		}

		if _, exists := mgw.MergedServers[httpServerPort]; !exists {
			t.Errorf("HTTP server port not found in MergedServers")
		}
	})

	// Test case 2: HTTP first, then HTTPS (should work without the fix too)
	t.Run("http-first-then-https", func(t *testing.T) {
		gwWithInstances := []gatewayWithInstances{
			{gateway: gwHTTP, legacyGatewaySelector: true, instances: nil},
			{gateway: gwHTTPS, legacyGatewaySelector: true, instances: nil},
		}

		mgw := mergeGateways(gwWithInstances, &Proxy{}, &PushContext{})
		if mgw == nil {
			t.Fatal("mergeGateways returned nil")
		}

		// Should have 2 merged servers
		if len(mgw.MergedServers) != 2 {
			t.Errorf("Expected 2 merged servers, got %d", len(mgw.MergedServers))
		}

		// Should have routes for both HTTP and HTTPS
		httpRoute := "http.443.10.0.0.2"
		httpsRoute := "https.443.https-port.foo-https.not-default.10.0.0.1"

		if _, exists := mgw.ServersByRouteName[httpRoute]; !exists {
			t.Errorf("HTTP route %s not found in ServersByRouteName", httpRoute)
		}

		if _, exists := mgw.ServersByRouteName[httpsRoute]; !exists {
			t.Errorf("HTTPS route %s not found in ServersByRouteName", httpsRoute)
		}
	})
}

func makeInternalConfig(name, namespace, host, portName, portProtocol string, portNumber uint32, gw, bind string,
	mode networking.ServerTLSSettings_TLSmode, credNames []string, sa string,
) config.Config {
	c := makeConfig(name, namespace, host, portName, portProtocol, portNumber, gw, bind, mode, credNames, sa)
	c.Meta.Annotations[constants.InternalGatewaySemantics] = constants.GatewaySemanticsGateway
	return c
}

func TestFilterStrictGatewayMerging(t *testing.T) {
	noCreds := []string{}
	tlsSimple := networking.ServerTLSSettings_SIMPLE

	// Istio gateways (no InternalGatewaySemantics annotation)
	istioGwNsA := makeConfig("istio-gw1", "ns-a", "foo.example.com",
		"http", "HTTP", 80, "ingressgateway", "", tlsSimple, noCreds, "sa")
	istioGwNsB := makeConfig("istio-gw2", "ns-b", "bar.example.com",
		"http", "HTTP", 80, "ingressgateway", "", tlsSimple, noCreds, "sa")

	// Managed GatewayAPI gateway (has InternalGatewaySemantics + non-empty service account)
	managedGwapiNsA := makeInternalConfig("gwapi-gw1", "ns-a", "baz.example.com",
		"http", "HTTP", 80, "ingressgateway", "", tlsSimple, noCreds, "sa")

	// Unmanaged GatewayAPI gateway (has InternalGatewaySemantics but empty service account = manual deployment)
	unmanagedGwapiNsB := makeInternalConfig("gwapi-gw2", "ns-b", "qux.example.com",
		"http", "HTTP", 80, "ingressgateway", "", tlsSimple, noCreds, "")

	// Managed GatewayAPI gateway in ns-b
	managedGwapiNsB := makeInternalConfig("gwapi-gw3", "ns-b", "quux.example.com",
		"http", "HTTP", 80, "ingressgateway", "", tlsSimple, noCreds, "sa")

	toInstances := func(configs ...config.Config) []gatewayWithInstances {
		var out []gatewayWithInstances
		for _, c := range configs {
			out = append(out, gatewayWithInstances{gateway: c, legacyGatewaySelector: true})
		}
		return out
	}

	names := func(instances []gatewayWithInstances) []string {
		var out []string
		for _, gwi := range instances {
			out = append(out, gwi.gateway.Namespace+"/"+gwi.gateway.Name)
		}
		return out
	}

	tests := []struct {
		name     string
		input    []gatewayWithInstances
		expected []string
	}{
		{
			name:     "no gwapi gateways - no filtering",
			input:    toInstances(istioGwNsA, istioGwNsB),
			expected: []string{"ns-a/istio-gw1", "ns-b/istio-gw2"},
		},
		{
			name:     "only unmanaged gwapi - no filtering",
			input:    toInstances(istioGwNsA, istioGwNsB, unmanagedGwapiNsB),
			expected: []string{"ns-a/istio-gw1", "ns-b/istio-gw2", "ns-b/gwapi-gw2"},
		},
		{
			name:     "managed gwapi in ns-a - keep istio in ns-a, drop istio in ns-b",
			input:    toInstances(managedGwapiNsA, istioGwNsA, istioGwNsB),
			expected: []string{"ns-a/gwapi-gw1", "ns-a/istio-gw1"},
		},
		{
			name:     "managed gwapi in ns-a - keep all gwapi regardless of namespace",
			input:    toInstances(managedGwapiNsA, unmanagedGwapiNsB, istioGwNsA, istioGwNsB),
			expected: []string{"ns-a/gwapi-gw1", "ns-b/gwapi-gw2", "ns-a/istio-gw1"},
		},
		{
			name:     "managed gwapi in both namespaces - keep all",
			input:    toInstances(managedGwapiNsA, managedGwapiNsB, istioGwNsA, istioGwNsB),
			expected: []string{"ns-a/gwapi-gw1", "ns-b/gwapi-gw3", "ns-a/istio-gw1", "ns-b/istio-gw2"},
		},
		{
			name:     "only managed gwapi - keep all",
			input:    toInstances(managedGwapiNsA, managedGwapiNsB),
			expected: []string{"ns-a/gwapi-gw1", "ns-b/gwapi-gw3"},
		},
		{
			name:     "order does not matter",
			input:    toInstances(istioGwNsB, istioGwNsA, managedGwapiNsA),
			expected: []string{"ns-a/istio-gw1", "ns-a/gwapi-gw1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterStrictGatewayMerging(tt.input)
			got := names(result)
			if len(got) != len(tt.expected) {
				t.Fatalf("expected %v gateways, got %v: %v", len(tt.expected), len(got), got)
			}
			expectedSet := sets.New(tt.expected...)
			for _, name := range got {
				if !expectedSet.Contains(name) {
					t.Errorf("unexpected gateway in result: %s", name)
				}
			}
			for _, name := range tt.expected {
				found := false
				for _, g := range got {
					if g == name {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected gateway %s not found in result", name)
				}
			}
		})
	}
}

// sa controls which service account names are allowed to get secrets
func makeConfig(name, namespace, host, portName, portProtocol string, portNumber uint32, gw, bind string,
	mode networking.ServerTLSSettings_TLSmode, credNames []string, sa string,
) config.Config {
	c := config.Config{
		Meta: config.Meta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"internal.istio.io/service-account-name": sa,
			},
		},
		Spec: &networking.Gateway{
			Selector: map[string]string{"istio": gw},
			Servers: []*networking.Server{
				{
					Hosts: []string{host},
					Port:  &networking.Port{Name: portName, Number: portNumber, Protocol: portProtocol},
					Bind:  bind,
					Tls:   makeTLSSettings(mode, credNames),
				},
			},
		},
	}

	return c
}

func makeTLSSettings(mode networking.ServerTLSSettings_TLSmode, credNames []string) *networking.ServerTLSSettings {
	switch len(credNames) {
	case 0:
		return &networking.ServerTLSSettings{Mode: mode, CredentialName: ""}
	case 1:
		return &networking.ServerTLSSettings{Mode: mode, CredentialName: credNames[0]}
	}

	return &networking.ServerTLSSettings{Mode: mode, CredentialNames: credNames}
}

// makeConfigWithCaCert creates a gateway config with an explicit CaCertCredentialName, used to test
// GEP-91 implementation-specific support for caCertificateRefs kind:Secret.
func makeConfigWithCaCert(name, namespace, host, portName, portProtocol string, portNumber uint32, gw string,
	mode networking.ServerTLSSettings_TLSmode, serverCredName, caCertCredName, sa string,
) config.Config {
	return config.Config{
		Meta: config.Meta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"internal.istio.io/service-account-name": sa,
			},
		},
		Spec: &networking.Gateway{
			Selector: map[string]string{"istio": gw},
			Servers: []*networking.Server{
				{
					Hosts: []string{host},
					Port:  &networking.Port{Name: portName, Number: portNumber, Protocol: portProtocol},
					Tls: &networking.ServerTLSSettings{
						Mode:                 mode,
						CredentialName:       serverCredName,
						CaCertCredentialName: caCertCredName,
					},
				},
			},
		},
	}
}

func makeProxy(fn func() *spiffe.Identity) *Proxy {
	return &Proxy{
		VerifiedIdentity: fn(),
	}
}

func makePushContext() *PushContext {
	return &PushContext{
		GatewayAPIController: FakeController{},
	}
}

func BenchmarkParseGatewayRDSRouteName(b *testing.B) {
	for range b.N {
		ParseGatewayRDSRouteName("https.443.app1.gw1.ns1")
		ParseGatewayRDSRouteName("https.scooby.dooby.doo")
		ParseGatewayRDSRouteName("http.80")
	}
}

func TestParseGatewayRDSRouteName(t *testing.T) {
	type args struct {
		name string
	}
	tests := []struct {
		name           string
		args           args
		wantPortNumber int
		wantPortName   string
		wantGateway    string
	}{
		{
			name:           "invalid rds name",
			args:           args{"https.scooby.dooby.doo"},
			wantPortNumber: 0,
			wantPortName:   "",
			wantGateway:    "",
		},
		{
			name:           "gateway http rds name",
			args:           args{"http.80"},
			wantPortNumber: 80,
			wantPortName:   "",
			wantGateway:    "",
		},
		{
			name:           "https rds name",
			args:           args{"https.443.app1.gw1.ns1"},
			wantPortNumber: 443,
			wantPortName:   "app1",
			wantGateway:    "ns1/gw1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPortNumber, gotPortName, gotGateway := ParseGatewayRDSRouteName(tt.args.name)
			if gotPortNumber != tt.wantPortNumber {
				t.Errorf("ParseGatewayRDSRouteName() gotPortNumber = %v, want %v", gotPortNumber, tt.wantPortNumber)
			}
			if gotPortName != tt.wantPortName {
				t.Errorf("ParseGatewayRDSRouteName() gotPortName = %v, want %v", gotPortName, tt.wantPortName)
			}
			if gotGateway != tt.wantGateway {
				t.Errorf("ParseGatewayRDSRouteName() gotGateway = %v, want %v", gotGateway, tt.wantGateway)
			}
		})
	}
}
