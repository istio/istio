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
	"fmt"
	"strconv"
	"testing"

	"golang.org/x/sync/errgroup"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/telemetry"
)

var (
	client, server    echo.Instances
	nonInjectedServer echo.Instances
	mockProm          echo.Instances
	ist               istio.Instance
	appNsInst         namespace.Instance
	promInst          prometheus.Instance
	ingr              []ingress.Instance
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
	return appNsInst
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
	return client
}

func GetServerInstances() echo.Instances {
	return server
}

// TestStatsFilter includes common test logic for stats and metadataexchange filters running
// with nullvm and wasm runtime.
func TestStatsFilter(t *testing.T, feature features.Feature) {
	framework.NewTest(t).
		Features(feature).
		Run(func(ctx framework.TestContext) {
			// Enable strict mTLS. This is needed for mock secured prometheus scraping test.
			ctx.ConfigIstio().ApplyYAMLOrFail(ctx, ist.Settings().SystemNamespace, PeerAuthenticationConfig)
			g, _ := errgroup.WithContext(context.Background())
			for _, cltInstance := range client {
				cltInstance := cltInstance
				g.Go(func() error {
					err := retry.UntilSuccess(func() error {
						if err := SendTraffic(cltInstance); err != nil {
							return err
						}
						c := cltInstance.Config().Cluster
						sourceCluster := "Kubernetes"
						if len(ctx.AllClusters()) > 1 {
							sourceCluster = c.Name()
						}
						sourceQuery, destinationQuery, appQuery := buildQuery(sourceCluster)
						prom := GetPromInstance()
						// Query client side metrics
						if _, err := prom.QuerySum(c, sourceQuery); err != nil {
							t.Logf("prometheus values for istio_requests_total for cluster %v: \n%s", c.Name(), util.PromDump(c, promInst, "istio_requests_total"))
							return err
						}
						// Query client side metrics for non-injected server
						outOfMeshServerQuery := buildOutOfMeshServerQuery(sourceCluster)
						if _, err := prom.QuerySum(c, outOfMeshServerQuery); err != nil {
							t.Logf("prometheus values for istio_requests_total for cluster %v: \n%s", c.Name(), util.PromDump(c, promInst, "istio_requests_total"))
							return err
						}
						// Query server side metrics.
						if _, err := prom.QuerySum(c, destinationQuery); err != nil {
							t.Logf("prometheus values for istio_requests_total for cluster %v: \n%s", c.Name(), util.PromDump(c, promInst, "istio_requests_total"))
							return err
						}
						// This query will continue to increase due to readiness probe; don't wait for it to converge
						if _, err := prom.QuerySum(c, appQuery); err != nil {
							t.Logf("prometheus values for istio_echo_http_requests_total for cluster %v: \n%s",
								c.Name(), util.PromDump(c, promInst, "istio_echo_http_requests_total"))
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
			for _, prom := range mockProm {
				st := server.GetOrFail(ctx, echo.InCluster(prom.Config().Cluster))
				prom.CallWithRetryOrFail(t, echo.CallOptions{
					Address:            st.WorkloadsOrFail(t)[0].Address(),
					Scheme:             scheme.HTTPS,
					Port:               &echo.Port{ServicePort: 15014},
					Path:               "/metrics",
					CertFile:           "/etc/certs/custom/cert-chain.pem",
					KeyFile:            "/etc/certs/custom/key.pem",
					CaCertFile:         "/etc/certs/custom/root-cert.pem",
					InsecureSkipVerify: true,
				})
			}
		})
}

// TestStatsTCPFilter includes common test logic for stats and metadataexchange filters running
// with nullvm and wasm runtime for TCP.
func TestStatsTCPFilter(t *testing.T, feature features.Feature) {
	framework.NewTest(t).
		Features(feature).
		Run(func(ctx framework.TestContext) {
			g, _ := errgroup.WithContext(context.Background())
			for _, cltInstance := range client {
				cltInstance := cltInstance
				g.Go(func() error {
					err := retry.UntilSuccess(func() error {
						if err := SendTCPTraffic(cltInstance); err != nil {
							return err
						}
						c := cltInstance.Config().Cluster
						sourceCluster := "Kubernetes"
						if len(ctx.AllClusters()) > 1 {
							sourceCluster = c.Name()
						}
						destinationQuery := buildTCPQuery(sourceCluster)
						if _, err := GetPromInstance().Query(c, destinationQuery); err != nil {
							t.Logf("prometheus values for istio_tcp_connections_opened_total: \n%s", util.PromDump(c, promInst, "istio_tcp_connections_opened_total"))
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

// TestSetup set up echo app for stats testing.
func TestSetup(ctx resource.Context) (err error) {
	appNsInst, err = namespace.New(ctx, namespace.Config{
		Prefix: "echo",
		Inject: true,
	})
	if err != nil {
		return
	}

	outputCertAnnot := `
proxyMetadata:
  OUTPUT_CERTS: /etc/certs/custom`

	echos, err := echoboot.NewBuilder(ctx).
		WithClusters(ctx.Clusters()...).
		With(nil, echo.Config{
			Service:   "client",
			Namespace: appNsInst,
			Ports:     nil,
			Subsets:   []echo.SubsetConfig{{}},
		}).
		With(nil, echo.Config{
			Service:   "server",
			Namespace: appNsInst,
			Subsets:   []echo.SubsetConfig{{}},
			Ports: []echo.Port{
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					InstancePort: 8090,
				},
				{
					Name:     "tcp",
					Protocol: protocol.TCP,
					// We use a port > 1024 to not require root
					InstancePort: 9000,
					ServicePort:  9000,
				},
			},
		}).
		With(nil, echo.Config{
			Service:   "server-no-sidecar",
			Namespace: appNsInst,
			Subsets: []echo.SubsetConfig{
				{
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.SidecarInject: {
							Value: strconv.FormatBool(false),
						},
					},
				},
			},
			Ports: []echo.Port{
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					InstancePort: 8090,
				},
				{
					Name:     "tcp",
					Protocol: protocol.TCP,
					// We use a port > 1024 to not require root
					InstancePort: 9000,
					ServicePort:  9000,
				},
			},
		}).
		With(nil, echo.Config{
			// mock prom instance is used to mock a prometheus server, which will visit other echo instance /metrics
			// endpoint with proxy provisioned certs.
			Service:   "mock-prom",
			Namespace: appNsInst,
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
	client = echos.Match(echo.Service("client"))
	server = echos.Match(echo.Service("server"))
	nonInjectedServer = echos.Match(echo.Service("server-no-sidecar"))
	mockProm = echos.Match(echo.Service("mock-prom"))
	promInst, err = prometheus.New(ctx, prometheus.Config{})
	if err != nil {
		return
	}
	return nil
}

// SendTraffic makes a client call to the "server" service on the http port.
func SendTraffic(cltInstance echo.Instance) error {
	_, err := cltInstance.Call(echo.CallOptions{
		Target:    server[0],
		PortName:  "http",
		Count:     util.RequestCountMultipler * len(server),
		Validator: echo.ExpectOK(),
	})
	if err != nil {
		return err
	}
	_, err = cltInstance.Call(echo.CallOptions{
		Target:   nonInjectedServer[0],
		PortName: "http",
		Count:    util.RequestCountMultipler * len(nonInjectedServer),
	})
	if err != nil {
		return err
	}
	return nil
}

// SendTCPTraffic makes a client call to the "server" service on the tcp port.
func SendTCPTraffic(cltInstance echo.Instance) error {
	_, err := cltInstance.Call(echo.CallOptions{
		Target:   server[0],
		PortName: "tcp",
		Count:    util.RequestCountMultipler * len(server),
	})
	if err != nil {
		return err
	}
	return nil
}

// BuildQueryCommon is the shared function to construct prom query for istio_request_total metric.
func BuildQueryCommon(labels map[string]string, ns string) (sourceQuery, destinationQuery, appQuery string) {
	sourceQuery = `istio_requests_total{reporter="source",`
	destinationQuery = `istio_requests_total{reporter="destination",`

	for k, v := range labels {
		sourceQuery += fmt.Sprintf(`%s=%q,`, k, v)
		destinationQuery += fmt.Sprintf(`%s=%q,`, k, v)
	}
	sourceQuery += "}"
	destinationQuery += "}"
	appQuery += `istio_echo_http_requests_total{namespace="` + ns + `"}`
	return
}

func buildQuery(sourceCluster string) (sourceQuery, destinationQuery, appQuery string) {
	ns := GetAppNamespace()
	labels := map[string]string{
		"request_protocol":               "http",
		"response_code":                  "200",
		"destination_app":                "server",
		"destination_version":            "v1",
		"destination_service":            "server." + ns.Name() + ".svc.cluster.local",
		"destination_service_name":       "server",
		"destination_workload_namespace": ns.Name(),
		"destination_service_namespace":  ns.Name(),
		"source_app":                     "client",
		"source_version":                 "v1",
		"source_workload":                "client-v1",
		"source_workload_namespace":      ns.Name(),
		"source_cluster":                 sourceCluster,
	}

	return BuildQueryCommon(labels, ns.Name())
}

func buildOutOfMeshServerQuery(sourceCluster string) string {
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
		"destination_service":            "server-no-sidecar." + ns.Name() + ".svc.cluster.local",
		"destination_service_name":       "server-no-sidecar",
		"destination_workload_namespace": ns.Name(),
		"destination_service_namespace":  ns.Name(),
		"source_app":                     "client",
		"source_version":                 "v1",
		"source_workload":                "client-v1",
		"source_workload_namespace":      ns.Name(),
		"source_cluster":                 sourceCluster,
	}

	q := `istio_requests_total{reporter="source",`

	for k, v := range labels {
		q += fmt.Sprintf(`%s=%q,`, k, v)
	}
	q += "}"
	return q
}

func buildTCPQuery(sourceCluster string) (destinationQuery string) {
	ns := GetAppNamespace()
	destinationQuery = `istio_tcp_connections_opened_total{reporter="destination",`
	labels := map[string]string{
		"request_protocol":               "tcp",
		"destination_service_name":       "server",
		"destination_canonical_revision": "v1",
		"destination_canonical_service":  "server",
		"destination_app":                "server",
		"destination_version":            "v1",
		"destination_workload_namespace": ns.Name(),
		"destination_service_namespace":  ns.Name(),
		"source_app":                     "client",
		"source_version":                 "v1",
		"source_workload":                "client-v1",
		"source_workload_namespace":      ns.Name(),
		"source_cluster":                 sourceCluster,
	}
	for k, v := range labels {
		destinationQuery += fmt.Sprintf(`%s=%q,`, k, v)
	}
	destinationQuery += "}"
	return
}
