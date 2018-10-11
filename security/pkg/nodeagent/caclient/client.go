// Copyright 2018 Istio Authors
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

package caclient

import (
	"crypto/x509"
	"errors"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"istio.io/istio/pkg/log"
	caClientInterface "istio.io/istio/security/pkg/nodeagent/caclient/interface"
	gca "istio.io/istio/security/pkg/nodeagent/caclient/providers/google"
)

const googleCA = "GoogleCA"

// NewCAClient create an CA client.
func NewCAClient(endpoint, CAProviderName string, tlsFlag bool) (caClientInterface.Client, error) {
	var opts grpc.DialOption
	if tlsFlag {
		pool, err := x509.SystemCertPool()
		if err != nil {
			log.Errorf("could not get SystemCertPool: %v", err)
			return nil, errors.New("could not get SystemCertPool")
		}
		creds := credentials.NewClientTLSFromCert(pool, "")
		opts = grpc.WithTransportCredentials(creds)
	} else {
		opts = grpc.WithInsecure()
	}

	conn, err := grpc.Dial(endpoint, opts)
	if err != nil {
		log.Errorf("Failed to connect to endpoint %q: %v", endpoint, err)
		return nil, fmt.Errorf("failed to connect to endpoint %q", endpoint)
	}

	switch CAProviderName {
	case googleCA:
		return gca.NewGoogleCAClient(conn), nil
	default:
		return nil, fmt.Errorf("CA provider %q isn't supported", CAProviderName)
	}
}
