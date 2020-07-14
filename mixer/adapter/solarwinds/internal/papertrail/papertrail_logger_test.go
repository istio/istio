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

package papertrail

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/mixer/adapter/solarwinds/config"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/logentry"
)

func TestNewLogger(t *testing.T) {
	type (
		args struct {
			paperTrailURL string
			logRetention  time.Duration
			logConfigs    map[string]*config.Params_LogInfo
		}
		testData struct {
			name    string
			args    args
			wantErr bool
		}
	)
	tests := []testData{
		{
			name: "All good",
			args: args{
				paperTrailURL: "hello.world.org",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := test.NewEnv(t)
			logger := env.Logger()
			logger.Infof("Starting %s - test run. . .", t.Name())
			defer logger.Infof("Finished %s - test run. . .", t.Name())

			got, err := NewLogger(tt.args.paperTrailURL, tt.args.logRetention, tt.args.logConfigs, env)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewLogger() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got == nil {
				t.Errorf("Expected a non-nil instance")
			}
		})
	}
}

func TestLog(t *testing.T) {
	t.Run("No log info for msg name", func(t *testing.T) {
		env := test.NewEnv(t)
		logger := env.Logger()
		logger.Infof("Starting %s - test run. . .", t.Name())
		defer logger.Infof("Finished %s - test run. . .", t.Name())

		pp := &Logger{
			paperTrailURL: "hello.world.hey",
			log:           logger,
			env:           env,
			logInfos:      map[string]*logInfo{},
			loopFactor:    make(chan bool),
		}

		if pp.Log(&logentry.Instance{
			Name: "NO ENTRY",
		}) == nil {
			t.Error("An error is expected here.")
		}
	})

	t.Run("All Good", func(t *testing.T) {
		env := test.NewEnv(t)
		logger := env.Logger()
		logger.Infof("Starting %s - test run. . .", t.Name())
		defer logger.Infof("Finished %s - test run. . .", t.Name())

		ppi, err := NewLogger(fmt.Sprintf("%s:%d", "localhost", 6767), time.Hour, map[string]*config.Params_LogInfo{
			"params1": {},
		}, env)
		if err != nil {
			t.Errorf("No error was expected")
		}

		pp, _ := ppi.(*Logger)
		defer pp.Close()

		pcount := getKeyCount(pp)

		if err = pp.Log(&logentry.Instance{
			Name:      "params1",
			Variables: map[string]interface{}{},
		}); err != nil {
			t.Errorf("No error was expected")
		}

		count := getKeyCount(pp)
		if count-pcount != 1 {
			t.Error("key counts don't match")
		}
	})
}

func getKeyCount(pp *Logger) int {
	count := 0
	pp.cmap.Range(func(k, v interface{}) bool {
		count++
		return true
	})
	return count
}
