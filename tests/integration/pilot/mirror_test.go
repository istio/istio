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

package pilot

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/util"
	"istio.io/pkg/log"
)

//	Virtual service topology
//
//	    a                      b                     c
//	|-------|             |-------|    mirror   |-------|
//	| Host0 | ----------> | Host1 | ----------> | Host2 |
//	|-------|             |-------|             |-------|
//

type VirtualServiceMirrorConfig struct {
	Name       string
	Absent     bool
	Percent    float64
	MirrorHost string
}

type testCaseMirror struct {
	name                string
	absent              bool
	percentage          float64
	threshold           float64
	expectedDestination echo.Instance
}

type mirrorTestOptions struct {
	t          *testing.T
	cases      []testCaseMirror
	mirrorHost string
}

var (
	mirrorProtocols = []protocol.Instance{protocol.HTTP, protocol.GRPC}
)

func TestMirroring(t *testing.T) {
	cases := []testCaseMirror{
		{
			name:       "mirror-percent-absent",
			absent:     true,
			percentage: 100.0,
			threshold:  0.0,
		},
		{
			name:       "mirror-50",
			percentage: 50.0,
			threshold:  10.0,
		},
		{
			name:       "mirror-10",
			percentage: 10.0,
			threshold:  5.0,
		},
		{
			name:       "mirror-0",
			percentage: 0.0,
			threshold:  0.0,
		},
	}

	runMirrorTest(mirrorTestOptions{
		t:     t,
		cases: cases,
	})
}

// Tests mirroring to an external service. Uses same topology as the test above, a -> b -> external, with "external" being external.
//
// Since we don't want to rely on actual external websites, we simulate that by using a Sidecar to limit connectivity
// from "a" so that it cannot reach "external" directly, and we use a ServiceEntry to define our "external" website, which
// is static and points to the service "external" ip.

// Thus when "a" tries to mirror to the external service, it is actually connecting to "external" (which is not part of the
// mesh because of the Sidecar), then we can inspect "external" logs to verify the requests were properly mirrored.
func TestMirroringExternalService(t *testing.T) {
	cases := []testCaseMirror{
		{
			name:                "mirror-external",
			absent:              true,
			percentage:          100.0,
			threshold:           0.0,
			expectedDestination: apps.external,
		},
	}

	runMirrorTest(mirrorTestOptions{
		t:          t,
		cases:      cases,
		mirrorHost: apps.externalHost,
	})
}

func runMirrorTest(options mirrorTestOptions) {
	framework.
		NewTest(options.t).
		Features("traffic.mirroring").
		RequiresSingleCluster().
		Run(func(ctx framework.TestContext) {
			for _, c := range options.cases {
				options.t.Run(c.name, func(t *testing.T) {
					mirrorHost := options.mirrorHost
					if len(mirrorHost) == 0 {
						mirrorHost = apps.podC.Config().Service
					}
					vsc := VirtualServiceMirrorConfig{
						c.name,
						c.absent,
						c.percentage,
						mirrorHost,
					}

					deployment := tmpl.EvaluateOrFail(t,
						file.AsStringOrFail(t, "testdata/traffic-mirroring-template.yaml"), vsc)
					ctx.Config().ApplyYAMLOrFail(t, apps.namespace.Name(), deployment)
					defer ctx.Config().DeleteYAMLOrFail(t, apps.namespace.Name(), deployment)

					for _, proto := range mirrorProtocols {
						t.Run(string(proto), func(t *testing.T) {
							retry.UntilSuccessOrFail(t, func() error {
								testID := util.RandomString(16)
								if err := sendTrafficMirror(apps.podA, apps.podB, proto, testID); err != nil {
									return err
								}
								expected := c.expectedDestination
								if expected == nil {
									expected = apps.podC
								}
								return verifyTrafficMirror(apps.podB, expected, c, testID)
							}, retry.Delay(time.Second))
						})
					}
				})
			}
		})
}

func sendTrafficMirror(from, to echo.Instance, proto protocol.Instance, testID string) error {
	options := echo.CallOptions{
		Target:   to,
		Count:    50,
		PortName: strings.ToLower(string(proto)),
	}
	switch proto {
	case protocol.HTTP:
		options.Path = "/" + testID
	case protocol.GRPC:
		options.Message = testID
	default:
		return fmt.Errorf("protocol not supported in mirror testing: %s", proto)
	}

	_, err := from.Call(options)
	if err != nil {
		return err
	}

	return nil
}

func verifyTrafficMirror(dest, mirror echo.Instance, tc testCaseMirror, testID string) error {
	countB, err := logCount(dest, testID)
	if err != nil {
		return err
	}

	countC, err := logCount(mirror, testID)
	if err != nil {
		return err
	}

	actualPercent := (countC / countB) * 100
	deltaFromExpected := math.Abs(actualPercent - tc.percentage)

	if tc.threshold-deltaFromExpected < 0 {
		err := fmt.Errorf("unexpected mirror traffic. Expected %g%%, got %.1f%% (threshold: %g%%, testID: %s)",
			tc.percentage, actualPercent, tc.threshold, testID)
		log.Infof("%v", err)
		return err
	}

	log.Infof("Got expected mirror traffic. Expected %g%%, got %.1f%% (threshold: %g%%, , testID: %s)",
		tc.percentage, actualPercent, tc.threshold, testID)
	return nil
}

func logCount(instance echo.Instance, testID string) (float64, error) {
	workloads, err := instance.Workloads()
	if err != nil {
		return -1, fmt.Errorf("failed to get Subsets: %v", err)
	}

	var logs string
	for _, w := range workloads {
		l, err := w.Logs()
		if err != nil {
			return -1, fmt.Errorf("failed getting logs: %v", err)
		}
		logs += l
	}

	return float64(strings.Count(logs, testID)), nil
}
