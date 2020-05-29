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

package circonus

import (
	"testing"
	"time"

	cgm "github.com/circonus-labs/circonus-gometrics"
	"github.com/circonus-labs/circonus-gometrics/checkmgr"
	"golang.org/x/net/context"

	"istio.io/istio/mixer/adapter/circonus/config"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/metric"
)

var (
	counterInfo = &config.Params_MetricInfo{
		Name: "the.counter",
		Type: config.COUNTER,
	}

	counterInstance = &metric.Instance{
		Name:  counterInfo.Name,
		Value: int64(1),
	}

	histogramInfo = &config.Params_MetricInfo{
		Name: "happy_histogram",
		Type: config.DISTRIBUTION,
	}

	histoDuration     = 1234 * time.Millisecond
	histogramInstance = &metric.Instance{
		Name:  histogramInfo.Name,
		Value: histoDuration,
	}

	histogramValSerialized = "H[1.2e+09]=1"

	gaugeInfo = &config.Params_MetricInfo{
		Name: "/funky::gauge",
		Type: config.GAUGE,
	}
	gaugeInstance = &metric.Instance{
		Name:  gaugeInfo.Name,
		Value: int64(123),
	}
)

func TestCirconusHandleMetrics(t *testing.T) {

	{
		builder := GetInfo().NewBuilder().(*builder)
		builder.SetAdapterConfig(makeBadConfig())
		if ce := builder.Validate(); ce != nil {
			multi := ce.Multi
			if len(multi.Errors) != 3 {
				t.Errorf("expected 3 bad config errors, found %v", multi.Errors)
			}
		}
	}

	metrics := map[string]*metric.Type{
		"counter":      {},
		"distribution": {},
		"gauge":        {},
	}

	// create a circonus gometrics instance
	submissionURL := "https://trap.noit.circonus.net/module/httptrap/myuuid/mysecret"
	cmc := &cgm.Config{
		CheckManager: checkmgr.Config{
			Check: checkmgr.CheckConfig{
				SubmissionURL: submissionURL,
			},
		},
		Debug:    true, // enable [DEBUG] level logging for env.Logger
		Interval: "0s", // do not autoflush
	}
	cm, err := cgm.NewCirconusMetrics(cmc)
	if err != nil {
		t.Errorf("could not create new cgm %v", err)
	}

	tests := []struct {
		name    string
		metrics []*config.Params_MetricInfo
		values  []*metric.Instance
	}{
		{"Counter",
			[]*config.Params_MetricInfo{counterInfo},
			[]*metric.Instance{counterInstance}},
		{"Gauge",
			[]*config.Params_MetricInfo{gaugeInfo},
			[]*metric.Instance{gaugeInstance}},
		{"Histogram", []*config.Params_MetricInfo{histogramInfo}, []*metric.Instance{histogramInstance}},
	}

	for _, v := range tests {

		t.Run(v.name, func(t *testing.T) {

			builder := GetInfo().NewBuilder().(*builder)
			builder.SetAdapterConfig(makeConfig(v.metrics...))
			builder.SetMetricTypes(metrics)

			metricsHandler, err := builder.Build(context.Background(), test.NewEnv(t))
			if err != nil {
				t.Errorf("Build() returned error: %v", err)
			}

			handler := metricsHandler.(*handler)
			handler.cm = cm

			if err = handler.HandleMetric(context.Background(), v.values); err != nil {
				t.Errorf("HandleMetric() returned error: %v", err)
			}

			for _, adapterVal := range v.values {
				mType, ok := handler.metrics[adapterVal.Name]
				if !ok {
					t.Errorf("no metric with name: %v, %d", adapterVal.Name, mType)
				}

				switch mType {
				case config.COUNTER:

					val, err := cm.GetCounterTest(counterInfo.Name)
					if err != nil {
						t.Errorf("error in GetCounterTest: %v", err)
					}
					if int64(val) != counterInstance.Value {
						t.Errorf("Expected counter value %v, got %v", counterInstance.Value, val)
					}

				case config.GAUGE:

					val, err := cm.GetGaugeTest(gaugeInfo.Name)
					if err != nil {
						t.Errorf("error in GetGaugeTest: %v", err)
					}
					if val != gaugeInstance.Value {
						t.Errorf("Expected gauge value %v, got %v", gaugeInstance.Value, val)
					}

				case config.DISTRIBUTION:

					val, err := cm.GetHistogramTest(histogramInfo.Name)
					if err != nil {
						t.Errorf("error in GetHistogramTest: %v", err)
					}
					if val[0] != histogramValSerialized {
						t.Errorf("Expected histogram value %v, got %v", histogramInstance.Value, val[0])
					}
				}
			}
		})
	}
}

func makeConfig(metrics ...*config.Params_MetricInfo) *config.Params {
	return &config.Params{
		SubmissionUrl:      "https://trap.noit.circonus.net/module/httptrap/myuuid/mysecret",
		SubmissionInterval: 100 * time.Second,
		Metrics:            metrics}
}

func makeBadConfig() *config.Params {
	return &config.Params{
		SubmissionUrl:      "this is not a url",
		SubmissionInterval: -100 * time.Second,
		Metrics:            []*config.Params_MetricInfo{counterInfo},
	}
}
