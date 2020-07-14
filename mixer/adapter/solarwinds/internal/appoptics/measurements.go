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

package appoptics

import (
	"math"
	"net/http"

	"istio.io/istio/mixer/pkg/adapter"
)

// https://www.AppOptics.com/docs/api/?shell#measurements
// Measurements are the individual time series samples sent to AppOptics. They are
// associated by name with a Metric.

// MeasurementTags is used to store the tags (labels)
type MeasurementTags map[string]string

// Measurement corresponds to the AppOptics API type of the same name
// TODO: support the full set of Measurement fields
type Measurement struct {
	// Name is the name of the Metric this Measurement is associated with
	Name string `json:"name"`
	// Tags add dimensionality to data, similar to Labels in Prometheus
	Tags MeasurementTags `json:"tags,omitempty"`
	// Time is the UNIX epoch timestamp of the Measurement
	Time int64 `json:"time"`
	// Value is the value of the
	Value float64 `json:"value"`
}

// MeasurementPayload is the construct we POST to the API
type MeasurementPayload struct {
	Measurements []*Measurement `json:"measurements"`
}

// MeasurementsCommunicator defines an interface for communicating with the Measurements portion of the AppOptics API
type MeasurementsCommunicator interface {
	Create([]*Measurement) (*http.Response, error)
}

// MeasurementsService implements MeasurementsCommunicator
type MeasurementsService struct {
	client *Client
	logger adapter.Logger
}

// Create persists the given MeasurementCollection to AppOptics
func (ms *MeasurementsService) Create(mc []*Measurement) (*http.Response, error) {
	payload := MeasurementPayload{mc}
	req, err := ms.client.NewRequest("POST", "measurements", payload)

	if err != nil {
		return nil, ms.logger.Errorf("error creating request: %v", err)
	}
	return ms.client.Do(req, nil)
}

func dumpMeasurements(measurements interface{}, logger adapter.Logger) {
	ms, ok := measurements.(MeasurementPayload)
	if ok {
		for i, measurement := range ms.Measurements {
			if math.IsNaN(measurement.Value) {
				logger.Infof("Found at index %d", i)
				logger.Infof("found in '%s'", measurement.Name)
			}
		}
	}
}
