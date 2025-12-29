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
	"os"
	"strings"

	spdyStream "k8s.io/apimachinery/pkg/util/httpstream/spdy"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport/spdy"
)

// kubeFeatureGate is a small fork of the 'cmdutil' library in kubectl, which has too many dependencies
type kubeFeatureGate string

const (
	remoteCommandWebsockets kubeFeatureGate = "KUBECTL_REMOTE_COMMAND_WEBSOCKETS"
	portForwardWebsockets   kubeFeatureGate = "KUBECTL_PORT_FORWARD_WEBSOCKETS"
)

// IsEnabled returns true iff environment variable is set to true.
// All other cases, it returns false.
func (f kubeFeatureGate) IsEnabled() bool {
	return strings.ToLower(os.Getenv(string(f))) == "true"
}

// IsDisabled returns true iff environment variable is set to false.
// All other cases, it returns true.
// This function is used for the cases where feature is enabled by default,
// but it may be needed to provide a way to ability to disable this feature.
func (f kubeFeatureGate) IsDisabled() bool {
	return strings.ToLower(os.Getenv(string(f))) == "false"
}

// roundTripperFor creates a SPDY upgrader that will work over custom transports.
func roundTripperFor(restConfig *rest.Config) (http.RoundTripper, spdy.Upgrader, error) {
	// Get the TLS config.
	tlsConfig, err := rest.TLSConfigFor(restConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed getting TLS config: %w", err)
	}
	if tlsConfig == nil && restConfig.Transport != nil {
		// If using a custom transport, skip server verification on the upgrade.
		// nolint: gosec
		tlsConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}

	var upgrader *spdyStream.SpdyRoundTripper
	if restConfig.Proxy != nil {
		upgrader, err = spdyStream.NewRoundTripperWithProxy(tlsConfig, restConfig.Proxy)
		if err != nil {
			return nil, nil, err
		}
	} else {
		upgrader, err = spdyStream.NewRoundTripper(tlsConfig)
		if err != nil {
			return nil, nil, err
		}
	}
	wrapper, err := rest.HTTPWrappersForConfig(restConfig, upgrader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed creating SPDY upgrade wrapper: %w", err)
	}
	return wrapper, upgrader, nil
}
