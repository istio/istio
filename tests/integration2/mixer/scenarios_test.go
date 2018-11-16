//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package mixer

import (
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/ids"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/components/bookinfo"
	"testing"

	"istio.io/istio/pkg/test"
)

// This file contains Mixer tests that are ported from Mixer E2E tests

// Port of TestMetric
func TestIngessToPrometheus_ServiceMetric(t *testing.T) {
	ctx := framework.GetContext(t)
	ctx.RequireOrSkip(t, lifecycle.Test, &descriptors.KubernetesEnvironment, &ids.Mixer, &ids.Prometheus, &ids.BookInfo, &ids.Ingress)

	label := "source_workload"
	labelValue := "istio-ingressgateway"
	testMetric(t, ctx, label, labelValue)
}

// Port of TestMetric
func TestIngessToPrometheus_IngressMetric(t *testing.T) {
	ctx := framework.GetContext(t)
	ctx.RequireOrSkip(t, lifecycle.Test, &descriptors.KubernetesEnvironment, &ids.Mixer, &ids.Prometheus, &ids.BookInfo, &ids.Ingress)

	label := "destination_service"
	labelValue := "productpage.{{.TestNamespace}}.svc.cluster.local"
	testMetric(t, ctx, label, labelValue)
}

func testMetric(t *testing.T, ctx component.Repository, label string, labelValue string) {
	t.Helper()

	mxr := components.GetMixer(ctx, t)
	mxr.Configure(t,
		lifecycle.Test,
		test.JoinConfigs(
			bookinfo.NetworkingBookinfoGateway.LoadOrFail(t),
			bookinfo.NetworkingDestinationRuleAll.LoadOrFail(t),
			bookinfo.NetworkingVirtualServiceAllV1.LoadOrFail(t),
		))

	prometheus := components.GetPrometheus(ctx, t)
	ingress := components.GetIngress(ctx, t)

	// Warm up
	_, err := ingress.Call("/productpage")
	if err != nil {
		t.Fatal(err)
	}

	// Wait for some data to arrive.
	initial, err := prometheus.WaitForQuiesce(`istio_requests_total{%s=%q,response_code="200"}`, label, labelValue)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Baseline established: initial = %v", initial)

	_, err = ingress.Call("/productpage")
	if err != nil {
		t.Fatal(err)
	}

	final, err := prometheus.WaitForQuiesce(`istio_requests_total{%s=%q,response_code="200"}`, label, labelValue)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Quiesced to: final = %v", final)

	i, err := prometheus.Sum(initial, nil)
	if err != nil {
		t.Fatal(err)
	}

	f, err := prometheus.Sum(final, nil)
	if err != nil {
		t.Fatal(err)
	}

	if (f - i) < float64(1) {
		t.Errorf("Bad metric value: got %f, want at least 1", f-i)
	}
}
