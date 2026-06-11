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

package ambient

import (
	"fmt"
	"testing"
	"time"

	"istio.io/api/label"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/util/traffic"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	// How long we monitor traffic before the migration.
	baselineWindow = 5 * time.Second
	// How long we monitor traffic after the migration.
	stabilizationWindow = 10 * time.Second
)

// TestEastWestServerFirst verifies that migrating workloads from sidecar to ambient mode does not
// cause any packet loss when a server is migrated before the clients. This exercises the
// sidecar-client -> ambient-server mixed-mode path under continuous traffic during the client
// restart.
func TestEastWestServerFirst(t *testing.T) {
	runMigrationTest(t, runEastWestServerFirstMigration)
}

// runEastWestServerFirstMigration executes the sidecar-to-ambient east-west migration flow with
// two staggered traffic generators for continuous coverage. While one client restarts, the other
// generator keeps running to catch any Istio disruptions.
func runEastWestServerFirstMigration(ctx framework.TestContext, env *testEnv) {
	ctx.Log("Verifying sidecar connectivity")
	waitForConnectivity(
		ctx,
		env.client1,
		echo.CallOptions{To: env.server, Port: echo.Port{Name: "http"}},
		30*time.Second,
	)
	ctx.Log("Sidecar connectivity verified")

	ctx.Log("Starting baseline traffic from both clients")
	gen1 := traffic.NewGenerator(ctx, eastWestTraffic(env.client1, env.server)).Start()
	gen2 := traffic.NewGenerator(ctx, eastWestTraffic(env.client2, env.server)).Start()
	time.Sleep(baselineWindow)

	ctx.Log("Migrating namespace to ambient mode")
	migrateNSToAmbient(ctx, env.ns)

	restartOrFail(ctx, "server", env.server)

	// Stop generator1 before restarting client1 to avoid spurious errors unrelated to the Istio
	// data plane. generator2 keeps monitoring the data plane while client1 is restarting.
	ctx.Log("Stopping generator1")
	result1 := gen1.Stop()
	ctx.Logf("generator1 (baseline through server restart):\n%s", result1)
	result1.CheckSuccessRate(ctx, 1.0)

	restartOrFail(ctx, "client1", env.client1)

	// client1 finished migrating to ambient. Stop generator2 and check the result.
	ctx.Log("Stopping generator2")
	result2 := gen2.Stop()
	ctx.Logf("generator2 (baseline through server + client1 restart):\n%s", result2)
	result2.CheckSuccessRate(ctx, 1.0)

	ctx.Log("Starting generator1 from ambient client")
	gen1 = traffic.NewGenerator(ctx, eastWestTraffic(env.client1, env.server)).Start()

	restartOrFail(ctx, "client2", env.client2)

	// Start generator2 again. generator1 keeps running.
	ctx.Log("Starting generator2 from ambient client")
	gen2 = traffic.NewGenerator(ctx, eastWestTraffic(env.client2, env.server)).Start()

	ctx.Log("Running post-migration traffic from both clients")
	time.Sleep(stabilizationWindow)

	post1 := gen1.Stop()
	ctx.Logf("generator1 (client2 restart + post-migration):\n%s", post1)
	post1.CheckSuccessRate(ctx, 1.0)

	post2 := gen2.Stop()
	ctx.Logf("generator2 (post-migration):\n%s", post2)
	post2.CheckSuccessRate(ctx, 1.0)

	ctx.Log("Verifying ambient data path")
	verifyAmbient(ctx, env.client1, env.server)
	verifyAmbient(ctx, env.client2, env.server)
}

// TestEastWestClientFirst verifies that migrating workloads from sidecar to ambient mode does not
// cause any packet loss when a client is migrated before the server. This exercises the
// ambient-client -> sidecar-server mixed-mode path under continuous traffic during the server
// restart.
func TestEastWestClientFirst(t *testing.T) {
	runMigrationTest(t, runEastWestClientFirstMigration)
}

// runEastWestClientFirstMigration executes a client-first sidecar-to-ambient migration flow.
// Clients are restarted before the server so that during the server restart both generators are
// sending from ambient clients to a transitioning server, thus exercising the ambient-client ->
// sidecar-server mixed-mode path under continuous traffic.
func runEastWestClientFirstMigration(ctx framework.TestContext, env *testEnv) {
	ctx.Log("Verifying sidecar connectivity")
	waitForConnectivity(
		ctx,
		env.client1,
		echo.CallOptions{To: env.server, Port: echo.Port{Name: "http"}},
		30*time.Second,
	)
	ctx.Log("Sidecar connectivity verified")

	ctx.Log("Starting baseline traffic from both clients")
	gen1 := traffic.NewGenerator(ctx, eastWestTraffic(env.client1, env.server)).Start()
	gen2 := traffic.NewGenerator(ctx, eastWestTraffic(env.client2, env.server)).Start()
	time.Sleep(baselineWindow)

	ctx.Log("Migrating namespace to ambient mode")
	migrateNSToAmbient(ctx, env.ns)

	// Stop generator1 before restarting client1 to avoid spurious errors unrelated to the Istio
	// data plane. generator2 keeps monitoring the data plane while client1 is restarting.
	ctx.Log("Stopping generator1")
	result1 := gen1.Stop()
	ctx.Logf("generator1 (baseline):\n%s", result1)
	result1.CheckSuccessRate(ctx, 1.0)

	restartOrFail(ctx, "client1", env.client1)

	// client1 finished migrating to ambient. Stop generator2 and check the result.
	ctx.Log("Stopping generator2")
	result2 := gen2.Stop()
	ctx.Logf("generator2 (baseline through client1 restart):\n%s", result2)
	result2.CheckSuccessRate(ctx, 1.0)

	ctx.Log("Starting generator1 from ambient client")
	gen1 = traffic.NewGenerator(ctx, eastWestTraffic(env.client1, env.server)).Start()

	restartOrFail(ctx, "client2", env.client2)

	// client2 finished migrating to ambient. Stop generator1 and check the result.
	ctx.Log("Stopping generator1 after client2 restart")
	result1 = gen1.Stop()
	ctx.Logf("generator1 (ambient client1 -> sidecar server, client2 restart):\n%s", result1)
	result1.CheckSuccessRate(ctx, 1.0)

	ctx.Log("Starting both generators for server restart")
	gen1 = traffic.NewGenerator(ctx, eastWestTraffic(env.client1, env.server)).Start()
	gen2 = traffic.NewGenerator(ctx, eastWestTraffic(env.client2, env.server)).Start()

	restartOrFail(ctx, "server", env.server)

	ctx.Log("Running post-migration traffic from both clients")
	time.Sleep(stabilizationWindow)

	post1 := gen1.Stop()
	ctx.Logf("generator1 (server restart + post-migration):\n%s", post1)
	post1.CheckSuccessRate(ctx, 1.0)

	post2 := gen2.Stop()
	ctx.Logf("generator2 (server restart + post-migration):\n%s", post2)
	post2.CheckSuccessRate(ctx, 1.0)

	ctx.Log("Verifying ambient data path")
	verifyAmbient(ctx, env.client1, env.server)
	verifyAmbient(ctx, env.client2, env.server)
}

// eastWestTraffic returns a traffic.Config for east-west HTTP traffic from source to server.
func eastWestTraffic(source echo.Caller, server echo.Instance) traffic.Config {
	return migrationTrafficConfig(source, echo.CallOptions{
		To:   server,
		Port: echo.Port{Name: "http"},
	})
}

// verifyAmbient verifies traffic flows through ztunnel rather than sidecar.
func verifyAmbient(ctx framework.TestContext, source echo.Caller, server echo.Instance) {
	ctx.Helper()
	if _, err := source.Call(echo.CallOptions{
		To:    server,
		Port:  echo.Port{Name: "http"},
		Count: 1,
		Check: check.And(check.OK(), IsL4()),
	}); err != nil {
		ctx.Fatal(err)
	}
}

// TestNorthSouth verifies that migrating workloads from sidecar to ambient mode does not cause
// any packet loss for north-south traffic (ingress gateway -> server).
func TestNorthSouth(t *testing.T) {
	runMigrationTest(t, runNorthSouthMigration)
}

// ingressGatewayConfig creates an Istio Gateway and VirtualService to route ingress traffic
// through the default istio-ingressgateway to the backend service. The %s is the backend service
// host and %d is the backend service port.
const ingressGatewayConfig = `
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: server-gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts: ["*"]
---
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: server-vs
spec:
  gateways:
  - server-gateway
  hosts:
  - "*"
  http:
  - route:
    - destination:
        host: "%s"
        port:
          number: %d
`

// runNorthSouthMigration executes the sidecar-to-ambient migration flow with continuous
// north-south traffic (ingress gateway -> server) measuring disruption.
func runNorthSouthMigration(ctx framework.TestContext, env *testEnv) {
	const (
		preMigrationDuration  = 5 * time.Second
		postMigrationDuration = 10 * time.Second
	)

	// Apply Istio Gateway + VirtualService routing ingress traffic to the server.
	httpPort := env.server.Config().Ports.MustForName("http")
	gwCfg := fmt.Sprintf(ingressGatewayConfig, env.server.Config().Service, httpPort.ServicePort)
	ctx.ConfigIstio().YAML(env.ns.Name(), gwCfg).ApplyOrFail(ctx)

	ingress := istio.DefaultIngressOrFail(ctx, ctx)
	ingressOpts := echo.CallOptions{
		Port:   echo.Port{Protocol: protocol.HTTP, ServicePort: 80},
		Scheme: scheme.HTTP,
	}

	ctx.Log("Waiting for ingress connectivity to server")
	waitForConnectivity(ctx, ingress, ingressOpts, 2*time.Minute)
	ctx.Log("Ingress connectivity verified")

	ctx.Log("Starting north-south traffic generator")
	gen := traffic.NewGenerator(ctx, migrationTrafficConfig(ingress, ingressOpts)).Start()

	ctx.Log("Running baseline north-south traffic")
	time.Sleep(preMigrationDuration)

	ctx.Log("Migrating namespace to ambient mode")
	migrateNSToAmbient(ctx, env.ns)

	restartOrFail(ctx, "server", env.server)

	// The ingress gateway is external to the namespace — it keeps running so we can continue
	// measuring north-south traffic through the entire migration window.
	ctx.Log("Waiting for north-south ambient connectivity")
	waitForConnectivity(ctx, ingress, ingressOpts, 5*time.Minute)
	ctx.Log("North-south ambient connectivity verified")

	ctx.Log("Running post-migration north-south traffic")
	time.Sleep(postMigrationDuration)
	result := gen.Stop()
	ctx.Logf("north-south generator (full migration):\n%s", result)
	result.CheckSuccessRate(ctx, 1.0)
}

// testEnv holds per-test isolated namespaces and echo deployments, preventing cross-test
// interference and enabling parallel execution.
type testEnv struct {
	ns      namespace.Instance
	client1 echo.Instance
	client2 echo.Instance
	server  echo.Instance
}

// newTestEnv creates an isolated namespace and echo deployments for a single test. Resources are
// automatically cleaned up when the test context completes.
func newTestEnv(ctx framework.TestContext) *testEnv {
	ctx.Helper()

	env := &testEnv{}
	var err error
	env.ns, err = namespace.New(ctx, namespace.Config{
		Prefix: "sidecar-to-ambient",
		Inject: true,
	})
	if err != nil {
		ctx.Fatal(err)
	}

	serverCfg := echo.Config{
		Service:   "server",
		Namespace: env.ns,
		Ports: []echo.Port{
			{
				Name:         "http",
				Protocol:     protocol.HTTP,
				WorkloadPort: 8090,
			},
		},
	}

	builder := deployment.New(ctx).
		With(&env.client1, echo.Config{
			Service:   "client1",
			Namespace: env.ns,
			Ports:     []echo.Port{},
		}).
		With(&env.client2, echo.Config{
			Service:   "client2",
			Namespace: env.ns,
			Ports:     []echo.Port{},
		}).
		With(&env.server, serverCfg)

	if _, err := builder.Build(); err != nil {
		ctx.Fatal(err)
	}

	return env
}

// runMigrationTest runs a migration scenario in both permissive and strict-mtls PeerAuthentication
// modes. Permissive mode may briefly allow plain-text traffic during the ambient transition;
// strict mode disallows plain-text entirely and may surface failures the permissive run hides.
func runMigrationTest(t *testing.T, run func(framework.TestContext, *testEnv)) {
	framework.NewTest(t).Run(func(ctx framework.TestContext) {
		if ctx.Settings().AmbientMultiNetwork {
			t.Skip("skipping cross-cluster test")
		}
		ctx.NewSubTest("permissive").Run(func(ctx framework.TestContext) {
			run(ctx, newTestEnv(ctx))
		})
		ctx.NewSubTest("strict-mtls").Run(func(ctx framework.TestContext) {
			env := newTestEnv(ctx)
			applyStrictPeerAuth(ctx, env)
			run(ctx, env)
		})
	})
}

// migrationTrafficConfig wraps opts with the migration-test timing defaults: short per-request
// timeout (so a hung connection during a pod restart is counted as a failure quickly rather than
// blocking the generator), no retries (so the generator goroutine is never blocked waiting for
// retries), 500ms interval, and a 10s stop timeout that exceeds the per-request timeout so the
// last in-flight call can finish before Stop() gives up.
func migrationTrafficConfig(source echo.Caller, opts echo.CallOptions) traffic.Config {
	opts.Count = 1
	opts.Timeout = 3 * time.Second
	opts.Retry = echo.Retry{NoRetry: true}

	return traffic.Config{
		Source:      source,
		Options:     opts,
		Interval:    500 * time.Millisecond,
		StopTimeout: 10 * time.Second,
	}
}

const peerAuthenticationStrict = `
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: strict-mtls
spec:
  mtls:
    mode: STRICT
`

// applyStrictPeerAuth applies a strict PeerAuthentication to env.ns and waits until
// requests from client1 to the server succeed under it.
func applyStrictPeerAuth(ctx framework.TestContext, env *testEnv) {
	ctx.ConfigIstio().YAML(env.ns.Name(), peerAuthenticationStrict).ApplyOrFail(ctx)

	ctx.Log("Waiting for strict PeerAuthentication to propagate")
	waitForConnectivity(
		ctx,
		env.client1,
		echo.CallOptions{To: env.server, Port: echo.Port{Name: "http"}},
		30*time.Second,
	)
	ctx.Log("Strict PeerAuthentication active")
}

// waitForConnectivity polls caller -> dest with single OK-checked calls until one succeeds or the
// timeout elapses. Only the destination fields of dest (To, Port, Scheme, ...) are honored; Count,
// Retry and Check are set by this helper.
func waitForConnectivity(
	ctx framework.TestContext,
	caller echo.Caller,
	dest echo.CallOptions,
	timeout time.Duration,
) {
	dest.Count = 1
	dest.Retry = echo.Retry{NoRetry: true}
	dest.Check = check.OK()

	retry.UntilSuccessOrFail(ctx, func() error {
		_, err := caller.Call(dest)
		return err
	}, retry.Timeout(timeout), retry.Delay(time.Second))
}

// restartOrFail restarts inst and verifies that the new pods have no sidecar proxy container,
// confirming the workload migrated to ambient mode. The name arg is used in log messages.
func restartOrFail(ctx framework.TestContext, name string, inst echo.Instance) {
	ctx.Helper()
	ctx.Logf("Restarting %s", name)
	if err := inst.Restart(); err != nil {
		ctx.Fatalf("failed to restart %s: %v", name, err)
	}

	for _, wl := range inst.WorkloadsOrFail(ctx) {
		if wl.Sidecar() != nil {
			ctx.Fatalf("%s workload %s still has a sidecar after ambient migration", name, wl.PodName())
		}
	}
}

// migrateNSToAmbient switches the given namespace from sidecar injection to ambient dataplane mode
// (labels only). The caller is responsible for restarting whichever workloads need the change.
func migrateNSToAmbient(ctx framework.TestContext, targetNS namespace.Instance) {
	ctx.Helper()
	if err := targetNS.RemoveLabel("istio-injection"); err != nil {
		ctx.Fatalf("failed to remove sidecar injection label: %v", err)
	}
	if err := targetNS.SetLabel(label.IoIstioDataplaneMode.Name, "ambient"); err != nil {
		ctx.Fatalf("failed to set ambient dataplane mode: %v", err)
	}
}
