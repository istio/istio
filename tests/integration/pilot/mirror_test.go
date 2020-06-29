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
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/telemetry/outboundtrafficpolicy"
)

//	Virtual service topology
//                                  in each cluster
//                                /                  \
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
	MirrorHost string
	TargetHost string
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
	cases          []testCaseMirror
	mirrorHostFmt  string
	fnInjectConfig func(ns namespace.Instance, ctx resource.Context, mirrorHost string, client, target, mirrored echo.Instance) (cleanup func())
	skipCondition  func(src kube.Cluster, dest kube.Cluster) (string, bool)
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

// Tests mirroring to an external service. Uses same topology as the test above, a -> b -> c, with "c" being external.
//
// Since we don't want to rely on actual external websites, we simulate that by using a Sidecar to limit connectivity
// from "a" so that it cannot reach "c" directly, and we use a ServiceEntry to define our "external" website, which
// is static and points to the service "c" ip.

// Thus when "a" tries to mirror to the external service, it is actually connecting to "c" (which is not part of the
// mesh because of the Sidecar), then we can inspect "c" logs to verify the requests were properly mirrored.

const fakeExternalURL = "external-website-url-just-for-testing-%d.extension"

func TestMirroringExternalService(t *testing.T) {
	cases := []testCaseMirror{
		{
			name:       "mirror-external",
			absent:     true,
			percentage: 100.0,
			threshold:  0.0,
		},
	}

	runMirrorTest(mirrorTestOptions{
		t:             t,
		cases:         cases,
		mirrorHostFmt: fakeExternalURL,
		skipCondition: func(src, dest kube.Cluster) (string, bool) {
			// TODO make the fake "external website" via ServiceEntry work with the cross-network gateway
			return "external service via ServiceEntry doesn't work across networks", src.NetworkName() != dest.NetworkName()
		},
		fnInjectConfig: func(ns namespace.Instance, ctx resource.Context, mirrorHost string, client, target, mirrored echo.Instance) (cleanup func()) {
			cfg := ExternalServiceMirrorConfig{
				Namespace:     ns.Name(),
				Domain:        target.Config().Domain,
				MirrorHost:    mirrorHost,
				MirrorAddress: mirrored.Address(),
				TargetHost:    target.Config().Service,
			}
			deployment := tmpl.EvaluateOrFail(t,
				file.AsStringOrFail(t, "testdata/traffic-mirroring-external-serviceentry.yaml"), cfg)
			ctx.Config().ApplyYAMLOrFail(t, ns.Name(), deployment)
			if err := outboundtrafficpolicy.WaitUntilNotCallable(client, mirrored); err != nil {
				t.Fatalf("failed to apply sidecar, %v", err)
			}
			return func() {
				ctx.Config().DeleteYAMLOrFail(t, ns.Name(), deployment)
			}
		},
	})
}

func runMirrorTest(options mirrorTestOptions) {
	framework.
		NewTest(options.t).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(options.t, ctx, namespace.Config{
				Prefix: "mirroring",
				Inject: true,
			})

			builder := echoboot.NewBuilderOrFail(options.t, ctx)
			clusterInstances := make([][3]echo.Instance, len(ctx.Environment().Clusters()))
			for _, c := range ctx.Environment().Clusters() {
				clusterInstances[c.Index()] = [3]echo.Instance{}
				builder = builder.
					With(&clusterInstances[c.Index()][0], echoConfigForCluster(ns, fmt.Sprintf("a-%d", c.Index()), c)). // client
					With(&clusterInstances[c.Index()][1], echoConfigForCluster(ns, fmt.Sprintf("b-%d", c.Index()), c)). // target
					With(&clusterInstances[c.Index()][2], echoConfigForCluster(ns, fmt.Sprintf("c-%d", c.Index()), c))  // receives mirrored requests
			}
			builder.BuildOrFail(options.t)

			for _, srcCluster := range ctx.Environment().Clusters() {
				for _, dstCluster := range ctx.Environment().Clusters() {
					for _, c := range options.cases {
						options.t.Run(c.name, func(t *testing.T) {
							if options.skipCondition != nil {
								if msg, skip := options.skipCondition(srcCluster.(kube.Cluster), dstCluster.(kube.Cluster)); skip {
									t.Skip(msg)
								}
							}

							client := clusterInstances[srcCluster.Index()][0]
							target := clusterInstances[dstCluster.Index()][1]
							mirrored := clusterInstances[dstCluster.Index()][2]

							mirrorHost := mirrored.Config().Service
							if len(options.mirrorHostFmt) > 0 {
								mirrorHost = fmt.Sprintf(options.mirrorHostFmt, dstCluster.Index())
							}
							vsc := VirtualServiceMirrorConfig{
								Name:       c.name,
								Namespace:  ns.Name(),
								Absent:     c.absent,
								Percent:    c.percentage,
								MirrorHost: mirrorHost,
								TargetHost: clusterInstances[dstCluster.Index()][1].Config().Service,
							}

							if options.fnInjectConfig != nil {
								cleanup := options.fnInjectConfig(ns, ctx, mirrorHost, client, target, mirrored)
								defer cleanup()
							}

							deployment := tmpl.EvaluateOrFail(t,
								file.AsStringOrFail(t, "testdata/traffic-mirroring-template.yaml"), vsc)
							ctx.Config().ApplyYAMLOrFail(t, ns.Name(), deployment)
							defer ctx.Config().DeleteYAMLOrFail(t, ns.Name(), deployment)

							for _, proto := range mirrorProtocols {
								t.Run(fmt.Sprintf("%d->%d %s", srcCluster.Index(), dstCluster.Index(), proto), func(t *testing.T) {
									retry.UntilSuccessOrFail(t, func() error {
										testID := fmt.Sprintf("%s_%d_%d_%s_%s",
											options.t.Name(),
											srcCluster.Index(),
											dstCluster.Index(),
											proto,
											util.RandomString(8),
										)

										if err := sendTrafficMirror(client, target, proto, testID); err != nil {
											return err
										}

										if err := verifyTrafficMirror(target, mirrored, c, testID); err != nil {
											return err
										}
										return nil
									}, retry.Delay(time.Second))
								})
							}
						})
					}
				}
			}
		})
}

func sendTrafficMirror(client, target echo.Instance, proto protocol.Instance, testID string) error {
	options := echo.CallOptions{
		Target:   target,
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

	_, err := client.Call(options)
	if err != nil {
		return err
	}

	return nil
}

func verifyTrafficMirror(target, mirrored echo.Instance, tc testCaseMirror, testID string) error {
	countB, err := logCount(target, testID)
	if err != nil {
		return err
	}

	countC, err := logCount(mirrored, testID)
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
