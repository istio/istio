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

import (
	"fmt"
	"io"
)

// Field is a list of fields returned in responses from the Echo server.
type Field string

func (f Field) String() string {
	return string(f)
}

func (f Field) Write(out io.StringWriter, value string) {
	_, _ = out.WriteString(fmt.Sprintf("%s=%s\n", f, value))
}

func (f Field) WriteNonEmpty(out io.StringWriter, value string) {
	if value != "" {
		_, _ = out.WriteString(fmt.Sprintf("%s=%s\n", f, value))
	}
}

func (f Field) WriteKeyValue(out io.StringWriter, key, value string) {
	f.Write(out, key+":"+value)
}

func (f Field) WriteForRequest(out io.StringWriter, requestID int, value string) {
	_, _ = out.WriteString(fmt.Sprintf("[%d] %s=%s\n", requestID, f, value))
}

func (f Field) WriteKeyValueForRequest(out io.StringWriter, requestID int, key, value string) {
	f.WriteForRequest(out, requestID, key+":"+value)
}

func WriteBodyLine(out io.StringWriter, requestID int, line string) {
	_, _ = out.WriteString(fmt.Sprintf("[%d body] %s\n", requestID, line))
}

func WriteError(out io.StringWriter, requestID int, err error) {
	_, _ = out.WriteString(fmt.Sprintf("[%d error] %v\n", requestID, err))
}

const (
	RequestIDField              Field = "X-Request-Id"
	ServiceVersionField         Field = "ServiceVersion"
	ServicePortField            Field = "ServicePort"
	StatusCodeField             Field = "StatusCode"
	URLField                    Field = "URL"
	ForwarderURLField           Field = "Url"
	ForwarderMessageField       Field = "Echo"
	ForwarderHeaderField        Field = "Header"
	HostField                   Field = "Host"
	HostnameField               Field = "Hostname"
	NamespaceField              Field = "Namespace"
	MethodField                 Field = "Method"
	ProtocolField               Field = "Proto"
	ProxyProtocolField          Field = "ProxyProtocol"
	AlpnField                   Field = "Alpn"
	SNIField                    Field = "Sni"
	ClientCertSubjectField      Field = "ClientCertSubject"
	ClientCertSerialNumberField Field = "ClientCertSerialNumber"
	RequestHeaderField          Field = "RequestHeader"
	ResponseHeaderField         Field = "ResponseHeader"
	ClusterField                Field = "Cluster"
	IstioVersionField           Field = "IstioVersion"
	IPField                     Field = "IP"       // The Requester’s IP Address, as reported from the destination
	SourceIPField               Field = "SourceIP" // The Requester’s IP Address, as reported from the source.
	LatencyField                Field = "Latency"
	ActiveRequestsField         Field = "ActiveRequests"
	DNSProtocolField            Field = "Protocol"
	DNSQueryField               Field = "Query"
	DNSServerField              Field = "DnsServer"
	CipherField                 Field = "Cipher"
	TLSVersionField             Field = "Version"
	TLSServerName               Field = "ServerName"
)
