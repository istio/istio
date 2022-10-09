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
	"strconv"
	"strings"

	"github.com/hashicorp/go-multierror"
	"golang.org/x/sync/errgroup"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

// Config for new echo deployment.
type Config struct {
	// Echos is the target Echos for the newly created echo apps. If nil, a new Echos
	// instance will be created.
	Echos *Echos

	// NamespaceCount indicates the number of echo namespaces to be generated.
	// Ignored if Namespaces is non-empty. Defaults to 1.
	NamespaceCount int

	// Namespaces is the user-provided list of echo namespaces. If empty, NamespaceCount
	// namespaces will be generated.
	Namespaces []namespace.Getter

	// NoExternalNamespace if true, no external namespace will be generated and no external echo
	// instance will be deployed. Ignored if ExternalNamespace is non-nil.
	NoExternalNamespace bool

	// ExternalNamespace the namespace to use for the external deployment. If nil, a namespace
	// will be generated unless NoExternalNamespace is specified.
	ExternalNamespace namespace.Getter

	// IncludeExtAuthz if enabled, an additional ext-authz container will be included in the deployment.
	// This is mainly used to test the CUSTOM authorization policy when the ext-authz server is deployed
	// locally with the application container in the same pod.
	IncludeExtAuthz bool

	// Custom allows for configuring custom echo deployments. If a deployment's namespace
	// is nil, it will be created in all namespaces. Otherwise, it must match one of the
	// namespaces configured above.
	//
	// Custom echo instances will be accessible from the `All` field in the namespace(s) under which they
	// were created.
	Configs echo.ConfigGetter
}

func (c *Config) fillDefaults(ctx resource.Context) error {
	// Create the namespaces concurrently.
	g, _ := errgroup.WithContext(context.TODO())

	if c.Echos == nil {
		c.Echos = &Echos{}
	}

	if c.Configs == nil {
		defaultConfigs := c.DefaultEchoConfigs(ctx)
		c.Configs = echo.ConfigFuture(&defaultConfigs)
	}

	// Verify the namespace for any custom deployments.
	for _, config := range c.Configs.Get() {
		if config.Namespace != nil {
			found := false
			for _, ns := range c.Namespaces {
				if config.Namespace.Name() == ns.Get().Name() {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("custom echo deployment %s uses unconfigured namespace %s",
					config.NamespacedName().String(), config.NamespaceName())
			}
		}
	}

	if len(c.Namespaces) > 0 {
		c.NamespaceCount = len(c.Namespaces)
	} else if c.NamespaceCount <= 0 {
		c.NamespaceCount = 1
	}

	// Create the echo namespaces.
	if len(c.Namespaces) == 0 {
		c.Namespaces = make([]namespace.Getter, c.NamespaceCount)
		if c.NamespaceCount == 1 {
			// If only using a single namespace, preserve the "echo" prefix.
			g.Go(func() error {
				ns, err := namespace.New(ctx, namespace.Config{
					Prefix: "echo",
					Inject: true,
				})
				if err != nil {
					return err
				}
				c.Namespaces[0] = namespace.Future(&ns)
				return nil
			})
		} else {
			for i := 0; i < c.NamespaceCount; i++ {
				i := i
				g.Go(func() error {
					ns, err := namespace.New(ctx, namespace.Config{
						Prefix: fmt.Sprintf("echo%d", i+1),
						Inject: true,
					})
					if err != nil {
						return err
					}
					c.Namespaces[i] = namespace.Future(&ns)
					return nil
				})
			}
		}
	}

	// Create the external namespace, if necessary.
	if c.ExternalNamespace == nil && !c.NoExternalNamespace {
		g.Go(func() error {
			ns, err := namespace.New(ctx, namespace.Config{
				Prefix: "external",
				Inject: false,
			})
			if err != nil {
				return err
			}
			c.ExternalNamespace = namespace.Future(&ns)
			return nil
		})
	}

	// Wait for the namespaces to be created.
	return g.Wait()
}

func (c *Config) DefaultEchoConfigs(t resource.Context) []echo.Config {
	var defaultConfigs []echo.Config

	a := echo.Config{
		Service:         ASvc,
		ServiceAccount:  true,
		Ports:           ports.All(),
		Subsets:         []echo.SubsetConfig{{}},
		Locality:        "region.zone.subzone",
		IncludeExtAuthz: c.IncludeExtAuthz,
	}

	b := echo.Config{
		Service:         BSvc,
		ServiceAccount:  true,
		Ports:           ports.All(),
		Subsets:         []echo.SubsetConfig{{}},
		IncludeExtAuthz: c.IncludeExtAuthz,
	}

	cSvc := echo.Config{
		Service:         CSvc,
		ServiceAccount:  true,
		Ports:           ports.All(),
		Subsets:         []echo.SubsetConfig{{}},
		IncludeExtAuthz: c.IncludeExtAuthz,
	}

	headless := echo.Config{
		Service:         HeadlessSvc,
		ServiceAccount:  true,
		Headless:        true,
		Ports:           ports.Headless(),
		Subsets:         []echo.SubsetConfig{{}},
		IncludeExtAuthz: c.IncludeExtAuthz,
	}

	stateful := echo.Config{
		Service:         StatefulSetSvc,
		ServiceAccount:  true,
		Headless:        true,
		StatefulSet:     true,
		Ports:           ports.Headless(),
		Subsets:         []echo.SubsetConfig{{}},
		IncludeExtAuthz: c.IncludeExtAuthz,
	}

	naked := echo.Config{
		Service:        NakedSvc,
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
	}

	tProxy := echo.Config{
		Service:        TproxySvc,
		ServiceAccount: true,
		Ports:          ports.All(),
		Subsets: []echo.SubsetConfig{{
			Annotations: echo.NewAnnotations().Set(echo.SidecarInterceptionMode, "TPROXY"),
		}},
		IncludeExtAuthz: c.IncludeExtAuthz,
	}

	vmSvc := echo.Config{
		Service:         VMSvc,
		ServiceAccount:  true,
		Ports:           ports.All(),
		DeployAsVM:      true,
		AutoRegisterVM:  true,
		Subsets:         []echo.SubsetConfig{{}},
		IncludeExtAuthz: c.IncludeExtAuthz,
	}

	defaultConfigs = append(defaultConfigs, a, b, cSvc, headless, stateful, naked, tProxy, vmSvc)

	if !skipDeltaXDS(t) {
		delta := echo.Config{
			Service:        DeltaSvc,
			ServiceAccount: true,
			Ports:          ports.All(),
			Subsets: []echo.SubsetConfig{{
				Annotations: echo.NewAnnotations().Set(echo.SidecarProxyConfig, `proxyMetadata:
ISTIO_DELTA_XDS: "true"`),
			}},
		}
		defaultConfigs = append(defaultConfigs, delta)
	}

	if !t.Clusters().IsMulticluster() {
		// TODO when agent handles secure control-plane connection for grpc-less, deploy to "remote" clusters
		proxylessGRPC := echo.Config{
			Service:        ProxylessGRPCSvc,
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
		}
		defaultConfigs = append(defaultConfigs, proxylessGRPC)
	}

	return defaultConfigs
}

// View of an Echos deployment.
type View interface {
	// Echos returns the underlying Echos deployment for this view.
	Echos() *Echos
}

var (
	_ View = &SingleNamespaceView{}
	_ View = &TwoNamespaceView{}
	_ View = &Echos{}
)

// SingleNamespaceView is a simplified view of Echos for tests that only require a single namespace.
type SingleNamespaceView struct {
	// Include the echos at the top-level, so there is no need for accessing sub-structures.
	EchoNamespace

	// External (out-of-mesh) deployments
	External External

	// All echo instances
	All echo.Services

	echos *Echos
}

func (v *SingleNamespaceView) Echos() *Echos {
	return v.echos
}

// TwoNamespaceView is a simplified view of Echos for tests that require 2 namespaces.
type TwoNamespaceView struct {
	// Ns1 contains the echo deployments in the first namespace
	Ns1 EchoNamespace

	// Ns2 contains the echo deployments in the second namespace
	Ns2 EchoNamespace

	// Ns1AndNs2 contains just the echo services in Ns1 and Ns2 (excludes External).
	Ns1AndNs2 echo.Services

	// External (out-of-mesh) deployments
	External External

	// All echo instances
	All echo.Services

	echos *Echos
}

func (v *TwoNamespaceView) Echos() *Echos {
	return v.echos
}

// Echos is a common set of echo deployments to support integration testing.
type Echos struct {
	// NS is the list of echo namespaces.
	NS []EchoNamespace

	// External (out-of-mesh) deployments
	External External

	// All echo instances.
	All echo.Services
}

func (e *Echos) Echos() *Echos {
	return e
}

// New echo deployment with the given configuration.
func New(ctx resource.Context, cfg Config) (*Echos, error) {
	if err := cfg.fillDefaults(ctx); err != nil {
		return nil, err
	}

	apps := cfg.Echos
	apps.NS = make([]EchoNamespace, len(cfg.Namespaces))
	for i, ns := range cfg.Namespaces {
		apps.NS[i].Namespace = ns.Get()
	}
	apps.External.Namespace = cfg.ExternalNamespace.Get()

	builder := deployment.New(ctx).WithClusters(ctx.Clusters()...)
	for _, n := range apps.NS {
		builder = n.build(builder, cfg)
	}

	if !cfg.NoExternalNamespace {
		builder = apps.External.build(builder)
	}

	echos, err := builder.Build()
	if err != nil {
		return nil, err
	}

	apps.All = echos.Services()

	g := multierror.Group{}
	for i := 0; i < len(apps.NS); i++ {
		i := i
		g.Go(func() error {
			return apps.NS[i].loadValues(ctx, echos, apps)
		})
	}

	if !cfg.NoExternalNamespace {
		g.Go(func() error {
			return apps.External.loadValues(echos)
		})
	}

	if err := g.Wait().ErrorOrNil(); err != nil {
		return nil, err
	}

	return apps, nil
}

// NewOrFail calls New and fails if an error is returned.
func NewOrFail(t test.Failer, ctx resource.Context, cfg Config) *Echos {
	t.Helper()
	out, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// SingleNamespaceView converts this Echos into a SingleNamespaceView.
func (e *Echos) SingleNamespaceView() SingleNamespaceView {
	return SingleNamespaceView{
		EchoNamespace: e.NS[0],
		External:      e.External,
		All:           e.NS[0].All.Append(e.External.All.Services()),
		echos:         e,
	}
}

// TwoNamespaceView converts this Echos into a TwoNamespaceView.
func (e *Echos) TwoNamespaceView() TwoNamespaceView {
	ns1AndNs2 := e.NS[0].All.Append(e.NS[1].All)
	return TwoNamespaceView{
		Ns1:       e.NS[0],
		Ns2:       e.NS[1],
		Ns1AndNs2: ns1AndNs2,
		External:  e.External,
		All:       ns1AndNs2.Append(e.External.All.Services()),
		echos:     e,
	}
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

// SetupSingleNamespace calls Setup and returns a SingleNamespaceView.
func SetupSingleNamespace(view *SingleNamespaceView, cfg Config) resource.SetupFn {
	cfg.NamespaceCount = 1
	return func(ctx resource.Context) error {
		// Perform a setup with 1 namespace.
		var apps Echos
		if err := Setup(&apps, cfg)(ctx); err != nil {
			return err
		}

		// Store the view.
		*view = apps.SingleNamespaceView()
		return nil
	}
}

// SetupTwoNamespaces calls Setup and returns a TwoNamespaceView.
func SetupTwoNamespaces(view *TwoNamespaceView, cfg Config) resource.SetupFn {
	cfg.NamespaceCount = 2
	return func(ctx resource.Context) error {
		// Perform a setup with 2 namespaces.
		var apps Echos
		if err := Setup(&apps, cfg)(ctx); err != nil {
			return err
		}

		// Store the view.
		*view = apps.TwoNamespaceView()
		return nil
	}
}

// Setup function for writing to a global deployment variable.
func Setup(apps *Echos, cfg Config) resource.SetupFn {
	return func(ctx resource.Context) error {
		// Set the target for the deployments.
		cfg.Echos = apps

		_, err := New(ctx, cfg)
		if err != nil {
			return err
		}

		return nil
	}
}

// TODO(nmittler): should ctx.Settings().Skip(echo.Delta) do all of this?
func skipDeltaXDS(ctx resource.Context) bool {
	return ctx.Settings().Skip(echo.Delta) || !ctx.Settings().Revisions.AtLeast("1.12")
}
