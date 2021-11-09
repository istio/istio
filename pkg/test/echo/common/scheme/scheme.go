//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package scheme

// Scheme enumerates the optional schemes for requests.
type Instance string

const (
	HTTP      Instance = "http"
	HTTPS     Instance = "https"
	GRPC      Instance = "grpc"
	XDS       Instance = "xds"
	WebSocket Instance = "ws"
	TCP       Instance = "tcp"
	// TLS sends a TLS connection and reports back the properties of the TLS connection
	// This is similar to `openssl s_client`
	// Response data is not returned; only information about the TLS handshake.
	TLS Instance = "tls"
	// DNS does a DNS query and reports back the results.
	DNS Instance = "dns"
)
