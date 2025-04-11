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
	"testing"

	corev1 "k8s.io/api/core/v1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test"
)

// First we duplicate the tests in ambientindex_test.go, but while enabling multinetwork.
// This is to ensure that the multinetwork code is not breaking anything.
func TestMulticlusterAmbientIndex_WaypointForWorkloadTraffic(t *testing.T) {
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
				s.assertEvent(s.t, s.svcXdsName("svc1"))
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
				s.assertEvent(s.t, s.svcXdsName("svc1"))
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
			test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
			s := newAmbientTestServer(t, testC, testNW)
			// These steps happen for every test regardless of traffic type.
			// It involves creating a waypoint for the specified traffic type
			// then creating a workload and a service with no annotations set
			// on these objects yet.
			s.addWaypoint(t, "10.0.0.10", "test-wp", c.trafficType, true)
			s.addPods(t, "127.0.0.1", "pod1", "sa1",
				map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
			s.assertEvent(t, s.podXdsName("pod1"))
			s.addService(t, "svc1",
				map[string]string{},
				map[string]string{},
				[]int32{80}, map[string]string{"app": "a"}, "10.0.0.1")
			s.assertEvent(t, s.svcXdsName("svc1"), s.podXdsName("pod1"))

			// Label the pod and check that the correct event is produced.
			s.labelPod(t, "pod1", testNS,
				map[string]string{"app": "a", label.IoIstioUseWaypoint.Name: "test-wp"})
			c.podAssertion(s)

			// Label the service and check that the correct event is produced.
			s.labelService(t, "svc1", testNS,
				map[string]string{label.IoIstioUseWaypoint.Name: "test-wp"})
			c.svcAssertion(s)

			// clean up resources
			s.deleteService(t, "svc1")
			s.assertEvent(t, s.podXdsName("pod1"), s.svcXdsName("svc1"))
			s.deletePod(t, "pod1")
			s.assertEvent(t, s.podXdsName("pod1"))
			s.deleteWaypoint(t, "test-wp")
			s.clearEvents()
		})
	}
}
