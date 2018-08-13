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
// WITHOUT WARRANTIES OR CONDITIONS OF ANY Type, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cloudmonitor

import (
	"istio.io/istio/mixer/template/metric"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/cms"
	"encoding/json"
	"fmt"
	"time"
	"strconv"
)

const (
	// CloudMonitor enforced limit
	// https://help.aliyun.com/document_detail/63275.html
	// At most only 100 datum can be reported per each batch
	// The max size of data body is 256KB
	metricDatumLimit = 100
)

// Custom Metric Request
type CustomMetricRequest struct {
	GroupId    int64                  `json:"groupId"`
	MetricName string                 `json:"metricName"`
	Dimensions map[string]interface{} `json:"dimensions"`
	Time       int64                  `json:"time"`
	Type       uint8                  `json:"type"`
	Period     uint16                 `json:"period"`
	Values     map[string]interface{} `json:"values"`
}

// Expands the identity of a metric.
type Dimension struct {
	_ struct{} `type:"structure"`

	// The name of the dimension.
	//
	// Name is a required field
	Name *string `min:"1" type:"string" required:"true"`

	// The value representing the dimension measurement.
	//
	// Value is a required field
	Value *string `min:"1" type:"string" required:"true"`
}

func (h *handler) sendMetricsToCloudMonitor(customMetricRequest *cms.PutCustomMetricRequest) (*cms.PutCustomMetricResponse, error) {
	response, err := h.client.PutCustomMetric(customMetricRequest)
	if err != nil {
		panic(err)
	}

	return response, err
}

func (h *handler) generateMetricData(insts []*metric.Instance) *cms.PutCustomMetricRequest {
	customMetricRequest := cms.CreatePutCustomMetricRequest()

	metricData := make([]*CustomMetricRequest, 0, len(insts))
	fmt.Println(h.cfg.GetMetricInfo())
	for _, inst := range insts {
		cmMetric := h.cfg.GetMetricInfo()[inst.Name]
		fmt.Printf("cmMetric Size: %d.\n", cmMetric.Size())

		datum := CustomMetricRequest{
			GroupId:    h.cfg.GroupId,
			MetricName: inst.Name,
		}
		d := make(map[string]interface{}, len(inst.Dimensions))
		for k, v := range inst.Dimensions {
			d[k] = v
		}
		datum.Dimensions = d
		datum.Time = time.Now().Unix() * 1000
		datum.Type = 0
		datum.Period = 60

		value, err := getNumericValue(inst.Value)
		if err != nil {
			// do not fail putting all instances into cloudwatch if one does not have a parsable value
			_ = h.env.Logger().Errorf("could not parse value %v", err)
			continue
		}
		values := make(map[string]interface{}, 1)
		values["value"] = value
		datum.Values = values

		metricData = append(metricData, &datum)
	}

	output, _ := json.Marshal(&metricData)
	customMetricRequest.MetricList = string(output)

	return customMetricRequest
}

func getNumericValue(value interface{}) (float64, error) {
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
	default:
		return 0, fmt.Errorf("unsupported value type %T. Only strings and numeric values are accepted", v)
	}
}
