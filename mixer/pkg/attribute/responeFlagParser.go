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

package attribute

import (
	accesslogpb "github.com/envoyproxy/go-control-plane/envoy/data/accesslog/v2"
)

const (
	none                             = "-"
	downstreamConnectionTermination  = "DC"
	failedLocalHealthcheck           = "LH"
	noHealthyUpstream                = "UH"
	upstreamRequestTimeout           = "UT"
	localReset                       = "LR"
	upstreamRemoteReset              = "UR"
	upstreamConnectionFailure        = "UF"
	upstreamConnectionTermination    = "UC"
	upstreamOverflow                 = "UO"
	upstreamRetryLimitExceeded       = "URX"
	noRouteFound                     = "NR"
	delayInjected                    = "DI"
	faultInjected                    = "FI"
	rateLimited                      = "RL"
	unauthorizedExternalService      = "UAEX"
	rateLimitServiceError            = "RLSE"
	streamIdleTimeout                = "SI"
	invalidEnvoyServiceRequestHeader = "IH"
	downstreamProtocolError          = "DPE"
)

func appendString(result *string, toAppend string) {
	if *result == "" {
		*result += toAppend
	} else {
		*result += "," + toAppend
	}
}

func ParseEnvoyResponseFlags(rawResponseFlags *accesslogpb.ResponseFlags) string {

	result := ""

	if rawResponseFlags.GetFailedLocalHealthcheck() {
		appendString(&result, failedLocalHealthcheck)
	}
	if rawResponseFlags.GetNoHealthyUpstream() {
		appendString(&result, noHealthyUpstream)
	}
	if rawResponseFlags.GetUpstreamRequestTimeout() {
		appendString(&result, upstreamRequestTimeout)
	}
	if rawResponseFlags.GetLocalReset() {
		appendString(&result, localReset)
	}
	if rawResponseFlags.GetUpstreamRemoteReset() {
		appendString(&result, upstreamRemoteReset)
	}
	if rawResponseFlags.GetUpstreamConnectionFailure() {
		appendString(&result, upstreamConnectionFailure)
	}
	if rawResponseFlags.GetUpstreamConnectionTermination() {
		appendString(&result, upstreamConnectionTermination)
	}
	if rawResponseFlags.GetUpstreamOverflow() {
		appendString(&result, upstreamOverflow)
	}
	if rawResponseFlags.GetNoRouteFound() {
		appendString(&result, noRouteFound)
	}
	if rawResponseFlags.GetDelayInjected() {
		appendString(&result, delayInjected)
	}
	if rawResponseFlags.GetFaultInjected() {
		appendString(&result, faultInjected)
	}
	if rawResponseFlags.GetRateLimited() {
		appendString(&result, rateLimited)
	}
	if rawResponseFlags.GetUnauthorizedDetails() != nil {
		appendString(&result, unauthorizedExternalService)
	}
	if rawResponseFlags.GetRateLimitServiceError() {
		appendString(&result, rateLimitServiceError)
	}
	if rawResponseFlags.GetDownstreamConnectionTermination() {
		appendString(&result, downstreamConnectionTermination)
	}
	if rawResponseFlags.GetUpstreamRetryLimitExceeded() {
		appendString(&result, upstreamRetryLimitExceeded)
	}
	if rawResponseFlags.GetStreamIdleTimeout() {
		appendString(&result, streamIdleTimeout)
	}
	if rawResponseFlags.GetInvalidEnvoyRequestHeaders() {
		appendString(&result, invalidEnvoyServiceRequestHeader)
	}
	if rawResponseFlags.GetDownstreamProtocolError() {
		appendString(&result, downstreamProtocolError)
	}

	if result == "" {
		return none
	}
	return result

}
