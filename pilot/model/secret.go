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

package model

// SecretRegistry defines a read-only interface for secret key material
// The implementation should not cache or persist the secrets and pass
// the data immediately to the client of this interface.
type SecretRegistry interface {
	// GetTLSSecret retrieves a TLS secret by implementation specific URI
	GetTLSSecret(uri string) (*TLSSecret, error)
}

// TLSSecret defines a TLS configuration.
type TLSSecret struct {
	Certificate []byte
	PrivateKey  []byte
}
