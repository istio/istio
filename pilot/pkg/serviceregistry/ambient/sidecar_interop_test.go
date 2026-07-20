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

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestWaypointInterop(t *testing.T) {
	for _, tt := range []struct {
		name          string
		enableFeature *bool
		serviceLabels map[string]string
	}{
		{
			name:          "IngressUseWaypoint",
			enableFeature: nil,
			serviceLabels: map[string]string{"istio.io/ingress-use-waypoint": "true"},
		},
		{
			name:          "AmbientMultiNetwork",
			enableFeature: &features.EnableAmbientMultiNetwork,
			serviceLabels: map[string]string{"istio.io/global": "true"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.enableFeature != nil {
				test.SetForTest(t, tt.enableFeature, true)
			}
			// Test that we can get updates for EDS when we have service bound waypoints.
			s := newAmbientTestServer(t, testC, testNW, "")
			// the two types return different keys.. rename to make it more clear
			addressUpdate := s.svcXdsName
			edsUpdate := s.hostnameForService
			assertServicesWithWaypoint := func(want ...string) {
				t.Helper()
				fetch := func() []string {
					got := s.ServicesWithWaypoint(s.svcXdsName("svc1"))
					return slices.Map(got, func(e model.ServiceWaypointInfo) string {
						return e.Service.Hostname + "/" + e.WaypointHostname
					})
				}
				assert.EventuallyEqual(t, fetch, want)
			}

			s.addService(t, "svc1",
				maps.MergeCopy(map[string]string{label.IoIstioUseWaypoint.Name: "wp-svc"}, tt.serviceLabels),
				map[string]string{},
				[]int32{80}, map[string]string{"app": "a"}, "10.0.0.2")
			s.assertEvent(t, addressUpdate("svc1"))
			assertServicesWithWaypoint()

			// Add waypoint...
			// We should get a service update for EDS to update
			// First we will test an IP-based waypoint...
			s.addWaypointSpecificAddress(t, "10.0.0.1", "", "wp-svc", constants.AllTraffic, true)
			s.addService(t, "wp-svc",
				map[string]string{},
				map[string]string{},
				[]int32{80}, map[string]string{"app": "waypoint"}, "10.0.0.1")
			s.assertEvent(t, addressUpdate("wp-svc"), addressUpdate("svc1"), edsUpdate("svc1"))
			assertServicesWithWaypoint(s.hostnameForService("svc1") + "/" + s.hostnameForService("wp-svc"))

			// add a waypoint instance... we should get an EDS update
			s.addPods(t, "127.0.0.4", "wp-pod1", "wp-sa", map[string]string{"app": "waypoint"}, nil, true, corev1.PodRunning)
			s.assertEvent(t, s.podXdsName("wp-pod1"), edsUpdate("svc1"))
			s.addPods(t, "127.0.0.5", "wp-pod2", "wp-sa", map[string]string{"app": "waypoint"}, nil, true, corev1.PodRunning)
			s.assertEvent(t, s.podXdsName("wp-pod2"), edsUpdate("svc1"))
			assertServicesWithWaypoint(s.hostnameForService("svc1") + "/" + s.hostnameForService("wp-svc"))

			// now we are going to change to a different waypoint, this will be hostname based
			s.addWaypointSpecificAddress(t, "", "example.com", "wp-svc-host", constants.AllTraffic, true)
			s.addService(t, "svc1",
				maps.MergeCopy(map[string]string{label.IoIstioUseWaypoint.Name: "wp-svc-host"}, tt.serviceLabels),
				map[string]string{},
				[]int32{80}, map[string]string{"app": "a"}, "10.0.0.2")
			s.assertEvent(t, addressUpdate("svc1"), edsUpdate("svc1"))
			assertServicesWithWaypoint(s.hostnameForService("svc1") + "/" + "example.com")
		})
	}
}

// TestWaypointInteropCanary verifies canary waypoint pod changes trigger EDS for the bound service.
func TestWaypointInteropCanary(t *testing.T) {
	s := newAmbientTestServer(t, testC, testNW, "")
	addressUpdate := s.svcXdsName
	edsUpdate := s.hostnameForService

	// Primary and canary waypoint services (IP based), no pods yet.
	s.addWaypointSpecificAddress(t, "10.0.0.1", "", "wp-primary", constants.AllTraffic, true)
	s.addService(t, "wp-primary",
		map[string]string{},
		map[string]string{},
		[]int32{80}, map[string]string{"app": "primary"}, "10.0.0.1")
	s.assertEvent(t, addressUpdate("wp-primary"))
	s.addWaypointSpecificAddress(t, "10.0.0.3", "", "wp-canary", constants.AllTraffic, true)
	s.addService(t, "wp-canary",
		map[string]string{},
		map[string]string{},
		[]int32{80}, map[string]string{"app": "canary"}, "10.0.0.3")
	s.assertEvent(t, addressUpdate("wp-canary"))

	// Bind svc1 to the primary waypoint with a weighted canary.
	s.addService(t, "svc1",
		map[string]string{
			label.IoIstioUseWaypoint.Name:       "wp-primary",
			label.IoIstioUseWaypointCanary.Name: "wp-canary",
			"istio.io/ingress-use-waypoint":     "true",
		},
		map[string]string{annotation.IoIstioUseWaypointCanaryWeight.Name: "20"},
		[]int32{80}, map[string]string{"app": "a"}, "10.0.0.2")
	s.assertEvent(t, addressUpdate("svc1"), edsUpdate("svc1"))

	// The weighted set should resolve to both waypoints.
	assert.EventuallyEqual(t, func() int {
		got := s.ServicesWithWaypoint(s.svcXdsName("svc1"))
		if len(got) == 0 {
			return 0
		}
		return len(got[0].WeightedWaypoints)
	}, 2)

	// Primary waypoint pod changes still push EDS for svc1.
	s.addPods(t, "127.0.0.10", "primary-pod", "primary-sa", map[string]string{"app": "primary"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("primary-pod"), edsUpdate("svc1"))

	// Canary waypoint pod changes must also push EDS for svc1.
	s.addPods(t, "127.0.0.11", "canary-pod", "canary-sa", map[string]string{"app": "canary"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("canary-pod"), edsUpdate("svc1"))

	// A weight change must push EDS for svc1.
	s.addService(t, "svc1",
		map[string]string{
			label.IoIstioUseWaypoint.Name:       "wp-primary",
			label.IoIstioUseWaypointCanary.Name: "wp-canary",
			"istio.io/ingress-use-waypoint":     "true",
		},
		map[string]string{annotation.IoIstioUseWaypointCanaryWeight.Name: "50"},
		[]int32{80}, map[string]string{"app": "a"}, "10.0.0.2")
	s.assertEvent(t, addressUpdate("svc1"), edsUpdate("svc1"))
}
