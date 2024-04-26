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

	// Not required for ambient, so disabled by default
	PreferHBONESend = registerAmbient(
		"PILOT_PREFER_SENDING_HBONE",
		false, false,
		"If enabled, HBONE will be preferred when sending to destinations. ")

	DefaultAllowFromWaypoint = registerAmbient(
		"PILOT_DEFAULT_ENFORCE_FROM_WAYPOINT",
		true, false,
		"If enabled, zTunnels will enforce that incoming traffic has traversed a Waypoint if traffic to an endpoint may traverse one.")
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
