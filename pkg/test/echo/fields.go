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

package echo

// Field is a list of fields returned in responses from the Echo server.
type Field string

func (f Field) String() string {
	return string(f)
}

const (
	RequestIDField      Field = "X-Request-Id"
	ServiceVersionField Field = "ServiceVersion"
	ServicePortField    Field = "ServicePort"
	StatusCodeField     Field = "StatusCode"
	URLField            Field = "URL"
	HostField           Field = "Host"
	HostnameField       Field = "Hostname"
	MethodField         Field = "Method"
	ProtocolField       Field = "Proto"
	AlpnField           Field = "Alpn"
	RequestHeaderField  Field = "RequestHeader"
	ResponseHeaderField Field = "ResponseHeader"
	ClusterField        Field = "Cluster"
	IstioVersionField   Field = "IstioVersion"
	IPField             Field = "IP" // The Requester’s IP Address.
)
