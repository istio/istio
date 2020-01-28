// Copyright 2019 Istio Authors
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
	"sync"
	"testing"
	"time"

	"istio.io/pkg/log"

	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/util"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/structpath"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/pilot/outboundtrafficpolicy"
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
	MirrorHost string
}

type testCaseMirror struct {
	name       string
	absent     bool
	percentage float64
	threshold  float64
}

type mirrorTestOptions struct {
	t                 *testing.T
	cases             []testCaseMirror
	mirrorHost        string
	mirrorClusterName string
	fnInjectConfig    func(ns namespace.Instance, instances [3]echo.Instance)
}

var (
	mirrorProtocols = []protocol.Instance{protocol.HTTP, protocol.GRPC}
	totalAttempts   = 3
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
			name:       "mirror-80",
			percentage: 80.0,
			threshold:  10.0,
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

const (
	fakeExternalURL = "external-website-url-just-for-testing.extension"

	serviceEntry = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: external-service
spec:
  hosts:
  - %s
  location: MESH_EXTERNAL
  ports:
  - name: http
    number: 80
    protocol: HTTP
  - name: grpc
    number: 7070
    protocol: GRPC
  resolution: STATIC
  endpoints:
  - address: %s
`

	sidecar = `
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: restrict-to-service-entry
spec:
  egress:
  - hosts:
    - "%s/*"
    - "./b.%s.svc.%s"
    - "*/%s"
  outboundTrafficPolicy:
    mode: REGISTRY_ONLY
`
)

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
		t:                 t,
		cases:             cases,
		mirrorHost:        fakeExternalURL,
		mirrorClusterName: fakeExternalURL,
		fnInjectConfig: func(ns namespace.Instance, instances [3]echo.Instance) {
			g.ApplyConfigOrFail(t, ns, fmt.Sprintf(sidecar, i.Settings().ConfigNamespace, ns.Name(),
				instances[1].Config().Domain, fakeExternalURL))
			g.ApplyConfigOrFail(t, ns, fmt.Sprintf(serviceEntry, fakeExternalURL, instances[2].Address()))
			if err := outboundtrafficpolicy.WaitUntilNotCallable(instances[0], instances[2]); err != nil {
				t.Fatalf("failed to apply sidecar, %v", err)
			}
		},
	})
}

func runMirrorTest(options mirrorTestOptions) {
	framework.
		NewTest(options.t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(options.t, ctx, namespace.Config{
				Prefix: "mirroring",
				Inject: true,
			})

			var instances [3]echo.Instance
			echoboot.NewBuilderOrFail(options.t, ctx).
				With(&instances[0], echoConfig(ns, "a")). // client
				With(&instances[1], echoConfig(ns, "b")). // target
				With(&instances[2], echoConfig(ns, "c")). // receives mirrored requests
				BuildOrFail(options.t)

			if options.fnInjectConfig != nil {
				options.fnInjectConfig(ns, instances)
			}

			for _, c := range options.cases {
				options.t.Run(c.name, func(t *testing.T) {
					mirrorHost := options.mirrorHost
					if len(mirrorHost) == 0 {
						mirrorHost = instances[2].Config().Service
					}
					vsc := VirtualServiceMirrorConfig{
						c.name,
						ns.Name(),
						c.absent,
						c.percentage,
						mirrorHost,
					}

					deployment := tmpl.EvaluateOrFail(t,
						file.AsStringOrFail(t, "testdata/traffic-mirroring-template.yaml"), vsc)
					g.ApplyConfigOrFail(t, ns, deployment)
					defer g.DeleteConfigOrFail(t, ns, deployment)

					workloads, err := instances[0].Workloads()
					if err != nil {
						t.Fatalf("Failed to get workloads. Error: %v", err)
					}
					mirrorClusterName := options.mirrorClusterName
					if len(mirrorClusterName) == 0 {
						mirrorClusterName = fmt.Sprintf("%s.%s.svc.%s", instances[2].Config().Service, instances[2].Config().Namespace.Name(), instances[2].Config().Domain)
					}

					for _, w := range workloads {
						if err = w.Sidecar().WaitForConfig(func(cfg *envoyAdmin.ConfigDump) (bool, error) {
							validator := structpath.ForProto(cfg)
							if err = checkIfMirrorWasApplied(instances[1], mirrorClusterName, c, validator); err != nil {
								return false, err
							}
							return true, nil
						}); err != nil {
							t.Fatalf("Failed to apply configuration. Error: %v", err)
						}
					}

					for _, proto := range mirrorProtocols {
						t.Run(string(proto), func(t *testing.T) {
							var err error
							for i := 1; i <= totalAttempts; i++ {
								testID := fmt.Sprintf("%d-%s", i, util.RandomString(16))
								err = sendTrafficMirror(instances, proto, testID)
								if err != nil {
									log.Errorf("Attempt %d/%d failed: %v", i, totalAttempts, err)
									continue
								}

								err = verifyTrafficMirror(instances, c, testID)
								if err != nil {
									log.Errorf("Attempt %d/%d failed: %v", i, totalAttempts, err)
									continue
								}

								break
							}
							if err != nil {
								log.Errorf("Too many attempts. Test failed")
								t.Fatal(err)
							}
						})
					}
				})
			}
		})
}

func checkIfMirrorWasApplied(target echo.Instance, mirrorClusterName string, tc testCaseMirror, validator *structpath.Instance) error {
	for _, port := range target.Config().Ports {
		vsName := vsName(target, port)
		instance := validator.Select(
			"{.configs[*].dynamicRouteConfigs[*].routeConfig.virtualHosts[?(@.name == %q)].routes[*].route}",
			vsName)

		if tc.percentage > 0 {
			instance.Exists("{.requestMirrorPolicy}")

			clusterName := fmt.Sprintf("outbound|%d||%s", port.ServicePort, mirrorClusterName)
			instance.Equals(clusterName, "{.requestMirrorPolicy.cluster}")

			instance.Equals(tc.percentage, "{.requestMirrorPolicy.runtimeFraction.defaultValue.numerator}")
		} else {
			instance.NotExists("{.requestMirrorPolicy}")
		}

		if err := instance.Check(); err != nil {
			return err
		}
	}
	return nil
}

func vsName(target echo.Instance, port echo.Port) string {
	cfg := target.Config()
	return fmt.Sprintf("%s.%s.svc.%s:%d", cfg.Service, cfg.Namespace.Name(), cfg.Domain, port.ServicePort)
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
	const totalThreads = 10
	errs := make(chan error, totalThreads)

	wg := sync.WaitGroup{}
	wg.Add(totalThreads)

	for i := 0; i < totalThreads; i++ {
		go func() {
			_, err := instances[0].Call(options)
			if err != nil {
				errs <- err
			}
			wg.Done()
		}()
	}

	wg.Wait()
	close(errs)

	var callerrors error
	for err := range errs {
		callerrors = multierror.Append(err, callerrors)
	}
	if callerrors != nil {
		return fmt.Errorf("error occurred during call: %v", callerrors)
	}

	return nil
}

func verifyTrafficMirror(instances [3]echo.Instance, tc testCaseMirror, testID string) error {
	_, err := retry.Do(func() (interface{}, bool, error) {
		countB, err := logCount(instances[1], testID)
		if err != nil {
			return nil, false, err
		}

		countC, err := logCount(instances[2], testID)
		if err != nil {
			return nil, false, err
		}

		actualPercent := (countC / countB) * 100
		deltaFromExpected := math.Abs(actualPercent - tc.percentage)

		if tc.threshold-deltaFromExpected < 0 {
			err := fmt.Errorf("unexpected mirror traffic. Expected %g%%, got %.1f%% (threshold: %g%%, testID: %s)",
				tc.percentage, actualPercent, tc.threshold, testID)
			log.Infof("%v", err)
			return nil, false, err
		}

		log.Infof("Got expected mirror traffic. Expected %g%%, got %.1f%% (threshold: %g%%, , testID: %s)",
			tc.percentage, actualPercent, tc.threshold, testID)
		return nil, true, nil
	}, retry.Delay(time.Second))

	return err
}

func logCount(instance echo.Instance, testID string) (float64, error) {
	workloads, err := instance.Workloads()
	if err != nil {
		return -1, fmt.Errorf("failed to get workloads: %v", err)
	}

	var logs string
	for _, w := range workloads {
		log, err := w.Logs()
		if err != nil {
			return -1, err
		}
		logs += log
	}

	return float64(strings.Count(logs, testID)), nil
}
