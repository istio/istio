package standardmetrics

import (
	"context"
	"errors"
	"io/ioutil"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/monitoring"
	"istio.io/istio/pkg/test/framework/components/monitoring/prometheus"
	"istio.io/istio/pkg/test/framework/components/monitoring/stackdriver"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
)

var (
	client, server echo.Instance
	ist            istio.Instance
	echoNsInst     namespace.Instance
	sdInst         stackdriver.Instance
	promInst       prometheus.Instance
)

const (
	stackdriverBootstrapOverride = "../stackdriver/testdata/custom_bootstrap.yaml.tmpl"
	// serverRequestCount           = "../stackdriver/testdata/server_request_count.json.tmpl"
	// clientRequestCount           = "testdata/client_request_count.json.tmpl"
	// serverLogEntry               = "testdata/server_access_log.json.tmpl"
	sdBootstrapConfigMap = "stackdriver-bootstrap-config"
)

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		RequireSingleCluster().
		Label(label.CustomSetup).
		Setup(istio.Setup(istioInstance(), setupConfig)).
		Setup(testSetup).
		Run()
}

// istioInstance gets Istio instance.
func istioInstance() *istio.Instance {
	return &ist
}

// appNamespace gets bookinfo instance.
func appNamespace() namespace.Instance {
	return echoNsInst
}

func expectedDimensions(namespace string) monitoring.TrafficDimensions {
	return monitoring.TrafficDimensions{
		Request: monitoring.Request{
			Protocol:                    monitoring.HTTP,
			ResponseCode:                "200",
			DestinationService:          "server." + namespace + ".svc.cluster.local",
			DestinationServiceName:      "server",
			DestinationServiceNamespace: namespace,
		},
		Context: monitoring.TrafficContext{},
		Destination: monitoring.Workload{
			App:       "server",
			Version:   "v1",
			Namespace: namespace,
		},
		Source: monitoring.Workload{
			App:       "client",
			Version:   "v1",
			Name:      "client-v1",
			Namespace: namespace,
		},
	}
}

func sendTraffic() error {
	_, err := client.Call(echo.CallOptions{
		Target:   server,
		PortName: "http",
	})
	return err
}

func TestStandardMetrics(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.stats.standard-metrics").
		Run(func(ctx framework.TestContext) {
			baseDims := expectedDimensions(echoNsInst.Name())
			cases := []struct {
				name     string
				querySvc monitoring.MetricsService
			}{
				{"prometheus", promInst},
				{"stackdriver", sdInst},
			}

			retry.UntilSuccessOrFail(t, func() error {
				if err := sendTraffic(); err != nil {
					return err
				}
				for _, v := range cases {
					ctx.NewSubTest(v.name).
						Run(func(ctx framework.TestContext) {
							retry.UntilSuccessOrFail(ctx, func() error {
								// Query client side metrics
								baseDims.Context.Reporter = "source"
								got, err := v.querySvc.Requests(context.Background(), baseDims)
								if err != nil {
									t.Logf("could not validate client requests: %v", err)
									return err
								}
								if want := 1.0; got < want {
									t.Logf("Requests(%q) => %f, want more than %f", baseDims, got, want)
									return errors.New("bad number of client requests")
								}
								// Query server side metrics
								baseDims.Context.Reporter = "destination"
								got, err = v.querySvc.Requests(context.Background(), baseDims)
								if err != nil {
									t.Logf("could not validate server requests: %v", err)
									return err
								}
								if want := 1.0; got < want {
									t.Logf("Requests(%q) => %f, want more than %f", baseDims, got, want)
									return errors.New("bad number of server requests")
								}
								return nil
							}, retry.Delay(10*time.Second), retry.Timeout(80*time.Second))
						})
				}
				return nil
			}, retry.Delay(3*time.Second), retry.Timeout(160*time.Second))
		})
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.Values["telemetry.v2.stackdriver.enabled"] = "true"
}

// testSetup set up bookinfo app for stats testing.
func testSetup(ctx resource.Context) (err error) {
	echoNsInst, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-echo",
		Inject: true,
	})
	if err != nil {
		return
	}
	promInst, err = prometheus.New(ctx, prometheus.Config{})
	if err != nil {
		return
	}

	sdInst, err = stackdriver.New(ctx, stackdriver.Config{})
	if err != nil {
		return
	}
	templateBytes, err := ioutil.ReadFile(stackdriverBootstrapOverride)
	if err != nil {
		return
	}
	sdBootstrap, err := tmpl.Evaluate(string(templateBytes), map[string]interface{}{
		"StackdriverNamespace": sdInst.GetStackdriverNamespace(),
		"EchoNamespace":        echoNsInst.Name(),
	})
	if err != nil {
		return
	}

	err = ctx.Config().ApplyYAML(echoNsInst.Name(), sdBootstrap)
	if err != nil {
		return
	}
	builder, err := echoboot.NewBuilder(ctx)
	if err != nil {
		return
	}
	err = builder.
		With(&client, echo.Config{
			Service:   "client",
			Namespace: echoNsInst,
			Subsets: []echo.SubsetConfig{
				{
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.SidecarBootstrapOverride: {
							Value: sdBootstrapConfigMap,
						},
					},
				},
			}}).
		With(&server, echo.Config{
			Service:   "server",
			Namespace: echoNsInst,
			Ports: []echo.Port{
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					InstancePort: 8090,
				},
			},
			Subsets: []echo.SubsetConfig{
				{
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.SidecarBootstrapOverride: {
							Value: sdBootstrapConfigMap,
						},
					},
				},
			}}).
		Build()
	if err != nil {
		return
	}
	return nil
}
