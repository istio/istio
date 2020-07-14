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

package solarwinds

import (
	"context"
	"runtime"
	"strconv"
	"strings"
	"time"

	"istio.io/istio/mixer/adapter/solarwinds/config"
	"istio.io/istio/mixer/adapter/solarwinds/internal/appoptics"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/metric"
)

// MeasurementPostMaxBatchSize defines the max number of Measurements to send to the API at once
const MeasurementPostMaxBatchSize = 1000

type metricsHandlerInterface interface {
	handleMetric(context.Context, []*metric.Instance) error
	close() error
}

type metricsHandler struct {
	logger      adapter.Logger
	metricInfo  map[string]*config.Params_MetricInfo
	prepChan    chan []*appoptics.Measurement
	stopChan    chan struct{}
	pushChan    chan []*appoptics.Measurement
	lc          *appoptics.Client
	batchWait   chan struct{}
	persistWait chan struct{}
}

func newMetricsHandler(env adapter.Env, cfg *config.Params) metricsHandlerInterface {
	buffChanSize := runtime.NumCPU() * 10

	// prepChan holds groups of Measurements to be batched
	prepChan := make(chan []*appoptics.Measurement, buffChanSize)

	// pushChan holds groups of Measurements conforming to the size constraint described
	// by AppOptics.MeasurementPostMaxBatchSize
	pushChan := make(chan []*appoptics.Measurement, buffChanSize)

	stopChan := make(chan struct{})
	persistWait := make(chan struct{})
	batchWait := make(chan struct{})

	var lc *appoptics.Client
	if strings.TrimSpace(cfg.AppopticsAccessToken) != "" {
		lc = appoptics.NewClient(cfg.AppopticsAccessToken, env.Logger())

		batchSize := cfg.AppopticsBatchSize
		if batchSize <= 0 || batchSize > MeasurementPostMaxBatchSize {
			batchSize = MeasurementPostMaxBatchSize
		}

		env.ScheduleDaemon(func() {
			appoptics.BatchMeasurements(prepChan, pushChan, stopChan, int(batchSize))
			batchWait <- struct{}{}
		})
		env.ScheduleDaemon(func() {
			appoptics.PersistBatches(lc, pushChan, stopChan, env.Logger())
			persistWait <- struct{}{}
		})
	}
	return &metricsHandler{
		logger:      env.Logger(),
		prepChan:    prepChan,
		stopChan:    stopChan,
		pushChan:    pushChan,
		lc:          lc,
		persistWait: persistWait,
		batchWait:   batchWait,
		metricInfo:  cfg.Metrics,
	}
}

func (h *metricsHandler) handleMetric(_ context.Context, vals []*metric.Instance) error {
	measurements := []*appoptics.Measurement{}
	for _, val := range vals {
		if mInfo, ok := h.metricInfo[val.Name]; ok {
			merticVal := h.aoVal(val.Value)

			m := &appoptics.Measurement{
				Name:  val.Name,
				Value: merticVal,
				Time:  time.Now().Unix(),
				Tags:  appoptics.MeasurementTags{},
			}

			for _, label := range mInfo.LabelNames {
				// val.Dimensions[label] should exists because we have validated this before during config time.
				m.Tags[label] = adapter.Stringify(val.Dimensions[label])
			}
			measurements = append(measurements, m)
		}
	}
	if h.lc != nil {
		h.prepChan <- measurements
	}
	return nil
}

func (h *metricsHandler) close() error {
	close(h.prepChan)
	close(h.pushChan)
	close(h.stopChan)
	defer close(h.batchWait)
	defer close(h.persistWait)
	if h.lc != nil {
		<-h.batchWait
		<-h.persistWait
	}
	return nil
}

func (h *metricsHandler) aoVal(i interface{}) float64 {
	switch vv := i.(type) {
	case float64:
		return vv
	case int64:
		return float64(vv)
	case time.Duration:
		// use seconds for now
		return vv.Seconds()
	case string:
		f, err := strconv.ParseFloat(vv, 64)
		if err != nil {
			_ = h.logger.Errorf("error parsing metric val: %v", i)
			f = 0
		}
		return f
	default:
		_ = h.logger.Errorf("could not extract numeric value for %v", i)
		return 0
	}
}
