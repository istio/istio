// Copyright 2017 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY Type, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cloudwatch

import (
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatch"

	"istio.io/istio/mixer/template/metric"
)

const (
	// CloudWatch enforced limit
	// https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/cloudwatch_limits.html
	metricDatumLimit = 20
	// CloudWatch enforced limit
	// https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/cloudwatch_limits.html
	dimensionLimit = 10
)

func handleValidMetrics(h *Handler, insts []*metric.Instance) error {
	metricData := generateMetricData(h, insts)
	_, err := sendMetricsToCloudWatch(h, metricData)
	return err
}

func sendMetricsToCloudWatch(h *Handler, metricData []*cloudwatch.MetricDatum) (int, error) {
	cloudWatchCallCount := 0
	var err error

	for i := 0; i < len(metricData); i += metricDatumLimit {
		size := i + metricDatumLimit
		if len(metricData) < size {
			size = len(metricData)
		}

		err = putMetricData(h, metricData[i:size])
		if err == nil {
			cloudWatchCallCount++
		}
	}

	return cloudWatchCallCount, err
}

func generateMetricData(h *Handler, insts []*metric.Instance) []*cloudwatch.MetricDatum {
	metricData := make([]*cloudwatch.MetricDatum, 0, len(insts))

	for _, inst := range insts {
		cwMetric := h.cfg.GetMetricInfo()[inst.Name]

		datum := cloudwatch.MetricDatum{
			MetricName: aws.String(inst.Name),
			Unit:       aws.String(cwMetric.GetUnit()),
		}

		if cwMetric.GetStorageResolution() != 0 {
			datum.SetStorageResolution(cwMetric.GetStorageResolution())
		}

		value, err := getNumericValue(h, inst)
		if err != nil {
			continue
		}
		datum.SetValue(value)

		// Only 10 dimenstions can be added to a metric. This is a cloudwatch limit.
		d := make([]*cloudwatch.Dimension, 0, dimensionLimit)

		for k, v := range inst.Dimensions {

			// check whether the dimension is a timestamp
			t, ok := v.(time.Time)
			if ok {
				datum.SetTimestamp(t)
				continue
			}

			// if not timestamp, add to the dimension map
			cdimension := &cloudwatch.Dimension{
				Name: aws.String(k),
			}
			dimensionValue, err := getDimensionValue(h, v)
			if err != nil {
				continue
			}
			cdimension.SetValue(dimensionValue)
			d = append(d, cdimension)
		}
		datum.SetDimensions(d)

		metricData = append(metricData, &datum)
	}

	return metricData
}

func getDimensionValue(h *Handler, value interface{}) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		return strconv.FormatFloat(v, 'E', -1, 64), nil
	case bool:
		return strconv.FormatBool(v), nil
	case time.Duration:
		return v.String(), nil
	case net.IP:
		return v.String(), nil
	default:
		return "", h.env.Logger().Errorf("Dimensions contain an unsupported type: %T", v)
	}
}

func getNumericValue(h *Handler, inst *metric.Instance) (float64, error) {
	switch v := inst.Value.(type) {
	case string:
		value, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, h.env.Logger().Errorf("Can't parse string %s into float", v)
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
	default:
		return 0, h.env.Logger().Errorf("Unsupported value type %T. Only strings and numeric values are accepted", v)
	}
}

func putMetricData(h *Handler, metricData []*cloudwatch.MetricDatum) error {
	if len(metricData) == 0 {
		err := errors.New("metricData is empty")
		return h.env.Logger().Errorf("cannot send empty metrics to cloudwatch: %v", err)
	}

	if len(h.cfg.Namespace) == 0 {
		err := errors.New("cloudwatch namespace is not provided in configuration")
		return h.env.Logger().Errorf("cloudwatch namespace is required: %v", err)
	}

	input := cloudwatch.PutMetricDataInput{
		Namespace:  aws.String(h.cfg.Namespace),
		MetricData: metricData,
	}

	_, err := h.cloudwatch.PutMetricData(&input)
	if err != nil {
		return h.env.Logger().Errorf("could not put metric data into cloudwatch: %v. %v", input, err)
	}

	return nil
}
