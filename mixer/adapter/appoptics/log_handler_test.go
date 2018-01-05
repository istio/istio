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
	"context"
	"fmt"
	"testing"

	"istio.io/istio/mixer/adapter/appoptics/config"
	"istio.io/istio/mixer/adapter/appoptics/papertrail"
	"istio.io/istio/mixer/template/logentry"
)

func TestNewLogHandler(t *testing.T) {
	ctx := context.Background()

	type testData struct {
		name      string
		cfg       *config.Params
		compareFn func(logHandlerInterface) bool
	}
	tests := []*testData{
		{
			name: "All good",
			cfg: &config.Params{
				PapertrailUrl: "hello.world.org",
			},
			compareFn: func(lhi logHandlerInterface) bool {
				lh, _ := lhi.(*logHandler)
				return lh.paperTrailLogger != nil
			},
		},
		{
			name: "Empty ref",
			cfg:  &config.Params{},
			compareFn: func(lhi logHandlerInterface) bool {
				lh, _ := lhi.(*logHandler)
				pp, _ := lh.paperTrailLogger.(*papertrail.Logger)
				return pp == nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger := &papertrail.LoggerImpl{}
			logger.Infof("Starting %s - test run. . .", t.Name())
			defer logger.Infof("Finished %s - test run. . .", t.Name())

			lh, err := newLogHandler(ctx, &adapterEnvInst{}, test.cfg)
			if err != nil {
				t.Errorf("Unexpected error: %v while running test: %s", err, t.Name())
				return
			}

			if !test.compareFn(lh) {
				t.Errorf("Unexpected response from compare function while running test: %s", t.Name())
			}
		})
	}
}

func TestHandleLogEntry(t *testing.T) {
	ctx := context.Background()
	logger := &papertrail.LoggerImpl{}
	t.Run("All good", func(t *testing.T) {
		logger.Infof("Starting %s - test run. . .", t.Name())
		defer logger.Infof("Finished %s - test run. . .", t.Name())
		port := 34543
		serverStopChan := make(chan struct{})
		serverTrackChan := make(chan struct{})

		go papertrail.RunUDPServer(port, logger, serverStopChan, serverTrackChan)
		go func() {
			count := 0
			for range serverTrackChan {
				count++
			}
			if count != 1 {
				t.Errorf("Expected data count (1) received by server dont match the actual number: %d", count)
			}
		}()

		lh, _ := newLogHandler(ctx, &adapterEnvInst{}, &config.Params{
			PapertrailUrl: fmt.Sprintf("localhost:%d", port),
			Logs: []*config.Params_LogInfo{
				{
					InstanceName: "params1",
				},
			},
		})
		err := lh.handleLogEntry(ctx, []*logentry.Instance{
			{
				Name:      "params1",
				Variables: map[string]interface{}{},
			},
		})
		if err != nil {
			t.Errorf("Unexpected error while executing test: %s - err: %v", t.Name(), err)
			return
		}
	})

	t.Run("papertrail instance is nil", func(t *testing.T) {
		logger.Infof("Starting %s - test run. . .", t.Name())
		lh, err := newLogHandler(ctx, &adapterEnvInst{}, &config.Params{})
		if err != nil {
			t.Errorf("Unexpected error while executing test: %s - err: %v", t.Name(), err)
			return
		}
		err = lh.handleLogEntry(ctx, []*logentry.Instance{
			{},
		})
		if err != nil {
			t.Errorf("Unexpected error while executing test: %s - err: %v", t.Name(), err)
			return
		}
	})
}