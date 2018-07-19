// Copyright 2018 Istio Authors
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

package topics

import (
	"html/template"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prom2json"

	"istio.io/istio/pkg/ctrlz/fw"
)

type metricsTopic struct {
}

// MetricsTopic returns a ControlZ topic that allows visualization of process metrics.
func MetricsTopic() fw.Topic {
	return metricsTopic{}
}

func (metricsTopic) Title() string {
	return "Metrics"
}

func (metricsTopic) Prefix() string {
	return "metric"
}

func (metricsTopic) Activate(context fw.TopicContext) {
	tmpl := template.Must(context.Layout().Parse(string(MustAsset("assets/templates/metrics.html"))))

	_ = context.HTMLRouter().StrictSlash(true).NewRoute().Path("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fw.RenderHTML(w, tmpl, getMetricInfo())
	})

	_ = context.JSONRouter().StrictSlash(true).NewRoute().Path("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fw.RenderJSON(w, http.StatusOK, getMetricInfo())
	})
}

func getMetricInfo() []*prom2json.Family {
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return nil
	}

	result := make([]*prom2json.Family, 0, len(families))
	for _, f := range families {
		result = append(result, prom2json.NewFamily(f))
	}

	return result
}
