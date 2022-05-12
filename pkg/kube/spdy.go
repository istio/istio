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

package kube

import (
	"crypto/tls"
	"fmt"
	"net/http"

	spdyStream "k8s.io/apimachinery/pkg/util/httpstream/spdy"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport/spdy"
)

// roundTripperFor creates a SPDY upgrader that will work over custom transports.
func roundTripperFor(restConfig *rest.Config) (http.RoundTripper, spdy.Upgrader, error) {
	// Get the TLS config.
	tlsConfig, err := rest.TLSConfigFor(restConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed getting TLS config: %w", err)
	}
	if tlsConfig == nil && restConfig.Transport != nil {
		// If using a custom transport, skip server verification on the upgrade.
		tlsConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}

	var upgrader *spdyStream.SpdyRoundTripper
	if restConfig.Proxy != nil {
		upgrader = spdyStream.NewRoundTripperWithProxy(tlsConfig, restConfig.Proxy)
	} else {
		upgrader = spdyStream.NewRoundTripper(tlsConfig)
	}
	wrapper, err := rest.HTTPWrappersForConfig(restConfig, upgrader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed creating SPDY upgrade wrapper: %w", err)
	}
	return wrapper, upgrader, nil
}
