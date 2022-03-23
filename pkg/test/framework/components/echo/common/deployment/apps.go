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
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/hashicorp/go-multierror"
	"golang.org/x/sync/errgroup"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

const (
	PodASvc          = "a"
	PodBSvc          = "b"
	PodCSvc          = "c"
	PodTproxySvc     = "tproxy"
	VMSvc            = "vm"
	HeadlessSvc      = "headless"
	StatefulSetSvc   = "statefulset"
	ProxylessGRPCSvc = "proxyless-grpc"
	NakedSvc         = "naked"
	ExternalSvc      = "external"
	DeltaSvc         = "delta"
	externalHostname = "fake.external.com"
)

type Namespace struct {
	// Namespace echo apps will be deployed
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

// ServiceNames returns the names of all services in this namespace.
func (n Namespace) ServiceNames() []string {
	out := make([]string, 0, len(n.All))
	for _, n := range n.All.ServiceNames() {
		out = append(out, n.Name)
	}
	sort.Strings(out)
	return out
}

func (n *Namespace) loadValues(t resource.Context, echos echo.Instances, d *Echos) error {
	ns := n.Namespace.Name()

	all := func(is echo.Instances) echo.Instances {
		if len(is) > 0 {
			n.All = append(n.All, is)
			return is
		}
		return nil
	}

	n.A = all(match.ServiceName(model.NamespacedName{Name: PodASvc, Namespace: ns}).GetMatches(echos))
	n.B = all(match.ServiceName(model.NamespacedName{Name: PodBSvc, Namespace: ns}).GetMatches(echos))
	n.C = all(match.ServiceName(model.NamespacedName{Name: PodCSvc, Namespace: ns}).GetMatches(echos))
	n.Tproxy = all(match.ServiceName(model.NamespacedName{Name: PodTproxySvc, Namespace: ns}).GetMatches(echos))
	n.Headless = all(match.ServiceName(model.NamespacedName{Name: HeadlessSvc, Namespace: ns}).GetMatches(echos))
	n.StatefulSet = all(match.ServiceName(model.NamespacedName{Name: StatefulSetSvc, Namespace: ns}).GetMatches(echos))
	n.Naked = all(match.ServiceName(model.NamespacedName{Name: NakedSvc, Namespace: ns}).GetMatches(echos))
	n.ProxylessGRPC = all(match.ServiceName(model.NamespacedName{Name: ProxylessGRPCSvc, Namespace: ns}).GetMatches(echos))
	if !t.Settings().Skip(echo.VM) {
		n.VM = all(match.ServiceName(model.NamespacedName{Name: VMSvc, Namespace: ns}).GetMatches(echos))
	}
	if !skipDeltaXDS(t) {
		n.DeltaXDS = all(match.ServiceName(model.NamespacedName{Name: DeltaSvc, Namespace: ns}).GetMatches(echos))
	}

	// Restrict egress from this namespace to only those endpoints in the same Echos.
	if err := t.ConfigIstio().Eval(ns, map[string]interface{}{
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
`).Apply(resource.NoCleanup); err != nil {
		return err
	}

	// Create a ServiceEntry to allow apps in this namespace to talk to the external service.
	if err := t.ConfigIstio().Eval(ns, map[string]interface{}{
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
`).Apply(resource.NoCleanup); err != nil {
		return err
	}
	return nil
}

type External struct {
	// Namespace where external echo app will be deployed
	Namespace namespace.Instance

	// Echos app to be used by tests, with no sidecar injected
	Echos echo.Instances
}

func (e *External) loadValues(echos echo.Instances) {
	e.Echos = match.ServiceName(model.NamespacedName{Name: ExternalSvc, Namespace: e.Namespace.Name()}).GetMatches(echos)
}

// Echos is a common set of echo deployments to support integration testing.
type Echos struct {
	// Ingressgateway instance
	Ingress   ingress.Instance
	Ingresses ingress.Instances

	// NS is the list of echo namespaces.
	NS []Namespace

	// External (out-of-mesh) deployments
	External External

	// All echo instances.
	All echo.Services
}

// NS1 is shorthand for NS[0]
func (d Echos) NS1() Namespace {
	return d.NS[0]
}

// NS2 is shorthand for NS[1]. Will panic if there are not at least 2 apps namespaces.
func (d Echos) NS2() Namespace {
	return d.NS[1]
}

// NS1AndNS2 returns the combined set of services in NS1 and NS2.
func (d Echos) NS1AndNS2() echo.Services {
	return d.NS1().All.Append(d.NS2().All)
}

func (d *Echos) loadValues(t resource.Context, echos echo.Instances) error {
	d.All = echos.Services()

	for i := 0; i < len(d.NS); i++ {
		if err := d.NS[i].loadValues(t, echos, d); err != nil {
			return err
		}
	}

	d.External.loadValues(echos)
	return nil
}

func (d Echos) namespaces(excludes ...namespace.Instance) []string {
	var out []string
	for _, n := range d.NS {
		include := true
		for _, e := range excludes {
			if n.Namespace.Name() == e.Name() {
				include = false
				break
			}
		}
		if include {
			out = append(out, n.Namespace.Name())
		}
	}

	sort.Strings(out)
	return out
}

func serviceEntryPorts() []echo.Port {
	var res []echo.Port
	for _, p := range ports.All().GetServicePorts() {
		if strings.HasPrefix(p.Name, "auto") {
			// The protocol needs to be set in common.EchoPorts to configure the echo deployment
			// But for service entry, we want to ensure we set it to "" which will use sniffing
			p.Protocol = ""
		}
		res = append(res, p)
	}
	return res
}

type Config struct {
	NamespaceCount int
}

func (c *Config) fillDefaults() {
	if c.NamespaceCount <= 1 {
		c.NamespaceCount = 1
	}
}

func Setup(t resource.Context, apps *Echos, cfg Config) error {
	cfg.fillDefaults()

	// Get the Istio component.
	i, err := istio.Get(t)
	if err != nil {
		return err
	}

	// Create the namespaces concurrently.
	g, _ := errgroup.WithContext(context.TODO())

	// Create the echo namespaces.
	apps.NS = make([]Namespace, cfg.NamespaceCount)
	if cfg.NamespaceCount == 1 {
		// If only using a single namespace, preserve the "echo" prefix.
		g.Go(func() (err error) {
			apps.NS[0].Namespace, err = namespace.New(t, namespace.Config{
				Prefix: "echo",
				Inject: true,
			})
			return
		})
	} else {
		for i := 0; i < cfg.NamespaceCount; i++ {
			i := i
			g.Go(func() (err error) {
				apps.NS[i].Namespace, err = namespace.New(t, namespace.Config{
					Prefix: fmt.Sprintf("echo%d", i),
					Inject: true,
				})
				return
			})
		}
	}

	// Create the external namespace.
	g.Go(func() (err error) {
		apps.External.Namespace, err = namespace.New(t, namespace.Config{
			Prefix: "external",
			Inject: false,
		})
		return
	})

	// Wait for the namespaces to be created.
	if err := g.Wait(); err != nil {
		return err
	}

	apps.Ingress = i.IngressFor(t.Clusters().Default())
	apps.Ingresses = i.Ingresses()

	builder := deployment.New(t).WithClusters(t.Clusters()...)
	for _, n := range apps.NS {
		builder = buildNamespace(t, builder, n.Namespace)
	}
	builder = buildExternal(builder, apps.External.Namespace)

	echos, err := builder.Build()
	if err != nil {
		return err
	}

	// Load values from the deployed echo instances.
	return apps.loadValues(t, echos)
}

// TODO: should t.Settings().Skip(echo.Delta) do all of this?
func skipDeltaXDS(t resource.Context) bool {
	return t.Settings().Skip(echo.Delta) || !t.Settings().Revisions.AtLeast("1.11")
}

func buildNamespace(t resource.Context, b deployment.Builder, ns namespace.Instance) deployment.Builder {
	b = b.WithConfig(echo.Config{
		Service:        PodASvc,
		Namespace:      ns,
		ServiceAccount: true,
		Ports:          ports.All(),
		Subsets:        []echo.SubsetConfig{{}},
		Locality:       "region.zone.subzone",
	}).
		WithConfig(echo.Config{
			Service:        PodBSvc,
			Namespace:      ns,
			ServiceAccount: true,
			Ports:          ports.All(),
			Subsets:        []echo.SubsetConfig{{}},
		}).
		WithConfig(echo.Config{
			Service:        PodCSvc,
			Namespace:      ns,
			ServiceAccount: true,
			Ports:          ports.All(),
			Subsets:        []echo.SubsetConfig{{}},
		}).
		WithConfig(echo.Config{
			Service:        HeadlessSvc,
			Namespace:      ns,
			ServiceAccount: true,
			Headless:       true,
			Ports:          ports.Headless(),
			Subsets:        []echo.SubsetConfig{{}},
		}).
		WithConfig(echo.Config{
			Service:        StatefulSetSvc,
			Namespace:      ns,
			ServiceAccount: true,
			Headless:       true,
			StatefulSet:    true,
			Ports:          ports.Headless(),
			Subsets:        []echo.SubsetConfig{{}},
		}).
		WithConfig(echo.Config{
			Service:        NakedSvc,
			Namespace:      ns,
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
			Service:        PodTproxySvc,
			Namespace:      ns,
			ServiceAccount: true,
			Ports:          ports.All(),
			Subsets: []echo.SubsetConfig{{
				Annotations: echo.NewAnnotations().Set(echo.SidecarInterceptionMode, "TPROXY"),
			}},
		}).
		WithConfig(echo.Config{
			Service:        VMSvc,
			Namespace:      ns,
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
				Namespace:      ns,
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
				Namespace:      ns,
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

func buildExternal(b deployment.Builder, ns namespace.Instance) deployment.Builder {
	return b.WithConfig(echo.Config{
		Service:           ExternalSvc,
		Namespace:         ns,
		DefaultHostHeader: externalHostname,
		Ports:             ports.All(),
		Subsets: []echo.SubsetConfig{
			{
				Annotations: map[echo.Annotation]*echo.AnnotationValue{
					echo.SidecarInject: {
						Value: strconv.FormatBool(false),
					},
				},
			},
		},
	})
}

// Restart restarts all echo deployments.
func (d Echos) Restart() error {
	wg := sync.WaitGroup{}
	aggregateErrMux := &sync.Mutex{}
	var aggregateErr error
	for _, app := range d.All.Instances() {
		app := app
		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := app.Restart(); err != nil {
				aggregateErrMux.Lock()
				aggregateErr = multierror.Append(aggregateErr, err)
				aggregateErrMux.Unlock()
			}
		}()
	}
	wg.Wait()
	if aggregateErr != nil {
		return aggregateErr
	}
	return nil
}
