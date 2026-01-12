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
	"net/http"
	"regexp"
	"strings"

	"istio.io/istio/pkg/test/echo/proto"
)

var (
	requestIDFieldRegex              = regexp.MustCompile("(?i)" + string(RequestIDField) + "=(.*)")
	serviceVersionFieldRegex         = regexp.MustCompile(string(ServiceVersionField) + "=(.*)")
	servicePortFieldRegex            = regexp.MustCompile(string(ServicePortField) + "=(.*)")
	statusCodeFieldRegex             = regexp.MustCompile(string(StatusCodeField) + "=(.*)")
	hostFieldRegex                   = regexp.MustCompile(string(HostField) + "=(.*)")
	hostnameFieldRegex               = regexp.MustCompile(string(HostnameField) + "=(.*)")
	requestHeaderFieldRegex          = regexp.MustCompile(string(RequestHeaderField) + "=(.*)")
	responseHeaderFieldRegex         = regexp.MustCompile(string(ResponseHeaderField) + "=(.*)")
	URLFieldRegex                    = regexp.MustCompile(string(URLField) + "=(.*)")
	ClusterFieldRegex                = regexp.MustCompile(string(ClusterField) + "=(.*)")
	IstioVersionFieldRegex           = regexp.MustCompile(string(IstioVersionField) + "=(.*)")
	IPFieldRegex                     = regexp.MustCompile(string(IPField) + "=(.*)")
	SourceIPFieldRegex               = regexp.MustCompile(string(SourceIPField) + "=(.*)")
	methodFieldRegex                 = regexp.MustCompile(string(MethodField) + "=(.*)")
	protocolFieldRegex               = regexp.MustCompile(string(ProtocolField) + "=(.*)")
	alpnFieldRegex                   = regexp.MustCompile(string(AlpnField) + "=(.*)")
	sniFieldRegex                    = regexp.MustCompile(string(SNIField) + "=(.*)")
	proxyProtocolFieldRegex          = regexp.MustCompile(string(ProxyProtocolField) + "=(.*)")
	clientCertSubjectFieldRegex      = regexp.MustCompile(string(ClientCertSubjectField) + "=(.*)")
	clientCertSerialNumberFieldRegex = regexp.MustCompile(string(ClientCertSerialNumberField) + "=(.*)")
)

func ParseResponses(req *proto.ForwardEchoRequest, resp *proto.ForwardEchoResponse) Responses {
	responses := make([]Response, len(resp.Output))
	for i, output := range resp.Output {
		responses[i] = parseResponse(output)
		responses[i].RequestURL = req.Url
	}
	return responses
}

func parseResponse(output string) Response {
	out := Response{
		RawContent:      output,
		RequestHeaders:  make(http.Header),
		ResponseHeaders: make(http.Header),
	}

	match := requestIDFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.ID = match[1]
	}

	match = methodFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Method = match[1]
	}

	match = protocolFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Protocol = match[1]
	}

	match = alpnFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Alpn = match[1]
	}

	match = sniFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.SNI = match[1]
	}

	match = proxyProtocolFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.ProxyProtocol = match[1]
	}

	match = serviceVersionFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Version = match[1]
	}

	match = servicePortFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Port = match[1]
	}

	match = statusCodeFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Code = match[1]
	}

	match = hostFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Host = match[1]
	}

	match = hostnameFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Hostname = match[1]
	}

	match = URLFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.URL = match[1]
	}

	match = ClusterFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Cluster = match[1]
	}

	match = IstioVersionFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.IstioVersion = match[1]
	}

	match = IPFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.IP = match[1]
	}

	match = SourceIPFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.SourceIP = match[1]
	}

	match = clientCertSubjectFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.ClientCertSubject = match[1]
	}

	match = clientCertSerialNumberFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.ClientCertSerialNumber = match[1]
	}

	out.RawBody = map[string]string{}

	matches := requestHeaderFieldRegex.FindAllStringSubmatch(output, -1)
	for _, kv := range matches {
		sl := strings.SplitN(kv[1], ":", 2)
		if len(sl) != 2 {
			continue
		}
		out.RequestHeaders.Add(sl[0], sl[1])
	}

	matches = responseHeaderFieldRegex.FindAllStringSubmatch(output, -1)
	for _, kv := range matches {
		sl := strings.SplitN(kv[1], ":", 2)
		if len(sl) != 2 {
			continue
		}
		out.ResponseHeaders.Add(sl[0], sl[1])
	}

	for _, l := range strings.Split(output, "\n") {
		prefixSplit := strings.Split(l, "body] ")
		if len(prefixSplit) != 2 {
			continue
		}
		kv := strings.SplitN(prefixSplit[1], "=", 2)
		if len(kv) != 2 {
			continue
		}
		out.RawBody[kv[0]] = kv[1]
	}

	return out
}
