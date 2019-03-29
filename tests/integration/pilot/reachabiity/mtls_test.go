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

// Verify reachability under different authN scenario.
package reachability

import (
	"fmt"
	"path"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/apps"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	pilot2 "istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/util/retry"
)

type testPolicy struct {
	t         *testing.T
	env       *kube.Environment
	namespace string
	name      string
}

func (p testPolicy) TearDown() {
	p.t.Logf("Tearing down policy %q.", p.name)
	if err := p.env.Delete(p.namespace, path.Join("testdata", p.name)); err != nil {
		p.t.Fatalf("Cannot delete %q from namespace %q: %v", p.name, p.namespace, err)
	}
}

func setupPolicy(t *testing.T, env *kube.Environment, namespace string, name string) *testPolicy {
	t.Logf("Applying policies from %q.", name)
	if err := env.Apply(namespace, path.Join("testdata", name)); err != nil {
		t.Fatalf("Cannot apply %q to namespace %q: %v", name, namespace, err)
		return nil
	}
	return &testPolicy{
		t:         t,
		env:       env,
		namespace: namespace,
		name:      name,
	}
}

type connection struct {
	from            apps.KubeApp
	to              apps.KubeApp
	protocol        apps.AppProtocol
	port            int
	expectedSuccess bool
}

func checkConnection(conn connection) error {
	ep := conn.to.EndpointForPort(conn.port)
	if ep == nil {
		return fmt.Errorf("cannot get upstream endpoint for connection test %v", conn)
	}

	results, err := conn.from.Call(ep, apps.AppCallOptions{Protocol: conn.protocol})
	if conn.expectedSuccess {
		if err != nil || len(results) == 0 || results[0].Code != "200" {
			// Addition log for debugging purpose.
			if err != nil {
				fmt.Printf("Error: %#v\n", err)
			} else if len(results) == 0 {
				fmt.Printf("No result\n")
			} else {
				fmt.Printf("Result: %v\n", results[0])
			}
			return fmt.Errorf("%s to %s:%d using %s: expected success, actually failed",
				conn.from.Name(), conn.to.Name(), conn.port, conn.protocol)
		}
	} else {
		if err == nil && len(results) > 0 && results[0].Code == "200" {
			return fmt.Errorf("%s to %s:%d using %s: expected failed, actually success",
				conn.from.Name(), conn.to.Name(), conn.port, conn.protocol)
		}
	}
	return nil
}

func TestMutualTlsReachability(t *testing.T) {
	ctx := framework.NewContext(t)
	defer ctx.Done(t)
	ctx.RequireOrSkip(t, environment.Kube)

	istioCfg, err := istio.DefaultConfig(ctx)
	if err != nil {
		t.Fatalf("Get istio config: %v", err)
	}

	env := ctx.Environment().(*kube.Environment)

	pilot := pilot2.NewOrFail(t, ctx, pilot2.Config{})
	appsInstance := apps.NewOrFail(ctx, t, apps.Config{Pilot: pilot})

	aApp, _ := appsInstance.GetAppOrFail("a", t).(apps.KubeApp)
	bApp, _ := appsInstance.GetAppOrFail("b", t).(apps.KubeApp)

	headlessApp, _ := appsInstance.GetAppOrFail("headless", t).(apps.KubeApp)
	// App without sidecar.
	nakedApp, _ := appsInstance.GetAppOrFail("t", t).(apps.KubeApp)

	testCases := []struct {
		configFile  string
		namespace   string
		connections []connection
	}{
		{
			configFile: "global-mtls-on.yaml",
			connections: []connection{
				{
					from:            aApp,
					to:              bApp,
					port:            80,
					protocol:        apps.AppProtocolHTTP,
					expectedSuccess: true,
				},
				{
					from:            aApp,
					to:              headlessApp,
					port:            80,
					protocol:        apps.AppProtocolHTTP,
					expectedSuccess: true,
				},
				{
					from:            nakedApp,
					to:              bApp,
					port:            80,
					protocol:        apps.AppProtocolHTTP,
					expectedSuccess: false,
				},
			},
		},
		{
			configFile: "global-mtls-permissive.yaml",
			connections: []connection{
				{
					from:            aApp,
					to:              bApp,
					port:            80,
					protocol:        apps.AppProtocolHTTP,
					expectedSuccess: true,
				},
				{
					from:            nakedApp,
					to:              bApp,
					port:            80,
					protocol:        apps.AppProtocolHTTP,
					expectedSuccess: true,
				},
			},
		},
		{
			configFile: "global-mtls-off.yaml",
			connections: []connection{
				{
					from:            aApp,
					to:              bApp,
					port:            80,
					protocol:        apps.AppProtocolHTTP,
					expectedSuccess: true,
				},
				{
					from:            nakedApp,
					to:              bApp,
					port:            80,
					protocol:        apps.AppProtocolHTTP,
					expectedSuccess: true,
				},
			},
		},
		{
			configFile: "single-port-mtls-on.yaml",
			namespace:  appsInstance.Namespace().Name(),
			connections: []connection{
				{
					from:            aApp,
					to:              bApp,
					port:            80,
					protocol:        apps.AppProtocolHTTP,
					expectedSuccess: false,
				},
				{
					from:            nakedApp,
					to:              bApp,
					port:            80,
					protocol:        apps.AppProtocolHTTP,
					expectedSuccess: false,
				},
				{
					from:            aApp,
					to:              bApp,
					port:            90,
					protocol:        apps.AppProtocolHTTP,
					expectedSuccess: true,
				},
				{
					from:            nakedApp,
					to:              bApp,
					port:            90,
					protocol:        apps.AppProtocolHTTP,
					expectedSuccess: true,
				},
			},
		},
	}

	for _, c := range testCases {
		namespace := c.namespace
		if len(namespace) == 0 {
			namespace = istioCfg.SystemNamespace
		}

		policy := setupPolicy(t, env, namespace, c.configFile)
		// Give some time for the policy propagate.
		// TODO: query pilot or app to know instead of sleep.
		time.Sleep(time.Second)
		for _, conn := range c.connections {
			retry.UntilSuccessOrFail(t, func() error {
				return checkConnection(conn)
			}, retry.Delay(time.Second), retry.Timeout(10*time.Second))
		}
		policy.TearDown()
	}
}

// TestAuthentictionPermissiveE2E these cases are covered end to end
// app A to app B using plaintext (mTLS),
// app A to app B using HTTPS (mTLS),
// app A to app B using plaintext (legacy),
// app A to app B using HTTPS (legacy).
// explained: app-to-app-protocol(sidecar-to-sidecar-protocol). "legacy" means
// no client sidecar, unable to send "istio" alpn indicator.
// TODO(incfly): implement this
// func TestAuthentictionPermissiveE2E(t *testing.T) {
// Steps:
// Configure authn policy.
// Wait for config propagation.
// Send HTTP requests between apps.
// }
