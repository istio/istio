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

package envoy

import (
	"istio.io/istio/pkg/version"
)

var (
	// Save the build version information.
	buildVersion = version.Info.String()

)

// DiscoveryServiceOptions contains options for create a new discovery
// service instance.
type DiscoveryServiceOptions struct {
	// The listening address for HTTP. If the port in the address is empty or "0" (as in "127.0.0.1:" or "[::1]:0")
	// a port number is automatically chosen.
	HTTPAddr string

	// The listening address for GRPC. If the port in the address is empty or "0" (as in "127.0.0.1:" or "[::1]:0")
	// a port number is automatically chosen.
	GrpcAddr string

	// The listening address for secure GRPC. If the port in the address is empty or "0" (as in "127.0.0.1:" or "[::1]:0")
	// a port number is automatically chosen.
	// "" means disabling secure GRPC, used in test.
	SecureGrpcAddr string

	// The listening address for the monitoring port. If the port in the address is empty or "0" (as in "127.0.0.1:" or "[::1]:0")
	// a port number is automatically chosen.
	MonitoringAddr string

	EnableProfiling bool
	EnableCaching   bool
}

