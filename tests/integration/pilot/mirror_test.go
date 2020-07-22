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

	"istio.io/pkg/log"

	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"

	"istio.io/istio/tests/util"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/telemetry/outboundtrafficpolicy"
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
	Namespace  string
	Absent     bool
	Percent    float64
	TargetHost string
	MirrorHost string
}

// ExternalServiceMirrorConfig sets up a Sidecar and ServiceEntry to prevent mirrored service from being called directly.
type ExternalServiceMirrorConfig struct {
	Namespace     string
	Domain        string
	MirrorHost    string
	MirrorAddress string
	TargetHost    string
}

type testCaseMirror struct {
	name       string
	absent     bool
	percentage float64
	threshold  float64
}

type mirrorTestOptions struct {
	t              *testing.T
	ctx            framework.TestContext
	cases          []testCaseMirror
	mirrorHost     string
	fnInjectConfig func(ns namespace.Instance, ctx resource.Context, instances [3]echo.Instance) (cleanup func())
	instances      [3]echo.Instance
}

var (
	mirrorProtocols = []protocol.Instance{protocol.HTTP, protocol.GRPC}
)

func TestMirroring(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
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
			for _, src := range ctx.Clusters() {
				for _, dst := range ctx.Clusters() {
					runMirrorTest(mirrorTestOptions{
						t:     t,
						ctx:   ctx,
						cases: cases,
						instances: [3]echo.Instance{
							apps.podA.GetOrFail(t, echo.InCluster(src)),
							apps.podB.GetOrFail(t, echo.InCluster(dst)),
							apps.podC.GetOrFail(t, echo.InCluster(dst)),
						},
					})
				}
			}
		})
}

// Tests mirroring to an external service. Uses same topology as the test above, a -> b -> c, with "c" being external.
//
// Since we don't want to rely on actual external websites, we simulate that by using a Sidecar to limit connectivity
// from "a" so that it cannot reach "c" directly, and we use a ServiceEntry to define our "external" website, which
// is static and points to the service "c" ip.

// Thus when "a" tries to mirror to the external service, it is actually connecting to "c" (which is not part of the
// mesh because of the Sidecar), then we can inspect "c" logs to verify the requests were properly mirrored.

const (
	fakeExternalURL = "external-website-url-just-for-testing.extension"
)

func TestMirroringExternalService(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			cases := []testCaseMirror{
				{
					name:       "mirror-external",
					absent:     true,
					percentage: 100.0,
					threshold:  0.0,
				},
			}

			for _, src := range ctx.Clusters() {
				for _, dst := range ctx.Clusters() {
					t.Run(fmt.Sprintf("%s->%s", src.Name(), dst.Name()), func(t *testing.T) {
						if src.NetworkName() != dst.NetworkName() {
							t.Skip("external serviceentry hack does not work with multi-network")
						}
						runMirrorTest(mirrorTestOptions{
							t:     t,
							ctx:   ctx,
							cases: cases,
							instances: [3]echo.Instance{
								apps.podA.GetOrFail(t, echo.InCluster(src)),
								apps.podB.GetOrFail(t, echo.InCluster(dst)),
								apps.podC.GetOrFail(t, echo.InCluster(dst)),
							},
							mirrorHost: fakeExternalURL,
							fnInjectConfig: func(ns namespace.Instance, ctx resource.Context, instances [3]echo.Instance) func() {
								cfg := ExternalServiceMirrorConfig{
									Namespace:     ns.Name(),
									Domain:        instances[1].Config().Domain,
									MirrorHost:    fakeExternalURL,
									MirrorAddress: instances[2].Address(),
									TargetHost:    instances[1].Config().Service,
								}
								deployment := tmpl.EvaluateOrFail(t,
									file.AsStringOrFail(t, "testdata/traffic-mirroring-external-template.yaml"), cfg)
								ctx.Config().ApplyYAMLOrFail(t, ns.Name(), deployment)
								if err := outboundtrafficpolicy.WaitUntilNotCallable(instances[0], instances[2]); err != nil {
									t.Fatalf("failed to apply sidecar, %v", err)
								}
								return func() {
									ctx.Config().DeleteYAMLOrFail(t, ns.Name(), deployment)
								}
							},
						})
					})
				}
			}

		})
}

func runMirrorTest(options mirrorTestOptions) {
	ctx := options.ctx
	instances := options.instances
	if options.fnInjectConfig != nil {
		cleanup := options.fnInjectConfig(apps.namespace, ctx, instances)
		defer cleanup()
	}

	for _, c := range options.cases {
		options.t.Run(c.name, func(t *testing.T) {
			mirrorHost := options.mirrorHost
			if len(mirrorHost) == 0 {
				mirrorHost = instances[2].Config().Service
			}
			vsc := VirtualServiceMirrorConfig{
				c.name,
				apps.namespace.Name(),
				c.absent,
				c.percentage,
				instances[1].Config().Service,
				mirrorHost,
			}

			deployment := tmpl.EvaluateOrFail(t,
				file.AsStringOrFail(t, "testdata/traffic-mirroring-template.yaml"), vsc)
			ctx.Config().ApplyYAMLOrFail(t, apps.namespace.Name(), deployment)
			defer ctx.Config().DeleteYAMLOrFail(t, apps.namespace.Name(), deployment)

			for _, proto := range mirrorProtocols {
				t.Run(string(proto), func(t *testing.T) {
					retry.UntilSuccessOrFail(t, func() error {
						testID := fmt.Sprintf("%s_%s", t.Name(), util.RandomString(8))
						if err := sendTrafficMirror(instances, proto, testID); err != nil {
							return err
						}

						if err := verifyTrafficMirror(instances, c, testID); err != nil {
							return err
						}
						return nil
					}, retry.Delay(time.Second))
				})
			}
		})
	}
}

func sendTrafficMirror(instances [3]echo.Instance, proto protocol.Instance, testID string) error {
	options := echo.CallOptions{
		Target:   instances[1],
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

	_, err := instances[0].Call(options)
	if err != nil {
		return err
	}

	return nil
}

func verifyTrafficMirror(instances [3]echo.Instance, tc testCaseMirror, testID string) error {
	countB, err := logCount(instances[1], testID)
	if err != nil {
		return err
	}

	countC, err := logCount(instances[2], testID)
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
