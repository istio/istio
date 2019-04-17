// Copyright 2019 Istio Authors
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

package agent

import (
	"net"

	"istio.io/istio/pkg/test/application"
	"istio.io/istio/pkg/test/envoy"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/reserveport"
)

type Config struct {
	Domain           string
	Namespace        namespace.Instance
	Galley           galley.Instance
	AppFactory       application.Factory
	PortManager      reserveport.PortManager
	ServiceName      string
	Version          string
	DiscoveryAddress *net.TCPAddr
	TmpDir           string
	EnvoyLogLevel    envoy.LogLevel
}
