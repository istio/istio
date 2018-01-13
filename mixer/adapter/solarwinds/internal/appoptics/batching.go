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
	"bytes"
	"net/http"
	"time"

	"istio.io/istio/mixer/pkg/adapter"
)

// BatchMeasurements reads slices of Measurement types off a channel populated by the web handler
// and packages them into batches conforming to the limitations imposed by the API.
func BatchMeasurements(loopFactor *bool, prepChan <-chan []*Measurement,
	pushChan chan<- []*Measurement, stopChan <-chan struct{}, batchSize int, logger adapter.Logger) {
	var currentBatch []*Measurement
	dobrk := false
	ticker := time.NewTicker(time.Millisecond * 500)
	defer ticker.Stop()
	for *loopFactor {
		select {
		case mslice := <-prepChan:
			currentBatch = append(currentBatch, mslice...)
			if len(currentBatch) >= batchSize {
				pushBatch := currentBatch[:batchSize]
				pushChan <- pushBatch
				currentBatch = currentBatch[batchSize:]
			}
		case <-ticker.C: // to drain based on time as well
			if len(currentBatch) > 0 {
				if len(currentBatch) >= batchSize {
					pushBatch := currentBatch[:batchSize]
					pushChan <- pushBatch
					currentBatch = currentBatch[batchSize:]
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
	stopChan <-chan struct{}, logger adapter.Logger) {
	ticker := time.NewTicker(time.Millisecond * 500)
	defer ticker.Stop()
	dobrk := false
	for *loopFactor {
		select {
		case <-ticker.C:
			batch := <-pushChan
			err := persistBatch(lc, batch, logger)
			if err != nil {
				logger.Errorf("ao - Persistence Errors: %v", err)
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

// persistBatch sends to the remote AppOptics endpoint unless config.SendStats()
// returns false, when it prints to stdout
func persistBatch(lc ServiceAccessor, batch []*Measurement,
	logger adapter.Logger) error {
	if len(batch) > 0 {
		//resp, err := lc.MeasurementsService().Create(batch)
		_, err := lc.MeasurementsService().Create(batch)
		if err != nil {
			logger.Errorf("ao - persist error: %v", err)
			return err
		}
		//dumpResponse(resp, logger)
	}
	return nil
}

func dumpResponse(resp *http.Response, logger adapter.Logger) {
	buf := new(bytes.Buffer)
	if resp.Body != nil {
		buf.ReadFrom(resp.Body)
		logger.Infof("ao - response body: %s", string(buf.Bytes()))
	}
}
