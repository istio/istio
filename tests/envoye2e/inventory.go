// Copyright 2020 Istio Authors
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

package envoye2e

import (
	"istio.io/istio/tests/envoye2e/env"
)

var ProxyE2ETests = &env.TestInventory{}

func init() {
	ProxyE2ETests.Tests = append(ProxyE2ETests.Tests, []string{
		"TestAttributeGen",
		"TestBasicFlow",
		"TestBasicHTTP",
		"TestBasicHTTPGateway",
		"TestBasicHTTPwithTLS",
		"TestBasicTCPFlow",
		"TestBasicCONNECT/quic",
		"TestBasicCONNECT/h2",
		"TestPassthroughCONNECT/quic",
		"TestPassthroughCONNECT/h2",
		"TestHTTPExchange",
		"TestNativeHTTPExchange",
		"TestStats403Failure/#00",
		"TestStatsECDS/#00",
		"TestStatsEndpointLabels/#00",
		"TestStatsServerWaypointProxy",
		"TestStatsServerWaypointProxyCONNECT",
		"TestStatsGrpc/#00",
		"TestStatsGrpcStream/#00",
		"TestStatsParallel/Default",
		"TestStatsParallel/Customized",
		"TestStatsPayload/Customized/",
		"TestStatsPayload/Default/",
		"TestStatsPayload/DisableHostHeader/",
		"TestStatsPayload/UseHostHeader/",
		"TestStatsParserRegression",
		"TestStatsExpiry",
		"TestTCPMetadataExchange/false",
		"TestTCPMetadataExchange/true",
		"TestTCPMetadataExchangeNoAlpn",
		"TestTCPMetadataExchangeWithConnectionTermination",
		"TestTCPMetadataNotFoundReporting",
		"TestStatsDestinationServiceNamespacePrecedence",
	}...)
}
