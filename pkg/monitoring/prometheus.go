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

package monitoring

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
)

// RegisterPrometheusExporter sets the global metrics handler to the provided Prometheus registerer and gatherer.
// Returned is an HTTP handler that can be used to read metrics from.
func RegisterPrometheusExporter(reg prometheus.Registerer, gatherer prometheus.Gatherer) (http.Handler, error) {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	if gatherer == nil {
		gatherer = prometheus.DefaultGatherer
	}
	promOpts := []otelprom.Option{
		otelprom.WithoutScopeInfo(),
		otelprom.WithoutTargetInfo(),
		otelprom.WithoutUnits(),
		otelprom.WithRegisterer(reg),
		otelprom.WithoutCounterSuffixes(),
	}

	prom, err := otelprom.New(promOpts...)
	if err != nil {
		return nil, err
	}

	opts := []metric.Option{metric.WithReader(prom)}
	opts = append(opts, knownMetrics.toHistogramViews()...)
	mp := metric.NewMeterProvider(opts...)
	otel.SetMeterProvider(mp)
	handler := promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{})
	return handler, nil
}
