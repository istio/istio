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

package loadshedding_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"istio.io/istio/mixer/pkg/loadshedding"
)

func TestOpts(t *testing.T) {
	cases := []struct {
		cmdLine string
		result  loadshedding.Options
	}{
		{"--loadsheddingMode logonly", loadshedding.Options{
			Mode:                        loadshedding.LogOnly,
			SamplesPerSecond:            loadshedding.DefaultSampleFrequency,
			SampleHalfLife:              loadshedding.DefaultHalfLife,
			LatencyEnforcementThreshold: loadshedding.DefaultEnforcementThreshold,
		}},

		{"--averageLatencyThreshold 1s", loadshedding.Options{
			AverageLatencyThreshold:     1 * time.Second,
			SamplesPerSecond:            loadshedding.DefaultSampleFrequency,
			SampleHalfLife:              loadshedding.DefaultHalfLife,
			LatencyEnforcementThreshold: loadshedding.DefaultEnforcementThreshold,
		}},

		{"--latencySamplesPerSecond 1000", loadshedding.Options{
			SamplesPerSecond:            1000,
			SampleHalfLife:              loadshedding.DefaultHalfLife,
			LatencyEnforcementThreshold: loadshedding.DefaultEnforcementThreshold,
		}},

		{"--latencySampleHalflife 10s", loadshedding.Options{
			SamplesPerSecond:            loadshedding.DefaultSampleFrequency,
			SampleHalfLife:              10 * time.Second,
			LatencyEnforcementThreshold: loadshedding.DefaultEnforcementThreshold,
		}},

		{"--maxRequestsPerSecond 100", loadshedding.Options{
			MaxRequestsPerSecond:        100,
			SamplesPerSecond:            loadshedding.DefaultSampleFrequency,
			SampleHalfLife:              loadshedding.DefaultHalfLife,
			LatencyEnforcementThreshold: loadshedding.DefaultEnforcementThreshold,
		}},

		{"--burstSize 10", loadshedding.Options{
			BurstSize:                   10,
			SamplesPerSecond:            loadshedding.DefaultSampleFrequency,
			SampleHalfLife:              loadshedding.DefaultHalfLife,
			LatencyEnforcementThreshold: loadshedding.DefaultEnforcementThreshold,
		}},
	}

	for _, c := range cases {
		t.Run(c.cmdLine, func(tt *testing.T) {
			o := loadshedding.DefaultOptions()
			tt.Logf("defaults: %#v", o)
			cmd := &cobra.Command{}
			o.AttachCobraFlags(cmd)
			cmd.SetArgs(strings.Split(c.cmdLine, " "))

			if err := cmd.Execute(); err != nil {
				tt.Errorf("Got %v, expecting success", err)
			}

			if !reflect.DeepEqual(c.result, o) {
				tt.Errorf("Got %v, expected %v", o, c.result)
			}
		})
	}
}
