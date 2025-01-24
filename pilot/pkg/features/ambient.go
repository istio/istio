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

package features

import (
	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/log"
)

var (
	EnableAmbient = env.Register(
		"PILOT_ENABLE_AMBIENT",
		false,
		"If enabled, ambient mode can be used. Individual flags configure fine grained enablement; this must be enabled for any ambient functionality.").Get()

	EnableAmbientWaypoints = registerAmbient("PILOT_ENABLE_AMBIENT_WAYPOINTS",
		true, false,
		"If enabled, controllers required for ambient will run. This is required to run ambient mesh.")

	EnableHBONESend = registerAmbient(
		"PILOT_ENABLE_SENDING_HBONE",
		true, false,
		"If enabled, HBONE will be allowed when sending to destinations.")

	EnableSidecarHBONEListening = registerAmbient(
		"PILOT_ENABLE_SIDECAR_LISTENING_HBONE",
		true, false,
		"If enabled, HBONE support can be configured for proxies.")

	EnableAmbientStatus = registerAmbient(
		"AMBIENT_ENABLE_STATUS",
		true, false,
		"If enabled, status messages for ambient mode will be written to resources. "+
			"Currently, this does not do leader election, so may be unsafe to enable with multiple replicas.")

	// Not required for ambient, so disabled by default
	PreferHBONESend = registerAmbient(
		"PILOT_PREFER_SENDING_HBONE",
		false, false,
		"If enabled, HBONE will be preferred when sending to destinations. ")

	DefaultAllowFromWaypoint = registerAmbient(
		"PILOT_AUTO_ALLOW_WAYPOINT_POLICY",
		false, false,
		"If enabled, zTunnel will receive synthetic authorization policies for each workload ALLOW the Waypoint's identity. "+
			"Unless other ALLOW policies are created, this effectively denies traffic that doesn't go through the waypoint.")

	EnableIngressWaypointRouting = registerAmbient("ENABLE_INGRESS_WAYPOINT_ROUTING", true, false,
		"If true, Gateways will call service waypoints if the 'istio.io/ingress-use-waypoint' label set on the Service.")

	EnableAmbientMultiNetwork = registerAmbient("AMBIENT_ENABLE_MULTI_NETWORK", false, false,
		"If true, the multi-network functionality will be enabled.")

	WaypointLayeredAuthorizationPolicies = env.Register(
		"ENABLE_LAYERED_WAYPOINT_AUTHORIZATION_POLICIES",
		false,
		"If enabled, selector based authorization policies will be enforced as L4 policies in front of the waypoint.").Get()
)

// registerAmbient registers a variable that is allowed only if EnableAmbient is set
func registerAmbient[T env.Parseable](name string, defaultWithAmbient, defaultWithoutAmbient T, description string) T {
	if EnableAmbient {
		return env.Register(name, defaultWithAmbient, description).Get()
	}

	_, f := env.Register(name, defaultWithoutAmbient, description).Lookup()
	if f {
		log.Warnf("ignoring %v; requires PILOT_ENABLE_AMBIENT=true", name)
	}
	return defaultWithoutAmbient
}
