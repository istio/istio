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
package pkg

import (
	"testing"
	"time"

	attributeV1beta1 "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/template/metric"
)

func TestSendHttpRequest(t *testing.T) {
	validInt := metric.InstanceMsg{
		Name: "NRAdapterUT_int",
		Value: &attributeV1beta1.Value{
			Value: &attributeV1beta1.Value_Int64Value{
				Int64Value: int64(1234),
			},
		},
		Dimensions: map[string]*attributeV1beta1.Value{
			"NRAdapterUT_dim1": {
				Value: &attributeV1beta1.Value_StringValue{
					StringValue: "This is a string dimension",
				},
			},
			"NRAdapterUT_dim2": {
				Value: &attributeV1beta1.Value_Int64Value{
					Int64Value: int64(5678),
				},
			},
			"NRAdapterUT_dim3": {
				Value: &attributeV1beta1.Value_DoubleValue{
					DoubleValue: float64(56.78),
				},
			},
			"NRAdapterUT_dim4": {
				Value: &attributeV1beta1.Value_BoolValue{
					BoolValue: true,
				},
			},
		},
	}

	validFloat := metric.InstanceMsg{
		Name: "NRAdapterUT_float",
		Value: &attributeV1beta1.Value{
			Value: &attributeV1beta1.Value_DoubleValue{
				DoubleValue: float64(1234.5678),
			},
		},
		Dimensions: map[string]*attributeV1beta1.Value{
			"NRAdapterUT_dim1": {
				Value: &attributeV1beta1.Value_StringValue{
					StringValue: "This is a string dimension",
				},
			},
			"NRAdapterUT_dim2": {
				Value: &attributeV1beta1.Value_Int64Value{
					Int64Value: int64(5678),
				},
			},
			"NRAdapterUT_dim3": {
				Value: &attributeV1beta1.Value_DoubleValue{
					DoubleValue: float64(56.78),
				},
			},
			"NRAdapterUT_dim4": {
				Value: &attributeV1beta1.Value_BoolValue{
					BoolValue: true,
				},
			},
		},
	}

	validString := metric.InstanceMsg{
		Name: "NRAdapterUT_string",
		Value: &attributeV1beta1.Value{
			Value: &attributeV1beta1.Value_StringValue{
				StringValue: string("ok"),
			},
		},
	}

	validBool := metric.InstanceMsg{
		Name: "NRAdapterUT_bool",
		Value: &attributeV1beta1.Value{
			Value: &attributeV1beta1.Value_BoolValue{
				BoolValue: true,
			},
		},
	}

	jobRequest := JobRequest{
		PayLoad: []*metric.InstanceMsg{&validInt, &validFloat, &validString, &validBool},
	}
	jobRequest.sendInsightEvents()
	time.Sleep(time.Second * 5) //make sure the goroutine of sendOutMetrics can finish
}
