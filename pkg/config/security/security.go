// Copyright 2017 Istio Authors
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

package security

import (
	"fmt"
	"net/url"
	"strconv"

	"istio.io/istio/pkg/config/host"
)

// JwksInfo provides values resulting from parsing a jwks URI.
type JwksInfo struct {
	Hostname host.Name
	Scheme   string
	Port     int
	UseSSL   bool
}

// ParseJwksURI parses the input URI and returns the corresponding hostname, port, and whether SSL is used.
// URI must start with "http://" or "https://", which corresponding to "http" or "https" scheme.
// Port number is extracted from URI if available (i.e from postfix :<port>, eg. ":80"), or assigned
// to a default value based on URI scheme (80 for http and 443 for https).
// Port name is set to URI scheme value.
// Note: this is to replace [buildJWKSURIClusterNameAndAddress]
// (https://github.com/istio/istio/blob/master/pilot/pkg/proxy/envoy/v1/mixer.go#L401),
// which is used for the old EUC policy.
func ParseJwksURI(jwksURI string) (JwksInfo, error) {
	u, err := url.Parse(jwksURI)
	if err != nil {
		return JwksInfo{}, err
	}
	info := JwksInfo{}
	switch u.Scheme {
	case "http":
		info.UseSSL = false
		info.Port = 80
	case "https":
		info.UseSSL = true
		info.Port = 443
	default:
		return JwksInfo{}, fmt.Errorf("URI scheme %q is not supported", u.Scheme)
	}

	if u.Port() != "" {
		info.Port, err = strconv.Atoi(u.Port())
		if err != nil {
			return JwksInfo{}, err
		}
	}
	info.Hostname = host.Name(u.Hostname())
	info.Scheme = u.Scheme

	return info, nil
}
