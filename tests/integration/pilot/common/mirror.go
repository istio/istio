//go:build integ

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

package common

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-multierror"
	"k8s.io/apimachinery/pkg/util/rand"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/util/retry"
)

// VirtualServiceMirrorConfig holds template parameters for the traffic-mirroring VS template.
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

// RunMirroringTests validates VirtualService traffic mirroring percentages (0%, 10%, 50%, 100%)
// over HTTP and gRPC through sidecar-intercepted traffic.
func RunMirroringTests(t framework.TestContext, apps deployment.SingleNamespaceView) {
	t.Helper()
	runMirrorTest(t, apps, mirrorTestOptions{
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

// RunMirroringExternalServiceTests validates that traffic can be mirrored to an external
// ServiceEntry host (simulated via Sidecar egress restrictions + static ServiceEntry).
func RunMirroringExternalServiceTests(t framework.TestContext, apps deployment.SingleNamespaceView) {
	t.Helper()
	header := ""
	if len(apps.External.All) > 0 {
		header = apps.External.All.Config().HostHeader()
	}
	runMirrorTest(t, apps, mirrorTestOptions{
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

func runMirrorTest(t framework.TestContext, apps deployment.SingleNamespaceView, options mirrorTestOptions) {
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

				t.ConfigIstio().EvalFile(apps.Namespace.Name(), vsc,
						filepath.Join(env.IstioSrc, "tests/integration/pilot/testdata/traffic-mirroring-template.yaml")).
						ApplyOrFail(t)

			for _, podA := range apps.A {
				t.NewSubTest(fmt.Sprintf("from %s", podA.Config().Cluster.StableName())).Run(func(t framework.TestContext) {
					for _, proto := range mirrorProtocols {
						t.NewSubTest(string(proto)).Run(func(t framework.TestContext) {
							retry.UntilSuccessOrFail(t, func() error {
								testID := rand.String(16)
								if err := sendMirrorTraffic(podA, apps.B, proto, testID); err != nil {
									return err
								}
								expected := c.expectedDestination
								if expected == nil {
									expected = apps.C
								}
								return verifyMirrorTraffic(apps.B, expected, c, testID)
							}, echo.DefaultCallRetryOptions()...)
						})
					}
				})
			}
		})
	}
}

func sendMirrorTraffic(from echo.Instance, to echo.Target, proto protocol.Instance, testID string) error {
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
	return err
}

func verifyMirrorTraffic(dest, mirror echo.Instances, tc testCaseMirror, testID string) error {
	countB, err := mirrorLogCount(dest, testID)
	if err != nil {
		return err
	}
	countC, err := mirrorLogCount(mirror, testID)
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

func mirrorLogCount(instances echo.Instances, testID string) (float64, error) {
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
	return total, nil
}
