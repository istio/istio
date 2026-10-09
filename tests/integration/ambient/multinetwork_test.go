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

	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/ambient"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
)

type workload struct {
	serviceName   string
	cluster       cluster.Cluster
	namespace     namespace.Instance
	replicas      int32
	serviceLabels map[string]string
}

func TestMultinetworkFailover(t *testing.T) {
	const brokenService1 = "broken1"
	const brokenService2 = "broken2"
	const clientService = "client"

	runTest := func(t framework.TestContext, healthy, unhealthy cluster.Cluster, ns1, ns2 namespace.Instance) {
		workloads := deployWorkloadsOrFail(t, []workload{
			{
				serviceName: brokenService1,
				cluster:     unhealthy,
				namespace:   ns1,
				serviceLabels: map[string]string{
					"istio.io/global": "true",
				},
				replicas: 0,
			},
			{
				serviceName: brokenService1,
				cluster:     healthy,
				namespace:   ns1,
				serviceLabels: map[string]string{
					"istio.io/global": "true",
				},
				replicas: 1,
			},
			{
				serviceName: brokenService2,
				cluster:     unhealthy,
				namespace:   ns2,
				serviceLabels: map[string]string{
					"istio.io/global": "true",
				},
				replicas: 0,
			},
			{
				serviceName: brokenService2,
				cluster:     healthy,
				namespace:   ns2,
				serviceLabels: map[string]string{
					"istio.io/global": "true",
				},
				replicas: 1,
			},
			{
				serviceName: clientService,
				cluster:     unhealthy,
				namespace:   ns1,
				replicas:    1,
			},
			{
				serviceName: clientService,
				cluster:     healthy,
				namespace:   ns1,
				replicas:    1,
			},
			{
				serviceName: clientService,
				cluster:     unhealthy,
				namespace:   ns2,
				replicas:    1,
			},
			{
				serviceName: clientService,
				cluster:     healthy,
				namespace:   ns2,
				replicas:    1,
			},
		})
		clients := match.AnyServiceName([]echo.NamespacedName{
			{Name: clientService, Namespace: ns1},
			{Name: clientService, Namespace: ns2},
		}).GetMatches(workloads)
		for _, src := range clients {
			// The services we call below are partially broken, i.e., one of the clusters does not have any healthy
			// replicas to talk to. However, because this setup is multi-cluster, we should be able to successfully
			// failover to replicas in a remote cluster
			//
			// NOTE: we talk to two different services here to cover the case reported in
			// https://github.com/istio/istio/issues/58630. That bug wasn't caught by any other tests we had.
			var wg errgroup.Group
			wg.Go(func() error {
				_, err := src.Call(echo.CallOptions{
					Address: fmt.Sprintf("%s.%s.svc.cluster.local", brokenService1, ns1.Name()),
					Port:    ports.HTTP,
					Scheme:  scheme.HTTP,
					Check:   check.OK(),
					HTTP: echo.HTTP{
						HTTP2: true,
						Path:  "/?delay=1s",
					},
					Retry: echo.Retry{NoRetry: true},
					Count: 1,
				})
				return err
			})
			wg.Go(func() error {
				_, err := src.Call(echo.CallOptions{
					Address: fmt.Sprintf("%s.%s.svc.cluster.local", brokenService2, ns2.Name()),
					Port:    ports.HTTP,
					Scheme:  scheme.HTTP,
					Check:   check.OK(),
					HTTP: echo.HTTP{
						HTTP2: true,
						Path:  "/?delay=1s",
					},
					Retry: echo.Retry{NoRetry: true},
					Count: 1,
				})
				return err
			})
			err := wg.Wait()
			if err != nil {
				t.Fatal(err)
			}
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
			// if we have multiple-cluster in the topology, find the cluster on a remote nework,
			// if one is available, to make sure that traffic goes through an E/W gateway.
			for _, c := range allClusters {
				if local.NetworkName() != c.NetworkName() {
					remote = c
					break
				}
			}

			ns1 := namespace.NewOrFail(t, namespace.Config{
				Prefix: "without-waypoint",
				Inject: false,
				Labels: map[string]string{
					label.IoIstioDataplaneMode.Name: "ambient",
				},
			})
			ns2 := namespace.NewOrFail(t, namespace.Config{
				Prefix: "without-waypoint",
				Inject: false,
				Labels: map[string]string{
					label.IoIstioDataplaneMode.Name: "ambient",
				},
			})

			runTest(t, local, remote, ns1, ns2)
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
			// if we have multiple-cluster in the topology, find the cluster on a remote nework,
			// if one is available, to make sure that traffic goes through an E/W gateway.
			for _, c := range allClusters {
				if local.NetworkName() != c.NetworkName() {
					remote = c
					break
				}
			}

			ns1 := namespace.NewOrFail(t, namespace.Config{
				Prefix: "with-waypoint",
				Inject: false,
				Labels: map[string]string{
					label.IoIstioDataplaneMode.Name: "ambient",
				},
			})
			ns2 := namespace.NewOrFail(t, namespace.Config{
				Prefix: "with-waypoint",
				Inject: false,
				Labels: map[string]string{
					label.IoIstioDataplaneMode.Name: "ambient",
				},
			})

			waypointName := "waypoint"
			deployWaypointsOrFail(t, local, waypointName, ns1)
			deployWaypointsOrFail(t, local, waypointName, ns2)
			runTest(t, local, remote, ns1, ns2)

			t.NewSubTest("ingress-use-waypoint").Run(func(t framework.TestContext) {
				SetNsIngressUseWaypoint(t, ns1.Name())

				t.ConfigIstio().Eval(ns1.Name(), map[string]string{
					"Service": brokenService1,
					"Port":    fmt.Sprintf("%d", ports.HTTP.ServicePort),
				}, `apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: tag-via-waypoint
spec:
  parentRefs:
  - group: ""
    kind: Service
    name: {{.Service}}
  rules:
  - filters:
    - type: ResponseHeaderModifier
      responseHeaderModifier:
        add:
        - name: x-traversed-waypoint
          value: "true"
    backendRefs:
    - group: ""
      kind: Service
      name: {{.Service}}
      port: {{.Port}}
`).ApplyOrFail(t)

				t.ConfigIstio().Eval(ns1.Name(), map[string]string{
					"Destination": fmt.Sprintf("%s.%s.svc.cluster.local", brokenService1, ns1.Name()),
				}, `apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: gateway
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
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: route
spec:
  gateways:
  - gateway
  hosts:
  - "*"
  http:
  - route:
    - destination:
        host: "{{.Destination}}"
`).ApplyOrFail(t)

				i.IngressFor(local).CallOrFail(t, echo.CallOptions{
					Port: echo.Port{
						Protocol:    protocol.HTTP,
						ServicePort: 80,
					},
					Scheme: scheme.HTTP,
					Check: check.And(
						check.OK(),
						check.ResponseHeader("x-traversed-waypoint", "true"),
					),
				})
			})
		})
	})
}

func deployWorkloadsOrFail(t framework.TestContext, workloads []workload) echo.Instances {
	t.Helper()

	builder := deployment.New(t)
	for _, w := range workloads {
		builder = builder.WithConfig(echo.Config{
			Service:       w.serviceName,
			Namespace:     w.namespace,
			Cluster:       w.cluster,
			Ports:         ports.All(),
			ServiceLabels: w.serviceLabels,
			Subsets: []echo.SubsetConfig{{
				Version:  w.serviceName,
				Replicas: 1,
			}},
		})
	}

	deployments := builder.BuildOrFail(t)

	for _, w := range workloads {
		if w.replicas != 1 {
			scaleDeploymentOrFail(t, w.cluster, w.namespace.Name(), fmt.Sprintf("%s-%s", w.serviceName, w.serviceName), w.replicas)
		}
	}

	return deployments
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
		t.Fatalf("failed to find deployment %s in namespace %s: %v", name, namespace, err)
	}

	s.Spec.Replicas = scale
	_, err = c.Kube().AppsV1().Deployments(namespace).UpdateScale(t.Context(), name, s, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("failed to scale deployment %s in namespace %s to %d replicas: %v", name, namespace, scale, err)
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
