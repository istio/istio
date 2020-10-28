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

package metrics

import (
	"istio.io/pkg/monitoring"
)

var (
	disconnectionTypeTag = monitoring.MustCreateLabel("type")

	// IstiodConnectionFailures records total number of connection failures to Istiod.
	IstiodConnectionFailures = monitoring.NewSum(
		"istiod_connection_failures",
		"The total number of connection failures to Istiod",
	)

	// istiodDisconnections records total number of unexpected disconnections by Istiod.
	istiodDisconnections = monitoring.NewSum(
		"istiod_connection_terminations",
		"The total number of connection errors to Istiod",
		monitoring.WithLabels(disconnectionTypeTag),
	)

	// envoyDisconnections records total number of unexpected disconnections by Envoy.
	envoyDisconnections = monitoring.NewSum(
		"envoy_connection_terminations",
		"The total number of connection errors from envoy",
		monitoring.WithLabels(disconnectionTypeTag),
	)

	// TODO: Add type url as type for requeasts and responses if needed.

	// XdsProxyRequests records total number of downstream requests.
	XdsProxyRequests = monitoring.NewSum(
		"xds_proxy_requests",
		"The total number of Xds Proxy Requests",
	)

	// XdsProxyResponses records total number of upstream responses.
	XdsProxyResponses = monitoring.NewSum(
		"xds_proxy_responses",
		"The total number of Xds Proxy Responses",
	)

	IstiodConnectionCancellations = istiodDisconnections.With(disconnectionTypeTag.Value(Cancel))
	IstiodConnectionErrors        = istiodDisconnections.With(disconnectionTypeTag.Value(Error))
	EnvoyConnectionCancellations  = envoyDisconnections.With(disconnectionTypeTag.Value(Cancel))
	EnvoyConnectionErrors         = envoyDisconnections.With(disconnectionTypeTag.Value(Error))
)

var (
	Cancel = "cancelled"
	Error  = "error"
)

func init() {
	monitoring.MustRegister(
		IstiodConnectionFailures,
		IstiodConnectionErrors,
		istiodDisconnections,
		envoyDisconnections,
	)
}
