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
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prom2json"
	"go.opencensus.io/stats/view"

	"istio.io/pkg/ctrlz/fw"
	"istio.io/pkg/ctrlz/topics/assets"
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
	tmpl := template.Must(context.Layout().Parse(string(assets.MustAsset("templates/metrics.html"))))

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

	// process any OpenCensus metrics and cram 'em into promjson.* data structures
	knownViewLock.Lock()

	for name, v := range knownViews {
		if rows, err := view.RetrieveData(name); err == nil {
			family := prom2json.Family{
				Name:    name,
				Help:    v.Description,
				Type:    strings.ToUpper(v.Aggregation.Type.String()),
				Metrics: make([]interface{}, 0),
			}
			result = append(result, &family)

			for _, row := range rows {
				labels := make(map[string]string)
				for _, tag := range row.Tags {
					labels[tag.Key.Name()] = tag.Value
				}

				var metric interface{}

				switch d := row.Data.(type) {
				case *view.LastValueData:
					metric = &prom2json.Metric{
						Labels: labels,
						Value:  fmt.Sprintf("%v", d.Value),
					}
				case *view.CountData:
					metric = &prom2json.Metric{
						Labels: labels,
						Value:  fmt.Sprintf("%v", d.Value),
					}
				case *view.SumData:
					metric = &prom2json.Summary{
						Labels: labels,
						Sum:    fmt.Sprintf("%v", d.Value),
					}
				}

				if metric != nil {
					family.Metrics = append(family.Metrics, metric)
				}
			}
		}
	}
	knownViewLock.Unlock()

	return result
}

type exporter struct{}

var (
	knownViews    = make(map[string]*view.View)
	knownViewLock sync.Mutex
)

// use this to keep track of all known OpenCensus views
func (e *exporter) ExportView(vd *view.Data) {
	knownViewLock.Lock()
	knownViews[vd.View.Name] = vd.View
	knownViewLock.Unlock()
}

func init() {
	view.RegisterExporter(&exporter{})
}
