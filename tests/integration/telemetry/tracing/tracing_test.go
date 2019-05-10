package tracing

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/zipkin"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/mixer"
)

var (
	ist            istio.Instance
	bookinfoNsInst namespace.Instance
	galInst        galley.Instance
	ingInst        ingress.Instance
	zipkinInst     zipkin.Instance
)

// TestProxyTracing exercises the trace generation features of Istio, based on the Envoy Trace driver for zipkin.
// The test verifies that all expected spans (a client span and a server span for each service call in the sample bookinfo app)
// are generated and that they are all a part of the same distributed trace with correct hierarchy and name.
//
// More information on distributed tracing can be found here: https://istio.io/docs/tasks/telemetry/distributed-tracing/zipkin/
func TestProxyTracing(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			// deploy bookinfo app, also deploy a virtualservice which forces all traffic to go to review v1,
			// which does not get ratings, so that exactly six spans will be included in the wanted trace.
			galInst.ApplyConfigOrFail(t,
				bookinfoNsInst,
				bookinfo.NetworkingBookinfoGateway.LoadGatewayFileWithNamespaceOrFail(t, bookinfoNsInst.Name()),
				bookinfo.GetDestinationRuleConfigFile(t, ctx).LoadWithNamespaceOrFail(t, bookinfoNsInst.Name()),
				bookinfo.NetworkingVirtualServiceAllV1.LoadWithNamespaceOrFail(t, bookinfoNsInst.Name()),
			)

			retry.UntilSuccessOrFail(t, func() error {
				// Send test traffic
				util.SendTraffic(ingInst, t, "Sending traffic", "", 10)
				traces, err := zipkinInst.QueryTraces(100,
					fmt.Sprintf("productpage.%s.svc.cluster.local:9080/productpage", bookinfoNsInst.Name()))
				if err != nil {
					return fmt.Errorf("cannot get traces from zipkin: %v", err)
				}
				if !verifyBookinfoTraces(t, bookinfoNsInst.Name(), traces) {
					return errors.New("cannot find expected traces")
				}
				return nil
			}, retry.Delay(3*time.Second), retry.Timeout(40*time.Second))
		})
}

func TestMain(m *testing.M) {
	framework.NewSuite("tracing_test", m).
		RequireEnvironment(environment.Kube).
		SetupOnEnv(environment.Kube, istio.Setup(&ist, setupConfig)).
		Setup(testSetup).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.Values["tracing.enabled"] = "true"
	cfg.Values["tracing.provider"] = "zipkin"
	cfg.Values["global.enableTracing"] = "true"
	cfg.Values["global.disablePolicyChecks"] = "true"
	cfg.Values["pilot.traceSampling"] = "100.0"
}

func testSetup(ctx resource.Context) (err error) {
	galInst, err = galley.New(ctx, galley.Config{})
	if err != nil {
		return
	}
	bookinfoNsInst, err = namespace.New(ctx, "istio-bookinfo", true)
	if err != nil {
		return
	}
	if _, err = bookinfo.Deploy(ctx, bookinfo.Config{Namespace: bookinfoNsInst, Cfg: bookinfo.BookInfo}); err != nil {
		return
	}
	ingInst, err = ingress.New(ctx, ingress.Config{Istio: ist})
	if err != nil {
		return
	}
	zipkinInst, err = zipkin.New(ctx)
	if err != nil {
		return
	}
	return nil
}

func verifyBookinfoTraces(t *testing.T, namespace string, traces []zipkin.Trace) bool {
	wtr := wantTraceRoot(namespace)
	for _, trace := range traces {
		// compare each candidate trace with the wanted trace
		for _, s := range trace.Spans {
			// find the root span of candidate trace and do recursive comparation
			if s.ParentSpanID == "" && compareTrace(t, s, wtr) {
				return true
			}
		}
	}
	return false
}

// compareTrace recursively compares the two given spans
func compareTrace(t *testing.T, got, want zipkin.Span) bool {
	if got.Name != want.Name || got.ServiceName != want.ServiceName {
		t.Logf("got span %+v, want span %+v", got, want)
		return false
	}
	if len(got.ChildSpans) != len(want.ChildSpans) {
		t.Logf("got %d child spans from, want %d child spans, maybe trace has not be fully reported",
			len(got.ChildSpans), len(want.ChildSpans))
		return false
	}
	for i := range got.ChildSpans {
		if !compareTrace(t, *got.ChildSpans[i], *want.ChildSpans[i]) {
			return false
		}
	}
	return true
}

// wantTraceRoot constructs the wanted trace and returns the root span of that trace
func wantTraceRoot(namespace string) (root zipkin.Span) {
	reviewServerSpan := zipkin.Span{
		Name:        fmt.Sprintf("reviews.%s.svc.cluster.local:9080/*", namespace),
		ServiceName: fmt.Sprintf("reviews.%s", namespace),
	}
	reviewClientSpan := zipkin.Span{
		Name:        fmt.Sprintf("reviews.%s.svc.cluster.local:9080/*", namespace),
		ServiceName: fmt.Sprintf("productpage.%s", namespace),
		ChildSpans:  []*zipkin.Span{&reviewServerSpan},
	}
	detailServerSpan := zipkin.Span{
		Name:        fmt.Sprintf("details.%s.svc.cluster.local:9080/*", namespace),
		ServiceName: fmt.Sprintf("details.%s", namespace),
	}
	detailClientSpan := zipkin.Span{
		Name:        fmt.Sprintf("details.%s.svc.cluster.local:9080/*", namespace),
		ServiceName: fmt.Sprintf("productpage.%s", namespace),
		ChildSpans:  []*zipkin.Span{&detailServerSpan},
	}
	productpageServerSpan := zipkin.Span{
		Name:        fmt.Sprintf("productpage.%s.svc.cluster.local:9080/productpage", namespace),
		ServiceName: fmt.Sprintf("productpage.%s", namespace),
		ChildSpans:  []*zipkin.Span{&detailClientSpan, &reviewClientSpan},
	}
	root = zipkin.Span{
		Name:        fmt.Sprintf("productpage.%s.svc.cluster.local:9080/productpage", namespace),
		ServiceName: fmt.Sprintf("istio-ingressgateway"),
		ChildSpans:  []*zipkin.Span{&productpageServerSpan},
	}
	return
}
