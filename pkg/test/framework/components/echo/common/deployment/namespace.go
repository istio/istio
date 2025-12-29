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
	"istio.io/istio/pkg/test/framework/resource/config"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
)

const (
	ASvc             = "a"
	BSvc             = "b"
	CSvc             = "c"
	DSvc             = "d"
	ESvc             = "e"
	TproxySvc        = "tproxy"
	VMSvc            = "vm"
	HeadlessSvc      = "headless"
	StatefulSetSvc   = "statefulset"
	ProxylessGRPCSvc = "proxyless-grpc"
	NakedSvc         = "naked"
	SotwSvc          = "sotw"
	WaypointSvc      = "waypoint"
	CapturedSvc      = "captured"
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
	// IPv4 only echo app to be used by tests if running in dual-stack mode
	D echo.Instances
	// IPv6 only echo app to be used by tests if running in dual-stack mode
	E echo.Instances
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
	// Sotw echo app uses the sotw XDS protocol. This should be functionally equivalent to A.
	Sotw echo.Instances
	// All echo apps in this namespace
	All echo.Services

	// Common prefix for Service names
	ServiceNamePrefix string
}

func (n EchoNamespace) build(b deployment.Builder, cfg Config) deployment.Builder {
	for _, config := range cfg.Configs.Get() {
		if config.Namespace == nil {
			config.Namespace = n.Namespace
		}
		if config.Namespace.Name() == n.Namespace.Name() {
			b = b.WithConfig(config)
		}
	}
	return b
}

func (n *EchoNamespace) loadValues(t resource.Context, echos echo.Instances, d *Echos) error {
	ns := n.Namespace
	n.All = match.Namespace(ns).GetMatches(echos).Services()

	n.A = match.ServiceName(echo.NamespacedName{Name: n.ServiceNamePrefix + ASvc, Namespace: ns}).GetMatches(echos)
	n.B = match.ServiceName(echo.NamespacedName{Name: n.ServiceNamePrefix + BSvc, Namespace: ns}).GetMatches(echos)
	n.C = match.ServiceName(echo.NamespacedName{Name: n.ServiceNamePrefix + CSvc, Namespace: ns}).GetMatches(echos)
	if len(t.Settings().IPFamilies) > 1 {
		n.D = match.ServiceName(echo.NamespacedName{Name: n.ServiceNamePrefix + DSvc, Namespace: ns}).GetMatches(echos)
		n.E = match.ServiceName(echo.NamespacedName{Name: n.ServiceNamePrefix + ESvc, Namespace: ns}).GetMatches(echos)
	}
	n.Tproxy = match.ServiceName(echo.NamespacedName{Name: n.ServiceNamePrefix + TproxySvc, Namespace: ns}).GetMatches(echos)
	n.Headless = match.ServiceName(echo.NamespacedName{Name: n.ServiceNamePrefix + HeadlessSvc, Namespace: ns}).GetMatches(echos)
	n.StatefulSet = match.ServiceName(echo.NamespacedName{Name: n.ServiceNamePrefix + StatefulSetSvc, Namespace: ns}).GetMatches(echos)
	n.Naked = match.ServiceName(echo.NamespacedName{Name: n.ServiceNamePrefix + NakedSvc, Namespace: ns}).GetMatches(echos)
	n.ProxylessGRPC = match.ServiceName(echo.NamespacedName{Name: n.ServiceNamePrefix + ProxylessGRPCSvc, Namespace: ns}).GetMatches(echos)
	if !t.Settings().Skip(echo.VM) {
		n.VM = match.ServiceName(echo.NamespacedName{Name: n.ServiceNamePrefix + VMSvc, Namespace: ns}).GetMatches(echos)
	}
	if !t.Settings().Skip(echo.Sotw) {
		n.Sotw = match.ServiceName(echo.NamespacedName{Name: n.ServiceNamePrefix + SotwSvc, Namespace: ns}).GetMatches(echos)
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
apiVersion: networking.istio.io/v1
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

	if !t.Settings().DisableDefaultExternalServiceConnectivity {
		// Create a ServiceEntry to allow apps in this namespace to talk to the external service.
		if d.External.Namespace != nil {
			DeployExternalServiceEntry(cfg, ns, d.External.Namespace, false)
		}
	}

	return cfg.Apply(apply.NoCleanup)
}

func DeployExternalServiceEntry(cfg config.Factory, deployedNamespace, externalNamespace namespace.Instance, manuallyAllocate bool) config.Plan {
	return cfg.Eval(deployedNamespace.Name(), map[string]any{
		"Namespace":        externalNamespace.Name(),
		"Hostname":         ExternalHostname,
		"Ports":            serviceEntryPorts(),
		"ManuallyAllocate": manuallyAllocate,
	}, `apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: external-service
spec:
{{- if .ManuallyAllocate }}
  addresses: [240.240.240.239, 2001:2::f0f0:239] # Semi-random addresses for the range Istio allocates in
{{- end }}
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
