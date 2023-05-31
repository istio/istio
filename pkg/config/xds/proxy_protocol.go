// Copyright Istio Authors
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

package xds

import (
	proxy_proto "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/proxy_protocol/v3"
	"google.golang.org/protobuf/proto"
)

// ProxyProtocol Envoy listener filter description.
type ProxyProtocol struct{}

func init() {
	initRegisterListener(&ProxyProtocol{})
}

func (*ProxyProtocol) Name() string {
	return "proxy_protocol"
}

func (*ProxyProtocol) TypeURL() string {
	return "type.googleapis.com/envoy.extensions.filters.listener.proxy_protocol.v3.ProxyProtocol"
}

func (*ProxyProtocol) New() proto.Message {
	return &proxy_proto.ProxyProtocol{}
}

func (*ProxyProtocol) Validate(pb proto.Message) error {
	return nil
}
