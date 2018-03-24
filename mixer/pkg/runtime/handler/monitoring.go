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

package handler

import (
	"fmt"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

const (
	configID     = "configID"
	initConfigID = "initConfigID" // the id of the config, at which the adapter was instantiated.
	handlerName  = "handlerName"
)

var (
	tableConfigLabels = []string{configID}
	envConfigLabels   = []string{initConfigID, handlerName}

	newHandlerCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mixer",
		Subsystem: "handler",
		Name:      "new_handler_count",
		Help:      "The number of handlers that were newly created during config transition.",
	}, tableConfigLabels)

	reusedCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mixer",
		Subsystem: "handler",
		Name:      "reused_handler_count",
		Help:      "The number of handlers that are re-used during config transition.",
	}, tableConfigLabels)

	closedCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mixer",
		Subsystem: "handler",
		Name:      "close_handler_count",
		Help:      "The number of handlers that were closed during config transition.",
	}, tableConfigLabels)

	buildFailureCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mixer",
		Subsystem: "handler",
		Name:      "handler_build_failure_count",
		Help:      "The number of handlers that failed creation during config transition.",
	}, tableConfigLabels)

	closeFailureCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mixer",
		Subsystem: "handler",
		Name:      "handler_close_failure_count",
		Help:      "The number of errors encountered while closing handlers during config transition.",
	}, tableConfigLabels)

	workerCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "mixer",
		Subsystem: "handler",
		Name:      "worker_count",
		Help:      "The current number of active worker routines in a given adapter environment.",
	}, envConfigLabels)

	daemonCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "mixer",
		Subsystem: "handler",
		Name:      "daemon_count",
		Help:      "The current number of active daemon routines in a given adapter environment.",
	}, envConfigLabels)
)

func init() {
	prometheus.MustRegister(newHandlerCount)
	prometheus.MustRegister(reusedCount)
	prometheus.MustRegister(closedCount)
	prometheus.MustRegister(buildFailureCount)
	prometheus.MustRegister(closeFailureCount)
}

// tableCounters are counters for handler related operations performed during table creation/cleanup
type tableCounters struct {
	reusedHandlers prometheus.Counter
	newHandlers    prometheus.Counter
	closedHandlers prometheus.Counter
	buildFailure   prometheus.Counter
	closeFailure   prometheus.Counter
}

func newTableCounters(cfgID int64) tableCounters {
	labels := prometheus.Labels{
		configID: strconv.FormatInt(cfgID, 10),
	}
	return tableCounters{
		newHandlers:    newHandlerCount.With(labels),
		reusedHandlers: reusedCount.With(labels),
		closedHandlers: closedCount.With(labels),
		buildFailure:   buildFailureCount.With(labels),
		closeFailure:   closeFailureCount.With(labels),
	}
}

// envCounters are counters for handler use of the env structure.
type envCounters struct {
	workers prometheus.Gauge
	daemons prometheus.Gauge
}

func (c envCounters) getDaemonCount() (float64, error) {
	if c.daemons != nil {
		m := dto.Metric{}

		var c prometheus.Metric = c.daemons
		if err := c.Write(&m); err != nil {
			return 0, fmt.Errorf("failed to fetch adapter's scheduled daemon counter: %v", err)

		}
		return *m.GetGauge().Value, nil
	}
	return 0, nil
}

func (c envCounters) getWorkerCount() (float64, error) {
	if c.workers != nil {
		m := dto.Metric{}

		var c prometheus.Metric = c.workers
		if err := c.Write(&m); err != nil {
			return 0, fmt.Errorf("failed to fetch adapter's scheduled workers counter: %v", err)

		}
		return *m.GetGauge().Value, nil
	}
	return 0, nil
}

func newEnvCounters(cfgID int64, handler string) envCounters {
	labels := prometheus.Labels{
		initConfigID: strconv.FormatInt(cfgID, 10),
		handlerName:  handler,
	}

	return envCounters{
		workers: workerCount.With(labels),
		daemons: daemonCount.With(labels),
	}
}
