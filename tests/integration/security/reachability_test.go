//go:build integ

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

package security

import (
	"testing"

	"istio.io/api/annotation"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/config"
	"istio.io/istio/pkg/test/framework/components/echo/config/param"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
)

const (
	migrationServiceName     = "migration"
	migrationVersionIstio    = "vistio"
	migrationVersionNonIstio = "vlegacy"
	migrationPathIstio       = "/" + migrationVersionIstio
	migrationPathNonIstio    = "/" + migrationVersionNonIstio
	mtlsModeParam            = "MTLSMode"
	mtlsModeOverrideParam    = "MTLSModeOverride"
	tlsModeParam             = "TLSMode"
	cMinIstioVersion         = "1.15.0"
	// cMinIstioVersionDS       = "1.16.0"
)

func TestReachability(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			systemNS := istio.ClaimSystemNamespaceOrFail(t, t)

			integIstioVersion := cMinIstioVersion
			// Create a custom echo deployment in NS1 with subsets that allows us to test the
			// migration of a workload to istio (from no sidecar to sidecar).
			migrationApp := deployment.New(t).
				WithClusters(t.Clusters()...).WithConfig(echo.Config{
				Namespace:      echo1NS,
				Service:        migrationServiceName,
				ServiceAccount: true,
				Ports:          ports.All(),
				Subsets: []echo.SubsetConfig{
					{
						// Istio deployment, with sidecar.
						Version:     migrationVersionIstio,
						Annotations: map[string]string{annotation.SidecarInject.Name: "true"},
					},
					{
						// Legacy (non-Istio) deployment subset, does not have sidecar injected.
						Version:     migrationVersionNonIstio,
						Annotations: map[string]string{annotation.SidecarInject.Name: "false"},
					},
				},
			}).BuildOrFail(t)

			// Add the migration app to the full list of services.
			allServices := apps.Ns1.All.Append(migrationApp.Services())

			// Create matchers for the migration app.
			migration := match.ServiceName(migrationApp.NamespacedName())
			notMigration := match.Not(migration)

			// Call options to be used for tests using the migration app.
			migrationOpts := []echo.CallOptions{
				{
					Port: echo.Port{
						Name: ports.HTTP.Name,
					},
					HTTP: echo.HTTP{
						Path: migrationPathIstio,
					},
				},
				{
					Port: echo.Port{
						Name: ports.HTTP.Name,
					},
					HTTP: echo.HTTP{
						Path: migrationPathNonIstio,
					},
				},
			}

			cases := []struct {
				name               string
				configs            config.Sources
				fromMatch          match.Matcher
				toMatch            match.Matcher
				callOpts           []echo.CallOptions
				expectMTLS         condition
				expectCrossCluster condition
				expectCrossNetwork condition
				expectSuccess      condition
				// minIstioVersion allows conditionally skipping based on required version
				minIstioVersion string
			}{
				{
					name: "global mtls strict",
					configs: config.Sources{
						config.File("testdata/reachability/global-peer-authn.yaml.tmpl"),
						config.File("testdata/reachability/global-dr.yaml.tmpl"),
					}.WithParams(param.Params{
						mtlsModeParam:            model.MTLSStrict.String(),
						tlsModeParam:             "ISTIO_MUTUAL",
						param.Namespace.String(): systemNS,
					}),
					fromMatch:          notMigration,
					toMatch:            notMigration,
					expectMTLS:         notNaked,
					expectCrossCluster: notFromNaked,
					expectCrossNetwork: notNaked,
					expectSuccess:      notNaked,
					minIstioVersion:    integIstioVersion,
					// For this one test, run all protocols.
					// For others, we will just run the 3 core (HTTP, HTTPS, TCP).
					// Because security code does not treat HTTP2, WS, or GRPC differently, we are skip those protocols to speed up tests,
					// and avoid expensive calls that give no coverage (TestReachability is by far the slowest test in Istio).
					callOpts: []echo.CallOptions{
						{
							Port: echo.Port{
								Name: ports.HTTP.Name,
							},
						},
						{
							Port: echo.Port{
								Name: ports.HTTP.Name,
							},
							Scheme: scheme.WebSocket,
						},
						{
							Port: echo.Port{
								Name: ports.HTTP2.Name,
							},
						},
						{
							Port: echo.Port{
								Name: ports.HTTPS.Name,
							},
						},
						{
							Port: echo.Port{
								Name: ports.TCP.Name,
							},
						},
						{
							Port: echo.Port{
								Name: ports.GRPC.Name,
							},
						},
					},
				},
				{
					name: "global mtls permissive",
					configs: config.Sources{
						config.File("testdata/reachability/global-peer-authn.yaml.tmpl"),
						config.File("testdata/reachability/global-dr.yaml.tmpl"),
					}.WithParams(param.Params{
						mtlsModeParam:            model.MTLSPermissive.String(),
						tlsModeParam:             "ISTIO_MUTUAL",
						param.Namespace.String(): systemNS,
					}),
					fromMatch:          notMigration,
					toMatch:            notMigration,
					expectMTLS:         notNaked,
					expectCrossCluster: notFromNaked,
					expectCrossNetwork: notNaked,
					expectSuccess:      notToNaked,
					minIstioVersion:    integIstioVersion,
				},
				{
					name: "global mtls disabled",
					configs: config.Sources{
						config.File("testdata/reachability/global-peer-authn.yaml.tmpl"),
						config.File("testdata/reachability/global-dr.yaml.tmpl"),
					}.WithParams(param.Params{
						mtlsModeParam:            model.MTLSDisable.String(),
						tlsModeParam:             "DISABLE",
						param.Namespace.String(): systemNS,
					}),
					fromMatch:          notMigration,
					toMatch:            notMigration,
					expectMTLS:         never,
					expectCrossCluster: notFromNaked,
					expectCrossNetwork: never,
					expectSuccess:      always,
					minIstioVersion:    integIstioVersion,
				},
				{
					name: "global plaintext to mtls permissive",
					configs: config.Sources{
						config.File("testdata/reachability/global-peer-authn.yaml.tmpl"),
						config.File("testdata/reachability/global-dr.yaml.tmpl"),
					}.WithParams(param.Params{
						mtlsModeParam:            model.MTLSPermissive.String(),
						tlsModeParam:             "DISABLE",
						param.Namespace.String(): systemNS,
					}),
					fromMatch:          notMigration,
					toMatch:            notMigration,
					expectMTLS:         never,
					expectCrossCluster: notFromNaked,
					expectCrossNetwork: never,
					expectSuccess:      always,
					minIstioVersion:    integIstioVersion,
				},
				{
					name: "global automtls strict",
					configs: config.Sources{
						// No DR is added for this test. enableAutoMtls is expected on by default.
						config.File("testdata/reachability/global-peer-authn.yaml.tmpl"),
					}.WithParams(param.Params{
						mtlsModeParam:            model.MTLSStrict.String(),
						param.Namespace.String(): systemNS,
					}),
					fromMatch:          notMigration,
					toMatch:            notMigration,
					expectMTLS:         notNaked,
					expectCrossCluster: notFromNaked,
					expectCrossNetwork: notNaked,
					expectSuccess:      notFromNaked,
				},
				{
					name: "global automtls disable",
					configs: config.Sources{
						// No DR is added for this test. enableAutoMtls is expected on by default.
						config.File("testdata/reachability/global-peer-authn.yaml.tmpl"),
					}.WithParams(param.Params{
						mtlsModeParam:            model.MTLSDisable.String(),
						param.Namespace.String(): systemNS,
					}),
					fromMatch:          notMigration,
					toMatch:            notMigration,
					expectMTLS:         never,
					expectCrossCluster: notFromNaked,
					expectCrossNetwork: never,
					expectSuccess:      always,
				},
				{
					name: "global automtls passthrough",
					configs: config.Sources{
						config.File("testdata/reachability/automtls-passthrough.yaml.tmpl"),
					}.WithNamespace(systemNS),
					fromMatch: notMigration,
					// VM passthrough doesn't work. We will send traffic to the ClusterIP of
					// the VM service, which will have 0 Endpoints. If we generated
					// EndpointSlice's for VMs this might work.
					toMatch:    match.And(match.NotVM, notMigration),
					expectMTLS: notNaked,
					// Since we are doing pass-through, all requests will stay in the same cluster,
					// as we are bypassing Istio load balancing.
					// TODO(https://github.com/istio/istio/issues/39700): Why does headless behave differently?
					expectCrossCluster: and(notFromNaked, or(toHeadless, toStatefulSet)),
					expectCrossNetwork: never,
					expectSuccess:      always,
					minIstioVersion:    integIstioVersion,
				},
				{
					name: "global no peer authn",
					configs: config.Sources{
						config.File("testdata/reachability/global-dr.yaml.tmpl"),
					}.WithParams(param.Params{
						tlsModeParam:             "ISTIO_MUTUAL",
						param.Namespace.String(): systemNS,
					}),
					fromMatch:          notMigration,
					toMatch:            notMigration,
					expectMTLS:         notNaked,
					expectCrossCluster: notFromNaked,
					expectCrossNetwork: notNaked,
					expectSuccess:      notToNaked,
					minIstioVersion:    integIstioVersion,
				},
				{
					name: "mtls strict",
					configs: config.Sources{
						config.File("testdata/reachability/workload-peer-authn.yaml.tmpl"),
						config.File("testdata/reachability/workload-dr.yaml.tmpl"),
					}.WithParams(param.Params{
						mtlsModeParam: model.MTLSStrict.String(),
						tlsModeParam:  "ISTIO_MUTUAL",
					}),
					fromMatch:          notMigration,
					toMatch:            notMigration,
					expectMTLS:         notNaked,
					expectCrossCluster: notFromNaked,
					expectCrossNetwork: notNaked,
					expectSuccess:      notNaked,
				},
				{
					name: "mtls permissive",
					configs: config.Sources{
						config.File("testdata/reachability/workload-peer-authn.yaml.tmpl"),
						config.File("testdata/reachability/workload-dr.yaml.tmpl"),
					}.WithParams(param.Params{
						mtlsModeParam: model.MTLSPermissive.String(),
						tlsModeParam:  "ISTIO_MUTUAL",
					}),
					fromMatch:          notMigration,
					toMatch:            notMigration,
					expectMTLS:         notNaked,
					expectCrossCluster: notFromNaked,
					expectCrossNetwork: notNaked,
					expectSuccess:      notToNaked,
				},
				{
					name: "mtls disabled",
					configs: config.Sources{
						config.File("testdata/reachability/workload-peer-authn.yaml.tmpl"),
						config.File("testdata/reachability/workload-dr.yaml.tmpl"),
					}.WithParams(param.Params{
						mtlsModeParam: model.MTLSDisable.String(),
						tlsModeParam:  "DISABLE",
					}),
					fromMatch:          notMigration,
					toMatch:            notMigration,
					expectMTLS:         never,
					expectCrossCluster: notFromNaked,
					expectCrossNetwork: never,
					expectSuccess:      always,
				},
				{
					name: "mtls port override",
					configs: config.Sources{
						config.File("testdata/reachability/workload-peer-authn-port-override.yaml.tmpl"),
					}.WithParams(param.Params{
						mtlsModeParam:         model.MTLSStrict.String(),
						mtlsModeOverrideParam: model.MTLSDisable.String(),
					}),
					fromMatch: notMigration,
					// TODO(https://github.com/istio/istio/issues/39439):
					toMatch:            match.And(match.NotHeadless, notMigration),
					expectMTLS:         never,
					expectCrossCluster: notFromNaked,
					expectCrossNetwork: never,
					expectSuccess:      always,
				},

				// --------start of auto mtls partial test cases ---------------
				// The follow three consecutive test together ensures the auto mtls works as intended
				// for sidecar migration scenario.
				{
					name: "migration no tls",
					configs: config.Sources{
						config.File("testdata/reachability/global-peer-authn.yaml.tmpl"),
						config.File("testdata/reachability/migration.yaml.tmpl"),
					}.WithParams(param.Params{
						mtlsModeParam:            model.MTLSStrict.String(),
						tlsModeParam:             "", // No TLS settings will be included.
						param.Namespace.String(): apps.Ns1.Namespace,
					}),
					fromMatch:          match.And(match.NotNaked, notMigration),
					toMatch:            migration,
					callOpts:           migrationOpts,
					expectMTLS:         toMigrationIstioSubset,
					expectCrossCluster: notFromNaked,
					expectCrossNetwork: toMigrationIstioSubset,
					expectSuccess:      always,
				},
				{
					name: "migration tls disabled",
					configs: config.Sources{
						config.File("testdata/reachability/global-peer-authn.yaml.tmpl"),
						config.File("testdata/reachability/migration.yaml.tmpl"),
					}.WithParams(param.Params{
						mtlsModeParam:            model.MTLSStrict.String(),
						tlsModeParam:             "DISABLE",
						param.Namespace.String(): apps.Ns1.Namespace,
					}),
					fromMatch:          match.And(match.NotNaked, notMigration),
					toMatch:            migration,
					callOpts:           migrationOpts,
					expectMTLS:         never,
					expectCrossCluster: notFromNaked,
					expectCrossNetwork: never,
					// Only the request to legacy one succeeds as we disable mtls explicitly.
					expectSuccess: toMigrationNonIstioSubset,
				},
				{
					name: "migration tls mutual",
					configs: config.Sources{
						config.File("testdata/reachability/global-peer-authn.yaml.tmpl"),
						config.File("testdata/reachability/migration.yaml.tmpl"),
					}.WithParams(param.Params{
						mtlsModeParam:            model.MTLSStrict.String(),
						tlsModeParam:             "ISTIO_MUTUAL",
						param.Namespace.String(): apps.Ns1.Namespace,
					}),
					fromMatch:          match.And(match.NotNaked, notMigration),
					toMatch:            migration,
					callOpts:           migrationOpts,
					expectMTLS:         toMigrationIstioSubset,
					expectCrossCluster: notFromNaked,
					expectCrossNetwork: toMigrationIstioSubset,
					// Only the request to vistio one succeeds as we enable mtls explicitly.
					expectSuccess: toMigrationIstioSubset,
				},
			}

			for _, c := range cases {
				t.NewSubTest(c.name).Run(func(t framework.TestContext) {
					if c.minIstioVersion != "" {
						skipMV := !t.Settings().Revisions.AtLeast(resource.IstioVersion(c.minIstioVersion))
						if skipMV {
							t.SkipNow()
						}
					}
					// Apply the configs.
					config.New(t).
						Source(c.configs...).
						BuildAll(nil, allServices).
						Apply()
					// Run the test against a number of ports.
					allOpts := append([]echo.CallOptions{}, c.callOpts...)
					if len(allOpts) == 0 {
						allOpts = []echo.CallOptions{
							{
								Port: echo.Port{
									Name: ports.HTTP.Name,
								},
							},
							{
								Port: echo.Port{
									Name: ports.HTTPS.Name,
								},
							},
							{
								Port: echo.Port{
									Name: ports.TCP.Name,
								},
							},
						}
					}

					// Iterate over all protocols outside, rather than inside, the destination match
					// This is to workaround a known bug (https://github.com/istio/istio/issues/38982) causing
					// connection resets when sending traffic to multiple ports at once
					for _, opts := range allOpts {
						schemeStr := string(opts.Scheme)
						if len(schemeStr) == 0 {
							schemeStr = opts.Port.Name
						}
						t.NewSubTestf("%s%s", schemeStr, opts.HTTP.Path).Run(func(t framework.TestContext) {
							// Run the test cases.
							echotest.New(t, allServices.Instances()).
								// Proxyless gRPC is not tested in this test
								// Headless and statefulset do not impact behavior as a client, so they are skipped in FromMatch to speed up tests
								FromMatch(match.And(c.fromMatch, match.NotProxylessGRPC, match.NotHeadless, match.NoStatefulSet)).
								ToMatch(match.And(c.toMatch, match.NotProxylessGRPC)).
								WithDefaultFilters(1, 1).
								ConditionallyTo(echotest.NoSelfCalls).
								Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
									opts := opts.DeepCopy()
									opts.To = to

									if c.expectSuccess(from, opts) {
										opts.Check = check.OK()

										// Check HTTP headers to confirm expected use of mTLS in the request.
										if c.expectMTLS(from, opts) {
											opts.Check = check.And(opts.Check, check.MTLSForHTTP())
										} else {
											opts.Check = check.And(opts.Check, check.PlaintextForHTTP())
										}

										// Check that the correct clusters/networks were reached.
										if c.expectCrossNetwork(from, opts) {
											if !check.IsDNSCaptureEnabled(t) && opts.To.Config().Headless {
												opts.Check = check.And(opts.Check, check.ReachedSourceCluster(t.Clusters()))
											} else {
												opts.Check = check.And(opts.Check, check.ReachedTargetClusters(t))
											}
										} else if c.expectCrossCluster(from, opts) {
											// Expect to stay in the same network as the source pod.
											expectedClusters := to.Clusters().ForNetworks(from.Config().Cluster.NetworkName())
											if !check.IsDNSCaptureEnabled(t) && opts.To.Config().Headless {
												opts.Check = check.And(opts.Check, check.ReachedSourceCluster(t.Clusters()))
											} else {
												opts.Check = check.And(opts.Check, check.ReachedClusters(t.Clusters(), expectedClusters))
											}
										} else {
											// Expect to stay in the same cluster as the source pod.
											expectedClusters := cluster.Clusters{from.Config().Cluster}
											if !check.IsDNSCaptureEnabled(t) && opts.To.Config().Headless {
												opts.Check = check.And(opts.Check, check.ReachedSourceCluster(t.Clusters()))
											} else {
												opts.Check = check.And(opts.Check, check.ReachedClusters(t.Clusters(), expectedClusters))
											}
										}
									} else {
										opts.Check = check.NotOK()
									}
									from.CallOrFail(t, opts)
								})
						})
					}
				})
			}
		})
}

type condition func(from echo.Instance, opts echo.CallOptions) bool

func not(c condition) condition {
	return func(from echo.Instance, opts echo.CallOptions) bool {
		return !c(from, opts)
	}
}

func and(conds ...condition) condition {
	return func(from echo.Instance, opts echo.CallOptions) bool {
		for _, c := range conds {
			if !c(from, opts) {
				return false
			}
		}
		return true
	}
}

func or(conds ...condition) condition {
	return func(from echo.Instance, opts echo.CallOptions) bool {
		for _, c := range conds {
			if c(from, opts) {
				return true
			}
		}
		return false
	}
}

var fromNaked condition = func(from echo.Instance, _ echo.CallOptions) bool {
	return from.Config().IsNaked()
}

var toNaked condition = func(_ echo.Instance, opts echo.CallOptions) bool {
	return opts.To.Config().IsNaked()
}

var toHeadless condition = func(_ echo.Instance, opts echo.CallOptions) bool {
	return opts.To.Config().IsHeadless()
}

var toStatefulSet condition = func(_ echo.Instance, opts echo.CallOptions) bool {
	return opts.To.Config().IsStatefulSet()
}

var toMigrationIstioSubset condition = func(_ echo.Instance, opts echo.CallOptions) bool {
	return opts.HTTP.Path == migrationPathIstio
}

var toMigrationNonIstioSubset condition = func(_ echo.Instance, opts echo.CallOptions) bool {
	return opts.HTTP.Path == migrationPathNonIstio
}

var anyNaked = or(fromNaked, toNaked)

var notNaked = not(anyNaked)

var notFromNaked = not(fromNaked)

var notToNaked = not(toNaked)

var always condition = func(echo.Instance, echo.CallOptions) bool {
	return true
}

var never condition = func(echo.Instance, echo.CallOptions) bool {
	return false
}
