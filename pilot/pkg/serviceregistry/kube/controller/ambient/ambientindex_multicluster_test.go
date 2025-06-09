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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sbeta "sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/api/label"
	apiv1alpha3 "istio.io/client-go/pkg/apis/networking/v1"
	clientsecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient/multicluster"
	"istio.io/istio/pilot/pkg/serviceregistry/util/xdsfake"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
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
		podAssertion func(s *ambientTestServer)
		svcAssertion func(s *ambientTestServer)
	}{
		{
			name:        "service traffic",
			trafficType: constants.ServiceTraffic,
			podAssertion: func(s *ambientTestServer) {
				s.t.Helper()
				s.assertNoEvent(s.t)
			},
			svcAssertion: func(s *ambientTestServer) {
				s.t.Helper()
				s.assertEvent(s.t, s.svcXdsName("svc2"))
			},
		},
		{
			name:        "all traffic",
			trafficType: constants.AllTraffic,
			podAssertion: func(s *ambientTestServer) {
				s.t.Helper()
				s.assertEvent(s.t, s.podXdsName("pod1"))
			},
			svcAssertion: func(s *ambientTestServer) {
				s.t.Helper()
				s.assertEvent(s.t, s.svcXdsName("svc2"))
			},
		},
		{
			name:        "workload traffic",
			trafficType: constants.WorkloadTraffic,
			podAssertion: func(s *ambientTestServer) {
				s.t.Helper()
				s.assertEvent(s.t, s.podXdsName("pod1"))
			},
			svcAssertion: func(s *ambientTestServer) {
				s.t.Helper()
				s.assertNoEvent(s.t)
			},
		},
		{
			name:        "no traffic",
			trafficType: constants.NoTraffic,
			podAssertion: func(s *ambientTestServer) {
				s.t.Helper()
				s.assertNoEvent(s.t)
			},
			svcAssertion: func(s *ambientTestServer) {
				s.t.Helper()
				s.assertNoEvent(s.t)
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
				ips := clusterToIPs[client.clusterID]
				// Test ambient index already creates istio-system namespace
				if client.clusterID != s.clusterID {
					client.ns.Create(&corev1.Namespace{
						ObjectMeta: metav1.ObjectMeta{
							Name:   systemNS,
							Labels: map[string]string{label.TopologyNetwork.Name: clusterToNetwork[client.clusterID]},
						},
					})
				}

				// These steps happen for every test regardless of traffic type.
				// It involves creating a waypoint for the specified traffic type
				// then creating a workload and a service with no annotations set
				// on these objects yet.
				s.addWaypointForClient(t, ips["waypoint"], "test-wp", c.trafficType, true, client.grc)
				s.addPodsForClient(t, ips["pod1"], "pod1", "sa1",
					map[string]string{"app": "a"}, nil, true, corev1.PodRunning, client.pc)
				s.assertEvent(t, s.podXdsNameForCluster("pod1", client.clusterID))
				s.addServiceForClient(t, "svc2",
					map[string]string{},
					map[string]string{},
					[]int32{80}, map[string]string{"app": "a"}, ips["svc2"], client.sc)
				s.assertEvent(t, s.svcXdsName("svc2"), s.podXdsNameForCluster("pod1", client.clusterID))
			}

			t.Run("xds event filtering", func(t *testing.T) {
				// Test that label selector change doesn't cause xDS push
				// especially in the context of the merge implementation.
				svc2 := s.sc.Get("svc2", testNS)
				tmp := svc2.DeepCopy()
				tmp.Spec.Selector["foo"] = "bar"
				s.sc.Update(tmp)
				// The new selector should disqualify pod1 from being a part
				// of this service. We should NOT get a service event though
				s.fx.StrictMatchOrFail(t, xdsfake.Event{
					Type: "xds",
					ID:   s.podXdsName("pod1"),
				})
				s.sc.Update(svc2)
				// We should get another event from the pod being a part of the
				// service again. Again, we should NOT get a service enent.
				s.fx.StrictMatchOrFail(t, xdsfake.Event{
					Type: "xds",
					ID:   s.podXdsName("pod1"),
				})
			})

			// Label the pod and check that the correct event is produced.
			s.labelPod(t, "pod1", testNS,
				map[string]string{"app": "a", label.IoIstioUseWaypoint.Name: "test-wp"})
			c.podAssertion(s)

			// Label the service and check that the correct event is produced.
			s.labelService(t, "svc2", testNS,
				map[string]string{label.IoIstioUseWaypoint.Name: "test-wp"})
			c.svcAssertion(s)

			// clean up resources
			s.deleteService(t, "svc2")
			s.assertEvent(t, s.podXdsName("pod1"), s.svcXdsName("svc2"))
			s.deletePod(t, "pod1")
			s.assertEvent(t, s.podXdsName("pod1"))
			s.deleteWaypoint(t, "test-wp")

			for _, rc := range remoteClients.List() {
				s.deleteServiceForClient(t, "svc2", rc.sc)
				// Removing the service changes the WDS workload in that cluster due to service attachments.
				// Note that we should NOT get an event changing the service attachment in our local cluster.
				// We also get a service event because we lost an IP
				s.assertEvent(t, s.podXdsNameForCluster("pod1", rc.clusterID), s.svcXdsName("svc2"))
				s.deletePodForClient(t, "pod1", rc.pc)
				s.assertEvent(t, s.podXdsNameForCluster("pod1", rc.clusterID))
				s.deleteWaypointForClient(t, "test-wp", rc.grc)
			}
			s.clearEvents()
		})
	}
}

// TODO: Test the merging details (the correct number of VIPs, no duplicates, etc.)

func (a *ambientTestServer) DeleteSecret(secretName string) {
	a.t.Helper()
	a.sec.Delete(secretName, secretNamespace)
}

func (a *ambientTestServer) AddSecret(secretName, clusterID string) {
	kubeconfig++
	a.sec.CreateOrUpdate(makeSecret(secretNamespace, secretName, clusterCredential{clusterID, fmt.Appendf(nil, "kubeconfig-%d", kubeconfig)}))
}
