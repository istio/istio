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

package constants

const (
	// In the below, we add the fwmask to ensure only that mark can match

	DNSCapturePort              = 15053
	ZtunnelInboundPort          = 15008
	ZtunnelOutboundPort         = 15001
	ZtunnelInboundPlaintextPort = 15006
	ProbeIPSet                  = "istio-inpod-probes"
)
