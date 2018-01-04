package appoptics

import (
	"encoding/json"
	"math"
	"net/http"

	"istio.io/istio/mixer/adapter/appoptics/config"
	"istio.io/istio/mixer/pkg/adapter"
)

// https://www.AppOptics.com/docs/api/?shell#measurements
// Measurements are the individual time series samples sent to AppOptics. They are
// associated by name with a Metric.

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
	d, err := json.Marshal(payload)
	if err != nil {
		ms.logger.Errorf("AO - Marshal error: %v\n", err)
		return nil, err
	}
	if ms.logger.VerbosityLevel(config.DebugLevel) {
		ms.logger.Infof("AO - sending data to AppOptics with payload: %v\n", string(d))
	}
	req, err := ms.client.NewRequest("POST", "measurements", payload)

	if err != nil {
		ms.logger.Errorf("error creating request:", err)
		return nil, err
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
