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

// Package iamclient is for IAM integration.
package iamclient

import (
	"crypto/x509"

	iam "google.golang.org/genproto/googleapis/iam/credentials/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"istio.io/istio/pkg/log"
)

var (
	iamEndpoint = "iamcredentials.googleapis.com:443"
	tlsFlag     = true
)

// NewPlugin returns an instance of the google iam client plugin
func NewPlugin() iam.IAMCredentialsClient {
	var opts grpc.DialOption
	if tlsFlag {
		pool, err := x509.SystemCertPool()
		if err != nil {
			log.Errorf("could not get SystemCertPool: %v", err)
			return nil
		}
		creds := credentials.NewClientTLSFromCert(pool, "")
		opts = grpc.WithTransportCredentials(creds)
	} else {
		opts = grpc.WithInsecure()
	}

	conn, err := grpc.Dial(iamEndpoint, opts)
	if err != nil {
		log.Errorf("Failed to connect to endpoint %q: %v", iamEndpoint, err)
		return nil
	}

	return iam.NewIAMCredentialsClient(conn)
}
