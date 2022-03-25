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
	"strconv"

	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
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

func (n EchoNamespace) build(t resource.Context, b deployment.Builder) deployment.Builder {
	b = b.WithConfig(echo.Config{
		Service:        ASvc,
		Namespace:      n.Namespace,
		ServiceAccount: true,
		Ports:          ports.All(),
		Subsets:        []echo.SubsetConfig{{}},
		Locality:       "region.zone.subzone",
	}).
		WithConfig(echo.Config{
			Service:        BSvc,
			Namespace:      n.Namespace,
			ServiceAccount: true,
			Ports:          ports.All(),
			Subsets:        []echo.SubsetConfig{{}},
		}).
		WithConfig(echo.Config{
			Service:        CSvc,
			Namespace:      n.Namespace,
			ServiceAccount: true,
			Ports:          ports.All(),
			Subsets:        []echo.SubsetConfig{{}},
		}).
		WithConfig(echo.Config{
			Service:        HeadlessSvc,
			Namespace:      n.Namespace,
			ServiceAccount: true,
			Headless:       true,
			Ports:          ports.Headless(),
			Subsets:        []echo.SubsetConfig{{}},
		}).
		WithConfig(echo.Config{
			Service:        StatefulSetSvc,
			Namespace:      n.Namespace,
			ServiceAccount: true,
			Headless:       true,
			StatefulSet:    true,
			Ports:          ports.Headless(),
			Subsets:        []echo.SubsetConfig{{}},
		}).
		WithConfig(echo.Config{
			Service:        NakedSvc,
			Namespace:      n.Namespace,
			ServiceAccount: true,
			Ports:          ports.All(),
			Subsets: []echo.SubsetConfig{
				{
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.SidecarInject: {
							Value: strconv.FormatBool(false),
						},
					},
				},
			},
		}).
		WithConfig(echo.Config{
			Service:        TproxySvc,
			Namespace:      n.Namespace,
			ServiceAccount: true,
			Ports:          ports.All(),
			Subsets: []echo.SubsetConfig{{
				Annotations: echo.NewAnnotations().Set(echo.SidecarInterceptionMode, "TPROXY"),
			}},
		}).
		WithConfig(echo.Config{
			Service:        VMSvc,
			Namespace:      n.Namespace,
			ServiceAccount: true,
			Ports:          ports.All(),
			DeployAsVM:     true,
			AutoRegisterVM: true,
			Subsets:        []echo.SubsetConfig{{}},
		})

	if !skipDeltaXDS(t) {
		b = b.
			WithConfig(echo.Config{
				Service:        DeltaSvc,
				Namespace:      n.Namespace,
				ServiceAccount: true,
				Ports:          ports.All(),
				Subsets: []echo.SubsetConfig{{
					Annotations: echo.NewAnnotations().Set(echo.SidecarProxyConfig, `proxyMetadata:
  ISTIO_DELTA_XDS: "true"`),
				}},
			})
	}

	if !t.Clusters().IsMulticluster() {
		b = b.
			// TODO when agent handles secure control-plane connection for grpc-less, deploy to "remote" clusters
			WithConfig(echo.Config{
				Service:        ProxylessGRPCSvc,
				Namespace:      n.Namespace,
				ServiceAccount: true,
				Ports:          ports.All(),
				Subsets: []echo.SubsetConfig{
					{
						Annotations: map[echo.Annotation]*echo.AnnotationValue{
							echo.SidecarInjectTemplates: {
								Value: "grpc-agent",
							},
						},
					},
				},
			})
	}
	return b
}

func (n *EchoNamespace) loadValues(t resource.Context, echos echo.Instances, d *Echos) error {
	ns := n.Namespace

	all := func(is echo.Instances) echo.Instances {
		if len(is) > 0 {
			n.All = append(n.All, is)
			return is
		}
		return nil
	}

	n.A = all(match.ServiceName(echo.NamespacedName{Name: ASvc, Namespace: ns}).GetMatches(echos))
	n.B = all(match.ServiceName(echo.NamespacedName{Name: BSvc, Namespace: ns}).GetMatches(echos))
	n.C = all(match.ServiceName(echo.NamespacedName{Name: CSvc, Namespace: ns}).GetMatches(echos))
	n.Tproxy = all(match.ServiceName(echo.NamespacedName{Name: TproxySvc, Namespace: ns}).GetMatches(echos))
	n.Headless = all(match.ServiceName(echo.NamespacedName{Name: HeadlessSvc, Namespace: ns}).GetMatches(echos))
	n.StatefulSet = all(match.ServiceName(echo.NamespacedName{Name: StatefulSetSvc, Namespace: ns}).GetMatches(echos))
	n.Naked = all(match.ServiceName(echo.NamespacedName{Name: NakedSvc, Namespace: ns}).GetMatches(echos))
	n.ProxylessGRPC = all(match.ServiceName(echo.NamespacedName{Name: ProxylessGRPCSvc, Namespace: ns}).GetMatches(echos))
	if !t.Settings().Skip(echo.VM) {
		n.VM = all(match.ServiceName(echo.NamespacedName{Name: VMSvc, Namespace: ns}).GetMatches(echos))
	}
	if !skipDeltaXDS(t) {
		n.DeltaXDS = all(match.ServiceName(echo.NamespacedName{Name: DeltaSvc, Namespace: ns}).GetMatches(echos))
	}

	// Restrict egress from this namespace to only those endpoints in the same Echos.
	cfg := t.ConfigIstio().New()
	cfg.Eval(ns.Name(), map[string]interface{}{
		"otherNS": d.namespaces(n.Namespace),
	}, `
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: restrict-to-namespace
spec:
  egress:
  - hosts:
    - "./*"
    - "istio-system/*"
{{ range $ns := .otherNS }}
    - "{{ $ns }}/*"
{{ end }}
`)

	// Create a ServiceEntry to allow apps in this namespace to talk to the external service.
	cfg.Eval(ns.Name(), map[string]interface{}{
		"Namespace": d.External.Namespace.Name(),
		"Hostname":  externalHostname,
		"Ports":     serviceEntryPorts(),
	}, `apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: external-service
spec:
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

	return cfg.Apply(resource.NoCleanup)
}
