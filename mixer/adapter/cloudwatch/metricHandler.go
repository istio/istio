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

package cloudwatch

import (
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/mixer/adapter/cloudwatch/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/metric"
)

const (
	// CloudWatch enforced limit
	// https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/cloudwatch_limits.html
	metricDatumLimit = 20
)

func (h *handler) sendMetricsToCloudWatch(metricData []*cloudwatch.MetricDatum) (int, error) {
	cloudWatchCallCount := 0
	var multiError *multierror.Error

	for i := 0; i < len(metricData); i += metricDatumLimit {
		size := i + metricDatumLimit
		if len(metricData) < size {
			size = len(metricData)
		}

		if err := h.putMetricData(metricData[i:size]); err == nil {
			cloudWatchCallCount++
		} else {
			multiError = multierror.Append(multiError, err)
		}
	}

	return cloudWatchCallCount, multiError.ErrorOrNil()
}

func (h *handler) generateMetricData(insts []*metric.Instance) []*cloudwatch.MetricDatum {
	metricData := make([]*cloudwatch.MetricDatum, 0, len(insts))

	for _, inst := range insts {
		cwMetric := h.cfg.GetMetricInfo()[inst.Name]

		datum := cloudwatch.MetricDatum{
			MetricName: aws.String(inst.Name),
			Unit:       aws.String(cwMetric.GetUnit().String()),
		}

		value, err := getNumericValue(inst.Value, cwMetric.GetUnit())
		if err != nil {
			// do not fail putting all instances into cloudwatch if one does not have a parsable value
			_ = h.env.Logger().Errorf("could not parse value %v", err)
			continue
		}
		datum.SetValue(value)

		d := make([]*cloudwatch.Dimension, 0, len(inst.Dimensions))

		for k, v := range inst.Dimensions {
			cdimension := &cloudwatch.Dimension{
				Name:  aws.String(k),
				Value: aws.String(adapter.Stringify(v)),
			}
			d = append(d, cdimension)
		}

		datum.SetDimensions(d)

		metricData = append(metricData, &datum)
	}

	return metricData
}

func getNumericValue(value interface{}, unit config.Params_MetricDatum_Unit) (float64, error) {
	switch v := value.(type) {
	case string:
		value, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("can't parse string %s into float", v)
		}
		return value, nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case time.Duration:
		return getDurationNumericValue(v, unit), nil
	default:
		return 0, fmt.Errorf("unsupported value type %T. Only strings and numeric values are accepted", v)
	}
}

func getDurationNumericValue(v time.Duration, unit config.Params_MetricDatum_Unit) float64 {
	switch unit {
	case config.Seconds:
		return float64(v / time.Second)
	case config.Microseconds:
		return float64(v / time.Microsecond)
	case config.Milliseconds:
		return float64(v / time.Millisecond)
	default:
		// assume milliseconds if timestamp unit is not provided
		return float64(v / time.Millisecond)
	}
}

func (h *handler) putMetricData(metricData []*cloudwatch.MetricDatum) error {
	input := cloudwatch.PutMetricDataInput{
		Namespace:  aws.String(h.cfg.Namespace),
		MetricData: metricData,
	}

	if _, err := h.cloudwatch.PutMetricData(&input); err != nil {
		return h.env.Logger().Errorf("could not put metric data into cloudwatch: %v. %v", input, err)
	}

	return nil
}
