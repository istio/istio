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

package tracing

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/zipkin"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	ist            istio.Instance
	bookinfoNsInst namespace.Instance
	ingInst        ingress.Instance
	zipkinInst     zipkin.Instance
)

func GetIstioInstance() *istio.Instance {
	return &ist
}

func GetBookinfoNamespaceInstance() namespace.Instance {
	return bookinfoNsInst
}

func GetIngressInstance() ingress.Instance {
	return ingInst
}

func GetZipkinInstance() zipkin.Instance {
	return zipkinInst
}

func TestSetup(ctx resource.Context) (err error) {
	bookinfoNsInst, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-bookinfo",
		Inject: true,
	})
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
	zipkinInst, err = zipkin.New(ctx, zipkin.Config{})
	if err != nil {
		return
	}
	// deploy bookinfo app, also deploy a virtualservice which forces all traffic to go to review v1,
	// which does not get ratings, so that exactly six spans will be included in the wanted trace.
	bookingfoGatewayFile, err := bookinfo.NetworkingBookinfoGateway.LoadGatewayFileWithNamespace(bookinfoNsInst.Name())
	if err != nil {
		return
	}
	destinationRule, err := bookinfo.GetDestinationRuleConfigFile(ctx)
	if err != nil {
		return
	}
	destinationRuleFile, err := destinationRule.LoadWithNamespace(bookinfoNsInst.Name())
	if err != nil {
		return
	}
	virtualServiceFile, err := bookinfo.NetworkingVirtualServiceAllV1.LoadWithNamespace(bookinfoNsInst.Name())
	if err != nil {
		return
	}
	err = ctx.Config().ApplyYAML(
		bookinfoNsInst.Name(),
		bookingfoGatewayFile,
		destinationRuleFile,
		virtualServiceFile,
	)
	if err != nil {
		return
	}
	return nil
}

func VerifyBookinfoTraces(t *testing.T, namespace string, traces []zipkin.Trace) bool {
	wtr := WantTraceRoot(namespace)
	for _, trace := range traces {
		// compare each candidate trace with the wanted trace
		for _, s := range trace.Spans {
			// find the root span of candidate trace and do recursive comparison
			if s.ParentSpanID == "" && CompareTrace(t, s, wtr) {
				return true
			}
		}
	}
	return false
}

// compareTrace recursively compares the two given spans
func CompareTrace(t *testing.T, got, want zipkin.Span) bool {
	if got.Name != want.Name || got.ServiceName != want.ServiceName {
		t.Logf("got span %+v, want span %+v", got, want)
		return false
	}
	if len(got.ChildSpans) < len(want.ChildSpans) {
		t.Logf("got %d child spans from, want %d child spans, maybe trace has not be fully reported",
			len(got.ChildSpans), len(want.ChildSpans))
		return false
	} else if len(got.ChildSpans) > len(want.ChildSpans) {
		t.Logf("got %d child spans from, want %d child spans, maybe destination rule has not became effective",
			len(got.ChildSpans), len(want.ChildSpans))
		return false
	}
	for i := range got.ChildSpans {
		if !CompareTrace(t, *got.ChildSpans[i], *want.ChildSpans[i]) {
			return false
		}
	}
	return true
}

// wantTraceRoot constructs the wanted trace and returns the root span of that trace
func WantTraceRoot(namespace string) (root zipkin.Span) {
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
		ServiceName: "istio-ingressgateway",
		ChildSpans:  []*zipkin.Span{&productpageServerSpan},
	}
	return
}
