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

package promadapter

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"istio.io/istio/mixer/adapter/appoptics/appoptics"
	"istio.io/istio/mixer/adapter/appoptics/papertrail"
)

func TestBatchMeasurements(t *testing.T) {

	t.Run("All Good", func(t *testing.T) {
		logger := &papertrail.LoggerImpl{}
		logger.Infof("Starting %s - test run. . .", t.Name())
		defer logger.Infof("Finished %s - test run. . .", t.Name())
		prepChan := make(chan []*appoptics.Measurement)
		pushChan := make(chan []*appoptics.Measurement)
		stopChan := make(chan struct{})

		loopFactor := true

		go BatchMeasurements(&loopFactor, prepChan, pushChan, stopChan, logger)

		go func() {
			measurements := []*appoptics.Measurement{}
			for i := 0; i < appoptics.MeasurementPostMaxBatchSize+1; i++ {
				measurements = append(measurements, &appoptics.Measurement{})
			}
			prepChan <- measurements
			loopFactor = false
			time.Sleep(time.Millisecond)
			close(prepChan)
			close(pushChan)
		}()
		count := 0
		for range pushChan {
			count++
		}
		if count != 1 {
			t.Errorf("Batching is not working properly. Expected batches is 1 but got %d", count)
		}
		close(stopChan)
	})

	t.Run("Using stop chan", func(t *testing.T) {
		logger := &papertrail.LoggerImpl{}
		logger.Infof("Starting %s - test run. . .", t.Name())
		defer logger.Infof("Finished %s - test run. . .", t.Name())
		prepChan := make(chan []*appoptics.Measurement)
		pushChan := make(chan []*appoptics.Measurement)
		stopChan := make(chan struct{})

		loopFactor := true
		go func() {
			time.Sleep(time.Millisecond)
			stopChan <- struct{}{}
		}()
		BatchMeasurements(&loopFactor, prepChan, pushChan, stopChan, logger)
		loopFactor = false
		close(prepChan)
		close(pushChan)
		close(stopChan)
	})

}

type MockServiceAccessor struct {
	// MeasurementsService implements an interface for dealing with  Measurements
	MockMeasurementsService func() appoptics.MeasurementsCommunicator
}

func (s *MockServiceAccessor) MeasurementsService() appoptics.MeasurementsCommunicator {
	return s.MockMeasurementsService()
}

func TestPersistBatches(t *testing.T) {
	tests := []struct {
		name           string
		expectedCount  int64
		response       *http.Response
		error          error
		sendOnStopChan bool
	}{
		{
			name:          "Persist all good",
			expectedCount: 0,
			response: &http.Response{
				Status:     http.StatusText(http.StatusOK),
				StatusCode: http.StatusOK,
			},
			error:          nil,
			sendOnStopChan: false,
		},
		{
			name:           "Response error",
			expectedCount:  1,
			response:       nil,
			error:          fmt.Errorf("damn"),
			sendOnStopChan: false,
		},
		{
			name:           "Stop chan test",
			expectedCount:  0,
			response:       nil,
			error:          nil,
			sendOnStopChan: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger := &papertrail.LoggerImpl{}
			logger.Infof("Starting %s - test run. . .\n", t.Name())
			defer logger.Infof("Finished %s - test run. . .", t.Name())
			pushChan := make(chan []*appoptics.Measurement)
			stopChan := make(chan struct{})
			errChan := make(chan error)
			var count int64
			var wg sync.WaitGroup
			if test.sendOnStopChan {
				wg.Add(1)
				go func() {
					time.Sleep(time.Millisecond)
					stopChan <- struct{}{}
					wg.Done()
				}()
			}
			go func() {
				time.Sleep(50 * time.Millisecond)
				pushChan <- []*appoptics.Measurement{
					{}, {}, {},
				}
			}()
			go func() {
				time.Sleep(time.Millisecond)
				<-errChan
				atomic.AddInt64(&count, 1)
			}()

			loopFactor := true

			go PersistBatches(&loopFactor, &MockServiceAccessor{
				MockMeasurementsService: func() appoptics.MeasurementsCommunicator {
					return &appoptics.MockMeasurementsService{
						OnCreate: func(measurements []*appoptics.Measurement) (*http.Response, error) {
							return test.response, test.error
						},
					}
				},
			}, pushChan, stopChan, errChan, logger)
			time.Sleep(2 * time.Second)
			logger.Infof("%s - waiting...\n", t.Name())
			if test.sendOnStopChan {
				wg.Wait()
			}
			if atomic.LoadInt64(&count) != test.expectedCount {
				t.Errorf("Count did not match the expected count: %d", test.expectedCount)
			}
			logger.Infof("Closing channels. . .")
			loopFactor = false
			close(pushChan)
			close(stopChan)
			close(errChan)
		})
	}
}
