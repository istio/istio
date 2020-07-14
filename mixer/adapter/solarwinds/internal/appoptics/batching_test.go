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
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	test2 "istio.io/istio/mixer/pkg/adapter/test"
)

func TestBatchMeasurements(t *testing.T) {
	t.Run("All Good", func(t *testing.T) {
		env := test2.NewEnv(t)
		logger := env.Logger()

		logger.Infof("Starting %s - test run. . .", t.Name())
		defer logger.Infof("Finished %s - test run. . .", t.Name())
		prepChan := make(chan []*Measurement)
		pushChan := make(chan []*Measurement)
		stopChan := make(chan struct{})
		defer close(prepChan)
		defer close(pushChan)

		batchSize := 100
		go func() {
			measurements := make([]*Measurement, 0)
			for i := 0; i < batchSize+1; i++ {
				measurements = append(measurements, new(Measurement))
			}
			prepChan <- measurements
			count := 0
			timeout := time.After(time.Second)
			for {
				exit := false
				select {
				case <-pushChan:
					count++
				case <-timeout:
					exit = true
				}
				if exit {
					break
				}
			}
			if count != 2 {
				t.Errorf("Batching is not working properly. Expected batches is 2 but got %d", count)
			}
			stopChan <- struct{}{}
		}()
		BatchMeasurements(prepChan, pushChan, stopChan, batchSize)
		close(stopChan)
	})

	t.Run("Using stop chan", func(t *testing.T) {
		env := test2.NewEnv(t)
		logger := env.Logger()
		logger.Infof("Starting %s - test run. . .", t.Name())
		defer logger.Infof("Finished %s - test run. . .", t.Name())
		prepChan := make(chan []*Measurement)
		pushChan := make(chan []*Measurement)
		stopChan := make(chan struct{})
		defer close(prepChan)
		defer close(pushChan)
		defer close(stopChan)

		batchSize := 100
		go func() {
			time.Sleep(time.Millisecond)
			stopChan <- struct{}{}
		}()
		BatchMeasurements(prepChan, pushChan, stopChan, batchSize)
	})
}

type MockServiceAccessor struct {
	// MeasurementsService implements an interface for dealing with  Measurements
	MockMeasurementsService func() MeasurementsCommunicator
}

func (s *MockServiceAccessor) MeasurementsService() MeasurementsCommunicator {
	return s.MockMeasurementsService()
}

func TestPersistBatches(t *testing.T) {
	tests := []struct {
		name           string
		response       *http.Response
		error          error
		expectedCount  int32
		sendOnStopChan bool
	}{
		{
			name:          "Persist all good",
			expectedCount: 1,
			response: &http.Response{
				Status:     http.StatusText(http.StatusOK),
				StatusCode: http.StatusOK,
			},
			error:          nil,
			sendOnStopChan: false,
		},
		{
			name:           "Persist with error",
			expectedCount:  0,
			response:       nil,
			error:          fmt.Errorf("metrics empty"),
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
			env := test2.NewEnv(t)
			logger := env.Logger()
			logger.Infof("Starting %s - test run. . .\n", t.Name())
			defer logger.Infof("Finished %s - test run. . .", t.Name())
			pushChan := make(chan []*Measurement)
			stopChan := make(chan struct{})
			var count int32
			var testWg sync.WaitGroup
			testWg.Add(1)

			var action sync.WaitGroup
			action.Add(1)
			go func() {
				PersistBatches(&MockServiceAccessor{
					MockMeasurementsService: func() MeasurementsCommunicator {
						return &MockMeasurementsService{
							OnCreate: func(measurements []*Measurement) (*http.Response, error) {
								if test.error == nil {
									atomic.AddInt32(&count, 1)
								}
								action.Done()
								return test.response, test.error
							},
						}
					},
				}, pushChan, stopChan, logger)
				testWg.Done()
			}()

			if test.sendOnStopChan {
				go func() {
					time.Sleep(time.Millisecond)
					stopChan <- struct{}{}
					action.Done()
				}()
			} else {
				go func() {
					time.Sleep(50 * time.Millisecond)
					pushChan <- []*Measurement{
						{}, {}, {},
					}
				}()
			}

			logger.Infof("%s - waiting...\n", t.Name())
			action.Wait()
			if c := atomic.LoadInt32(&count); c != test.expectedCount {
				t.Errorf("Count %d did not match the expected count: %d", c, test.expectedCount)
			}
			logger.Infof("Closing channels. . .")
			if !test.sendOnStopChan {
				stopChan <- struct{}{}
			}
			close(pushChan)
			close(stopChan)
			testWg.Wait()
		})
	}
}
