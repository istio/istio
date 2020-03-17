// Copyright 2020 Istio Authors
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

package common

// TLSSettings defines TLS configuration for Echo server
type TLSSettings struct {
	RootCert   string
	ClientCert string
	Key        string
	// If provided, override the host name used for the connection
	// This needed for integration tests, as we are connecting using a port-forward (127.0.0.1), so
	// any DNS certs will not validate.
	Hostname string
}
