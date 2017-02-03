// Copyright 2017 Google Inc.
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

// Package prometheus publishes metric values collected by the mixer for
// ingestion by prometheus.
package prometheus

import (
	"fmt"
	"sync"

	"istio.io/mixer/adapter/prometheus/config"
	"istio.io/mixer/pkg/adapter"
)

type (
	factory struct {
		adapter.DefaultBuilder

		srv  server
		once sync.Once
	}

	prom struct{}
)

var (
	name = "prometheus"
	desc = "Publishes prometheus metrics"
	conf = &config.Params{}
)

// Register records the builders exposed by this adapter.
func Register(r adapter.Registrar) {
	r.RegisterMetricsBuilder(newFactory())
}

func newFactory() *factory {
	return &factory{adapter.NewDefaultBuilder(name, desc, conf), newServer(defaultAddr), sync.Once{}}
}

func (f *factory) Close() error {
	return f.srv.Close()
}

func (f *factory) NewMetricsAspect(env adapter.Env, cfg adapter.AspectConfig, metrics []adapter.MetricDefinition) (adapter.MetricsAspect, error) {
	var serverErr error
	f.once.Do(func() { serverErr = f.srv.Start(env.Logger()) })
	if serverErr != nil {
		return nil, fmt.Errorf("could not start prometheus server: %v", serverErr)
	}

	return &prom{}, nil
}

func (*prom) Record([]adapter.Value) error {
	return nil
}

func (*prom) Close() error { return nil }
