// Copyright 2019 Istio Authors
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
	"sort"
	"strings"
	"sync"
	"time"

	"go.opencensus.io/stats/view"
)

// OpenCensusRegistry should only be used to collected exported metrics for
// the offline generation of collateral. It is not suitable for production usage.
type OpenCensusRegistry struct {
	sync.RWMutex
	metrics map[string]Exported
}

// NewOpenCensusRegistry collects a list of exported metrics. As part of the setup,
// it configures the OpenCensus mechanisms for rapid reporting (1ms) and sleeps for
// double that period (2ms) to ensure an export happens before generation.
func NewOpenCensusRegistry() *OpenCensusRegistry {
	// note: only use this for collateral generation
	// this reporting period is NOT suitable for all exporters
	view.SetReportingPeriod(1 * time.Millisecond)

	r := &OpenCensusRegistry{
		metrics: make(map[string]Exported),
	}
	view.RegisterExporter(r)

	time.Sleep(10 * time.Millisecond) // allow export to happen

	return r
}

// ExportView implements view.Exporter
func (r *OpenCensusRegistry) ExportView(d *view.Data) {
	name := promName(d.View.Name)
	r.Lock()
	if _, ok := r.metrics[name]; !ok {
		r.metrics[name] = Exported{name, d.View.Aggregation.Type.String(), d.View.Description}
	}
	r.Unlock()
}

func (r *OpenCensusRegistry) ExportedMetrics() []Exported {
	r.RLock()
	names := []string{}
	for key := range r.metrics {
		names = append(names, key)
	}
	r.RUnlock()

	sort.Strings(names)

	tmp := make([]Exported, 0, len(names))
	for _, n := range names {
		tmp = append(tmp, r.metrics[n])
	}

	return tmp
}

var charReplacer = strings.NewReplacer("/", "_", ".", "_", " ", "_", "-", "")

func promName(metricName string) string {
	s := strings.TrimPrefix(metricName, "/")
	return charReplacer.Replace(s)
}
