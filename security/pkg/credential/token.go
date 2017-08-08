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

package credential

import (
	"cloud.google.com/go/compute/metadata"
)

// TokenFetcher defines the interface to fetch token.
type TokenFetcher interface {
	FetchToken() (string, error)
}

// GcpTokenFetcher implements the token fetcher in GCP.
type GcpTokenFetcher struct {
	// aud is the unique URI agreed upon by both the instance and the system verifying the instance's identity.
	// For more info: https://cloud.google.com/compute/docs/instances/verifying-instance-identity
	Aud string
}

func (fetcher *GcpTokenFetcher) getTokenURI() string {
	// The GCE metadata service URI to get identity token of current (i.e., default) service account.
	return "instance/service-accounts/default/identity?audience=" + fetcher.Aud
}

// FetchToken fetchs the GCE VM identity jwt token from its metadata server.
// Note: this function only works in a GCE VM environment.
func (fetcher *GcpTokenFetcher) FetchToken() (string, error) {
	return metadata.Get(fetcher.getTokenURI())
}
