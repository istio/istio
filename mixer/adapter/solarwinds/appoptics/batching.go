// Copyright 2017 Istio Authors.
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
	"net/http"
	"time"

	"bytes"

	"istio.io/istio/mixer/adapter/solarwinds/config"
	"istio.io/istio/mixer/pkg/adapter"
)

// BatchMeasurements reads slices of Measurement types off a channel populated by the web handler
// and packages them into batches conforming to the limitations imposed by the API.
func BatchMeasurements(loopFactor *bool, prepChan <-chan []*Measurement,
	pushChan chan<- []*Measurement, stopChan <-chan struct{}, logger adapter.Logger) {
	var currentBatch []*Measurement
	dobrk := false
	ticker := time.NewTicker(time.Millisecond * 500)
	for *loopFactor {
		select {
		case mslice := <-prepChan:
			if logger.VerbosityLevel(config.DebugLevel) {
				logger.Infof("AO - batching measurements: %v", mslice)
			}
			currentBatch = append(currentBatch, mslice...)
			if logger.VerbosityLevel(config.DebugLevel) {
				logger.Infof("AO - current batch size: %d, batch max size: %d", len(currentBatch),
					MeasurementPostMaxBatchSize)
			}
			if len(currentBatch) >= MeasurementPostMaxBatchSize {
				pushBatch := currentBatch[:MeasurementPostMaxBatchSize]
				pushChan <- pushBatch
				currentBatch = currentBatch[MeasurementPostMaxBatchSize:]
			}
		case <-ticker.C: // to drain based on time as well
			if len(currentBatch) > 0 {
				if len(currentBatch) >= MeasurementPostMaxBatchSize {
					pushBatch := currentBatch[:MeasurementPostMaxBatchSize]
					pushChan <- pushBatch
					currentBatch = currentBatch[MeasurementPostMaxBatchSize:]
				} else {
					pushChan <- currentBatch
					currentBatch = []*Measurement{}
				}
			}
		case <-stopChan:
			dobrk = true
		}
		if dobrk {
			break
		}
	}
}

// PersistBatches reads maximal slices of Measurement types off a channel and persists them to the remote AppOptics
// API. Errors are placed on the error channel.
func PersistBatches(loopFactor *bool, lc ServiceAccessor, pushChan <-chan []*Measurement,
	stopChan <-chan struct{}, errorChan chan<- error, logger adapter.Logger) {
	ticker := time.NewTicker(time.Millisecond * 500)
	dobrk := false
	for *loopFactor {
		select {
		case <-ticker.C:
			batch := <-pushChan
			if logger.VerbosityLevel(config.DebugLevel) {
				logger.Infof("AO - persisting batch. . .")
			}
			err := persistBatch(lc, batch, logger)
			if err != nil {
				errorChan <- err
			}
		case <-stopChan:
			ticker.Stop()
			dobrk = true
		}
		if dobrk {
			break
		}
	}
}

// ManagePersistenceErrors tracks errors on the provided channel and sends a stop signal if the ErrorLimit is reached
func ManagePersistenceErrors(loopFactor *bool, errorChan <-chan error, stopChan chan<- struct{},
	logger adapter.Logger) {
	// var errors []error
	for *loopFactor {
		select {
		case err := <-errorChan:
			if err != nil {
				// errors = append(errors, err)
				// if len(errors) > config.PushErrorLimit() {
				// 	stopChan <- true
				// 	break
				// }
				logger.Errorf("AO - Persistence Errors: %v", err)
			}
		}

	}
}

// persistBatch sends to the remote AppOptics endpoint unless config.SendStats()
// returns false, when it prints to stdout
func persistBatch(lc ServiceAccessor, batch []*Measurement,
	logger adapter.Logger) error {
	if logger.VerbosityLevel(config.DebugLevel) {
		logger.Infof("AO - persisting %d Measurements to AppOptics", len(batch))
	}
	if len(batch) > 0 {
		resp, err := lc.MeasurementsService().Create(batch)
		if err != nil {
			logger.Errorf("AO - persist error: %v", err)
			return err
		}
		dumpResponse(resp, logger)
	}
	return nil
}

func dumpResponse(resp *http.Response, logger adapter.Logger) {
	buf := new(bytes.Buffer)
	if logger.VerbosityLevel(config.DebugLevel) {
		logger.Infof("AO - response status: %s", resp.Status)
	}
	if resp.Body != nil {
		buf.ReadFrom(resp.Body)
		if logger.VerbosityLevel(config.DebugLevel) {
			logger.Infof("AO - response body: %s", string(buf.Bytes()))
		}
	}
}
