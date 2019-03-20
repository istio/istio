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

package trace

import (
	"context"
	"sync"

	ocstackdriver "contrib.go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/trace"

	"istio.io/istio/mixer/adapter/stackdriver/config"
	"istio.io/istio/mixer/adapter/stackdriver/helper"
	"istio.io/istio/mixer/pkg/adapter"
)

// getExporterFunc allows indirect construction of exporters, for testing.
var getExporterFunc = getStackdriverExporter

var (
	// The Stackdriver exporter only supports a single instance per project.
	// See: https://github.com/census-ecosystem/opencensus-go-exporter-stackdriver/issues/8
	projectsToExporters = make(map[string]*ocstackdriver.Exporter)
	exportersMu         sync.Mutex
)

func getStackdriverExporter(_ context.Context, env adapter.Env, params *config.Params) (trace.Exporter, error) {
	opts := ocstackdriver.Options{
		MonitoringClientOptions: helper.ToOpts(params), // not used, but causes errors if invalid
		TraceClientOptions:      helper.ToOpts(params),
		ProjectID:               params.ProjectId,
		OnError: func(err error) {
			_ = env.Logger().Errorf("Stackdriver trace: %s", err.Error())
		},
	}

	exportersMu.Lock()
	defer exportersMu.Unlock()

	if e, ok := projectsToExporters[opts.ProjectID]; ok {
		return e, nil
	}

	e, err := ocstackdriver.NewExporter(opts)
	if err != nil {
		return nil, err
	}
	projectsToExporters[opts.ProjectID] = e

	return e, nil
}
