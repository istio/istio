// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package ambient

import (
	"fmt"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sv1 "sigs.k8s.io/gateway-api/apis/v1"
	k8sbeta "sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/api/label"
	apiv1alpha3 "istio.io/client-go/pkg/apis/networking/v1"
	clientsecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient/multicluster"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/workloadapi"
)

type ambientclients struct {
	pc    clienttest.TestClient[*corev1.Pod]
	sc    clienttest.TestClient[*corev1.Service]
	sec   clienttest.TestWriter[*corev1.Secret]
	ns    clienttest.TestWriter[*corev1.Namespace]
	grc   clienttest.TestWriter[*k8sbeta.Gateway]
	gwcls clienttest.TestWriter[*k8sbeta.GatewayClass]
	se    clienttest.TestWriter[*apiv1alpha3.ServiceEntry]
	we    clienttest.TestWriter[*apiv1alpha3.WorkloadEntry]
	pa    clienttest.TestWriter[*clientsecurityv1beta1.PeerAuthentication]
	authz clienttest.TestWriter[*clientsecurityv1beta1.AuthorizationPolicy]
}

type remoteAmbientClients struct {
	clusterID cluster.ID
	*ambientclients
}

func (r *remoteAmbientClients) ResourceName() string {
	return string(r.clusterID)
}

func TestAmbientMulticlusterIndex_WaypointForWorkloadTraffic(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
	cases := []struct {
		name         string
		trafficType  string
		podAssertion func(s *ambientTestServer, c cluster.ID)
		svcAssertion func(s *ambientTestServer, svcKey string)
	}{
		{
			name:        "service traffic",
			trafficType: constants.ServiceTraffic,
			podAssertion: func(s *ambientTestServer, c cluster.ID) {
				s.t.Helper()
				// Test that there's no events for workloads in our cluster
				// as a result of our actions (we can't control pushes for
				// workloads from other clusters)
				s.assertNoMatchingEvent(s.t, c.String())
			},
			svcAssertion: func(s *ambientTestServer, _ string) {
				s.t.Helper()
				s.assertEvent(s.t, s.svcXdsName("svc2"))
			},
		},
		{
			name:        "all traffic",
			trafficType: constants.AllTraffic,
			podAssertion: func(s *ambientTestServer, _ cluster.ID) {
				s.t.Helper()
				s.assertEvent(s.t, s.podXdsName("pod1"))
			},
			svcAssertion: func(s *ambientTestServer, _ string) {
				s.t.Helper()
				s.assertEvent(s.t, s.svcXdsName("svc2"))
			},
		},
		{
			name:        "workload traffic",
			trafficType: constants.WorkloadTraffic,
			podAssertion: func(s *ambientTestServer, c cluster.ID) {
				s.t.Helper()
				s.assertEvent(s.t, s.podXdsName("pod1"))
			},
			svcAssertion: func(s *ambientTestServer, svcKey string) {
				s.t.Helper()
				s.assertNoMatchingEvent(s.t, svcKey)
			},
		},
		{
			name:        "no traffic",
			trafficType: constants.NoTraffic,
			podAssertion: func(s *ambientTestServer, c cluster.ID) {
				s.t.Helper()
				s.assertNoMatchingEvent(s.t, c.String())
			},
			svcAssertion: func(s *ambientTestServer, svcKey string) {
				s.t.Helper()
				s.assertNoMatchingEvent(s.t, svcKey)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newAmbientTestServer(t, testC, testNW, "")
			s.AddSecret("s1", "c1") // overlapping ips
			s.AddSecret("s2", "c2") // Non-overlapping ips
			remoteClients := krt.NewCollection(s.remoteClusters, func(_ krt.HandlerContext, c *multicluster.Cluster) **remoteAmbientClients {
				cl := c.Client
				return ptr.Of(&remoteAmbientClients{
					clusterID: c.ID,
					ambientclients: &ambientclients{
						pc:    clienttest.NewDirectClient[*corev1.Pod, corev1.Pod, *corev1.PodList](t, cl),
						sc:    clienttest.NewDirectClient[*corev1.Service, corev1.Service, *corev1.ServiceList](t, cl),
						ns:    clienttest.NewWriter[*corev1.Namespace](t, cl),
						grc:   clienttest.NewWriter[*k8sbeta.Gateway](t, cl),
						gwcls: clienttest.NewWriter[*k8sbeta.GatewayClass](t, cl),
						se:    clienttest.NewWriter[*apiv1alpha3.ServiceEntry](t, cl),
						we:    clienttest.NewWriter[*apiv1alpha3.WorkloadEntry](t, cl),
						pa:    clienttest.NewWriter[*clientsecurityv1beta1.PeerAuthentication](t, cl),
						authz: clienttest.NewWriter[*clientsecurityv1beta1.AuthorizationPolicy](t, cl),
						sec:   clienttest.NewWriter[*corev1.Secret](t, cl),
					},
				})
			})

			assert.EventuallyEqual(t, func() int {
				return len(remoteClients.List())
			}, 2)

			differentCIDRIPs := map[string]string{
				"waypoint": "10.1.0.10",
				"pod1":     "127.0.0.6",
				"svc2":     "10.1.0.1",
			}
			duplicateCIDRIPs := map[string]string{
				"waypoint": "10.0.0.10",
				"pod1":     "127.0.0.1",
				"svc2":     "10.0.0.1",
			}
			clusterToIPs := map[cluster.ID]map[string]string{
				"cluster0": duplicateCIDRIPs,
				"c1":       duplicateCIDRIPs,
				"c2":       differentCIDRIPs,
			}
			clusterToNetwork := map[cluster.ID]string{
				"cluster0": testNW,
				"c1":       "testnetwork-2", // overlapping ips
				"c2":       testNW,          // different ips
			}
			networkGatewayIps := map[cluster.ID]string{ // these have to be global
				"cluster0": "77.1.2.4",
				"c1":       "77.1.2.49",
				"c2":       "", // c2 and cluster0 share the same network, so no gw
			}
			rClients := remoteClients.List()
			clients := append([]*remoteAmbientClients{
				{
					clusterID: s.clusterID,
					ambientclients: &ambientclients{
						pc:    s.pc,
						sc:    s.sc,
						ns:    s.ns,
						grc:   s.grc,
						gwcls: s.gwcls,
						se:    s.se,
						we:    s.we,
						pa:    s.pa,
						authz: s.authz,
						sec:   s.sec,
					},
				},
			}, rClients...)
			for _, client := range clients {
				// Test ambient index already creates istio-system namespace
				if client.clusterID != s.clusterID {
					client.ns.Create(&corev1.Namespace{
						ObjectMeta: metav1.ObjectMeta{
							Name:   systemNS,
							Labels: map[string]string{label.TopologyNetwork.Name: clusterToNetwork[client.clusterID]},
						},
					})
					client.ns.Create(&corev1.Namespace{
						ObjectMeta: metav1.ObjectMeta{
							Name:   testNS,
							Labels: map[string]string{label.TopologyNetwork.Name: clusterToNetwork[client.clusterID]},
						},
					})
					client.gwcls.Create(&k8sbeta.GatewayClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: constants.EastWestGatewayClassName,
						},
						Spec: k8sv1.GatewayClassSpec{
							ControllerName: constants.ManagedGatewayMeshController,
						},
					})

					// Ensure the namespace network is set in up in the collection before doing other assertions
					assert.EventuallyEqual(t, func() bool {
						networks := s.networks.SystemNamespaceNetworkByCluster.Lookup(client.clusterID)
						if len(networks) == 0 {
							return false
						}

						nw := networks[0].Network
						return string(nw) == clusterToNetwork[client.clusterID]
					}, true)
				}
				if networkGatewayIps[client.clusterID] != "" {
					s.addNetworkGatewayForClient(t, networkGatewayIps[client.clusterID], clusterToNetwork[client.clusterID], client.grc)
				}
				if networkGatewayIps[client.clusterID] != "" {
					s.addNetworkGatewayForClient(t, networkGatewayIps[client.clusterID], clusterToNetwork[client.clusterID], client.grc)
				}
			}

			// Need the namespaces to exist before creating services
			assert.EventuallyEqual(t, func() int {
				ns := s.namespaces.List()
				return len(ns)
			}, 2)

			for _, client := range clients {
				ips := clusterToIPs[client.clusterID]
				// These steps happen for every test regardless of traffic type.
				// It involves creating a waypoint for the specified traffic type
				// then creating a workload and a service with no annotations set
				// on these objects yet.
				s.addWaypointForClient(t, ips["waypoint"], "test-wp", c.trafficType, true, client.grc)

				s.addServiceForClient(t, "svc2",
					map[string]string{
						"istio.io/global": "true",
					},
					map[string]string{},
					[]int32{80}, map[string]string{"app": "a"}, ips["svc2"], client.sc)
				s.assertEvent(t, s.svcXdsName("svc2"))
			}

			// Service configuration needs to be uniform, so we add services to all clusters first,
			// then check for events
			events := make([]string, 0, len(clients))
			for _, client := range clients {
				ips := clusterToIPs[client.clusterID]
				s.addPodsForClient(t, ips["pod1"], "pod1", "sa1",
					map[string]string{"app": "a"}, nil, true, corev1.PodRunning, client.pc)
				if clusterToNetwork[client.clusterID] == clusterToNetwork[s.clusterID] {
					events = append(events, s.podXdsNameForCluster("pod1", client.clusterID))
				} else if networkGatewayIps[client.clusterID] != "" {
					events = append(events, fmt.Sprintf("%s/SplitHorizonWorkload/ns1/east-west/%s/%s",
						clusterToNetwork[client.clusterID],
						networkGatewayIps[client.clusterID],
						s.svcXdsName("svc2"),
					))
				}
			}
			// the service sans get updated.
			events = append(events, s.svcXdsName("svc2"))
			s.assertEvent(t, events...)

			// Label the pod and check that the correct event is produced.
			s.labelPod(t, "pod1", testNS,
				map[string]string{"app": "a", label.IoIstioUseWaypoint.Name: "test-wp"})
			c.podAssertion(s, testC)

			// Label the service and check that the correct event is produced.
			s.labelService(t, "svc2", testNS,
				map[string]string{
					label.IoIstioUseWaypoint.Name: "test-wp",
					"istio.io/global":             "true",
				})
			c.svcAssertion(s, s.svcXdsName("svc2"))

			// clean up resources
			s.deleteService(t, "svc2")
			s.assertEvent(t, s.podXdsName("pod1"), s.svcXdsName("svc2"))
			s.deletePod(t, "pod1")
			s.assertEvent(t, s.podXdsName("pod1"))
			s.deleteWaypoint(t, "test-wp")

			for _, rc := range remoteClients.List() {
				// Removing the service changes the WDS workload in that cluster due to service attachments.
				// Note that we should NOT get an event changing the service attachment in our local cluster.
				// We also get a service event because we lost an IP
				if clusterToNetwork[rc.clusterID] == clusterToNetwork[s.clusterID] {
					s.deleteServiceForClient(t, "svc2", rc.sc)
					s.assertEvent(t, s.podXdsNameForCluster("pod1", rc.clusterID), s.svcXdsName("svc2"))
					s.deletePodForClient(t, "pod1", rc.pc)
					s.assertEvent(t, s.podXdsNameForCluster("pod1", rc.clusterID))
				} else {
					s.deleteServiceForClient(t, "svc2", rc.sc)
					s.assertEvent(t, fmt.Sprintf("%s/SplitHorizonWorkload/ns1/east-west/%s/%s",
						clusterToNetwork[rc.clusterID],
						networkGatewayIps[rc.clusterID],
						s.svcXdsName("svc2"),
					), s.svcXdsName("svc2"))
					s.deletePodForClient(t, "pod1", rc.pc)
				}
				s.deleteWaypointForClient(t, "test-wp", rc.grc)
			}
			s.clearEvents()
		})
	}
}

func TestMulticlusterAmbientIndex_ServicesForWaypoint(t *testing.T) {
	t.Skip("This test is flaky, see https://github.com/istio/istio/issues/57126")
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
	wpKey := model.WaypointKey{
		Namespace: testNS,
		Hostnames: []string{fmt.Sprintf("%s.%s.svc.company.com", "wp", testNS)},
		Addresses: []string{"10.0.0.1"},
		Network:   testNW,
	}
	t.Run("waypoint eds", func(t *testing.T) {
		s := newAmbientTestServer(t, testC, testNW, "")
		s.addService(t, "svc1",
			map[string]string{
				label.IoIstioUseWaypoint.Name: "wp",
				"istio.io/global":             "true",
			},
			map[string]string{},
			[]int32{80}, map[string]string{"app": "app1"}, "11.0.0.1")
		s.assertEvent(s.t, s.svcXdsName("svc1"))

		s.addWaypointSpecificAddress(t, "", s.hostnameForService("wp"), "wp", constants.AllTraffic, true)
		s.addService(t, "wp",
			map[string]string{},
			map[string]string{},
			[]int32{80}, map[string]string{"app": "waypoint"}, "10.0.0.1")
		s.assertEvent(s.t, s.svcXdsName("svc1"))

		s.addPodsForClient(t, "10.0.1.5", "wp-pod", "sa1",
			map[string]string{"app": "waypoint"}, nil, true, corev1.PodRunning, s.pc)
		// Event IDs do not have the namespace prefix for EDS
		s.assertEvent(s.t, "svc1.ns1.svc.company.com")
	})

	t.Run("hostname (multicluster but unused)", func(t *testing.T) {
		s := newAmbientTestServer(t, testC, testNW, "")
		s.addService(t, "svc1",
			map[string]string{label.IoIstioUseWaypoint.Name: "wp"},
			map[string]string{},
			[]int32{80}, map[string]string{"app": "app1"}, "11.0.0.1")
		s.assertEvent(s.t, s.svcXdsName("svc1"))

		s.addWaypointSpecificAddress(t, "", s.hostnameForService("wp"), "wp", constants.AllTraffic, true)
		s.addService(t, "wp",
			map[string]string{},
			map[string]string{},
			[]int32{80}, map[string]string{"app": "waypoint"}, "10.0.0.2")
		s.assertEvent(s.t, s.svcXdsName("svc1"))

		svc1Host := ptr.ToList(s.services.GetKey(fmt.Sprintf("%s/%s", testNS, s.hostnameForService("svc1"))))
		assert.Equal(t, len(svc1Host), 1)
		assert.EventuallyEqual(t, func() []model.ServiceInfo {
			return s.ServicesForWaypoint(wpKey)
		}, svc1Host)
	})
	t.Run("ip (multicluster but unused)", func(t *testing.T) {
		s := newAmbientTestServer(t, testC, testNW, "")
		s.addService(t, "svc1",
			map[string]string{label.IoIstioUseWaypoint.Name: "wp"},
			map[string]string{},
			[]int32{80}, map[string]string{"app": "app1"}, "11.0.0.1")
		s.assertEvent(s.t, s.svcXdsName("svc1"))

		s.addWaypointSpecificAddress(t, "10.0.0.1", "", "wp", constants.AllTraffic, true)
		s.addService(t, "wp",
			map[string]string{},
			map[string]string{},
			[]int32{80}, map[string]string{"app": "waypoint"}, "10.0.0.1")
		s.assertEvent(s.t, s.svcXdsName("svc1"))

		svc1Host := ptr.ToList(s.services.GetKey(fmt.Sprintf("%s/%s", testNS, s.hostnameForService("svc1"))))
		assert.Equal(t, len(svc1Host), 1)
		assert.EventuallyEqual(t, func() []model.ServiceInfo {
			return s.ServicesForWaypoint(wpKey)
		}, svc1Host)
	})
	t.Run("mixed (multicluster but unused)", func(t *testing.T) {
		s := newAmbientTestServer(t, testC, testNW, "")
		s.addService(t, "svc1",
			map[string]string{label.IoIstioUseWaypoint.Name: "wp"},
			map[string]string{},
			[]int32{80}, map[string]string{"app": "app1"}, "11.0.0.1")
		s.assertEvent(s.t, s.svcXdsName("svc1"))

		s.addWaypointSpecificAddress(t, "10.0.0.1", s.hostnameForService("wp"), "wp", constants.AllTraffic, true)
		s.addService(t, "wp",
			map[string]string{},
			map[string]string{},
			[]int32{80}, map[string]string{"app": "waypoint"}, "10.0.0.1")
		s.assertEvent(s.t, s.svcXdsName("svc1"))

		svc1Host := ptr.ToList(s.services.GetKey(fmt.Sprintf("%s/%s", testNS, s.hostnameForService("svc1"))))
		assert.Equal(t, len(svc1Host), 1)
		assert.EventuallyEqual(t, func() []model.ServiceInfo {
			return s.ServicesForWaypoint(wpKey)
		}, svc1Host)
	})
}

func TestMulticlusterAmbientIndex_TestServiceMerging(t *testing.T) {
	t.Skip("This test is flaky, see https://github.com/istio/istio/issues/57126")
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
	s := newAmbientTestServer(t, testC, testNW, "")
	s.AddSecret("s1", "remote-cluster") // overlapping ips
	remoteClients := krt.NewCollection(s.remoteClusters, func(_ krt.HandlerContext, c *multicluster.Cluster) **remoteAmbientClients {
		cl := c.Client
		return ptr.Of(&remoteAmbientClients{
			clusterID: c.ID,
			ambientclients: &ambientclients{
				pc:    clienttest.NewDirectClient[*corev1.Pod, corev1.Pod, *corev1.PodList](t, cl),
				sc:    clienttest.NewDirectClient[*corev1.Service, corev1.Service, *corev1.ServiceList](t, cl),
				ns:    clienttest.NewWriter[*corev1.Namespace](t, cl),
				grc:   clienttest.NewWriter[*k8sbeta.Gateway](t, cl),
				gwcls: clienttest.NewWriter[*k8sbeta.GatewayClass](t, cl),
				se:    clienttest.NewWriter[*apiv1alpha3.ServiceEntry](t, cl),
				we:    clienttest.NewWriter[*apiv1alpha3.WorkloadEntry](t, cl),
				pa:    clienttest.NewWriter[*clientsecurityv1beta1.PeerAuthentication](t, cl),
				authz: clienttest.NewWriter[*clientsecurityv1beta1.AuthorizationPolicy](t, cl),
				sec:   clienttest.NewWriter[*corev1.Secret](t, cl),
			},
		})
	})

	const remoteNetwork = "remote-network"

	assert.EventuallyEqual(t, func() int {
		return len(remoteClients.List())
	}, 1)

	remoteClient := remoteClients.List()[0]
	localClient := remoteAmbientClients{
		clusterID: s.clusterID,
		ambientclients: &ambientclients{
			pc:    s.pc,
			sc:    s.sc,
			ns:    s.ns,
			grc:   s.grc,
			gwcls: s.gwcls,
			se:    s.se,
			we:    s.we,
			pa:    s.pa,
			authz: s.authz,
			sec:   s.sec,
		},
	}
	remoteClient.ns.Create(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   systemNS,
			Labels: map[string]string{label.TopologyNetwork.Name: remoteNetwork},
		},
	})
	remoteClient.ns.Create(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNS,
		},
	})
	s.addServiceForClient(t, "svc2",

		map[string]string{},
		map[string]string{},
		[]int32{80}, map[string]string{"app": "a"}, "10.0.0.1", localClient.sc,
	)
	s.addServiceForClient(t, "svc2",
		map[string]string{},
		map[string]string{},
		[]int32{80}, map[string]string{"app": "a"}, "127.1.0.1", remoteClient.sc,
	)
	retry.UntilSuccessOrFail(t, func() error {
		svc := s.lookupService("ns1/svc2.ns1.svc.company.com")
		if svc == nil {
			return fmt.Errorf("service not found")
		}
		if svc.Scope == model.Global {
			return fmt.Errorf("expected service scope to be Local, got %s", svc.Scope)
		}
		if len(svc.Service.Addresses) != 1 {
			return fmt.Errorf("expected service to have 1 address, got %d", len(svc.Service.Addresses))
		}
		return nil
	})
	s.deleteService(t, "svc2")
	retry.UntilSuccessOrFail(t, func() error {
		svc := s.lookupService("ns1/svc2.ns1.svc.company.com")
		if svc != nil {
			return fmt.Errorf("expected service to be deleted, but it still exists")
		}
		return nil
	})
	s.labelServiceForClient(t, "svc2", testNS,
		map[string]string{"istio.io/global": "true"},
		remoteClient.sc,
	)
	retry.UntilSuccessOrFail(t, func() error {
		svc := s.lookupService("ns1/svc2.ns1.svc.company.com")
		if svc == nil {
			return fmt.Errorf("service not found")
		}
		if svc.Scope != model.Global {
			return fmt.Errorf("expected service scope to be Global, got %s", svc.Scope)
		}
		if len(svc.Service.Addresses) != 1 {
			return fmt.Errorf("expected service to have 1 addresses, got %d", len(svc.Service.Addresses))
		}
		return nil
	})
	// Add a service to local cluster
	s.addService(t, "svc2",
		map[string]string{"istio.io/global": "true"},
		map[string]string{},
		[]int32{80}, map[string]string{"app": "a"}, "10.0.0.1",
	)
	retry.UntilSuccessOrFail(t, func() error {
		svc := s.lookupService("ns1/svc2.ns1.svc.company.com")
		if svc == nil {
			return fmt.Errorf("service not found")
		}
		if svc.Scope != model.Global {
			return fmt.Errorf("expected service scope to be Global, got %s", svc.Scope)
		}
		if len(svc.Service.Addresses) != 2 {
			return fmt.Errorf("expected service to have 2 addresses, got %d", len(svc.Service.Addresses))
		}
		return nil
	})
}

func TestMulticlusterAmbientIndex_SplitHorizon(t *testing.T) {
	t.Skip("This test is flaky, see https://github.com/istio/istio/issues/57126")
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
	s := newAmbientTestServer(t, testC, testNW, "")
	s.AddSecret("s1", "remote-cluster") // overlapping ips
	remoteClients := krt.NewCollection(s.remoteClusters, func(_ krt.HandlerContext, c *multicluster.Cluster) **remoteAmbientClients {
		cl := c.Client
		return ptr.Of(&remoteAmbientClients{
			clusterID: c.ID,
			ambientclients: &ambientclients{
				pc:    clienttest.NewDirectClient[*corev1.Pod, corev1.Pod, *corev1.PodList](t, cl),
				sc:    clienttest.NewDirectClient[*corev1.Service, corev1.Service, *corev1.ServiceList](t, cl),
				ns:    clienttest.NewWriter[*corev1.Namespace](t, cl),
				grc:   clienttest.NewWriter[*k8sbeta.Gateway](t, cl),
				gwcls: clienttest.NewWriter[*k8sbeta.GatewayClass](t, cl),
				se:    clienttest.NewWriter[*apiv1alpha3.ServiceEntry](t, cl),
				we:    clienttest.NewWriter[*apiv1alpha3.WorkloadEntry](t, cl),
				pa:    clienttest.NewWriter[*clientsecurityv1beta1.PeerAuthentication](t, cl),
				authz: clienttest.NewWriter[*clientsecurityv1beta1.AuthorizationPolicy](t, cl),
				sec:   clienttest.NewWriter[*corev1.Secret](t, cl),
			},
		})
	})

	const remoteNetwork = "remote-network"

	assert.EventuallyEqual(t, func() int {
		return len(remoteClients.List())
	}, 1)

	remoteClient := remoteClients.List()[0]
	localClient := remoteAmbientClients{
		clusterID: s.clusterID,
		ambientclients: &ambientclients{
			pc:    s.pc,
			sc:    s.sc,
			ns:    s.ns,
			grc:   s.grc,
			gwcls: s.gwcls,
			se:    s.se,
			we:    s.we,
			pa:    s.pa,
			authz: s.authz,
			sec:   s.sec,
		},
	}
	remoteClient.ns.Create(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   systemNS,
			Labels: map[string]string{label.TopologyNetwork.Name: remoteNetwork},
		},
	})
	remoteClient.ns.Create(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNS,
		},
	})
	networkGatewayIP := "172.0.1.2"
	s.addNetworkGatewayForClient(t, networkGatewayIP, remoteNetwork, remoteClient.grc)
	s.addServiceForClient(t, "svc2",
		map[string]string{
			"istio.io/global": "true",
		},
		map[string]string{},
		[]int32{80}, map[string]string{"app": "a"}, "10.0.0.1", localClient.sc,
	)
	s.addServiceForClient(t, "svc2",
		map[string]string{
			"istio.io/global": "true",
		},
		map[string]string{},
		[]int32{80}, map[string]string{"app": "a"}, "127.1.0.1", remoteClient.sc,
	)
	retry.UntilSuccessOrFail(t, func() error {
		svc := s.lookupService("ns1/svc2.ns1.svc.company.com")
		if svc == nil {
			return fmt.Errorf("service not found")
		}
		if svc.Scope != model.Global {
			return fmt.Errorf("expected service scope to be Global, got %s", svc.Scope)
		}
		gwwl := s.workloads.GetKey("NetworkGateway/remote-network/172.0.1.2/0")
		if gwwl == nil {
			return fmt.Errorf("expected network gateway workload to exist, but it does not")
		}
		if len(gwwl.Workload.Addresses) != 1 {
			return fmt.Errorf("expected network gateway workload to have addresses, got %v", gwwl.Workload.Addresses)
		}
		expectedAddress := []uint8{172, 0, 1, 2}
		if !reflect.DeepEqual(gwwl.Workload.Addresses[0], expectedAddress) {
			return fmt.Errorf("expected network gateway workload to have address %s, got %s",
				networkGatewayIP,
				gwwl.Workload.Addresses[0],
			)
		}
		return nil
	})

	s.addPodsForClient(t, "10.0.1.1", "pod1", "sa1",
		map[string]string{"app": "a"}, nil, true, corev1.PodRunning, localClient.pc)
	s.addPodsForClient(t, "127.0.0.1", "pod1-abc", "sa1",
		map[string]string{"app": "a"}, nil, true, corev1.PodRunning, remoteClient.pc)

	splitHorizonName := fmt.Sprintf("%s/SplitHorizonWorkload/ns1/east-west/%s/%s",
		remoteNetwork, networkGatewayIP, s.svcXdsName("svc2"))
	retry.UntilSuccessOrFail(t, func() error {
		ais := s.Lookup("ns1/svc2.ns1.svc.company.com")
		if len(ais) != 3 {
			return fmt.Errorf("expected 3 pods, got %d", len(ais))
		}
		shwl := s.workloads.GetKey(splitHorizonName)
		if shwl == nil {
			return fmt.Errorf("expected split horizon workload to exist, but it does not")
		}
		if len(shwl.Workload.Addresses) != 0 {
			return fmt.Errorf("expected no addresses in split horizon workload, got %v", shwl.Workload.Addresses)
		}
		if !reflect.DeepEqual(
			shwl.Workload.NetworkGateway.Destination.(*workloadapi.GatewayAddress_Address).Address.Address,
			[]byte{172, 0, 1, 2},
		) {
			return fmt.Errorf(
				"expected split horizon workload to have network gateway address %s, got %s",
				networkGatewayIP,
				shwl.Workload.NetworkGateway.Destination.(*workloadapi.GatewayAddress_Address).Address.Address,
			)
		}
		if shwl.Workload.NetworkGateway.Destination.(*workloadapi.GatewayAddress_Address).Address.Network != remoteNetwork {
			return fmt.Errorf("expected split horizon workload to have network %s, got %s",
				remoteNetwork,
				shwl.Workload.NetworkGateway.Destination.(*workloadapi.GatewayAddress_Address).Address.Network,
			)
		}
		if shwl.Workload.Capacity.GetValue() != 1 {
			return fmt.Errorf("expected split horizon workload to have capacity 1, got %d",
				shwl.Workload.Capacity.GetValue(),
			)
		}
		if len(shwl.Workload.Addresses) != 0 {
			return fmt.Errorf("expected no addresses in split horizon workload, got %v",
				shwl.Workload.Addresses,
			)
		}
		return nil
	})

	s.addPodsForClient(t, "127.0.0.2", "pod2", "sa1",
		map[string]string{"app": "a"}, nil, true, corev1.PodRunning, remoteClient.pc)
	s.assertEvent(t, s.podXdsName("pod1"), splitHorizonName, s.svcXdsName("svc2"))

	retry.UntilSuccessOrFail(t, func() error {
		ais := s.Lookup("ns1/svc2.ns1.svc.company.com")
		if len(ais) != 3 {
			return fmt.Errorf("expected 3 pods, got %d", len(ais))
		}
		shwl := s.workloads.GetKey(splitHorizonName)
		if shwl == nil {
			return fmt.Errorf("expected split horizon workload to exist, but it does not")
		}
		if shwl.Workload.Capacity.GetValue() != 2 {
			return fmt.Errorf("expected split horizon workload to have capacity 2, got %d", shwl.Workload.Capacity.GetValue())
		}
		return nil
	})

	// Remove network gateway: Split horizon workload should be removed
	s.deleteNetworkGatewayForClient(t, "east-west", remoteClient.grc)
	assert.EventuallyEqual(t, func() int {
		return len(s.Lookup("ns1/svc2.ns1.svc.company.com"))
	}, 2) // 2 pods, no split horizon workload
	retry.UntilSuccessOrFail(t, func() error {
		if s.workloads.GetKey(splitHorizonName) != nil {
			return fmt.Errorf("expected split horizon workload to be removed, but it still exists")
		}
		return nil
	})

	// Add the network gateway back: Split horizon workload should be recreated
	s.addNetworkGatewayForClient(t, networkGatewayIP, remoteNetwork, remoteClient.grc)
	retry.UntilSuccessOrFail(t, func() error {
		ais := s.Lookup("ns1/svc2.ns1.svc.company.com")
		if len(ais) != 3 {
			return fmt.Errorf("expected 3 pods, got %d", len(ais))
		}
		shwl := s.workloads.GetKey(splitHorizonName)
		if shwl == nil {
			return fmt.Errorf("expected split horizon workload to exist, but it does not")
		}
		if shwl.Workload.Capacity.GetValue() != 2 {
			return fmt.Errorf("expected split horizon workload to have capacity 2, got %d", shwl.Workload.Capacity.GetValue())
		}
		return nil
	})
	// make the service local local
	s.labelServiceForClient(t, "svc2", testNS, map[string]string{}, localClient.sc)
	s.labelServiceForClient(t, "svc2", testNS, map[string]string{}, remoteClient.sc)
	assert.EventuallyEqual(t, func() int {
		ais := s.Lookup("ns1/svc2.ns1.svc.company.com")
		return len(ais)
	}, 2)
	retry.UntilSuccessOrFail(t, func() error {
		if s.workloads.GetKey(splitHorizonName) != nil {
			return fmt.Errorf("expected split horizon workload to be removed, but it still exists")
		}
		return nil
	})

	// label remote cluster to have same network local and mark the service as global
	remoteClient.ns.Update(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   systemNS,
			Labels: map[string]string{label.TopologyNetwork.Name: testNW},
		},
	})
	log.Infof("Remote cluster %s now has network %s", remoteClient.clusterID, testNW)

	s.labelServiceForClient(t, "svc2", testNS,
		map[string]string{"istio.io/global": "true"}, localClient.sc)
	s.labelServiceForClient(t, "svc2", testNS,
		map[string]string{"istio.io/global": "true"}, remoteClient.sc)
	s.assertEvent(t, s.podXdsNameForCluster("pod2", remoteClient.clusterID),
		s.podXdsNameForCluster("pod1-abc", remoteClient.clusterID),
	)
	assert.EventuallyEqual(t, func() int {
		ais := s.Lookup("ns1/svc2.ns1.svc.company.com")
		return len(ais)
	}, 4)

	// Finally, change the network back
	remoteClient.ns.Update(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   systemNS,
			Labels: map[string]string{label.TopologyNetwork.Name: remoteNetwork},
		},
	})
	log.Infof("Remote cluster %s now has network %s", remoteClient.clusterID, remoteNetwork)
	assert.EventuallyEqual(t, func() int {
		ais := s.Lookup("ns1/svc2.ns1.svc.company.com")
		return len(ais)
	}, 3)
	s.assertEvent(t, s.podXdsNameForCluster("pod2", remoteClient.clusterID),
		s.podXdsNameForCluster("pod1-abc", remoteClient.clusterID),
		splitHorizonName,
	)
}

func (a *ambientTestServer) DeleteSecret(secretName string) {
	a.t.Helper()
	a.sec.Delete(secretName, secretNamespace)
}

func (a *ambientTestServer) AddSecret(secretName, clusterID string) {
	kubeconfig++
	a.sec.CreateOrUpdate(makeSecret(secretNamespace, secretName, clusterCredential{clusterID, fmt.Appendf(nil, "kubeconfig-%d", kubeconfig)}))
}
