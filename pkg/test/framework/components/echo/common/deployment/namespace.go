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

package deployment

import (
	"fmt"

	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
)

const (
	ASvc             = "a"
	BSvc             = "b"
	CSvc             = "c"
	TproxySvc        = "tproxy"
	VMSvc            = "vm"
	HeadlessSvc      = "headless"
	StatefulSetSvc   = "statefulset"
	ProxylessGRPCSvc = "proxyless-grpc"
	NakedSvc         = "naked"
	DeltaSvc         = "delta"
)

// EchoNamespace contains the echo instances for a single namespace.
type EchoNamespace struct {
	// Namespace where the services are deployed.
	Namespace namespace.Instance

	// Standard echo app to be used by tests
	A echo.Instances
	// Standard echo app to be used by tests
	B echo.Instances
	// Standard echo app to be used by tests
	C echo.Instances
	// Standard echo app with TPROXY interception mode to be used by tests
	Tproxy echo.Instances
	// Headless echo app to be used by tests
	Headless echo.Instances
	// StatefulSet echo app to be used by tests
	StatefulSet echo.Instances
	// ProxylessGRPC echo app to be used by tests
	ProxylessGRPC echo.Instances
	// Echo app to be used by tests, with no sidecar injected
	Naked echo.Instances
	// A virtual machine echo app (only deployed to one cluster)
	VM echo.Instances
	// DeltaXDS echo app uses the delta XDS protocol. This should be functionally equivalent to A.
	DeltaXDS echo.Instances
	// All echo apps in this namespace
	All echo.Services
}

func (n EchoNamespace) build(b deployment.Builder, cfg Config) deployment.Builder {
	for _, config := range cfg.Configs.Get() {
		if config.Namespace == nil {
			config.Namespace = n.Namespace
		}
		b = b.WithConfig(config)
	}

	return b
}

func (n *EchoNamespace) loadValues(t resource.Context, echos echo.Instances, d *Echos) error {
	ns := n.Namespace
	n.All = match.Namespace(ns).GetMatches(echos).Services()

	n.A = match.ServiceName(echo.NamespacedName{Name: ASvc, Namespace: ns}).GetMatches(echos)
	n.B = match.ServiceName(echo.NamespacedName{Name: BSvc, Namespace: ns}).GetMatches(echos)
	n.C = match.ServiceName(echo.NamespacedName{Name: CSvc, Namespace: ns}).GetMatches(echos)
	n.Tproxy = match.ServiceName(echo.NamespacedName{Name: TproxySvc, Namespace: ns}).GetMatches(echos)
	n.Headless = match.ServiceName(echo.NamespacedName{Name: HeadlessSvc, Namespace: ns}).GetMatches(echos)
	n.StatefulSet = match.ServiceName(echo.NamespacedName{Name: StatefulSetSvc, Namespace: ns}).GetMatches(echos)
	n.Naked = match.ServiceName(echo.NamespacedName{Name: NakedSvc, Namespace: ns}).GetMatches(echos)
	n.ProxylessGRPC = match.ServiceName(echo.NamespacedName{Name: ProxylessGRPCSvc, Namespace: ns}).GetMatches(echos)
	if !t.Settings().Skip(echo.VM) {
		n.VM = match.ServiceName(echo.NamespacedName{Name: VMSvc, Namespace: ns}).GetMatches(echos)
	}
	if !skipDeltaXDS(t) {
		n.DeltaXDS = match.ServiceName(echo.NamespacedName{Name: DeltaSvc, Namespace: ns}).GetMatches(echos)
	}

	namespaces, err := namespace.GetAll(t)
	if err != nil {
		return fmt.Errorf("failed retrieving list of namespaces: %v", err)
	}

	// Restrict egress from this namespace to only those endpoints in the same Echos.
	cfg := t.ConfigIstio().New()
	cfg.Eval(ns.Name(), map[string]any{
		"Namespaces": namespaces,
	}, `
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: restrict-to-namespace
spec:
  egress:
  - hosts:
    - "istio-system/*"
{{ range $ns := .Namespaces }}
    - "{{ $ns.Name }}/*"
{{ end }}
`)

	// Create a ServiceEntry to allow apps in this namespace to talk to the external service.
	if d.External.Namespace != nil {
		cfg.Eval(ns.Name(), map[string]any{
			"Namespace": d.External.Namespace.Name(),
			"Hostname":  ExternalHostname,
			"Ports":     serviceEntryPorts(),
		}, `apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: external-service
spec:
  exportTo: [.]
  hosts:
  - {{.Hostname}}
  location: MESH_EXTERNAL
  resolution: DNS
  endpoints:
  - address: external.{{.Namespace}}.svc.cluster.local
  ports:
  - name: http-tls-origination
    number: 8888
    protocol: http
    targetPort: 443
  - name: http2-tls-origination
    number: 8882
    protocol: http2
    targetPort: 443
{{- range $i, $p := .Ports }}
  - name: {{$p.Name}}
    number: {{$p.ServicePort}}
    protocol: "{{$p.Protocol}}"
{{- end }}
`)
	}

	return cfg.Apply(apply.NoCleanup)
}
