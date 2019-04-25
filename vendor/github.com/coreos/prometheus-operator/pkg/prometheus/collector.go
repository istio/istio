// Copyright 2016 The prometheus-operator Authors
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
	"github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/tools/cache"
)

var (
	descPrometheusSpecReplicas = prometheus.NewDesc(
		"prometheus_operator_spec_replicas",
		"Number of expected replicas for the object.",
		[]string{
			"namespace",
			"name",
		}, nil,
	)
)

type prometheusCollector struct {
	store cache.Store
}

func NewPrometheusCollector(s cache.Store) *prometheusCollector {
	return &prometheusCollector{store: s}
}

// Describe implements the prometheus.Collector interface.
func (c *prometheusCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descPrometheusSpecReplicas
}

// Collect implements the prometheus.Collector interface.
func (c *prometheusCollector) Collect(ch chan<- prometheus.Metric) {
	for _, p := range c.store.List() {
		c.collectPrometheus(ch, p.(*v1.Prometheus))
	}
}

func (c *prometheusCollector) collectPrometheus(ch chan<- prometheus.Metric, p *v1.Prometheus) {
	replicas := float64(minReplicas)
	if p.Spec.Replicas != nil {
		replicas = float64(*p.Spec.Replicas)
	}
	ch <- prometheus.MustNewConstMetric(descPrometheusSpecReplicas, prometheus.GaugeValue, replicas, p.Namespace, p.Name)
}
