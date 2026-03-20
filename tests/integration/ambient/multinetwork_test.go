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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/label"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/ambient"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
)

func TestMultinetworkFailover(t *testing.T) {
	const brokenService = "broken"

	runTest := func(t framework.TestContext, healthy, unhealthy cluster.Cluster, ns namespace.Instance) {
		clients := deployEchoOrFail(t, brokenService, healthy, unhealthy, ns)
		for _, src := range clients {
			// This service is partially broken, but because we can failover to remote network the request
			// should still succeed.
			src.CallOrFail(t, echo.CallOptions{
				Address: fmt.Sprintf("%s.%s.svc.cluster.local", brokenService, ns.Name()),
				Port:    ports.HTTP,
				Scheme:  scheme.HTTP,
				Check:   check.OK(),
			})
		}
	}
	framework.NewTest(t).Run(func(t framework.TestContext) {
		t.NewSubTest("without-waypoint").Run(func(t framework.TestContext) {
			if !t.Settings().Ambient || !t.Settings().AmbientMultiNetwork {
				t.Skip("this test is ambient multi-network specific")
			}

			if len(t.Clusters()) < 2 {
				t.Fatal("ambient multi-network failover test requires at least 2 clusters")
			}

			allClusters := t.Clusters()
			local := allClusters[0]
			remote := allClusters[1]

			nsConfig := namespace.NewOrFail(t, namespace.Config{
				Prefix: "without-waypoint",
				Inject: false,
				Labels: map[string]string{
					label.IoIstioDataplaneMode.Name: "ambient",
				},
			})

			runTest(t, local, remote, nsConfig)
		})
		t.NewSubTest("with-waypoints").Run(func(t framework.TestContext) {
			if !t.Settings().Ambient || !t.Settings().AmbientMultiNetwork {
				t.Skip("this test is ambient multi-network specific")
			}

			if len(t.Clusters()) < 2 {
				t.Fatal("ambient multi-network failover test requires at least 2 clusters")
			}

			allClusters := t.Clusters()
			local := allClusters[0]
			remote := allClusters[1]

			nsConfig := namespace.NewOrFail(t, namespace.Config{
				Prefix: "with-waypoint",
				Inject: false,
				Labels: map[string]string{
					label.IoIstioDataplaneMode.Name: "ambient",
				},
			})

			waypointName := "waypoint"
			deployWaypointsOrFail(t, local, waypointName, nsConfig)

			runTest(t, local, remote, nsConfig)
		})
	})
}

// deployEchoOrFail deploys global (a.k.a. multi-network) Echo servers and local clients in provided clusters.
// One of the clusters is designated as unhealthy and one as healthy.
// This function will deploy an "unhealthy" version of the service in the unhealthy cluster and a "healthy" version in
// the healthy cluster.
func deployEchoOrFail(t framework.TestContext, serviceName string, healthy, unhealthy cluster.Cluster, ns namespace.Instance) echo.Instances {
	t.Helper()

	broken := serviceName
	client := "client"

	builder := deployment.New(t).
		WithConfig(echo.Config{
			Service:   broken,
			Namespace: ns,
			Cluster:   unhealthy,
			Ports:     ports.All(),
			ServiceLabels: map[string]string{
				"istio.io/global": "true",
			},
			Subsets: []echo.SubsetConfig{
				{
					Version: broken,
				},
			},
		}).
		WithConfig(echo.Config{
			Service:   broken,
			Namespace: ns,
			Cluster:   healthy,
			Ports:     ports.All(),
			ServiceLabels: map[string]string{
				"istio.io/global": "true",
			},
			Subsets: []echo.SubsetConfig{
				{
					Version: broken,
				},
			},
		}).
		// This is the client, we deploy it in all clusters and use clients in different clusters to validate
		// different traffic paths.
		WithConfig(echo.Config{
			Service:   client,
			Namespace: ns,
			Cluster:   unhealthy,
			Ports:     ports.All(),
			Subsets: []echo.SubsetConfig{
				{
					Version: client,
				},
			},
		}).
		WithConfig(echo.Config{
			Service:   client,
			Namespace: ns,
			Cluster:   healthy,
			Ports:     ports.All(),
			Subsets: []echo.SubsetConfig{
				{
					Version: client,
				},
			},
		})

	echos := builder.BuildOrFail(t)

	// NOTE: We cannot just specify replicas 0, because the way the Deployment config template is written
	// it treats 0 as unset value and defaults to 1 replica in that case defeating the point of setting
	// replicas to 0 explicitly.
	scaleDeploymentOrFail(t, unhealthy, ns.Name(), fmt.Sprintf("%s-%s", broken, broken), 0)

	return match.ServiceName(echo.NamespacedName{Name: client, Namespace: ns}).GetMatches(echos)
}

// deployWaypointsOrFail deploys global (a.k.a. multi-network) waypoints in two clusters.
// One of the clusters designated as unhealthy the deployed waypoint will be unhealthy there.
// The other cluster is designated as healthy and deployed waypoint will be healthy there.
func deployWaypointsOrFail(t framework.TestContext, unhealthy cluster.Cluster, waypoint string, ns namespace.Instance) {
	t.Helper()

	_ = ambient.NewWaypointProxyOrFail(t, ns, waypoint)
	ambient.SetWaypointForNamespace(t, ns, waypoint)
	labelService(t, ns.Name(), waypoint, "istio.io/global", "true", t.AllClusters()...)

	scaleDeploymentOrFail(t, unhealthy, ns.Name(), waypoint, 0)
}

func scaleDeploymentOrFail(t framework.TestContext, c cluster.Cluster, namespace, name string, scale int32) {
	t.Helper()

	s, err := c.Kube().AppsV1().Deployments(namespace).GetScale(t.Context(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to find deployment %s in namespace %s: %w", name, namespace, err)
	}

	s.Spec.Replicas = scale
	_, err = c.Kube().AppsV1().Deployments(namespace).UpdateScale(t.Context(), name, s, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("failed to scale deployment %s in namespace %s to %d replicas: %w", name, namespace, scale, err)
	}

	retry.UntilSuccessOrFail(t, func() error {
		s, err := c.Kube().AppsV1().Deployments(namespace).GetScale(t.Context(), name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to find deployment %s in namespace %s: %w", name, namespace, err)
		}
		pods, err := c.PodsForSelector(t.Context(), namespace, s.Status.Selector)
		if err != nil {
			return fmt.Errorf("failed to query pods matching selector %s in namespace %s: %w", s.Status.Selector, namespace, err)
		}
		if s.Status.Replicas == scale && len(pods.Items) == int(scale) {
			return nil
		}
		return fmt.Errorf("deployment still has different number of pods, want %d, got %d", scale, len(pods.Items))
	})
}

// TestEastWestGatewayTLSRoute checks if we can expose passthrough ports on E/W gateways
// and use TLSRoutes so internal services that don't belong to the mesh can be reached from
// outside the cluster. A use case for this is when users want to expose the Kubenertes API
// service through their E/W gateway.
func TestEastWestGatewayTLSRoute(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		if !t.Settings().Ambient || !t.Settings().AmbientMultiNetwork {
			t.Skip("this test is ambient multi-network specific")
		}
		if len(t.Clusters()) < 2 {
			t.Fatal("east-west gateway TLSRoute test requires at least 2 clusters")
		}

		crd.DeployGatewayAPIOrSkip(t)

		allClusters := t.Clusters()
		// gwCluster hosts the E/W gateway + backend; remoteCluster is the client.
		gwCluster := allClusters[0]
		remoteCluster := allClusters[1]

		nsConfig := namespace.NewOrFail(t, namespace.Config{
			Prefix: "ew-tlsroute",
			Inject: false,
		})

		const backendSvc = "tlsroute-backend"
		const clientSvc = "tlsroute-client"
		// Note that for legacy reasons we can't use
		// port 15443. If port 15443 is used, pilot
		// automagically converts the TLS mode to AUTO_PASSTHROUGH.
		const tlsPassthroughPort = 16443

		echos := deployment.New(t).
			WithConfig(echo.Config{
				Service:   backendSvc,
				Namespace: nsConfig,
				Cluster:   gwCluster,
				Ports: echo.Ports{
					{Name: "tls", Protocol: protocol.HTTPS, ServicePort: tlsPassthroughPort, TLS: true},
				},
				ServiceLabels: map[string]string{
					"istio.io/global": "true",
				},
			}).
			WithConfig(echo.Config{
				Service:   clientSvc,
				Namespace: nsConfig,
				Cluster:   remoteCluster,
				Ports: echo.Ports{
					{Name: "tls", Protocol: protocol.HTTPS, ServicePort: tlsPassthroughPort},
				},
			}).
			BuildOrFail(t)

		backends := match.ServiceName(echo.NamespacedName{Name: backendSvc, Namespace: nsConfig}).GetMatches(echos)
		clients := match.ServiceName(echo.NamespacedName{Name: clientSvc, Namespace: nsConfig}).GetMatches(echos)

		if len(backends) == 0 {
			t.Fatal("no backend instances found")
		}
		if len(clients) == 0 {
			t.Fatal("no client instances found")
		}

		systemNS := i.Settings().SystemNamespace

		// Deploy a (second) E/W gateway with a TLS passthrough listener
		// and a TLSRoute to the backend service.
		templateArgs := map[string]string{
			"GatewayNetwork": gwCluster.NetworkName(),
			"BackendSvc":     backendSvc,
			"BackendNS":      nsConfig.Name(),
			"TLSPort":        fmt.Sprintf("%d", tlsPassthroughPort),
			"SystemNS":       systemNS,
		}
		t.ConfigKube(gwCluster).
			Eval(systemNS, templateArgs, `
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: eastwest-tlsroute-test
  labels:
    topology.istio.io/network: "{{ .GatewayNetwork }}"
spec:
  gatewayClassName: istio-east-west
  listeners:
  - name: tls-passthrough
    port: {{ .TLSPort }}
    protocol: TLS
    tls:
      mode: Passthrough
---
apiVersion: gateway.networking.k8s.io/v1
kind: TLSRoute
metadata:
  name: eastwest-tlsroute-test
spec:
  parentRefs:
  - name: eastwest-tlsroute-test
    kind: Gateway
    sectionName: tls-passthrough
  hostnames:
  - "{{ .BackendSvc }}.{{ .BackendNS }}.svc.cluster.local"
  rules:
  - backendRefs:
    - name: {{ .BackendSvc }}
      namespace: {{ .BackendNS }}
      port: {{ .TLSPort }}
`).
			Eval(nsConfig.Name(), templateArgs, `
apiVersion: gateway.networking.k8s.io/v1beta1
kind: ReferenceGrant
metadata:
  name: eastwest-tlsroute-test
  namespace: {{ .BackendNS }}
spec:
  from:
  - group: gateway.networking.k8s.io
    kind: TLSRoute
    namespace: {{ .SystemNS }}
  to:
  - group: ""
    kind: Service
`).ApplyOrFail(t)

		// Wait for the gateway to be programmed.
		retry.UntilSuccessOrFail(t, func() error {
			gw, err := gwCluster.GatewayAPI().GatewayV1().Gateways(systemNS).
				Get(t.Context(), "eastwest-tlsroute-test", metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("gateway not found: %v", err)
			}
			for _, cond := range gw.Status.Conditions {
				if cond.Type == string(k8sv1.GatewayConditionProgrammed) && string(cond.Status) == "True" {
					return nil
				}
			}
			return fmt.Errorf("gateway eastwest-tlsroute-test is not yet programmed")
		}, retry.Timeout(2*time.Minute), retry.Delay(time.Second))

		// Resolve the gateway's external address for the TLS passthrough port.
		ewgw := i.CustomIngressFor(gwCluster,
			types.NamespacedName{Name: "eastwest-tlsroute-test", Namespace: systemNS},
			"gateway.istio.io/managed="+constants.ManagedGatewayEastWestControllerLabel)

		addrs, portList := ewgw.AddressesForPort(tlsPassthroughPort)
		if len(addrs) == 0 || len(portList) == 0 {
			t.Fatal("east-west gateway TLS passthrough address or port not found")
		}

		sni := fmt.Sprintf("%s.%s.svc.cluster.local", backendSvc, nsConfig.Name())

		// From the remote cluster, call the TLS passthrough listener on the E/W gateway.
		// The TLSRoute should route based on SNI to the backend service.
		clients[0].CallOrFail(t, echo.CallOptions{
			Address: addrs[0],
			Port: echo.Port{
				ServicePort: portList[0],
				Protocol:    protocol.HTTPS,
			},
			Scheme: scheme.HTTPS,
			TLS: echo.TLS{
				InsecureSkipVerify: true,
				ServerName:         sni,
			},
			Count: 1,
			Check: check.OK(),
			Retry: echo.Retry{
				Options: []retry.Option{retry.Timeout(2 * time.Minute), retry.Delay(time.Second)},
			},
		})
	})
}
