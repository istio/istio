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

package api

import (
	"context"
	"fmt"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/telemetry"
)

const (
	additionalLabelConfigPath          = "testdata/additional-labels.yaml"
	strictMtlsPeerAuthenticationConfig = `
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: default
spec:
  mtls:
    mode: STRICT
`
)

func GetClientInstances() echo.Instances {
	return apps.A
}

func GetTarget() echo.Target {
	return apps.B
}

// TestStatsFilter verifies the stats filter could emit expected client and server side
// metrics when configured with the Telemetry API (with EnvoyFilters disabled).
// This test focuses on stats filter and metadata exchange filter could work coherently with
// proxy bootstrap config with Wasm runtime. To avoid flake, it does not verify correctness
// of metrics, which should be covered by integration test in proxy repo.
func TestStatsFilter(t *testing.T) {
	expectedBuckets := DefaultBucketCount
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			t.NewSubTest("default").Run(func(t framework.TestContext) {
				runStatsTest(t, expectedBuckets, false)
			})
			t.NewSubTest("additional-labels").Run(func(t framework.TestContext) {
				if t.Settings().PeerMetadataDiscovery {
					t.Skipf("Peer metadata discovery is enabled, skipping additional-labels test")
				}
				runStatsTest(t, expectedBuckets, true)
			})
			// In addition, verifies that mocked prometheus could call metrics endpoint with proxy provisioned certs
			t.NewSubTest("mockprom-to-metrics").Run(
				func(t framework.TestContext) {
					// Enable strict mTLS. This is needed for mock secured prometheus scraping test.
					t.ConfigIstio().YAML(ist.Settings().SystemNamespace, strictMtlsPeerAuthenticationConfig).ApplyOrFail(t)
					for _, prom := range mockProm {
						retry.UntilSuccessOrFail(t, func() error {
							st := match.Cluster(prom.Config().Cluster).FirstOrFail(t, GetTarget().Instances())
							_, callErr := prom.Call(echo.CallOptions{
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
							if callErr != nil {
								t.Logf("failed to call metrics endpoint: %v", st.NamespacedName())
							}
							return callErr
						})
					}
				})
		})
}

func runStatsTest(t framework.TestContext, expectedBuckets int, enableMXAdditionalLabels bool) {
	// Enable strict mTLS. This is needed for mock secured prometheus scraping test.
	t.ConfigIstio().YAML(ist.Settings().SystemNamespace, strictMtlsPeerAuthenticationConfig).ApplyOrFail(t)
	if enableMXAdditionalLabels {
		// use namespace scoped Telemetry API to override the root level config
		t.ConfigIstio().File(apps.Namespace.Name(), additionalLabelConfigPath).ApplyOrFail(t)
	}
	g, _ := errgroup.WithContext(context.Background())
	for _, cltInstance := range GetClientInstances() {
		g.Go(func() error {
			err := retry.UntilSuccess(func() error {
				if err := SendTraffic(cltInstance); err != nil {
					return err
				}
				c := cltInstance.Config().Cluster
				sourceCluster := constants.DefaultClusterName
				if len(t.AllClusters()) > 1 {
					sourceCluster = c.Name()
				}
				sourceQuery, destinationQuery, appQuery := buildQuery(sourceCluster, enableMXAdditionalLabels)
				// Query client side metrics
				prom := promInst
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

				if err := ValidateBucket(c, prom, cltInstance.Config().Service, "source", expectedBuckets); err != nil {
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
}

// TestStatsTCPFilter includes common test logic for stats and MX filters.
func TestStatsTCPFilter(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			t.NewSubTest("default").Run(func(t framework.TestContext) {
				runTCPStatsTest(t, false)
			})
			t.NewSubTest("additional-labels").Run(func(t framework.TestContext) {
				if t.Settings().PeerMetadataDiscovery {
					t.Skipf("Peer metadata discovery is enabled, skipping additional-labels test")
				}
				runTCPStatsTest(t, true)
			})
		})
}

func runTCPStatsTest(t framework.TestContext, enableMXAdditionalLabels bool) {
	g, _ := errgroup.WithContext(t.Context())
	if enableMXAdditionalLabels {
		// use namespace scoped Telemetry API to override the root level config
		t.ConfigIstio().File(apps.Namespace.Name(), additionalLabelConfigPath).ApplyOrFail(t)
	}
	for _, cltInstance := range GetClientInstances() {
		g.Go(func() error {
			err := retry.UntilSuccess(func() error {
				if err := SendTCPTraffic(cltInstance); err != nil {
					return err
				}
				c := cltInstance.Config().Cluster
				sourceCluster := constants.DefaultClusterName
				if len(t.AllClusters()) > 1 {
					sourceCluster = c.Name()
				}
				destinationQuery := buildTCPQuery(sourceCluster, enableMXAdditionalLabels)
				if _, err := promInst.Query(c, destinationQuery); err != nil {
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
}

func TestStatsGatewayServerTCPFilter(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			base := filepath.Join(env.IstioSrc, "tests/integration/telemetry/testdata/")
			// Following resources are being deployed to test sidecar->gateway communication. With following resources,
			// routing is being setup from sidecar to external site, via egress gateway.
			// app(https:443) -> sidecar(tls:443) -> istio-mtls -> (TLS:443)egress-gateway-> vs(tcp:443) -> external.external.svc.cluster.local
			address := "240.240.240.255"
			if _, ipv6 := getSupportedIPFamilies(t); ipv6 {
				address = "2001:2::f0f0:255"
			}
			values := map[string]any{
				"Address":   address,
				"Namespace": apps.External.Namespace.Name(),
			}
			t.ConfigIstio().File(apps.Namespace.Name(), filepath.Join(base, "istio-mtls-dest-rule.yaml")).ApplyOrFail(t)
			t.ConfigIstio().File(apps.Namespace.Name(), filepath.Join(base, "istio-mtls-gateway.yaml")).ApplyOrFail(t)
			t.ConfigIstio().EvalFile(apps.Namespace.Name(), values, filepath.Join(base, "istio-mtls-vs.yaml")).ApplyOrFail(t)
			t.ConfigIstio().EvalFile(ist.Settings().SystemNamespace, values, filepath.Join(base, "external-service-entry.yaml")).ApplyOrFail(t, apply.NoCleanup)
			t.ConfigIstio().EvalFile(apps.A.NamespaceName(), values, filepath.Join(base, "external-service-entry.yaml")).ApplyOrFail(t, apply.NoCleanup)

			g, _ := errgroup.WithContext(context.Background())
			for _, cltInstance := range GetClientInstances() {
				g.Go(func() error {
					err := retry.UntilSuccess(func() error {
						if _, err := cltInstance.Call(echo.CallOptions{
							Address: address,
							TLS:     echo.TLS{ServerName: "server.default.svc"},
							Scheme:  scheme.HTTPS,
							Port:    ports.HTTPS,
							Count:   1,
							Retry:   echo.Retry{NoRetry: true}, // we do retry in outer loop
							Check:   check.OK(),
						}); err != nil {
							return err
						}

						c := cltInstance.Config().Cluster
						sourceCluster := constants.DefaultClusterName
						if len(t.AllClusters()) > 1 {
							sourceCluster = c.Name()
						}
						destinationQuery := buildGatewayTCPServerQuery(sourceCluster)
						if _, err := promInst.Query(c, destinationQuery); err != nil {
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

func SendTrafficOrFailExpectForbidden(t test.Failer, from echo.Instance) {
	from.CallOrFail(t, echo.CallOptions{
		To: GetTarget(),
		Port: echo.Port{
			Name: "http",
		},
		Retry: echo.Retry{
			Options: []retry.Option{
				retry.Delay(framework.TelemetryRetryDelay),
				retry.MaxAttempts(10),
			},
		},
		Check: check.Forbidden(protocol.HTTP),
	})
	from.CallOrFail(t, echo.CallOptions{
		To: apps.Naked,
		Port: echo.Port{
			Name: "http",
		},
		Retry: echo.Retry{
			Options: []retry.Option{
				retry.Delay(framework.TelemetryRetryDelay),
				retry.MaxAttempts(10),
			},
		},
		Check: check.Forbidden(protocol.HTTP),
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

func buildQuery(sourceCluster string, enableMXAdditionalLabels bool) (sourceQuery, destinationQuery, appQuery prometheus.Query) {
	ns := apps.Namespace
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

	sourceQuery, destinationQuery, appQuery = BuildQueryCommon(labels, ns.Name())
	if enableMXAdditionalLabels {
		sourceQuery.Labels["upstream_custom_label"] = "b"
		destinationQuery.Labels["downstream_custom_label"] = "a"
	}

	return
}

func buildOutOfMeshServerQuery(sourceCluster string) prometheus.Query {
	ns := apps.Namespace
	labels := map[string]string{
		"request_protocol": "http",
		"response_code":    "200",
		// For out of mesh server, client side metrics rely on endpoint resource metadata
		// to fill in workload labels. To limit size of endpoint resource, we only populate
		// workload name and namespace, canonical service name and version in endpoint metadata.
		// Thus destination_app and destination_version labels are unknown.
		// However, they are known with WDS, so we can relax this check.
		// "destination_app":                "unknown",
		// "destination_version":            "unknown",
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

func buildTCPQuery(sourceCluster string, enableAdditionalLabels bool) (destinationQuery prometheus.Query) {
	ns := apps.Namespace
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
	if enableAdditionalLabels {
		// reporter is destination, so custom label is `b`.
		labels["downstream_custom_label"] = "a"
	}
	return prometheus.Query{
		Metric: "istio_tcp_connections_opened_total",
		Labels: labels,
	}
}

func buildGatewayTCPServerQuery(sourceCluster string) (destinationQuery prometheus.Query) {
	ns := apps.Namespace
	labels := map[string]string{
		"request_protocol":               "tcp",
		"destination_service_name":       "istio-egressgateway",
		"destination_canonical_revision": "latest",
		"destination_canonical_service":  "istio-egressgateway",
		"destination_app":                "istio-egressgateway",
		// Does not play well with canonical revision which defaults to "latest".
		// "destination_version":            "unknown",
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

func ValidateBucket(cluster cluster.Cluster, prom prometheus.Instance, sourceApp string, reporter string, expectedBuckets int) error {
	return retry.UntilSuccess(func() error {
		promQL := fmt.Sprintf(`count(sum by(le) (rate(istio_request_duration_milliseconds_bucket{source_app="%s",reporter="%s",response_code="200"}[24h])))`,
			sourceApp, reporter)
		v, err := prom.RawQuery(cluster, promQL)
		if err != nil {
			return err
		}
		totalBuckets, err := prometheus.Sum(v)
		if err != nil {
			return err
		}
		if int(totalBuckets) != expectedBuckets {
			return fmt.Errorf("expected %d buckets, got %v", expectedBuckets, totalBuckets)
		}
		return nil
	}, retry.Delay(time.Second), retry.Timeout(time.Second*20))
}

// TestGRPCCountMetrics tests that istio_[request/response]_messages_total are present https://github.com/istio/istio/issues/44144
// Kiali depends on these metrics
func TestGRPCCountMetrics(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			// Metrics to be queried and tested
			metrics := []string{"istio_request_messages_total", "istio_response_messages_total"}
			for _, metric := range metrics {
				t.NewSubTestf(metric).Run(func(t framework.TestContext) {
					t.Cleanup(func() {
						if t.Failed() {
							util.PromDump(t.Clusters().Default(), promInst, prometheus.Query{Metric: metric})
						}
						grpcSourceQuery := buildGRPCQuery(metric)
						cluster := t.Clusters().Default()
						retry.UntilSuccessOrFail(t, func() error {
							if err := SendGRPCTraffic(); err != nil {
								t.Log("failed to send grpc traffic")
								return err
							}
							if _, err := util.QueryPrometheus(t, cluster, grpcSourceQuery, promInst); err != nil {
								util.PromDiff(t, promInst, cluster, grpcSourceQuery)
								return err
							}
							return nil
						}, retry.Delay(1*time.Second), retry.Timeout(300*time.Second))
						util.ValidateMetric(t, cluster, promInst, grpcSourceQuery, 1)
					})
				})
			}
		})
}

func buildGRPCQuery(metric string) (destinationQuery prometheus.Query) {
	ns := apps.Namespace

	labels := map[string]string{
		"destination_app":                "b",
		"destination_version":            "v1",
		"destination_service":            "b." + ns.Name() + ".svc.cluster.local",
		"destination_service_name":       "b",
		"destination_workload_namespace": ns.Name(),
		"destination_service_namespace":  ns.Name(),
	}
	sourceQuery := prometheus.Query{}
	sourceQuery.Metric = metric
	sourceQuery.Labels = labels

	return sourceQuery
}

func SendGRPCTraffic() error {
	for _, cltInstance := range GetClientInstances() {
		_, err := cltInstance.Call(echo.CallOptions{
			To: GetTarget(),
			Port: echo.Port{
				Name: "grpc",
			},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func getSupportedIPFamilies(t framework.TestContext) (v4 bool, v6 bool) {
	addrs := apps.A.WorkloadsOrFail(t).Addresses()
	for _, a := range addrs {
		ip, err := netip.ParseAddr(a)
		assert.NoError(t, err)
		if ip.Is4() {
			v4 = true
		} else if ip.Is6() {
			v6 = true
		}
	}
	if !v4 && !v6 {
		t.Fatalf("pod is neither v4 nor v6? %v", addrs)
	}
	return
}
