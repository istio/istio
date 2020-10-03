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

package metrics

import (
	"fmt"

	ocprom "contrib.go.opencensus.io/exporter/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"go.opencensus.io/stats/view"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// RegisterOperatorMetricsServer registers HTTP endpoint to serve
// custom operator metrics.This is different endpoint that the one
// provided by controller-runtime (which contains metrics like reconcile
// time, request latency and so on)
func RegisterOperatorMetricsServer(mgr manager.Manager) error {
	exporter, err := ocprom.NewExporter(ocprom.Options{
		Registry: prometheus.DefaultRegisterer.(*prometheus.Registry),
	})
	if err != nil {
		return fmt.Errorf("could not set up prometheus exporter: %v", err)
	}
	view.RegisterExporter(exporter)
	mgr.AddMetricsExtraHandler("/op-metrics", exporter)
	return nil
}
