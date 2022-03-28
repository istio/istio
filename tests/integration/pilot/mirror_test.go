//go:build integ
// +build integ

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

	"github.com/hashicorp/go-multierror"
	"k8s.io/apimachinery/pkg/util/rand"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/util/retry"
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
	expectedDestination echo.Instances
}

type mirrorTestOptions struct {
	cases      []testCaseMirror
	mirrorHost string
}

var mirrorProtocols = []protocol.Instance{protocol.HTTP, protocol.GRPC}

func TestMirroring(t *testing.T) {
	runMirrorTest(t, mirrorTestOptions{
		cases: []testCaseMirror{
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
		},
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
	header := ""
	if len(apps.External.All) > 0 {
		header = apps.External.All.Config().HostHeader()
	}
	runMirrorTest(t, mirrorTestOptions{
		mirrorHost: header,
		cases: []testCaseMirror{
			{
				name:                "mirror-external",
				absent:              true,
				percentage:          100.0,
				threshold:           0.0,
				expectedDestination: apps.External.All,
			},
		},
	})
}

func runMirrorTest(t *testing.T, options mirrorTestOptions) {
	framework.
		NewTest(t).
		Features("traffic.mirroring").
		Run(func(t framework.TestContext) {
			for _, c := range options.cases {
				t.NewSubTest(c.name).Run(func(t framework.TestContext) {
					mirrorHost := options.mirrorHost
					if len(mirrorHost) == 0 {
						mirrorHost = deployment.CSvc
					}
					vsc := VirtualServiceMirrorConfig{
						c.name,
						c.absent,
						c.percentage,
						mirrorHost,
					}

					// we only apply to config clusters
					t.ConfigIstio().EvalFile(apps.Namespace.Name(), vsc, "testdata/traffic-mirroring-template.yaml").ApplyOrFail(t)

					for _, podA := range apps.A {
						podA := podA
						t.NewSubTest(fmt.Sprintf("from %s", podA.Config().Cluster.StableName())).Run(func(t framework.TestContext) {
							for _, proto := range mirrorProtocols {
								t.NewSubTest(string(proto)).Run(func(t framework.TestContext) {
									retry.UntilSuccessOrFail(t, func() error {
										testID := rand.String(16)
										if err := sendTrafficMirror(podA, apps.B, proto, testID); err != nil {
											return err
										}
										expected := c.expectedDestination
										if expected == nil {
											expected = apps.C
										}

										return verifyTrafficMirror(apps.B, expected, c, testID)
									}, echo.DefaultCallRetryOptions()...)
								})
							}
						})
					}
				})
			}
		})
}

func sendTrafficMirror(from echo.Instance, to echo.Target, proto protocol.Instance, testID string) error {
	options := echo.CallOptions{
		To:    to,
		Count: 100,
		Port: echo.Port{
			Name: strings.ToLower(proto.String()),
		},
		Retry: echo.Retry{
			NoRetry: true,
		},
	}
	switch proto {
	case protocol.HTTP:
		options.HTTP.Path = "/" + testID
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

func verifyTrafficMirror(dest, mirror echo.Instances, tc testCaseMirror, testID string) error {
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

	var merr *multierror.Error
	if tc.threshold-deltaFromExpected < 0 {
		err := fmt.Errorf("unexpected mirror traffic. Expected %g%%, got %.1f%% (threshold: %g%%, testID: %s)",
			tc.percentage, actualPercent, tc.threshold, testID)
		log.Infof("%v", err)
		merr = multierror.Append(merr, err)
	} else {
		log.Infof("Got expected mirror traffic. Expected %g%%, got %.1f%% (threshold: %g%%, , testID: %s)",
			tc.percentage, actualPercent, tc.threshold, testID)
	}

	return merr.ErrorOrNil()
}

func logCount(instances echo.Instances, testID string) (float64, error) {
	counts := map[string]float64{}
	for _, instance := range instances {
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
		if c := float64(strings.Count(logs, testID)); c > 0 {
			counts[instance.Config().Cluster.Name()] = c
		}
	}
	var total float64
	for _, c := range counts {
		total += c
	}
	// TODO(landow) mirorr split does not always hit all clusters
	return total, nil
}
