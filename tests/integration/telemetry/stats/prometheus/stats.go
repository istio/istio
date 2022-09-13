//go:build integ
// +build integ

// Copyright Istio Authors. All Rights Reserved.
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

package prometheus

import (
	"context"
	"path/filepath"
	"testing"

	"golang.org/x/sync/errgroup"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	cdeployment "istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/telemetry"
)

var (
	apps cdeployment.SingleNamespaceView

	mockProm echo.Instances
	ist      istio.Instance
	promInst prometheus.Instance
	ingr     []ingress.Instance
)

var PeerAuthenticationConfig = `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
spec:
  mtls:
    mode: STRICT
`

// GetIstioInstance gets Istio instance.
func GetIstioInstance() *istio.Instance {
	return &ist
}

// GetAppNamespace gets bookinfo instance.
func GetAppNamespace() namespace.Instance {
	return apps.Namespace
}

// GetPromInstance gets prometheus instance.
func GetPromInstance() prometheus.Instance {
	return promInst
}

// GetIstioInstance gets Istio instance.
func GetIngressInstance() []ingress.Instance {
	return ingr
}

func GetClientInstances() echo.Instances {
	return apps.A
}

func GetTarget() echo.Target {
	return apps.B
}

// TestStatsFilter includes common test logic for stats and metadataexchange filters running
// with nullvm and wasm runtime.
func TestStatsFilter(t *testing.T, feature features.Feature) {
	framework.NewTest(t).
		Features(feature).
		Run(func(t framework.TestContext) {
			// Enable strict mTLS. This is needed for mock secured prometheus scraping test.
			t.ConfigIstio().YAML(ist.Settings().SystemNamespace, PeerAuthenticationConfig).ApplyOrFail(t)
			g, _ := errgroup.WithContext(context.Background())
			for _, cltInstance := range GetClientInstances() {
				cltInstance := cltInstance
				g.Go(func() error {
					err := retry.UntilSuccess(func() error {
						if err := SendTraffic(cltInstance); err != nil {
							return err
						}
						c := cltInstance.Config().Cluster
						sourceCluster := "Kubernetes"
						if len(t.AllClusters()) > 1 {
							sourceCluster = c.Name()
						}
						sourceQuery, destinationQuery, appQuery := buildQuery(sourceCluster)
						prom := GetPromInstance()
						// Query client side metrics
						if _, err := prom.QuerySum(c, sourceQuery); err != nil {
							util.PromDiff(t, prom, c, sourceQuery)
							return err
						}
						// Query client side metrics for non-injected server
						outOfMeshServerQuery := buildOutOfMeshServerQuery(sourceCluster)
						if _, err := prom.QuerySum(c, outOfMeshServerQuery); err != nil {
							util.PromDiff(t, prom, c, outOfMeshServerQuery)
							return err
						}
						// Query server side metrics.
						if _, err := prom.QuerySum(c, destinationQuery); err != nil {
							util.PromDiff(t, prom, c, destinationQuery)
							return err
						}
						// This query will continue to increase due to readiness probe; don't wait for it to converge
						if _, err := prom.QuerySum(c, appQuery); err != nil {
							util.PromDiff(t, prom, c, appQuery)
							return err
						}

						return nil
					}, retry.Delay(framework.TelemetryRetryDelay), retry.Timeout(framework.TelemetryRetryTimeout))
					if err != nil {
						return err
					}
					return nil
				})
			}
			if err := g.Wait(); err != nil {
				t.Fatalf("test failed: %v", err)
			}

			// In addition, verifies that mocked prometheus could call metrics endpoint with proxy provisioned certs
			t.NewSubTest("mockprom-to-metrics").Run(
				func(t framework.TestContext) {
					for _, prom := range mockProm {
						st := match.Cluster(prom.Config().Cluster).FirstOrFail(t, GetTarget().Instances())
						prom.CallOrFail(t, echo.CallOptions{
							ToWorkload: st,
							Scheme:     scheme.HTTPS,
							Port:       echo.Port{ServicePort: 15014},
							HTTP: echo.HTTP{
								Path: "/metrics",
							},
							TLS: echo.TLS{
								CertFile:           "/etc/certs/custom/cert-chain.pem",
								KeyFile:            "/etc/certs/custom/key.pem",
								CaCertFile:         "/etc/certs/custom/root-cert.pem",
								InsecureSkipVerify: true,
							},
						})
					}
				})
		})
}

// TestStatsTCPFilter includes common test logic for stats and metadataexchange filters running
// with nullvm and wasm runtime for TCP.
func TestStatsTCPFilter(t *testing.T, feature features.Feature) {
	framework.NewTest(t).
		Features(feature).
		Run(func(t framework.TestContext) {
			g, _ := errgroup.WithContext(context.Background())
			for _, cltInstance := range GetClientInstances() {
				cltInstance := cltInstance
				g.Go(func() error {
					err := retry.UntilSuccess(func() error {
						if err := SendTCPTraffic(cltInstance); err != nil {
							return err
						}
						c := cltInstance.Config().Cluster
						sourceCluster := "Kubernetes"
						if len(t.AllClusters()) > 1 {
							sourceCluster = c.Name()
						}
						destinationQuery := buildTCPQuery(sourceCluster)
						if _, err := GetPromInstance().Query(c, destinationQuery); err != nil {
							util.PromDiff(t, promInst, c, destinationQuery)
							return err
						}

						return nil
					}, retry.Delay(framework.TelemetryRetryDelay), retry.Timeout(framework.TelemetryRetryTimeout))
					if err != nil {
						return err
					}
					return nil
				})
			}
			if err := g.Wait(); err != nil {
				t.Fatalf("test failed: %v", err)
			}
		})
}

func TestStatsGatewayServerTCPFilter(t *testing.T, feature features.Feature) {
	framework.NewTest(t).
		Features(feature).
		Run(func(t framework.TestContext) {
			base := filepath.Join(env.IstioSrc, "tests/integration/telemetry/stats/prometheus/testdata/")
			// Following resources are being deployed to test sidecar->gateway communication. With following resources,
			// routing is being setup from sidecar to external site, via egress gateway.
			// clt(https:443) -> sidecar(tls:443) -> istio-mtls -> (TLS:443)egress-gateway-> vs(tcp:443) -> cnn.com
			t.ConfigIstio().File(GetAppNamespace().Name(), filepath.Join(base, "istio-mtls-dest-rule.yaml")).ApplyOrFail(t)
			t.ConfigIstio().File(GetAppNamespace().Name(), filepath.Join(base, "istio-mtls-gateway.yaml")).ApplyOrFail(t)
			t.ConfigIstio().File(GetAppNamespace().Name(), filepath.Join(base, "istio-mtls-vs.yaml")).ApplyOrFail(t)

			// The main SE is available only to app namespace, make one the egress can access.
			t.ConfigIstio().Eval(ist.Settings().SystemNamespace, map[string]any{
				"Namespace": apps.External.Namespace.Name(),
				"Hostname":  cdeployment.ExternalHostname,
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
  - name: https
    number: 443
    protocol: HTTPS
`).ApplyOrFail(t, apply.NoCleanup)
			g, _ := errgroup.WithContext(context.Background())
			for _, cltInstance := range GetClientInstances() {
				cltInstance := cltInstance
				g.Go(func() error {
					err := retry.UntilSuccess(func() error {
						if _, err := cltInstance.Call(echo.CallOptions{
							Address: "fake.external.com",
							Scheme:  scheme.HTTPS,
							Port:    echo.Port{ServicePort: ports.All().MustForName(ports.HTTPS).ServicePort},
							Count:   1,
							Retry:   echo.Retry{NoRetry: true}, // we do retry in outer loop
							Check:   check.OK(),
						}); err != nil {
							return err
						}

						c := cltInstance.Config().Cluster
						sourceCluster := "Kubernetes"
						if len(t.AllClusters()) > 1 {
							sourceCluster = c.Name()
						}
						destinationQuery := buildGatewayTCPServerQuery(sourceCluster)
						if _, err := GetPromInstance().Query(c, destinationQuery); err != nil {
							util.PromDiff(t, promInst, c, destinationQuery)
							return err
						}
						return nil
					}, retry.Delay(framework.TelemetryRetryDelay), retry.Timeout(framework.TelemetryRetryTimeout))
					if err != nil {
						t.Fatalf("test failed: %v", err)
					}
					return nil
				})
			}
			if err := g.Wait(); err != nil {
				t.Fatalf("test failed: %v", err)
			}
		})
}

// TestSetup set up echo app for stats testing.
func TestSetup(ctx resource.Context) (err error) {
	if err := cdeployment.SetupSingleNamespace(&apps, cdeployment.Config{})(ctx); err != nil {
		return err
	}

	outputCertAnnot := `
proxyMetadata:
  OUTPUT_CERTS: /etc/certs/custom`

	echos, err := deployment.New(ctx).
		WithClusters(ctx.Clusters()...).
		With(nil, echo.Config{
			// mock prom instance is used to mock a prometheus server, which will visit other echo instance /metrics
			// endpoint with proxy provisioned certs.
			Service:   "mock-prom",
			Namespace: apps.Namespace,
			Subsets: []echo.SubsetConfig{
				{
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.SidecarIncludeInboundPorts: {
							Value: "",
						},
						echo.SidecarIncludeOutboundIPRanges: {
							Value: "",
						},
						echo.SidecarProxyConfig: {
							Value: outputCertAnnot,
						},
						echo.SidecarVolumeMount: {
							Value: `[{"name": "custom-certs", "mountPath": "/etc/certs/custom"}]`,
						},
					},
				},
			},
			TLSSettings: &common.TLSSettings{
				ProxyProvision: true,
			},
			Ports: []echo.Port{},
		}).Build()
	if err != nil {
		return err
	}
	for _, c := range ctx.Clusters() {
		ingr = append(ingr, ist.IngressFor(c))
	}
	mockProm = match.ServiceName(echo.NamespacedName{Name: "mock-prom", Namespace: apps.Namespace}).GetMatches(echos)
	promInst, err = prometheus.New(ctx, prometheus.Config{})
	if err != nil {
		return
	}
	return nil
}

// SendTraffic makes a client call to the "server" service on the http port.
func SendTraffic(from echo.Instance) error {
	_, err := from.Call(echo.CallOptions{
		To: GetTarget(),
		Port: echo.Port{
			Name: "http",
		},
		Check: check.OK(),
		Retry: echo.Retry{
			NoRetry: true,
		},
	})
	if err != nil {
		return err
	}
	_, err = from.Call(echo.CallOptions{
		To: apps.Naked,
		Port: echo.Port{
			Name: "http",
		},
		Retry: echo.Retry{
			NoRetry: true,
		},
	})
	if err != nil {
		return err
	}
	return nil
}

func SendTrafficOrFail(t test.Failer, from echo.Instance) {
	from.CallOrFail(t, echo.CallOptions{
		To: GetTarget(),
		Port: echo.Port{
			Name: "http",
		},
		Check: check.OK(),
	})
	from.CallOrFail(t, echo.CallOptions{
		To: apps.Naked,
		Port: echo.Port{
			Name: "http",
		},
		Retry: echo.Retry{
			NoRetry: true,
		},
	})
}

// SendTCPTraffic makes a client call to the "server" service on the tcp port.
func SendTCPTraffic(from echo.Instance) error {
	_, err := from.Call(echo.CallOptions{
		To: GetTarget(),
		Port: echo.Port{
			Name: "tcp",
		},
		Retry: echo.Retry{
			NoRetry: true,
		},
	})
	if err != nil {
		return err
	}
	return nil
}

// BuildQueryCommon is the shared function to construct prom query for istio_request_total metric.
func BuildQueryCommon(labels map[string]string, ns string) (sourceQuery, destinationQuery, appQuery prometheus.Query) {
	sourceQuery.Metric = "istio_requests_total"
	sourceQuery.Labels = clone(labels)
	sourceQuery.Labels["reporter"] = "source"

	destinationQuery.Metric = "istio_requests_total"
	destinationQuery.Labels = clone(labels)
	destinationQuery.Labels["reporter"] = "destination"

	appQuery.Metric = "istio_echo_http_requests_total"
	appQuery.Labels = map[string]string{"namespace": ns}

	return
}

func clone(labels map[string]string) map[string]string {
	ret := map[string]string{}
	for k, v := range labels {
		ret[k] = v
	}
	return ret
}

func buildQuery(sourceCluster string) (sourceQuery, destinationQuery, appQuery prometheus.Query) {
	ns := GetAppNamespace()
	labels := map[string]string{
		"request_protocol":               "http",
		"response_code":                  "200",
		"destination_app":                "b",
		"destination_version":            "v1",
		"destination_service":            "b." + ns.Name() + ".svc.cluster.local",
		"destination_service_name":       "b",
		"destination_workload_namespace": ns.Name(),
		"destination_service_namespace":  ns.Name(),
		"source_app":                     "a",
		"source_version":                 "v1",
		"source_workload":                "a-v1",
		"source_workload_namespace":      ns.Name(),
		"source_cluster":                 sourceCluster,
	}

	return BuildQueryCommon(labels, ns.Name())
}

func buildOutOfMeshServerQuery(sourceCluster string) prometheus.Query {
	ns := GetAppNamespace()
	labels := map[string]string{
		"request_protocol": "http",
		"response_code":    "200",
		// For out of mesh server, client side metrics rely on endpoint resource metadata
		// to fill in workload labels. To limit size of endpoint resource, we only populate
		// workload name and namespace, canonical service name and version in endpoint metadata.
		// Thus destination_app and destination_version labels are unknown.
		"destination_app":                "unknown",
		"destination_version":            "unknown",
		"destination_service":            "naked." + ns.Name() + ".svc.cluster.local",
		"destination_service_name":       "naked",
		"destination_workload_namespace": ns.Name(),
		"destination_service_namespace":  ns.Name(),
		"source_app":                     "a",
		"source_version":                 "v1",
		"source_workload":                "a-v1",
		"source_workload_namespace":      ns.Name(),
		"source_cluster":                 sourceCluster,
	}

	source, _, _ := BuildQueryCommon(labels, ns.Name())
	return source
}

func buildTCPQuery(sourceCluster string) (destinationQuery prometheus.Query) {
	ns := GetAppNamespace()
	labels := map[string]string{
		"request_protocol":               "tcp",
		"destination_service_name":       "b",
		"destination_canonical_revision": "v1",
		"destination_canonical_service":  "b",
		"destination_app":                "b",
		"destination_version":            "v1",
		"destination_workload_namespace": ns.Name(),
		"destination_service_namespace":  ns.Name(),
		"source_app":                     "a",
		"source_version":                 "v1",
		"source_workload":                "a-v1",
		"source_workload_namespace":      ns.Name(),
		"source_cluster":                 sourceCluster,
		"reporter":                       "destination",
	}
	return prometheus.Query{
		Metric: "istio_tcp_connections_opened_total",
		Labels: labels,
	}
}

func buildGatewayTCPServerQuery(sourceCluster string) (destinationQuery prometheus.Query) {
	ns := GetAppNamespace()
	labels := map[string]string{
		"request_protocol":               "tcp",
		"destination_service_name":       "istio-egressgateway",
		"destination_canonical_revision": "latest",
		"destination_canonical_service":  "istio-egressgateway",
		"destination_app":                "istio-egressgateway",
		"destination_version":            "unknown",
		"destination_workload_namespace": "istio-system",
		"destination_service_namespace":  "istio-system",
		"source_app":                     "a",
		"source_version":                 "v1",
		"source_workload":                "a-v1",
		"source_workload_namespace":      ns.Name(),
		"source_cluster":                 sourceCluster,
		"reporter":                       "source",
	}
	return prometheus.Query{
		Metric: "istio_tcp_connections_opened_total",
		Labels: labels,
	}
}
