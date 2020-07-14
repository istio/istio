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

package solarwinds

import (
	"context"
	"fmt"
	"testing"

	"istio.io/istio/mixer/adapter/solarwinds/config"
	"istio.io/istio/mixer/adapter/solarwinds/internal/papertrail"
	test2 "istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/logentry"
)

func TestNewLogHandler(t *testing.T) {
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
			env := test2.NewEnv(t)
			logger := env.Logger()
			logger.Infof("Starting %s - test run. . .", t.Name())
			defer logger.Infof("Finished %s - test run. . .", t.Name())

			lh, err := newLogHandler(test2.NewEnv(t), test.cfg)
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
	env := test2.NewEnv(t)
	logger := env.Logger()
	t.Run("All good", func(t *testing.T) {
		logger.Infof("Starting %s - test run. . .", t.Name())
		defer logger.Infof("Finished %s - test run. . .", t.Name())
		port := 34543

		lh, _ := newLogHandler(test2.NewEnv(t), &config.Params{
			PapertrailUrl: fmt.Sprintf("localhost:%d", port),
			Logs: map[string]*config.Params_LogInfo{
				"params1": {},
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
		lh, err := newLogHandler(test2.NewEnv(t), &config.Params{})
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
